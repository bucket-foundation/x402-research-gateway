package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/federate"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

const testFederatedKey = "SECRET-FEDERATED-KEY-do-not-leak"

// federatedTestHandler wires the OpenAlex and Semantic Scholar search
// routes at fake upstreams. Both declare the search capability, so one
// query reaches both.
func federatedTestHandler(t *testing.T, openAlexURL, s2URL string) *Handler {
	t.Helper()
	cfg := testCfg()
	cfg.Feed402.Federated = config.FederatedConfig{
		Enabled: true, Path: "/research/federated", Price: "0.005",
		MaxConcurrency: 2, TimeoutSeconds: 5,
		// Restricted to the two mocked upstreams. pubmed-search is also
		// search-capable and would otherwise be dialed for real.
		ProviderRouteIDs: []string{"openalex-works", "semantic-scholar-search"},
	}
	cfg.Routes = append(cfg.Routes,
		config.RouteConfig{
			ID: "openalex-works", Path: "/research/openalex/works", Method: "GET",
			Price: "0.001", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "openalex", ProviderURL: "https://openalex.org/", License: "CC0"},
			Upstream: config.UpstreamConfig{
				BaseURL: openAlexURL, Path: "/works", PassThrough: []string{"search"}, Timeout: 5,
				Headers: map[string]string{"Authorization": "Bearer " + testFederatedKey},
			},
		},
		config.RouteConfig{
			ID: "semantic-scholar-search", Path: "/research/s2/search", Method: "GET",
			Price: "0.002", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "s2", ProviderURL: "https://semanticscholar.org/", License: "odc-by"},
			Upstream: config.UpstreamConfig{
				BaseURL: s2URL, Path: "/paper/search", PassThrough: []string{"query"}, Timeout: 5,
			},
		},
		config.RouteConfig{
			ID: "feed402-federated", Path: "/research/federated", Method: "POST",
			Price: "0.005", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "federated", License: "mixed"},
		},
	)
	h := newTestHandler(cfg)
	h.httpClient = &http.Client{Timeout: 5 * time.Second}
	return h
}

// Both providers return three hits, and the two share a DOI.
const fedOpenAlexBody = `{"results":[
 {"id":"https://openalex.org/W1","ids":{"openalex":"https://openalex.org/W1","doi":"https://doi.org/10.7717/peerj.4375"}},
 {"id":"https://openalex.org/W2","ids":{"openalex":"https://openalex.org/W2"}},
 {"id":"https://openalex.org/W3","ids":{"openalex":"https://openalex.org/W3"}}]}`

const fedS2Body = `{"data":[
 {"paperId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","externalIds":{"DOI":"10.7717/peerj.4375"}},
 {"paperId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","externalIds":{}}]}`

// fedOpenAlexBodyWithTitle carries the bibliographic fields OpenAlex's own
// Descriptor implementation reads, so a test can prove the federated
// result surface copies them rather than leaving a caller to reparse Raw.
const fedOpenAlexBodyWithTitle = `{"results":[
 {"id":"https://openalex.org/W1","ids":{"openalex":"https://openalex.org/W1"},
  "title":"Spectral inverse problems in photosynthetic systems","publication_year":2019,
  "authorships":[{"author":{"display_name":"Ada Lovelace"}},{"author":{"display_name":"Grace Hopper"}}]}]}`

func TestFanOutFederated_ProvenanceSurvivesTheMerge(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 200, fedOpenAlexBody).URL, serve(t, 200, fedS2Body).URL)
	selected := []string{"openalex-works", "semantic-scholar-search"}

	results, reports := h.fanOutFederated(context.Background(), selected, "photosynthesis")
	if len(results) != 5 {
		t.Fatalf("got %d results, want 5", len(results))
	}
	resp := federate.Merge("photosynthesis", "search", results, reports,
		federate.Estimate("search", h.federatedPrices(selected), 0), time.Now())

	// Each provider's own ranking is intact in the merged list.
	seen := map[string]int{}
	for _, r := range resp.Results {
		if r.ProviderRank != seen[r.Provider]+1 {
			t.Errorf("%s ranks out of order: got %d after %d", r.Provider, r.ProviderRank, seen[r.Provider])
		}
		seen[r.Provider] = r.ProviderRank
		if r.SourceID == "" {
			t.Error("a merged result must keep its provider source id")
		}
		if r.FusedRank == 0 {
			t.Error("a fused result must carry its merged position")
		}
	}
	if seen["openalex-works"] != 3 || seen["semantic-scholar-search"] != 2 {
		t.Errorf("provider result counts = %v", seen)
	}
	// Raw provider records survive.
	for _, r := range resp.Results {
		if len(r.Raw) == 0 {
			t.Errorf("raw record lost on %s", r.SourceID)
		}
	}
	// The shared DOI surfaces as a candidate without collapsing anything.
	if len(resp.DuplicateCandidates) != 1 {
		t.Fatalf("expected one duplicate candidate, got %+v", resp.DuplicateCandidates)
	}
	if len(resp.Results) != 5 {
		t.Error("a duplicate candidate must not remove a result")
	}
	if resp.Cost.TotalUSD != 0.003 {
		t.Errorf("cost total = %v, want 0.003", resp.Cost.TotalUSD)
	}
}

