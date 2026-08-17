package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// The OpenCitations access token a leakage test proves never reaches the
// response body. Fixture string, not a credential.
const testCitationToken = "SECRET-OPENCITATIONS-TOKEN-do-not-leak"

// citationsTestHandler wires the OpenCitations and Crossref references
// routes at fake upstreams. Both accept a DOI, so one query reaches both
// and their answers can be made to disagree.
func citationsTestHandler(t *testing.T, ocURL, crURL string) *Handler {
	t.Helper()
	cfg := testCfg()
	cfg.Feed402.Citations = config.CitationsConfig{
		Enabled: true, Path: "/research/citations", Price: "0.005",
		MaxConcurrency: 2, TimeoutSeconds: 5,
		// Restricted to the two mocked upstreams. No unit test in this repo
		// contacts a live provider.
		ProviderRouteIDs: []string{"opencitations-references", "crossref-references"},
	}
	cfg.Routes = append(cfg.Routes,
		config.RouteConfig{
			ID: "opencitations-references", Path: "/research/opencitations/references", Method: "GET",
			Price: "0.001", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "opencitations", ProviderURL: "https://opencitations.net/", License: "CC0"},
			Upstream: config.UpstreamConfig{
				BaseURL: ocURL, PathTemplate: "/index/v2/references/{id}",
				PassThrough: []string{"id"}, Timeout: 5,
				Headers: map[string]string{"Authorization": testCitationToken},
			},
		},
		config.RouteConfig{
			ID: "crossref-references", Path: "/research/crossref/references", Method: "GET",
			Price: "0.001", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "crossref", ProviderURL: "https://www.crossref.org/", License: "CC0"},
			Upstream: config.UpstreamConfig{
				BaseURL: crURL, PathTemplate: "/works/{id}",
				PassThrough: []string{"id"}, Timeout: 5,
			},
		},
		// Declared so the manifest covers both directions. Excluded from
		// the fan-out allowlist above, so no test dials it.
		config.RouteConfig{
			ID: "opencitations-cited-by", Path: "/research/opencitations/cited-by", Method: "GET",
			Price: "0.001", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "opencitations", License: "CC0"},
			Upstream: config.UpstreamConfig{
				BaseURL: ocURL, PathTemplate: "/index/v2/citations/{id}",
				PassThrough: []string{"id"}, Timeout: 5,
			},
		},
		config.RouteConfig{
			ID: "feed402-citations", Path: "/research/citations", Method: "POST",
			Price: "0.005", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "citations", License: "mixed"},
		},
	)
	h := newTestHandler(cfg)
	h.httpClient = &http.Client{Timeout: 5 * time.Second}
	return h
}

// OpenCitations and Crossref agree on one reference and each hold one the
// other does not. Both views survive whole.
const ocDisagreeBody = `[
 {"oci":"0201-1","citing":"doi:10.1234/citing","cited":"doi:10.5678/shared"},
 {"oci":"0201-2","citing":"doi:10.1234/citing","cited":"doi:10.9999/only-oc"}]`

const crDisagreeBody = `{"message":{"DOI":"10.1234/citing","reference-count":2,"reference":[
 {"key":"r1","DOI":"10.5678/shared"},
 {"key":"r2","DOI":"10.1111/only-cr"}]}}`

func testQueryDOI(t *testing.T) identity.Identifier {
	t.Helper()
	id, ok := identity.New(identity.SchemeDOI, "10.1234/citing")
	if !ok {
		t.Fatal("fixture DOI rejected")
	}
	return id
}

