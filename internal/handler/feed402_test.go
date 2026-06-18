package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gianyrox/x402-research-gateway/internal/config"
)

// newTestHandler returns a Handler with only the fields the feed402 layer
// reads. It deliberately avoids NewHandler (which wires the x402 SDK +
// facilitator client + router), because the envelope / manifest helpers
// should be independently testable.
func newTestHandler(cfg *config.GatewayConfig) *Handler {
	return &Handler{cfg: cfg, hitParsers: defaultHitParsers()}
}

func testCfg() *config.GatewayConfig {
	return &config.GatewayConfig{
		Port:             8092,
		RecipientAddress: "0x0000000000000000000000000000000000000001",
		Network:          "base-sepolia",
		FacilitatorURL:   "https://facilitator.x402.rs",
		DefaultPrice:     "0.001",
		Feed402: config.Feed402Config{
			Enabled:        true,
			Name:           "x402-research-gateway",
			Version:        "0.1.0",
			Spec:           "feed402/0.2",
			CitationPolicy: "mixed",
			Contact:        "research@viatika.ai",
		},
		Routes: []config.RouteConfig{
			{
				ID:          "pubmed-search",
				Path:        "/research/pubmed/search",
				Method:      "GET",
				Description: "Search PubMed",
				MimeType:    "application/json",
				Price:       "0.001",
				Feed402Tier: "query",
				Citation: config.RouteCitation{
					SourcePrefix: "pubmed",
					ProviderURL:  "https://pubmed.ncbi.nlm.nih.gov/",
					License:      "public-domain",
				},
				Upstream: config.UpstreamConfig{
					BaseURL:     "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
					Path:        "/esearch.fcgi",
					PassThrough: []string{"term"},
				},
			},
			{
				ID:          "pubmed-fetch",
				Path:        "/research/pubmed/fetch",
				Method:      "GET",
				Description: "Fetch a PubMed abstract",
				MimeType:    "application/json",
				Price:       "0.002",
				Feed402Tier: "raw",
				Citation: config.RouteCitation{
					SourcePrefix:         "pubmed",
					CanonicalURLTemplate: "https://pubmed.ncbi.nlm.nih.gov/{id}/",
					ProviderURL:          "https://pubmed.ncbi.nlm.nih.gov/",
					License:              "public-domain",
				},
				Upstream: config.UpstreamConfig{
					BaseURL:     "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
					Path:        "/efetch.fcgi",
					PassThrough: []string{"id"},
				},
			},
		},
	}
}

func mustReq(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return &http.Request{Method: "GET", URL: u, Host: "api.example.com"}
}

func TestBuildFeed402Manifest_ShapeAndCheapestPerTier(t *testing.T) {
	h := newTestHandler(testCfg())
	m := h.buildFeed402Manifest()

	if m.Spec != "feed402/0.2" {
		t.Errorf("spec: got %q want feed402/0.2", m.Spec)
	}
	if m.Wallet != "0x0000000000000000000000000000000000000001" {
		t.Errorf("wallet mismatch: %q", m.Wallet)
	}
	if m.Chain != "base-sepolia" {
		t.Errorf("chain: got %q want base-sepolia", m.Chain)
	}

	// Cheapest query route is pubmed-search @ 0.001; raw is pubmed-fetch @ 0.002.
	if q, ok := m.Tiers["query"]; !ok || q.PriceUSD != 0.001 {
		t.Errorf("query tier: got %+v", q)
	}
	if r, ok := m.Tiers["raw"]; !ok || r.PriceUSD != 0.002 {
		t.Errorf("raw tier: got %+v", r)
	}

	if len(m.Routes) != 2 {
		t.Errorf("routes: got %d want 2", len(m.Routes))
	}

	// citation_types must include "source" (v0.1 required, v0.2 unchanged).
	found := false
	for _, ct := range m.CitationTypes {
		if ct == "source" {
			found = true
		}
	}
	if !found {
		t.Errorf("citation_types must include 'source'; got %v", m.CitationTypes)
	}
}