// Two providers succeeding and one failing returns results plus an explicit
// per-provider statement. A failure never reads as an empty result.
func TestFanOutFederated_PartialFailureIsExplicit(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 503, `{"error":"unavailable"}`).URL, serve(t, 200, fedS2Body).URL)

	results, reports := h.fanOutFederated(context.Background(),
		[]string{"openalex-works", "semantic-scholar-search"}, "q")
	if len(results) != 2 {
		t.Fatalf("the healthy provider must still contribute, got %d", len(results))
	}
	byProvider := map[string]federate.ProviderReport{}
	for _, r := range reports {
		byProvider[r.Provider] = r
	}
	failed := byProvider["openalex-works"]
	if failed.Outcome != federate.OutcomeUpstreamStatus || failed.UpstreamStatus != 503 {
		t.Errorf("the failing provider report = %+v", failed)
	}
	if !failed.Consulted || failed.ResultCount != 0 {
		t.Errorf("a failed provider must read as consulted with zero results, got %+v", failed)
	}

	// Contrast: a provider that answers with nothing.
	h = federatedTestHandler(t, serve(t, 200, `{"results":[]}`).URL, serve(t, 200, fedS2Body).URL)
	_, reports = h.fanOutFederated(context.Background(),
		[]string{"openalex-works", "semantic-scholar-search"}, "q")
	for _, r := range reports {
		if r.Provider != "openalex-works" {
			continue
		}
		if r.Outcome != federate.OutcomeOK || r.ResultCount != 0 {
			t.Errorf("an empty answer must read as ok/0, got %+v", r)
		}
	}
}

// One slow upstream cannot extend the whole request.
func TestFanOutFederated_TimeoutIsolation(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(4 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)

	h := federatedTestHandler(t, slow.URL, serve(t, 200, fedS2Body).URL)
	// The route's own timeoutSeconds is the per-provider deadline.
	for i := range h.cfg.Routes {
		if h.cfg.Routes[i].ID == "openalex-works" {
			h.cfg.Routes[i].Upstream.Timeout = 1
		}
	}

	start := time.Now()
	results, reports := h.fanOutFederated(context.Background(),
		[]string{"openalex-works", "semantic-scholar-search"}, "q")
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("one slow provider extended the whole request: %s", elapsed)
	}
	if len(results) != 2 {
		t.Errorf("the fast provider must still contribute, got %d", len(results))
	}
	for _, r := range reports {
		if r.Provider != "openalex-works" {
			continue
		}
		if r.Outcome != federate.OutcomeTimeout && r.Outcome != federate.OutcomeUpstreamError {
			t.Errorf("expected a timeout-class outcome, got %q", r.Outcome)
		}
	}
}

func TestFanOutFederated_Deterministic(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 200, fedOpenAlexBody).URL, serve(t, 200, fedS2Body).URL)
	selected := []string{"openalex-works", "semantic-scholar-search"}
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	var first string
	for i := 0; i < 10; i++ {
		results, reports := h.fanOutFederated(context.Background(), selected, "q")
		// Latency is wall-clock, so it is normalized before comparing.
		for j := range reports {
			reports[j].LatencyMs = 0
		}
		out, _ := json.Marshal(federate.Merge("q", "search", results, reports,
			federate.Estimate("search", h.federatedPrices(selected), 0), at))
		if i == 0 {
			first = string(out)
			continue
		}
		if string(out) != first {
			t.Fatal("federated fan-out is not deterministic")
		}
	}
}