func TestFanOutCitations_ProviderDisagreementSurvives(t *testing.T) {
	h := citationsTestHandler(t, serve(t, 200, ocDisagreeBody).URL, serve(t, 200, crDisagreeBody).URL)

	edges, reports := h.fanOutCitations(context.Background(), citation.DirectionReferences, testQueryDOI(t))
	if len(edges) != 4 {
		t.Fatalf("both providers' edges must survive, got %d", len(edges))
	}
	result := citation.Build(citation.DirectionReferences, testQueryDOI(t), edges, reports, time.Now())

	// One shared reference, recognized across providers without a merge.
	if len(result.Equivalences) != 1 {
		t.Fatalf("expected one equivalence for the shared reference, got %+v", result.Equivalences)
	}
	if len(result.Equivalences[0].Edges) != 2 {
		t.Errorf("the equivalence should name both providers' edges, got %v", result.Equivalences[0].Edges)
	}
	// The edges each provider holds alone stay present and attributed.
	targets := map[string]string{}
	for _, e := range result.Edges {
		for _, id := range e.Target.Identifiers {
			targets[id.Value] = e.Provider
		}
	}
	if targets["10.9999/only-oc"] != "opencitations-references" {
		t.Errorf("the OpenCitations-only edge was lost or misattributed: %v", targets)
	}
	if targets["10.1111/only-cr"] != "crossref-references" {
		t.Errorf("the Crossref-only edge was lost or misattributed: %v", targets)
	}
	// Neither provider is declared correct.
	if len(result.ProvidersConsulted) != 2 {
		t.Fatalf("both providers must be reported, got %+v", result.ProvidersConsulted)
	}
	for _, r := range result.ProvidersConsulted {
		if !r.Consulted || r.Outcome != citation.OutcomeOK || r.EdgeCount != 2 {
			t.Errorf("provider report = %+v", r)
		}
	}
}

// The four ways a provider can produce no edges must stay distinguishable.
func TestFanOutCitations_OutcomesAreDistinguishable(t *testing.T) {
	// A provider consulted that answered with nothing.
	h := citationsTestHandler(t, serve(t, 200, `[]`).URL, serve(t, 500, `{"error":"boom"}`).URL)
	_, reports := h.fanOutCitations(context.Background(), citation.DirectionReferences, testQueryDOI(t))
	byProvider := map[string]citation.ProviderReport{}
	for _, r := range reports {
		byProvider[r.Provider] = r
	}
	if r := byProvider["opencitations-references"]; !r.Consulted || r.Outcome != citation.OutcomeOK || r.EdgeCount != 0 {
		t.Errorf("an empty answer must read as consulted/ok/0, got %+v", r)
	}
	if r := byProvider["crossref-references"]; !r.Consulted || r.Outcome != citation.OutcomeUpstreamStatus || r.UpstreamStatus != 500 {
		t.Errorf("a 500 must read as an upstream status failure, got %+v", r)
	}

	// A direction a provider does not serve.
	_, reports = h.fanOutCitations(context.Background(), citation.DirectionCitedBy, testQueryDOI(t))
	for _, r := range reports {
		if r.Outcome != citation.OutcomeUnsupportedDirection {
			t.Errorf("%s serves references only, got outcome %q", r.Provider, r.Outcome)
		}
		if r.Consulted {
			t.Errorf("%s must not be marked consulted for a direction it does not serve", r.Provider)
		}
	}

	// An identifier scheme a provider cannot express a query for.
	openalexID, _ := identity.New(identity.SchemeOpenAlex, "W1")
	_, reports = h.fanOutCitations(context.Background(), citation.DirectionReferences, openalexID)
	for _, r := range reports {
		if r.Outcome != citation.OutcomeUnsupportedIdentifier {
			t.Errorf("%s takes a DOI, so an OpenAlex id must be unsupported, got %q", r.Provider, r.Outcome)
		}
		if r.Consulted {
			t.Errorf("%s must not be marked consulted when it was never called", r.Provider)
		}
	}
}

