package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/harvest"
)

// harvestTestHandler wires PubMed search at a fake upstream carrying a
// key-bearing query param, so the leakage assertions exercise the real
// path.
func harvestTestHandler(t *testing.T, pubmedURL string) *Handler {
	t.Helper()
	cfg := testCfg()
	cfg.Feed402.Harvest = config.HarvestConfig{
		Enabled: true, Path: "/research/harvest", Price: "0.002",
		TimeoutSeconds: 5, CursorSecret: "test-cursor-secret",
		ProviderRouteIDs: []string{"pubmed-search"},
	}
	// testCfg already carries a pubmed-search route pointed at the live
	// NCBI endpoint. It is repointed at the fake upstream rather than
	// shadowed, because findRouteByID returns the first match and no unit
	// test in this repo touches a live provider.
	for i := range cfg.Routes {
		if cfg.Routes[i].ID != "pubmed-search" {
			continue
		}
		cfg.Routes[i].Upstream = config.UpstreamConfig{
			BaseURL: pubmedURL, Path: "/esearch.fcgi", PassThrough: []string{"term"}, Timeout: 5,
			// Key-bearing params, so the leakage assertions exercise the
			// real path.
			QueryParams: map[string]string{"email": testUpstreamKey, "api_key": testUpstreamKey},
		}
	}
	cfg.Routes = append(cfg.Routes,
		config.RouteConfig{
			ID: "feed402-harvest", Path: "/research/harvest", Method: "POST",
			Price: "0.002", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "harvest", License: "mixed"},
		},
	)
	h := newTestHandler(cfg)
	h.httpClient = &http.Client{Timeout: 5 * time.Second}
	h.harvestSigner = harvest.NewSigner(cfg.Feed402.Harvest.CursorSecret)
	return h
}

// pagedPubMed serves a three-page result set and records which offsets were
// requested, so a resume can be shown to skip what it already holds.
func pagedPubMed(t *testing.T, release string) (*httptest.Server, *[]string, *int64) {
	t.Helper()
	var offsets []string
	var calls int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		start := r.URL.Query().Get("retstart")
		offsets = append(offsets, start)
		w.Header().Set("X-RateLimit-Remaining", "7")
		w.Header().Set("X-Request-Id", "upstream-req-1")
		if release != "" {
			w.Header().Set("X-API-Version", release)
		}
		w.Header().Set("Content-Type", "application/json")
		ids := `["1","2"]`
		if start == "2" {
			ids = `["3","4"]`
		}
		if start == "4" {
			ids = `["5"]`
		}
		_, _ = w.Write([]byte(`{"esearchresult":{"count":"5","retmax":"2","idlist":` + ids + `}}`))
	}))
	t.Cleanup(s.Close)
	return s, &offsets, &calls
}