// Selection is by declared capability: a capability no adapter implements
// selects nobody, and one only some implement selects only those.
func TestFederatedCandidates_CapabilityRouting(t *testing.T) {
	h := federatedTestHandler(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	h.cfg.Feed402.Federated.ProviderRouteIDs = nil

	search := h.federatedCandidates(provider.CapSearch)
	if len(search) == 0 {
		t.Fatal("search-capable providers should be selected")
	}
	for _, id := range search {
		if id == "pubmed-fetch" {
			t.Error("a fetch-only route must not be selected for search")
		}
	}
	for i := 1; i < len(search); i++ {
		if search[i-1] > search[i] {
			t.Fatal("candidates must be sorted")
		}
	}

	// cited_by selects only the cited-by adapters. Asking for it never
	// reaches a provider that cannot answer it.
	for _, id := range h.federatedCandidates(provider.CapCitedBy) {
		if !strings.Contains(id, "cited-by") {
			t.Errorf("%s cannot answer cited_by and must not be selected", id)
		}
	}

	// A capability nothing implements selects nobody, which the cost
	// estimate then prices at zero rather than erroring.
	if got := h.federatedCandidates(provider.Capability("telepathy")); len(got) != 0 {
		t.Errorf("an unimplemented capability must select nobody, got %v", got)
	}

	h.cfg.Feed402.Federated.ProviderRouteIDs = []string{"openalex-works"}
	if got := h.federatedCandidates(provider.CapSearch); len(got) != 1 || got[0] != "openalex-works" {
		t.Errorf("allowlist not honored, got %v", got)
	}
}

// The estimate is free, makes no upstream call, and answers before payment.
func TestHandleFederatedEstimate_FreeAndPrePayment(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(fedOpenAlexBody))
	}))
	t.Cleanup(upstream.Close)

	h := federatedTestHandler(t, upstream.URL, upstream.URL)
	req := httptest.NewRequest("GET", "/research/federated?capability=search&max_cost_usd=0.001", nil)
	w := httptest.NewRecorder()
	h.handleFederatedEstimate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("estimate status = %d", w.Code)
	}
	if called {
		t.Error("the estimate must not call any upstream")
	}
	var body struct {
		Cost      federate.CostEstimate `json:"cost"`
		Providers []string              `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Providers) != 2 {
		t.Errorf("estimate providers = %v", body.Providers)
	}
	// The cap of 0.001 affords the 0.001 provider only.
	if body.Cost.WithinCap {
		t.Error("a cap that drops a provider must report within_cap false")
	}
	if got := body.Cost.Included(); len(got) != 1 || got[0] != "openalex-works" {
		t.Errorf("cap should keep only the cheapest provider, got %v", got)
	}
	if body.Cost.TotalUSD > 0.001+1e-9 {
		t.Errorf("estimate exceeds the cap: %v", body.Cost.TotalUSD)
	}
}

// Providers a cost cap excludes are reported rather than silently dropped.
func TestFederatedCitations_AndCapExclusionsAreReported(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 200, fedOpenAlexBody).URL, serve(t, 200, fedS2Body).URL)
	selected := []string{"openalex-works"}
	results, reports := h.fanOutFederated(context.Background(), selected, "q")
	reports = append(reports, federate.ProviderReport{
		Provider: "semantic-scholar-search", Outcome: federate.OutcomeCostCapExceeded, PriceUSD: 0.002,
	})
	resp := federate.Merge("q", "search", results, reports,
		federate.Estimate("search", h.federatedPrices([]string{"openalex-works", "semantic-scholar-search"}), 0.001),
		time.Now())

	var excluded federate.ProviderReport
	for _, r := range resp.Providers {
		if r.Provider == "semantic-scholar-search" {
			excluded = r
		}
	}
	if excluded.Outcome != federate.OutcomeCostCapExceeded || excluded.Consulted || excluded.Charged {
		t.Errorf("a cap-excluded provider must read as unconsulted and uncharged, got %+v", excluded)
	}

	cits := h.federatedCitations(h.findRouteByID("feed402-federated"), resp)
	if len(cits) != 1 {
		t.Fatalf("one citation per contributing provider, got %d", len(cits))
	}
	if !strings.HasPrefix(cits[0].SourceID, "openalex:") {
		t.Errorf("citation should use the route source prefix, got %q", cits[0].SourceID)
	}
	if len(cits[0].ResultIndex) != len(resp.Results) {
		t.Errorf("citation must bind to the results it grounds, got %v", cits[0].ResultIndex)
	}
	// A fan-out that returned nothing still carries a citation array.
	empty := h.federatedCitations(h.findRouteByID("feed402-federated"),
		federate.Merge("q", "search", nil, nil, federate.CostEstimate{}, time.Now()))
	if len(empty) != 1 {
		t.Errorf("an empty response must still carry one citation, got %d", len(empty))
	}
}

func TestFederated_NoCredentialLeakage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"success", 200, fedOpenAlexBody},
		{"upstream_error", 500, `{"error":"` + testFederatedKey + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := federatedTestHandler(t, serve(t, tc.status, tc.body).URL, serve(t, 200, fedS2Body).URL)
			selected := []string{"openalex-works", "semantic-scholar-search"}
			results, reports := h.fanOutFederated(context.Background(), selected, "q")
			resp := federate.Merge("q", "search", results, reports,
				federate.Estimate("search", h.federatedPrices(selected), 0), time.Now())
			payload, _ := json.Marshal(map[string]any{
				"data":     resp,
				"citation": h.federatedCitations(h.findRouteByID("feed402-federated"), resp),
			})
			if strings.Contains(string(payload), testFederatedKey) {
				t.Fatal("an upstream credential reached the response payload")
			}
			if strings.Contains(string(payload), "Authorization") {
				t.Fatal("an upstream Authorization header reached the response payload")
			}
		})
	}
}