func TestFanOutCitations_TimeoutIsReportedNotSilent(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)

	h := citationsTestHandler(t, slow.URL, serve(t, 200, crDisagreeBody).URL)
	h.cfg.Feed402.Citations.TimeoutSeconds = 1
	for i := range h.cfg.Routes {
		if h.cfg.Routes[i].ID == "opencitations-references" {
			h.cfg.Routes[i].Upstream.Timeout = 1
		}
	}

	start := time.Now()
	edges, reports := h.fanOutCitations(context.Background(), citation.DirectionReferences, testQueryDOI(t))
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("fan-out did not honor the per-provider timeout: %s", elapsed)
	}
	if len(edges) != 2 {
		t.Errorf("the healthy provider should still contribute, got %d edges", len(edges))
	}
	var slowReport citation.ProviderReport
	for _, r := range reports {
		if r.Provider == "opencitations-references" {
			slowReport = r
		}
	}
	if slowReport.Outcome != citation.OutcomeTimeout && slowReport.Outcome != citation.OutcomeUpstreamError {
		t.Errorf("expected a timeout-class outcome, got %q", slowReport.Outcome)
	}
	if slowReport.EdgeCount != 0 || !slowReport.Consulted {
		t.Errorf("a timed-out provider must read as consulted with zero edges, got %+v", slowReport)
	}
}

func TestFanOutCitations_Deterministic(t *testing.T) {
	h := citationsTestHandler(t, serve(t, 200, ocDisagreeBody).URL, serve(t, 200, crDisagreeBody).URL)
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	var first string
	for i := 0; i < 10; i++ {
		edges, reports := h.fanOutCitations(context.Background(), citation.DirectionReferences, testQueryDOI(t))
		// Edge timestamps come from the wall clock, so they are normalized
		// away before comparing structure.
		for j := range edges {
			edges[j].RetrievedAt = ""
		}
		out, _ := json.Marshal(citation.Build(citation.DirectionReferences, testQueryDOI(t), edges, reports, at))
		if i == 0 {
			first = string(out)
			continue
		}
		if string(out) != first {
			t.Fatal("citation fan-out is not deterministic across runs")
		}
	}
}

// The upstream Authorization token must never reach the response body.
func TestCitations_NoCredentialLeakage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"success", 200, ocDisagreeBody},
		{"upstream_error", 500, `{"error":"` + testCitationToken + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := citationsTestHandler(t, serve(t, tc.status, tc.body).URL, serve(t, 200, crDisagreeBody).URL)
			edges, reports := h.fanOutCitations(context.Background(), citation.DirectionReferences, testQueryDOI(t))
			result := citation.Build(citation.DirectionReferences, testQueryDOI(t), edges, reports, time.Now())
			payload, _ := json.Marshal(map[string]any{
				"data":     result,
				"citation": h.citationEnvelopeCitations(h.findRouteByID("feed402-citations"), result),
			})
			if strings.Contains(string(payload), testCitationToken) {
				t.Fatal("an upstream token reached the response payload")
			}
			if strings.Contains(string(payload), "Authorization") {
				t.Fatal("an upstream Authorization header reached the response payload")
			}
		})
	}
}

// One citation per provider that contributed edges, bound to those edges.
func TestCitationEnvelopeCitations_PerContributingProvider(t *testing.T) {
	h := citationsTestHandler(t, serve(t, 200, ocDisagreeBody).URL, serve(t, 200, crDisagreeBody).URL)
	edges, reports := h.fanOutCitations(context.Background(), citation.DirectionReferences, testQueryDOI(t))
	result := citation.Build(citation.DirectionReferences, testQueryDOI(t), edges, reports, time.Now())
	cits := h.citationEnvelopeCitations(h.findRouteByID("feed402-citations"), result)

	if len(cits) != 2 {
		t.Fatalf("got %d citations, want one per contributing provider", len(cits))
	}
	prefixes := map[string]bool{}
	for _, c := range cits {
		if c.Type != "source" || c.RetrievedAt == "" {
			t.Errorf("malformed citation: %+v", c)
		}
		if len(c.ResultIndex) == 0 {
			t.Errorf("citation %s must bind to the edges it grounds", c.SourceID)
		}
		for _, idx := range c.ResultIndex {
			if idx < 0 || idx >= len(result.Edges) {
				t.Errorf("result_index %d out of range", idx)
			}
		}
		prefixes[strings.SplitN(c.SourceID, ":", 2)[0]] = true
	}
	if !prefixes["opencitations"] || !prefixes["crossref"] {
		t.Errorf("citations should use each route's source prefix, got %v", prefixes)
	}

	// A query where nothing was returned still carries a citation array.
	empty := h.citationEnvelopeCitations(h.findRouteByID("feed402-citations"),
		citation.Build(citation.DirectionCitedBy, testQueryDOI(t), nil, nil, time.Now()))
	if len(empty) != 1 {
		t.Errorf("an empty result must still produce one citation, got %d", len(empty))
	}
}