func TestWrapFeed402Envelope_SearchTier_SynthesizesQuerySourceID(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	route := &cfg.Routes[0] // pubmed-search, query tier
	req := mustReq(t, "https://api.example.com/research/pubmed/search?term=caloric+restriction")

	body := []byte(`{"esearchresult":{"idlist":["38831607","34588695"]}}`)
	wrapped, err := h.wrapFeed402Envelope(route, body, "0xabc", "0xdeadbeef", req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	var env feed402Envelope
	if err := json.Unmarshal(wrapped, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if env.Citation.Type != "source" {
		t.Errorf("citation.type: got %q want source", env.Citation.Type)
	}
	if !strings.HasPrefix(env.Citation.SourceID, "pubmed:query:") {
		t.Errorf("search source_id should be synthesized; got %q", env.Citation.SourceID)
	}
	if env.Citation.CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/" {
		t.Errorf("canonical_url for search: got %q", env.Citation.CanonicalURL)
	}
	if env.Citation.License != "public-domain" {
		t.Errorf("license: got %q", env.Citation.License)
	}
	if env.Receipt.Tier != "query" {
		t.Errorf("receipt.tier: got %q want query", env.Receipt.Tier)
	}
	if env.Receipt.PriceUSD != 0.001 {
		t.Errorf("receipt.price_usd: got %v want 0.001", env.Receipt.PriceUSD)
	}
	if env.Receipt.TX != "0xdeadbeef" {
		t.Errorf("receipt.tx: got %q want 0xdeadbeef", env.Receipt.TX)
	}
	// data must round-trip the upstream JSON.
	var roundTrip map[string]interface{}
	if err := json.Unmarshal(env.Data, &roundTrip); err != nil {
		t.Errorf("data did not round-trip as json: %v", err)
	}
}

func TestWrapFeed402Envelope_RawFetchTier_UsesCanonicalURLTemplate(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	route := &cfg.Routes[1] // pubmed-fetch, raw tier
	req := mustReq(t, "https://api.example.com/research/pubmed/fetch?id=38831607")

	body := []byte(`{"abstract":"caloric restriction..."}`)
	wrapped, err := h.wrapFeed402Envelope(route, body, "0xabc", "", req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	var env feed402Envelope
	if err := json.Unmarshal(wrapped, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if env.Citation.SourceID != "pubmed:38831607" {
		t.Errorf("raw-tier source_id: got %q want pubmed:38831607", env.Citation.SourceID)
	}
	if env.Citation.CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/38831607/" {
		t.Errorf("canonical_url: got %q", env.Citation.CanonicalURL)
	}
	if env.Receipt.Tier != "raw" {
		t.Errorf("receipt.tier: got %q want raw", env.Receipt.Tier)
	}
	if !strings.HasPrefix(env.Receipt.TX, "pending:") {
		t.Errorf("receipt.tx should be placeholder when txHash empty; got %q", env.Receipt.TX)
	}
}

func TestWrapFeed402Envelope_NonJSONBody_StringifiedIntoData(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	route := &cfg.Routes[1]
	req := mustReq(t, "https://api.example.com/research/pubmed/fetch?id=38831607")

	// PubMed efetch returns XML, not JSON.
	body := []byte(`<?xml version="1.0"?><PubmedArticleSet><abstract/>...</PubmedArticleSet>`)
	wrapped, err := h.wrapFeed402Envelope(route, body, "0xabc", "0xtx", req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	var env feed402Envelope
	if err := json.Unmarshal(wrapped, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var s string
	if err := json.Unmarshal(env.Data, &s); err != nil {
		t.Errorf("xml body should be stringified into data: %v", err)
	}
	if !strings.Contains(s, "PubmedArticleSet") {
		t.Errorf("data did not contain upstream xml; got %q", s)
	}
}

func TestWrapFeed402Envelope_SearchTier_EmitsHits(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	route := &cfg.Routes[0] // pubmed-search
	req := mustReq(t, "https://api.example.com/research/pubmed/search?term=caloric")

	body := []byte(`{"esearchresult":{"idlist":["38831607","34588695","11111111"]}}`)
	wrapped, err := h.wrapFeed402Envelope(route, body, "0xabc", "0xtx", req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	var env feed402Envelope
	if err := json.Unmarshal(wrapped, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Hits) != 3 {
		t.Fatalf("hits: got %d want 3", len(env.Hits))
	}
	if env.Hits[0].SourceID != "pubmed:38831607" {
		t.Errorf("hits[0].source_id: got %q", env.Hits[0].SourceID)
	}
	if env.Hits[0].CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/38831607/" {
		t.Errorf("hits[0].canonical_url: got %q", env.Hits[0].CanonicalURL)
	}
	if env.Hits[0].Rank != 1 || env.Hits[2].Rank != 3 {
		t.Errorf("ranks should be 1..N; got %d, %d", env.Hits[0].Rank, env.Hits[2].Rank)
	}
}

func TestWrapFeed402Envelope_RawTier_NoHits(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	route := &cfg.Routes[1] // pubmed-fetch (raw)
	req := mustReq(t, "https://api.example.com/research/pubmed/fetch?id=38831607")

	wrapped, _ := h.wrapFeed402Envelope(route, []byte(`{"abstract":"..."}`), "0xabc", "0xtx", req)
	var env feed402Envelope
	_ = json.Unmarshal(wrapped, &env)
	if env.Hits != nil {
		t.Errorf("raw-tier envelopes should not emit hits; got %v", env.Hits)
	}
}

func TestMockSummarizer_ProducesDeterministicOutput(t *testing.T) {
	m := &mockSummarizer{}
	if !strings.HasPrefix(m.id(), "mock:") {
		t.Errorf("mock summarizer id: got %q", m.id())
	}
	s1, err := m.summarize(nil, "does caloric restriction work?", []string{"a study says yes", "another says maybe"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s1, "caloric restriction") {
		t.Errorf("summary should echo question: got %q", s1)
	}
	s2, _ := m.summarize(nil, "does caloric restriction work?", []string{"a study says yes", "another says maybe"})
	if s1 != s2 {
		t.Errorf("mock summarizer must be deterministic")
	}
	empty, _ := m.summarize(nil, "q", nil)
	if !strings.Contains(empty, "No retrieval context") {
		t.Errorf("empty-context summary should flag missing context; got %q", empty)
	}
}

func TestExtractSnippets_WalksNestedJSON(t *testing.T) {
	body := []byte(`{"results":[{"title":"Caloric Restriction and Lifespan in Mammals","abstract":"A comprehensive review of multi-decade studies across rodents and primates showing extended healthspan."}]}`)
	snips := extractSnippets(body, 10_000)
	if len(snips) < 2 {
		t.Fatalf("expected at least 2 snippets (title + abstract); got %d", len(snips))
	}
	// Cap check.
	capped := extractSnippets(body, 50)
	total := 0
	for _, s := range capped {
		total += len(s)
	}
	if total > 50+200 { // small grace
		t.Errorf("max chars not honored: got %d", total)
	}
}

func TestBuildInsightCitation_UsesTopHitWhenAvailable(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	h.summarizer = &mockSummarizer{}
	insightRoute := &config.RouteConfig{ID: "feed402-insight", Citation: config.RouteCitation{SourcePrefix: "insight"}}
	retRoute := &cfg.Routes[0]
	hits := []feed402Hit{{SourceID: "pubmed:38831607", CanonicalURL: "https://pubmed.ncbi.nlm.nih.gov/38831607/", Rank: 1}}
	req := mustReq(t, "https://api.example.com/research/insight")
	cit := h.buildInsightCitation(insightRoute, retRoute, hits, "caloric restriction", req)
	if cit.SourceID != "pubmed:38831607" {
		t.Errorf("source_id should promote top hit; got %q", cit.SourceID)
	}
	if cit.CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/38831607/" {
		t.Errorf("canonical_url should be top hit's; got %q", cit.CanonicalURL)
	}
	// No-hits fallback → synthetic insight source_id.
	cit2 := h.buildInsightCitation(insightRoute, retRoute, nil, "mitochondria", req)
	if !strings.HasPrefix(cit2.SourceID, "pubmed:insight:") {
		t.Errorf("no-hits citation should synthesize prefix:insight:<hash>; got %q", cit2.SourceID)
	}
}

func TestExtractSettleTxHash(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"transaction field", `{"success":true,"transaction":"0xabc123"}`, "0xabc123"},
		{"txHash field", `{"success":true,"txHash":"0xdeadbeef"}`, "0xdeadbeef"},
		{"snake case", `{"success":true,"tx_hash":"0xfeed"}`, "0xfeed"},
		{"no success flag still accepted", `{"transaction":"0xabc"}`, "0xabc"},
		{"success false rejected", `{"success":false,"transaction":"0xabc"}`, ""},
		{"empty body", ``, ""},
		{"not json", `plaintext`, ""},
		{"no tx field", `{"success":true,"foo":"bar"}`, ""},
	}
	for _, c := range cases {
		if got := extractSettleTxHash([]byte(c.body)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestParsePriceUSD(t *testing.T) {
	cases := map[string]float64{
		"0.001": 0.001,
		"0.5":   0.5,
		"1":     1,
		"":      0,
		"abc":   0,
	}
	for in, want := range cases {
		if got := parsePriceUSD(in); got != want {
			t.Errorf("parsePriceUSD(%q): got %v want %v", in, got, want)
		}
	}
}
