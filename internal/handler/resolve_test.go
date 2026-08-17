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
	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

// The API key a leakage test proves never reaches the response body. It is
// a fixture string, not a credential.
const testUpstreamKey = "SECRET-UPSTREAM-KEY-do-not-leak"

// resolveTestHandler wires two identity-capable routes at fake upstreams so
// fan-out can be driven end to end without touching a live provider.
func resolveTestHandler(t *testing.T, openAlexURL, s2URL string) *Handler {
	t.Helper()
	cfg := testCfg()
	cfg.Feed402.Resolve = config.ResolveConfig{
		Enabled: true, Path: "/research/resolve", Price: "0.005",
		MaxConcurrency: 2, TimeoutSeconds: 5,
		// Restricted to the two mocked upstreams. pubmed-search is
		// identity-capable and would otherwise be dialed for real, and no
		// unit test in this repo touches a live provider.
		ProviderRouteIDs: []string{"openalex-works", "semantic-scholar-search"},
	}
	cfg.Routes = append(cfg.Routes,
		config.RouteConfig{
			ID: "openalex-works", Path: "/research/openalex/works", Method: "GET",
			Price: "0.001", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "openalex", ProviderURL: "https://openalex.org/", License: "cc0"},
			Upstream: config.UpstreamConfig{
				BaseURL: openAlexURL, Path: "/works", PassThrough: []string{"search"},
				Timeout: 5,
				// A key-bearing header, so the leakage assertions below
				// exercise the real code path rather than a hypothetical.
				Headers: map[string]string{"Authorization": "Bearer " + testUpstreamKey},
			},
		},
		config.RouteConfig{
			ID: "semantic-scholar-search", Path: "/research/s2/search", Method: "GET",
			Price: "0.001", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "s2", ProviderURL: "https://semanticscholar.org/", License: "odc-by"},
			Upstream: config.UpstreamConfig{
				BaseURL: s2URL, Path: "/paper/search", PassThrough: []string{"query"}, Timeout: 5,
			},
		},
		config.RouteConfig{
			ID: "feed402-resolve", Path: "/research/resolve", Method: "POST",
			Price: "0.005", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "resolve", License: "mixed"},
		},
	)
	h := newTestHandler(cfg)
	h.httpClient = &http.Client{Timeout: 5 * time.Second}
	return h
}

const openAlexBody = `{"results":[{
  "id":"https://openalex.org/W2741809807",
  "doi":"https://doi.org/10.7717/peerj.4375",
  "ids":{"openalex":"https://openalex.org/W2741809807","doi":"https://doi.org/10.7717/peerj.4375","pmid":"https://pubmed.ncbi.nlm.nih.gov/29456894"},
  "title":"The state of OA",
  "publication_year":2018,
  "authorships":[{"author":{"display_name":"Heather Piwowar"}}]
}]}`

const s2Body = `{"data":[{
  "paperId":"649def34f8be52c8b66281af98ae884c09aef38b",
  "externalIds":{"DOI":"10.7717/peerj.4375","PubMed":"29456894","ArXiv":"1802.01234"},
  "title":"The state of OA",
  "year":2018,
  "authors":[{"name":"Heather Piwowar"}]
}]}`

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestFanOutIdentity_TwoProvidersLinkedByDOI(t *testing.T) {
	h := resolveTestHandler(t, serve(t, 200, openAlexBody).URL, serve(t, 200, s2Body).URL)

	records, failures := h.fanOutIdentity(context.Background(), "10.7717/peerj.4375")
	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %+v", failures)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	g := (&identity.Resolver{}).Resolve(records)
	if len(g.Nodes) != 2 {
		t.Fatalf("both provider records must remain addressable, got %d", len(g.Nodes))
	}
	// DOI and PMID both match, giving two same_work edges over the same
	// pair. Both are retained: the schemes that agreed are the evidence.
	sameWork := 0
	for _, r := range g.Relations {
		if r.Type == identity.RelSameWork {
			sameWork++
		}
		if r.Evidence.RetrievedAt == "" {
			t.Error("every relation must carry a timestamp")
		}
	}
	if sameWork == 0 {
		t.Fatalf("expected a same_work relation from the shared DOI, got %+v", g.Relations)
	}
	// Raw provider bytes survive next to the normalized identity.
	for _, n := range g.Nodes {
		if len(n.Raw) == 0 {
			t.Errorf("node %s lost its raw provider record", n.ID)
		}
	}
	// The provider-local record ids survive unchanged.
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		ids[n.RecordID] = true
	}
	if !ids["W2741809807"] || !ids["649def34f8be52c8b66281af98ae884c09aef38b"] {
		t.Errorf("raw provider identifiers did not survive resolution: %v", ids)
	}
}

