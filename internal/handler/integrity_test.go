package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/integrity"
)

// integrityTestHandler wires Crossref and Europe PMC at fake upstreams.
func integrityTestHandler(t *testing.T, crossrefURL, epmcURL string) *Handler {
	t.Helper()
	cfg := testCfg()
	cfg.Feed402.Integrity = config.IntegrityConfig{
		Enabled: true, Path: "/research/integrity", Price: "0.005",
		MaxConcurrency: 2, TimeoutSeconds: 5,
		ProviderRouteIDs: []string{"crossref-fetch", "epmc-fetch"},
	}
	cfg.Routes = append(cfg.Routes,
		config.RouteConfig{
			ID: "crossref-fetch", Path: "/research/crossref/fetch", Method: "GET",
			Price: "0.001", Feed402Tier: "raw",
			Citation: config.RouteCitation{SourcePrefix: "crossref", ProviderURL: "https://crossref.org/", License: "CC0"},
			Upstream: config.UpstreamConfig{
				BaseURL: crossrefURL, Path: "/works", PassThrough: []string{"id"}, Timeout: 5,
				Headers: map[string]string{"Authorization": "Bearer " + testUpstreamKey},
			},
		},
		config.RouteConfig{
			ID: "epmc-fetch", Path: "/research/epmc/fetch", Method: "GET",
			Price: "0.001", Feed402Tier: "raw",
			Citation: config.RouteCitation{SourcePrefix: "epmc", ProviderURL: "https://europepmc.org/", License: "per-record"},
			Upstream: config.UpstreamConfig{
				BaseURL: epmcURL, Path: "/search", PassThrough: []string{"query"}, Timeout: 5,
			},
		},
		config.RouteConfig{
			ID: "feed402-integrity", Path: "/research/integrity", Method: "POST",
			Price: "0.005", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "integrity", License: "mixed"},
		},
	)
	h := newTestHandler(cfg)
	h.httpClient = &http.Client{Timeout: 5 * time.Second}
	return h
}

const crossrefRetractionBody = `{"message": {
  "DOI": "10.1016/j.example.2019.01.001",
  "type": "journal-article",
  "title": ["A result that did not hold"],
  "updated-by": [{"DOI": "10.1016/j.example.2021.06.010", "type": "retraction",
                  "updated": {"date-parts": [[2021, 6, 15]]}}]
}}`

// Europe PMC has not ingested the retraction and reports only an erratum.
const epmcErratumBody = `{"resultList": {"result": [
  {"id": "31234567", "source": "MED", "pmid": "31234567",
   "doi": "10.1016/j.example.2019.01.001", "title": "A result that did not hold",
   "commentCorrectionList": {"commentCorrection": [{"id": "33000001", "type": "Erratum in"}]}}
]}}`

const epmcCleanBody = `{"resultList": {"result": [
  {"id": "31234567", "source": "MED", "pmid": "31234567",
   "doi": "10.1016/j.example.2019.01.001", "title": "A result that did not hold"}
]}}`

func TestFanOutIntegrity_DisagreementSurvives(t *testing.T) {
	h := integrityTestHandler(t,
		serve(t, 200, crossrefRetractionBody).URL,
		serve(t, 200, epmcErratumBody).URL)

	assertions, reports := h.fanOutIntegrity(context.Background(), "10.1016/j.example.2019.01.001")
	set := integrity.Build(mustParse(t, "10.1016/j.example.2019.01.001"), assertions, reports, time.Now())

	if len(set.Assertions) != 2 {
		t.Fatalf("want both providers' assertions, got %d", len(set.Assertions))
	}
	if !set.ProvidersDisagree {
		t.Fatal("a retraction from one provider and an erratum from the other was not flagged")
	}
	providers := map[string]bool{}
	for _, a := range set.Assertions {
		providers[a.Provider] = true
		if a.RetrievedAt == "" {
			t.Fatalf("%s: assertion carries no retrieval timestamp", a.Provider)
		}
	}
	if len(providers) != 2 {
		t.Fatalf("assertions collapsed to one provider: %v", providers)
	}
}

func TestFanOutIntegrity_SilenceIsNotClearance(t *testing.T) {
	h := integrityTestHandler(t, serve(t, 200, crossrefRetractionBody).URL, serve(t, 200, epmcCleanBody).URL)
	assertions, reports := h.fanOutIntegrity(context.Background(), "10.1016/j.example.2019.01.001")
	set := integrity.Build(mustParse(t, "10.1016/j.example.2019.01.001"), assertions, reports, time.Now())

	var epmc integrity.ProviderReport
	for _, r := range set.ProvidersConsulted {
		if r.Provider == "epmc-fetch" {
			epmc = r
		}
	}
	if !epmc.Consulted || epmc.Outcome != integrity.OutcomeOK || epmc.AssertionCount != 0 {
		t.Fatalf("a provider that answered with nothing must say so: %+v", epmc)
	}
	if epmc.Coverage == "" {
		t.Fatal("a zero count with no coverage statement is unreadable")
	}
	if set.AbsenceNotice == "" {
		t.Fatal("absence notice missing")
	}
	// The retraction is still reported, from the provider that has it.
	if len(set.Assertions) != 1 || set.Assertions[0].Status != integrity.StatusRetraction {
		t.Fatalf("assertions = %+v", set.Assertions)
	}
}

func TestFanOutIntegrity_UpstreamFailureIsNotSilence(t *testing.T) {
	h := integrityTestHandler(t, serve(t, 503, `{}`).URL, serve(t, 200, epmcCleanBody).URL)
	_, reports := h.fanOutIntegrity(context.Background(), "10.1016/j.example.2019.01.001")
	for _, r := range reports {
		if r.Provider != "crossref-fetch" {
			continue
		}
		if r.Outcome != integrity.OutcomeUpstreamStatus || r.UpstreamStatus != 503 {
			t.Fatalf("a 503 was reported as %+v", r)
		}
	}
}

// The upstream credential must never reach an integrity response.
func TestIntegrityResponse_NoCredentialLeakage(t *testing.T) {
	h := integrityTestHandler(t, serve(t, 503, `{}`).URL, serve(t, 200, epmcErratumBody).URL)
	assertions, reports := h.fanOutIntegrity(context.Background(), "10.1016/j.example.2019.01.001")
	set := integrity.Build(mustParse(t, "10.1016/j.example.2019.01.001"), assertions, reports, time.Now())
	blob := mustJSON(t, set)
	if strings.Contains(blob, testUpstreamKey) {
		t.Fatal("the upstream credential leaked into the integrity response")
	}
	if strings.Contains(blob, h.findRouteByID("crossref-fetch").Upstream.BaseURL) {
		t.Fatal("the upstream base URL leaked into the integrity response")
	}
}