func firstPage(t *testing.T, h *Handler) harvest.Cursor {
	t.Helper()
	page, err := h.harvestPage(context.Background(), "pubmed-search",
		h.findRouteByID("pubmed-search"), h.providers["pubmed-search"], "mitochondria", harvest.Position{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	c := page.cursor
	c.ResultCount = c.PageResultCount
	c.StartedAt, c.StartedRelease = c.RetrievedAt, c.ProviderRelease
	return c
}

func TestHarvest_ResumesWithoutRefetchingPages(t *testing.T) {
	upstream, offsets, calls := pagedPubMed(t, "eutils-2026-07")
	h := harvestTestHandler(t, upstream.URL)

	first := firstPage(t, h)
	if first.PageResultCount != 2 || first.NextCursor.Offset != 2 || first.Exhausted {
		t.Fatalf("first page cursor = %+v", first)
	}

	// A client that dies here holds the cursor and nothing else. Resuming
	// starts at the position the cursor names.
	token, err := h.harvestSigner.Encode(first)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := h.harvestSigner.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.harvestPage(context.Background(), "pubmed-search",
		h.findRouteByID("pubmed-search"), h.providers["pubmed-search"], "mitochondria", resumed.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	cont := resumed.Continue(second.cursor)
	if cont.ResultCount != 4 {
		t.Fatalf("result_count = %d, want the running total 4", cont.ResultCount)
	}
	if cont.ProviderCursor.Offset != 2 {
		t.Fatalf("the resumed page started at offset %d", cont.ProviderCursor.Offset)
	}
	if got := *offsets; len(got) != 2 || got[0] != "0" || got[1] != "2" {
		t.Fatalf("upstream offsets requested = %v; a resume must not re-fetch", got)
	}
	if atomic.LoadInt64(calls) != 2 {
		t.Fatalf("upstream called %d times for two pages", atomic.LoadInt64(calls))
	}
}

func TestHarvest_ExhaustionIsReported(t *testing.T) {
	upstream, _, _ := pagedPubMed(t, "")
	h := harvestTestHandler(t, upstream.URL)
	last, err := h.harvestPage(context.Background(), "pubmed-search",
		h.findRouteByID("pubmed-search"), h.providers["pubmed-search"], "mitochondria",
		harvest.Position{Offset: 4}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !last.cursor.Exhausted {
		t.Fatal("the final page did not report the set exhausted")
	}
}

func TestHarvest_CursorCarriesProvenanceAndRateLimit(t *testing.T) {
	upstream, _, _ := pagedPubMed(t, "eutils-2026-07")
	h := harvestTestHandler(t, upstream.URL)
	c := firstPage(t, h)

	if c.RateLimitRemaining != "7" {
		t.Fatalf("rate_limit_remaining = %q", c.RateLimitRemaining)
	}
	if c.UpstreamRequestID != "upstream-req-1" {
		t.Fatalf("upstream_request_id = %q", c.UpstreamRequestID)
	}
	if !strings.HasPrefix(c.ResponseSHA256, "sha256:") {
		t.Fatalf("response_sha256 = %q", c.ResponseSHA256)
	}
	if !strings.HasPrefix(c.RequestFingerprint, "hmac-sha256:") {
		t.Fatalf("request_fingerprint = %q", c.RequestFingerprint)
	}
	if c.ProviderRelease != "eutils-2026-07" {
		t.Fatalf("provider_release = %q", c.ProviderRelease)
	}
	if c.PaginationModel != harvest.ModelOffset {
		t.Fatalf("pagination_model = %q", c.PaginationModel)
	}
	if c.RetrievedAt == "" {
		t.Fatal("retrieved_at missing")
	}
}

func TestHarvest_ReleaseBoundaryAcrossResumeIsDetected(t *testing.T) {
	before, _, _ := pagedPubMed(t, "eutils-2026-07")
	h := harvestTestHandler(t, before.URL)
	first := firstPage(t, h)

	// The harvest resumes after the provider ships a new release.
	after, _, _ := pagedPubMed(t, "eutils-2026-08")
	h2 := harvestTestHandler(t, after.URL)
	second, err := h2.harvestPage(context.Background(), "pubmed-search",
		h2.findRouteByID("pubmed-search"), h2.providers["pubmed-search"], "mitochondria", first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	cont := first.Continue(second.cursor)
	if !cont.ReleaseChanged {
		t.Fatal("a release change across a resumed harvest was not detected")
	}
	if cont.StartedRelease != "eutils-2026-07" || cont.ProviderRelease != "eutils-2026-08" {
		t.Fatalf("release state = %q -> %q", cont.StartedRelease, cont.ProviderRelease)
	}
}

// Cursor state travels to the client and back, so nothing sensitive may
// enter it. Same rule as TestUnpaywallIdentity_NoEmailLeakage.
func TestHarvestCursor_NoCredentialLeakage(t *testing.T) {
	upstream, _, _ := pagedPubMed(t, "eutils-2026-07")
	h := harvestTestHandler(t, upstream.URL)
	c := firstPage(t, h)
	token, err := h.harvestSigner.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(c)
	inspected, err := harvest.Inspect(token)
	if err != nil {
		t.Fatal(err)
	}
	readable, _ := json.Marshal(inspected)

	for _, surface := range []string{string(state), string(readable), token} {
		for _, banned := range []string{
			testUpstreamKey, "api_key", "email=", "test-cursor-secret",
			upstream.URL, // the upstream address never travels either
		} {
			if strings.Contains(surface, banned) {
				t.Fatalf("cursor state carries %q", banned)
			}
		}
	}
}

func TestHarvest_ForgedCursorIsRejected(t *testing.T) {
	upstream, _, _ := pagedPubMed(t, "")
	h := harvestTestHandler(t, upstream.URL)
	c := firstPage(t, h)
	token, _ := h.harvestSigner.Encode(c)

	// A client that rewrites its position to skip ahead loses the tag, so
	// the gateway refuses the cursor and the client is back to paying for
	// the page it asks for. A forged cursor buys nothing.
	forged := strings.Replace(token, token[:8], "AAAAAAAA", 1)
	if _, err := h.harvestSigner.Decode(forged); err == nil {
		t.Fatal("a tampered cursor verified")
	}
	if _, err := harvest.NewSigner("some other gateway").Decode(token); err == nil {
		t.Fatal("a cursor from this gateway verified under a different key")
	}
}

func TestHarvestRouteIDs_OnlyConfiguredPaginatedRoutes(t *testing.T) {
	upstream, _, _ := pagedPubMed(t, "")
	h := harvestTestHandler(t, upstream.URL)
	got := h.harvestRouteIDs()
	if len(got) != 1 || got[0] != "pubmed-search" {
		t.Fatalf("harvestable routes = %v", got)
	}
}