// A provider returning 500 must appear as an explicit failure. It must
// never be indistinguishable from a provider that answered with no results.
func TestFanOutIdentity_PartialFailureIsExplicit(t *testing.T) {
	h := resolveTestHandler(t, serve(t, 500, `{"error":"boom"}`).URL, serve(t, 200, s2Body).URL)

	records, failures := h.fanOutIdentity(context.Background(), "10.7717/peerj.4375")
	if len(records) != 1 {
		t.Fatalf("the healthy provider should still contribute, got %d records", len(records))
	}
	if len(failures) != 1 {
		t.Fatalf("the failing provider must be reported, got %+v", failures)
	}
	if failures[0].Provider != "openalex-works" || failures[0].Reason != resolveFailStatus {
		t.Errorf("wrong failure record: %+v", failures[0])
	}
	if failures[0].Status != 500 {
		t.Errorf("upstream status should be reported, got %d", failures[0].Status)
	}

	// The contrast case: a provider that answers with an empty result set
	// contributes no records and no failure.
	h = resolveTestHandler(t, serve(t, 200, `{"results":[]}`).URL, serve(t, 200, s2Body).URL)
	records, failures = h.fanOutIdentity(context.Background(), "10.7717/peerj.4375")
	if len(failures) != 0 {
		t.Errorf("an empty result set is not a failure, got %+v", failures)
	}
	if len(records) != 1 {
		t.Errorf("got %d records, want 1", len(records))
	}
}

// A provider that hangs past the per-call timeout is reported as a timeout,
// and does not stall the providers that answered.
func TestFanOutIdentity_TimeoutIsReportedNotSilent(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)

	h := resolveTestHandler(t, slow.URL, serve(t, 200, s2Body).URL)
	h.cfg.Feed402.Resolve.TimeoutSeconds = 1
	h.cfg.Routes[len(h.cfg.Routes)-3].Upstream.Timeout = 1

	start := time.Now()
	records, failures := h.fanOutIdentity(context.Background(), "10.7717/peerj.4375")
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("fan-out did not honor the per-provider timeout: took %s", elapsed)
	}
	if len(records) != 1 {
		t.Errorf("the healthy provider should still contribute, got %d", len(records))
	}
	if len(failures) != 1 {
		t.Fatalf("the slow provider must be reported, got %+v", failures)
	}
	if failures[0].Reason != resolveFailTimeout && failures[0].Reason != resolveFailUpstream {
		t.Errorf("expected a timeout-class failure, got %q", failures[0].Reason)
	}
}

// Fan-out order and graph shape must not depend on which upstream answers
// first.
func TestFanOutIdentity_Deterministic(t *testing.T) {
	h := resolveTestHandler(t, serve(t, 200, openAlexBody).URL, serve(t, 200, s2Body).URL)
	var first string
	for i := 0; i < 10; i++ {
		records, _ := h.fanOutIdentity(context.Background(), "10.7717/peerj.4375")
		out, _ := json.Marshal((&identity.Resolver{}).Resolve(records))
		if i == 0 {
			first = string(out)
			continue
		}
		if string(out) != first {
			t.Fatal("fan-out result is not deterministic across runs")
		}
	}
}

