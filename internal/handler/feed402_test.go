package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

// newTestHandler returns a Handler with only the fields the feed402 layer
// reads. It deliberately avoids NewHandler (which wires the x402 SDK +
// facilitator client + router), because the envelope / manifest helpers
// should be independently testable.
func newTestHandler(cfg *config.GatewayConfig) *Handler {
	return &Handler{cfg: cfg, providers: provider.DefaultRegistry()}
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
			Spec:           "feed402/0.3",
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

	if m.Spec != "feed402/0.3" {
		t.Errorf("spec: got %q want feed402/0.3", m.Spec)
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

func TestBuildFeed402Manifest_CapabilitiesFromAdapterRegistry(t *testing.T) {
	h := newTestHandler(testCfg())
	m := h.buildFeed402Manifest()

	var search, fetch *feed402RouteEntry
	for i := range m.Routes {
		switch m.Routes[i].ID {
		case "pubmed-search":
			search = &m.Routes[i]
		case "pubmed-fetch":
			fetch = &m.Routes[i]
		}
	}
	if search == nil || fetch == nil {
		t.Fatal("expected pubmed-search and pubmed-fetch routes in manifest")
	}

	foundSearch, foundPagination := false, false
	for _, c := range search.Capabilities {
		if c == "search" {
			foundSearch = true
		}
		if c == "pagination" {
			foundPagination = true
		}
	}
	if !foundSearch || !foundPagination {
		t.Errorf("pubmed-search capabilities should include search+pagination, got %v", search.Capabilities)
	}
	for _, c := range fetch.Capabilities {
		if c == "search" {
			t.Error("pubmed-fetch (raw-tier, Fetcher-only) must not report search")
		}
	}

	// A route with no registered adapter omits Capabilities entirely rather
	// than reporting an empty array.
	cfg := testCfg()
	cfg.Routes = append(cfg.Routes, config.RouteConfig{
		ID: "no-adapter-route", Path: "/x", Method: "GET", Price: "0.001", Feed402Tier: "query",
	})
	h2 := newTestHandler(cfg)
	m2 := h2.buildFeed402Manifest()
	for _, r := range m2.Routes {
		if r.ID == "no-adapter-route" && r.Capabilities != nil {
			t.Errorf("route with no adapter should have nil Capabilities, got %v", r.Capabilities)
		}
	}
}

func TestBuildFeed402Manifest_OperationsAndManifestLevelCapabilities(t *testing.T) {
	h := newTestHandler(testCfg())
	m := h.buildFeed402Manifest()

	if m.Spec != "feed402/0.3" {
		t.Fatalf("spec: got %q want feed402/0.3", m.Spec)
	}
	if len(m.Operations) != 2 {
		t.Fatalf("operations: got %d want 2", len(m.Operations))
	}

	var search, fetch *feed402Operation
	for i := range m.Operations {
		switch m.Operations[i].OperationID {
		case "pubmed-search":
			search = &m.Operations[i]
		case "pubmed-fetch":
			fetch = &m.Operations[i]
		}
	}
	if search == nil || fetch == nil {
		t.Fatal("expected pubmed-search and pubmed-fetch operations")
	}
	if search.Capability != "search" {
		t.Errorf("search operation capability: got %q want search", search.Capability)
	}
	if search.PaginationModel != "offset" {
		t.Errorf("search operation pagination_model: got %q want offset", search.PaginationModel)
	}
	if fetch.Capability != "fetch" {
		t.Errorf("fetch operation capability: got %q want fetch", fetch.Capability)
	}
	if len(fetch.IdentifierSchemes) != 1 || fetch.IdentifierSchemes[0] != "pmid" {
		t.Errorf("fetch operation identifier_schemes: got %v want [pmid]", fetch.IdentifierSchemes)
	}

	// Manifest-level Capabilities is the union across operations.
	foundSearch, foundFetch := false, false
	for _, c := range m.Capabilities {
		if c == "search" {
			foundSearch = true
		}
		if c == "fetch" {
			foundFetch = true
		}
	}
	if !foundSearch || !foundFetch {
		t.Errorf("manifest capabilities should include both search and fetch, got %v", m.Capabilities)
	}

	// routes / tier_routes stay populated during the deprecation window.
	if len(m.Routes) != 2 {
		t.Errorf("deprecated routes[] should still be populated during the window, got %d", len(m.Routes))
	}
}

func TestBuildOperationFor_DeclarativeOnlyRouteFallsBackToNameHeuristic(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	search := config.RouteConfig{ID: "custom-search-route", Path: "/x/search"}
	fetch := config.RouteConfig{ID: "custom-lookup-route", Path: "/x/lookup"}
	if got := h.buildOperationFor(&search).Capability; got != "search" {
		t.Errorf("route with 'search' in id: got capability %q want search", got)
	}
	if got := h.buildOperationFor(&fetch).Capability; got != "fetch" {
		t.Errorf("route with no search/query in id: got capability %q want fetch", got)
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

	if len(env.Citation) == 0 {
		t.Fatal("citation must be a non-empty array (SPEC §3, v0.3)")
	}
	primary := env.Citation[0]
	if primary.Type != "source" {
		t.Errorf("citation[0].type: got %q want source", primary.Type)
	}
	if !strings.HasPrefix(primary.SourceID, "pubmed:query:") {
		t.Errorf("search source_id should be synthesized; got %q", primary.SourceID)
	}
	if primary.CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/" {
		t.Errorf("canonical_url for search: got %q", primary.CanonicalURL)
	}
	if primary.License != "public-domain" {
		t.Errorf("license: got %q", primary.License)
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
	// data must round-trip the upstream JSON (plus the additive rows/hits keys).
	var roundTrip map[string]interface{}
	if err := json.Unmarshal(env.Data, &roundTrip); err != nil {
		t.Errorf("data did not round-trip as json: %v", err)
	}
	if _, ok := roundTrip["esearchresult"]; !ok {
		t.Error("data should still carry the original upstream key esearchresult")
	}
}

func TestWrapFeed402Envelope_CitationLegacyMirrorsCitationZero(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	route := &cfg.Routes[0]
	req := mustReq(t, "https://api.example.com/research/pubmed/search?term=x")

	body := []byte(`{"esearchresult":{"idlist":["1","2"]}}`)
	wrapped, err := h.wrapFeed402Envelope(route, body, "0xabc", "0xtx", req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	var env feed402Envelope
	if err := json.Unmarshal(wrapped, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.CitationLegacy == nil {
		t.Fatal("citation_legacy should be emitted during the deprecation window (SPEC §7.1)")
	}
	got, _ := json.Marshal(*env.CitationLegacy)
	want, _ := json.Marshal(env.Citation[0])
	if string(got) != string(want) {
		t.Errorf("citation_legacy must equal citation[0]; got %s want %s", got, want)
	}
}

// TestWrapFeed402Envelope_SearchTier_CitationArrayConformsToResultIndexRules
// pins the SPEC §3.3 shape this migration produces: the provider-level
// query citation grounds every result via an explicit ResultIndex spanning
// the whole result set, and one citation per hit grounds only its own
// result — every citation in the array carries ResultIndex (SPEC §3.3
// rule 3: all-or-nothing explicit binding), and every result index
// 0..N-1 is covered by at least one citation.
func TestWrapFeed402Envelope_SearchTier_CitationArrayConformsToResultIndexRules(t *testing.T) {
	cfg := testCfg()
	h := newTestHandler(cfg)
	route := &cfg.Routes[0]
	req := mustReq(t, "https://api.example.com/research/pubmed/search?term=x")

	body := []byte(`{"esearchresult":{"idlist":["38831607","34588695","11111111"]}}`)
	wrapped, err := h.wrapFeed402Envelope(route, body, "0xabc", "0xtx", req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	var env feed402Envelope
	if err := json.Unmarshal(wrapped, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(env.Citation) != 4 { // primary + 3 hits
		t.Fatalf("citation array: got %d entries, want 4 (primary + 3 hits)", len(env.Citation))
	}

	// Rule 3: every citation carries result_index once any of them does.
	covered := map[int]bool{}
	for i, c := range env.Citation {
		if c.ResultIndex == nil {
			t.Errorf("citation[%d] missing result_index; explicit binding requires it on every entry", i)
		}
		for _, idx := range c.ResultIndex {
			covered[idx] = true
		}
	}
	for i := 0; i < 3; i++ {
		if !covered[i] {
			t.Errorf("result %d is not grounded by any citation", i)
		}
	}

	primary := env.Citation[0]
	if len(primary.ResultIndex) != 3 {
		t.Errorf("primary citation should ground every result; got result_index %v", primary.ResultIndex)
	}
	if env.Citation[1].SourceID != "pubmed:38831607" {
		t.Errorf("citation[1] should be the first hit; got %q", env.Citation[1].SourceID)
	}
	if !(len(env.Citation[1].ResultIndex) == 1 && env.Citation[1].ResultIndex[0] == 0) {
		t.Errorf("citation[1].result_index: got %v want [0]", env.Citation[1].ResultIndex)
	}

	// data.rows makes the response recognizably multi-record (SPEC §3.3
	// resultList()), and data.hits is the retained pre-0.3 alias, same
	// content, same field spelling.
	var d struct {
		Rows []feed402Hit `json:"rows"`
		Hits []feed402Hit `json:"hits"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(d.Rows) != 3 || len(d.Hits) != 3 {
		t.Errorf("data.rows / data.hits: got %d / %d, want 3 / 3", len(d.Rows), len(d.Hits))
	}
	if d.Hits[0].SourceID != "pubmed:38831607" || d.Hits[0].Rank != 1 {
		t.Errorf("data.hits[0] should preserve the original per-hit shape; got %+v", d.Hits[0])
	}

	// A duplicate dedup key would be non-conformant (SPEC §3.3 rule 5).
	// Provider-level source_id and per-hit source_ids all differ here.
	seen := map[string]bool{}
	for i, c := range env.Citation {
		if seen[c.SourceID] {
			t.Errorf("citation[%d] duplicates source_id %q", i, c.SourceID)
		}
		seen[c.SourceID] = true
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

	if len(env.Citation) != 1 {
		t.Fatalf("raw-tier (single-record) response must carry exactly 1 citation (SPEC §3.3 rule 1), got %d", len(env.Citation))
	}
	if env.Citation[0].SourceID != "pubmed:38831607" {
		t.Errorf("raw-tier source_id: got %q want pubmed:38831607", env.Citation[0].SourceID)
	}
	if env.Citation[0].CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/38831607/" {
		t.Errorf("canonical_url: got %q", env.Citation[0].CanonicalURL)
	}
	if env.Citation[0].ResultIndex != nil {
		t.Errorf("a single-record citation needs no result_index; got %v", env.Citation[0].ResultIndex)
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
	// hits[] moved from the top-level envelope field into data.hits (SPEC
	// §7.2 field mapping table), same shape, same content.
	var d struct {
		Hits []feed402Hit `json:"hits"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(d.Hits) != 3 {
		t.Fatalf("data.hits: got %d want 3", len(d.Hits))
	}
	if d.Hits[0].SourceID != "pubmed:38831607" {
		t.Errorf("hits[0].source_id: got %q", d.Hits[0].SourceID)
	}
	if d.Hits[0].CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/38831607/" {
		t.Errorf("hits[0].canonical_url: got %q", d.Hits[0].CanonicalURL)
	}
	if d.Hits[0].Rank != 1 || d.Hits[2].Rank != 3 {
		t.Errorf("ranks should be 1..N; got %d, %d", d.Hits[0].Rank, d.Hits[2].Rank)
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
	var d map[string]interface{}
	_ = json.Unmarshal(env.Data, &d)
	if _, ok := d["hits"]; ok {
		t.Errorf("raw-tier envelopes should not emit data.hits; got %v", d["hits"])
	}
	if _, ok := d["rows"]; ok {
		t.Errorf("raw-tier envelopes should not emit data.rows; got %v", d["rows"])
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

func TestInsightEnvelope_CitationIsSingleElementArrayWithLegacyAlias(t *testing.T) {
	// Mirrors the envelope construction in handleInsight (insight.go) without
	// the HTTP/payment plumbing: a single synthesized citation still
	// satisfies SPEC §3's "citation is always an array" rule, and
	// citation_legacy carries the same object during the deprecation window.
	cfg := testCfg()
	h := newTestHandler(cfg)
	insightRoute := &config.RouteConfig{ID: "feed402-insight", Citation: config.RouteCitation{SourcePrefix: "insight"}}
	retRoute := &cfg.Routes[0]
	hits := []feed402Hit{{SourceID: "pubmed:38831607", CanonicalURL: "https://pubmed.ncbi.nlm.nih.gov/38831607/", Rank: 1}}
	req := mustReq(t, "https://api.example.com/research/insight")
	citation := h.buildInsightCitation(insightRoute, retRoute, hits, "caloric restriction", req)

	env := feed402Envelope{
		Data:           json.RawMessage(`{"question":"q","summary":"s"}`),
		Citation:       []feed402CitationSource{citation},
		CitationLegacy: &citation,
		Receipt:        feed402Receipt{Tier: "insight", PriceUSD: 0.005, TX: "0xtx", PaidAt: "2026-08-17T00:00:00Z"},
	}
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round feed402Envelope
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(round.Citation) != 1 {
		t.Fatalf("insight citation array: got %d entries want 1", len(round.Citation))
	}
	if round.CitationLegacy == nil || round.CitationLegacy.SourceID != round.Citation[0].SourceID {
		t.Error("citation_legacy should mirror citation[0]")
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