// Provider selection: adapters without a CitationGraphProvider never
// participate, and the allowlist narrows further.
func TestCitationRouteIDs_SelectionAndAllowlist(t *testing.T) {
	h := citationsTestHandler(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	h.cfg.Feed402.Citations.ProviderRouteIDs = nil
	ids := h.citationRouteIDs()
	for _, id := range ids {
		if id == "pubmed-search" || id == "openalex-works" {
			t.Errorf("%s has no CitationGraphProvider and must not participate", id)
		}
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatal("route ids must be sorted")
		}
	}
	h.cfg.Feed402.Citations.ProviderRouteIDs = []string{"crossref-references"}
	if got := h.citationRouteIDs(); len(got) != 1 || got[0] != "crossref-references" {
		t.Errorf("allowlist not honored, got %v", got)
	}
}

// The manifest advertises the citation-graph operations with their
// directions as capabilities.
func TestManifest_AdvertisesCitationOperations(t *testing.T) {
	h := citationsTestHandler(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	m := h.buildFeed402Manifest()
	byID := map[string]feed402Operation{}
	for _, op := range m.Operations {
		byID[op.OperationID] = op
	}
	if op := byID["crossref-references"]; op.Capability != "references" {
		t.Errorf("crossref-references capability = %q, want references", op.Capability)
	}
	if op := byID["opencitations-references"]; op.PaginationModel != "none" {
		t.Errorf("opencitations pagination model = %q, want none", op.PaginationModel)
	}
	fanout, ok := byID["feed402-citations"]
	if !ok {
		t.Fatal("the citation-graph operation is missing from the manifest")
	}
	if fanout.Capability != "references" {
		t.Errorf("fan-out capability = %q", fanout.Capability)
	}
	if len(fanout.IdentifierSchemes) != len(identity.Schemes()) {
		t.Errorf("fan-out should advertise every normalizable scheme, got %v", fanout.IdentifierSchemes)
	}
	// The manifest-level capability union carries both directions.
	caps := map[string]bool{}
	for _, c := range m.Capabilities {
		caps[c] = true
	}
	if !caps["references"] || !caps["cited_by"] {
		t.Errorf("manifest capabilities should include both directions, got %v", m.Capabilities)
	}
}

// The coverage statement reaches the response, whatever the outcome, so a
// consumer reading edge_count knows whose view it is looking at.
func TestFanOutCitations_CoverageStatementIsReported(t *testing.T) {
	h := citationsTestHandler(t, serve(t, 200, ocDisagreeBody).URL, serve(t, 200, crDisagreeBody).URL)

	_, reports := h.fanOutCitations(context.Background(), citation.DirectionReferences, testQueryDOI(t))
	var oc citation.ProviderReport
	for _, r := range reports {
		if r.Provider == "opencitations-references" {
			oc = r
		}
	}
	if !strings.Contains(oc.Coverage, "Index v2") {
		t.Errorf("OpenCitations coverage statement missing from the report: %q", oc.Coverage)
	}

	// It is present even when the provider was never called, so a caller
	// reading unsupported_identifier still knows what it would have asked.
	openalexID, _ := identity.New(identity.SchemeOpenAlex, "W1")
	_, reports = h.fanOutCitations(context.Background(), citation.DirectionReferences, openalexID)
	for _, r := range reports {
		if r.Provider != "opencitations-references" {
			continue
		}
		if r.Outcome != citation.OutcomeUnsupportedIdentifier {
			t.Fatalf("precondition: outcome = %q", r.Outcome)
		}
		if r.Coverage == "" {
			t.Error("coverage must be stated even for a provider that was not called")
		}
	}
}