// The upstream Authorization header must never reach the response body, the
// failure records, or the citations.
func TestResolve_NoCredentialLeakage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"success", 200, openAlexBody},
		{"upstream_error", 500, `{"error":"` + testUpstreamKey + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := resolveTestHandler(t, serve(t, tc.status, tc.body).URL, serve(t, 200, s2Body).URL)
			records, failures := h.fanOutIdentity(context.Background(), "10.7717/peerj.4375")
			g := (&identity.Resolver{}).Resolve(records)
			route := h.findRouteByID("feed402-resolve")
			payload, _ := json.Marshal(map[string]any{
				"graph":            g,
				"providers_failed": failures,
				"citation":         h.resolveCitations(route, g),
			})
			if strings.Contains(string(payload), testUpstreamKey) {
				t.Fatal("an upstream credential reached the response payload")
			}
			if strings.Contains(string(payload), "Authorization") {
				t.Fatal("an upstream Authorization header reached the response payload")
			}
		})
	}
}

// One citation per contributing provider, each bound to the graph nodes it
// supplied.
func TestResolveCitations_OnePerContributingProvider(t *testing.T) {
	h := resolveTestHandler(t, serve(t, 200, openAlexBody).URL, serve(t, 200, s2Body).URL)
	records, _ := h.fanOutIdentity(context.Background(), "10.7717/peerj.4375")
	g := (&identity.Resolver{}).Resolve(records)
	cits := h.resolveCitations(h.findRouteByID("feed402-resolve"), g)

	if len(cits) != 2 {
		t.Fatalf("got %d citations, want one per provider", len(cits))
	}
	seen := map[string]bool{}
	for _, c := range cits {
		if c.Type != "source" {
			t.Errorf("citation type should be source, got %q", c.Type)
		}
		if c.RetrievedAt == "" {
			t.Error("citation must carry retrieved_at")
		}
		if len(c.ResultIndex) == 0 {
			t.Errorf("citation %s must bind to the nodes it grounds", c.SourceID)
		}
		prefix := strings.SplitN(c.SourceID, ":", 2)[0]
		seen[prefix] = true
	}
	if !seen["openalex"] || !seen["s2"] {
		t.Errorf("citations should use each route's source prefix, got %v", seen)
	}

	// A resolution nobody answered still carries a citation array, because
	// feed402 §3 requires one.
	empty := h.resolveCitations(h.findRouteByID("feed402-resolve"), identity.Graph{})
	if len(empty) != 1 {
		t.Errorf("empty graph must still produce one citation, got %d", len(empty))
	}
}

// The manifest advertises the resolve operation with its extension
// capability and the identifier schemes the resolver can normalize, so an
// agent can decide before paying.
func TestManifest_AdvertisesResolveOperation(t *testing.T) {
	h := resolveTestHandler(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	m := h.buildFeed402Manifest()
	var op *feed402Operation
	for i := range m.Operations {
		if m.Operations[i].OperationID == "feed402-resolve" {
			op = &m.Operations[i]
		}
	}
	if op == nil {
		t.Fatal("resolve operation missing from the manifest")
	}
	if op.Capability != string(provider.CapIdentityResolution) {
		t.Errorf("capability = %q, want %q", op.Capability, provider.CapIdentityResolution)
	}
	if len(op.IdentifierSchemes) != len(identity.Schemes()) {
		t.Errorf("resolve should advertise every normalizable scheme, got %v", op.IdentifierSchemes)
	}
}

// A route with no IdentityProvider is never asked to resolve, and the
// allowlist narrows the set further.
func TestIdentityRouteIDs_SelectionAndAllowlist(t *testing.T) {
	h := resolveTestHandler(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	h.cfg.Feed402.Resolve.ProviderRouteIDs = nil
	ids := h.identityRouteIDs()
	if len(ids) != 3 {
		t.Fatalf("expected the three identity-capable routes, got %v", ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatal("route ids must be sorted")
		}
	}
	for _, id := range ids {
		if id == "pubmed-fetch" {
			t.Error("a route with no IdentityProvider must not participate")
		}
	}
	h.cfg.Feed402.Resolve.ProviderRouteIDs = []string{"openalex-works"}
	if got := h.identityRouteIDs(); len(got) != 1 || got[0] != "openalex-works" {
		t.Errorf("allowlist not honored, got %v", got)
	}
}