// Every single-provider route keeps working exactly as before. Federation
// is an operation alongside them.
func TestFederated_SingleProviderRoutesUnchanged(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 200, fedOpenAlexBody).URL, serve(t, 200, fedS2Body).URL)
	for _, id := range []string{
		"pubmed-search", "pubmed-fetch", "openalex-works", "semantic-scholar-search",
	} {
		if h.findRouteByID(id) == nil {
			t.Errorf("route %q disappeared", id)
		}
	}
	// The search-tier envelope path for a single route is untouched: the
	// same adapter still produces the same hits.
	hits := h.hitsForRoute("openalex-works", []byte(fedOpenAlexBody))
	if len(hits) != 3 || hits[0].SourceID != "openalex:W1" || hits[0].Rank != 1 {
		t.Errorf("single-provider hit extraction changed: %+v", hits)
	}
}

// A federated result copies Title/Authors/Year from the adapter's
// DescriptorProvider when the adapter implements one, so a caller gets a
// usable bibliographic surface without reparsing provider-specific Raw.
func TestFederatedFromProvider_DescriptorProviderPopulatesTitleAuthorsYear(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 200, fedOpenAlexBodyWithTitle).URL, serve(t, 200, fedS2Body).URL)
	fan := h.federatedFromProvider(context.Background(), "openalex-works", "photosynthesis")
	if fan.report.Outcome != federate.OutcomeOK {
		t.Fatalf("provider call failed: %+v", fan.report)
	}
	if len(fan.results) != 1 {
		t.Fatalf("got %d results, want 1", len(fan.results))
	}
	got := fan.results[0]
	if got.Title != "Spectral inverse problems in photosynthetic systems" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Year != 2019 {
		t.Errorf("Year = %d, want 2019", got.Year)
	}
	if len(got.Authors) != 2 || got.Authors[0] != "Ada Lovelace" || got.Authors[1] != "Grace Hopper" {
		t.Errorf("Authors = %v", got.Authors)
	}
	// Raw stays intact for provenance/debugging alongside the normalized
	// fields, never replaced by them.
	if len(got.Raw) == 0 {
		t.Error("Raw must survive alongside the copied descriptor fields")
	}
}

// A record from a provider whose adapter has no DescriptorProvider must
// come back with no title/authors/year rather than an invented one.
func TestFederatedFromProvider_NoDescriptorProviderOmitsMetadata(t *testing.T) {
	cfg := &config.GatewayConfig{
		Routes: []config.RouteConfig{{
			ID: "pubmed-search", Path: "/research/pubmed/search", Method: "GET",
			Price: "0.001", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "pubmed"},
			Upstream: config.UpstreamConfig{PassThrough: []string{"term"}, Timeout: 5},
		}},
	}
	h := newTestHandler(cfg)
	upstream := serve(t, 200, `{"esearchresult":{"idlist":["38831607"]}}`)
	h.cfg.Routes[0].Upstream.BaseURL = upstream.URL
	h.httpClient = &http.Client{Timeout: 5 * time.Second}

	fan := h.federatedFromProvider(context.Background(), "pubmed-search", "mitochondria")
	if fan.report.Outcome != federate.OutcomeOK || len(fan.results) != 1 {
		t.Fatalf("provider call failed: %+v %+v", fan.report, fan.results)
	}
	got := fan.results[0]
	if got.Title != "" || got.Year != 0 || got.Authors != nil {
		t.Errorf("a descriptor-less provider must not invent metadata, got %+v", got)
	}
}

// A `limit` posted as JSON caps the merged result count.
func TestHandleFederated_JSONLimitCapsResults(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 200, fedOpenAlexBody).URL, serve(t, 200, fedS2Body).URL)
	selected := []string{"openalex-works", "semantic-scholar-search"}
	results, reports := h.fanOutFederated(context.Background(), selected, "photosynthesis")
	resp := federate.Merge("photosynthesis", "search", results, reports,
		federate.Estimate("search", h.federatedPrices(selected), 0), time.Now())
	if len(resp.Results) != 5 {
		t.Fatalf("fixture setup: got %d results, want 5", len(resp.Results))
	}

	req := h.parseFederatedRequest(httptest.NewRequest("POST", "/research/federated",
		strings.NewReader(`{"query":"photosynthesis","limit":3}`)))
	if req.Limit != 3 {
		t.Fatalf("parsed limit = %d, want 3", req.Limit)
	}
	limited := resp.Truncate(req.Limit)
	if len(limited.Results) != 3 {
		t.Fatalf("Hits after limiting = %d, want 3", len(limited.Results))
	}
	// Limiting must not touch reports or cost.
	if len(limited.Providers) != len(resp.Providers) {
		t.Errorf("provider reports changed under a limit: %v vs %v", limited.Providers, resp.Providers)
	}
	if limited.Cost.TotalUSD != resp.Cost.TotalUSD {
		t.Errorf("cost changed under a limit: %v vs %v", limited.Cost, resp.Cost)
	}

	// Citations built from the limited response only reference kept indices.
	cits := h.federatedCitations(h.findRouteByID("feed402-federated"), limited)
	for _, c := range cits {
		for _, idx := range c.ResultIndex {
			if idx >= len(limited.Results) {
				t.Fatalf("citation result_index %d outside %d returned results", idx, len(limited.Results))
			}
		}
	}
}

// The `?limit=N` query parameter works identically to the JSON field.
func TestParseFederatedRequest_LimitFromQueryString(t *testing.T) {
	h := federatedTestHandler(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	req := h.parseFederatedRequest(httptest.NewRequest("GET", "/research/federated?query=x&limit=3", nil))
	if req.Limit != 3 {
		t.Errorf("limit from query string = %d, want 3", req.Limit)
	}
}

// limit=0 (or an absent limit) preserves the pre-existing unlimited
// behavior: Truncate is a no-op and every merged result comes back.
func TestHandleFederated_LimitZeroPreservesExistingBehavior(t *testing.T) {
	h := federatedTestHandler(t, serve(t, 200, fedOpenAlexBody).URL, serve(t, 200, fedS2Body).URL)
	selected := []string{"openalex-works", "semantic-scholar-search"}
	results, reports := h.fanOutFederated(context.Background(), selected, "photosynthesis")
	resp := federate.Merge("photosynthesis", "search", results, reports,
		federate.Estimate("search", h.federatedPrices(selected), 0), time.Now())

	for _, req := range []string{
		`{"query":"photosynthesis"}`,
		`{"query":"photosynthesis","limit":0}`,
	} {
		parsed := h.parseFederatedRequest(httptest.NewRequest("POST", "/research/federated", strings.NewReader(req)))
		if parsed.Limit != 0 {
			t.Fatalf("%s: parsed limit = %d, want 0", req, parsed.Limit)
		}
		got := resp
		if parsed.Limit > 0 {
			got = resp.Truncate(parsed.Limit)
		}
		if len(got.Results) != len(resp.Results) {
			t.Errorf("%s: limit=0 truncated results, got %d want %d", req, len(got.Results), len(resp.Results))
		}
	}
}

// A negative limit is rejected by the same validation gate handleFederated
// calls before it does any provider fan-out or truncation. Driving the
// full paid handler needs a real x402 payment signature, so this exercises
// the guard directly, the way TestHandleFederatedEstimate_FreeAndPrePayment
// exercises the free path directly.
func TestFederatedRequest_ValidateRejectsNegativeLimit(t *testing.T) {
	h := federatedTestHandler(t, "http://127.0.0.1:1", "http://127.0.0.1:1")

	req := h.parseFederatedRequest(httptest.NewRequest("POST", "/research/federated?limit=-1",
		strings.NewReader(`{"query":"photosynthesis"}`)))
	if req.Limit != -1 {
		t.Fatalf("parsed limit = %d, want -1", req.Limit)
	}
	if msg := req.validate(); msg == "" {
		t.Fatal("a negative limit must fail validate()")
	}

	// A non-negative limit alongside a non-empty query must pass.
	ok := h.parseFederatedRequest(httptest.NewRequest("POST", "/research/federated",
		strings.NewReader(`{"query":"photosynthesis","limit":3}`)))
	if msg := ok.validate(); msg != "" {
		t.Fatalf("a valid request must not fail validate(), got %q", msg)
	}
}
