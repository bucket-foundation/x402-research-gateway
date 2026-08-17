package provider

import "testing"

// stubSync is a minimal SyncProvider for testing Adapter.Capabilities()'s
// bulk/incremental reporting.
type stubSync struct{ bulk, incremental bool }

func (s stubSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: s.bulk, Incremental: s.incremental}
}

type stubSearcher struct{}

func (stubSearcher) PaginationModel() string { return "cursor" }

type stubFetcher struct{}

func (stubFetcher) IdentifierSchemes() []string { return []string{"doi"} }

type stubAsset struct{}

func (stubAsset) Assets(NormalizedRecord) []Asset { return nil }

type stubVocab struct{}

func (stubVocab) LookupTerm(string) (NormalizedRecord, bool) { return NormalizedRecord{}, false }

func TestAdapter_CapabilitiesReflectsOnlyWhatsImplemented(t *testing.T) {
	empty := &Adapter{ID: "empty"}
	if caps := empty.Capabilities(); len(caps) != 0 {
		t.Errorf("empty adapter should report no capabilities, got %v", caps)
	}
	if empty.Supports(CapSearch) {
		t.Error("empty adapter must not support search")
	}

	full := &Adapter{
		ID:                 "full",
		Searcher:           stubSearcher{},
		Fetcher:            stubFetcher{},
		AssetProvider:      stubAsset{},
		VocabularyProvider: stubVocab{},
		SyncProvider:       stubSync{bulk: true, incremental: true},
	}
	for _, c := range []Capability{CapSearch, CapPagination, CapFetch, CapAssets, CapVocabulary, CapBulk, CapIncrementalSync} {
		if !full.Supports(c) {
			t.Errorf("full adapter should support %q", c)
		}
	}
	if full.Supports(CapPatents) {
		t.Error("full adapter should not report an unimplemented capability")
	}
}

func TestAdapter_SyncProviderPartialCapability(t *testing.T) {
	a := &Adapter{ID: "bulk-only", SyncProvider: stubSync{bulk: true, incremental: false}}
	if !a.Supports(CapBulk) {
		t.Error("should support bulk")
	}
	if a.Supports(CapIncrementalSync) {
		t.Error("should not claim incremental_sync when SyncCapability says false")
	}
}

func TestAdapter_NilAdapterIsSafe(t *testing.T) {
	var a *Adapter
	if caps := a.Capabilities(); caps != nil {
		t.Errorf("nil adapter should report nil capabilities, got %v", caps)
	}
	if a.Supports(CapSearch) {
		t.Error("nil adapter must not support anything")
	}
}

func TestGenericCitationProvider_SkipsEmptyIDPreservingOriginalRank(t *testing.T) {
	route := testRoute("x", "prefix")
	records := []NormalizedRecord{
		{ID: "a", CanonicalURL: "https://example.org/a"},
		{ID: "", CanonicalURL: "https://example.org/missing"},
		{ID: "c", CanonicalURL: "https://example.org/c"},
	}
	hits := GenericCitationProvider{}.Citations(route, records)
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].Rank != 1 || hits[1].Rank != 3 {
		t.Errorf("ranks should reflect original position: got %d, %d want 1, 3", hits[0].Rank, hits[1].Rank)
	}
}

func TestGenericCitationProvider_CapsAtMaxHitsPerEnvelope(t *testing.T) {
	route := testRoute("x", "prefix")
	records := make([]NormalizedRecord, 15)
	for i := range records {
		records[i] = NormalizedRecord{ID: string(rune('a' + i))}
	}
	hits := GenericCitationProvider{}.Citations(route, records)
	if len(hits) != maxHitsPerEnvelope {
		t.Errorf("got %d hits, want %d (capped)", len(hits), maxHitsPerEnvelope)
	}
}

func TestGenericCitationProvider_FallsBackToRouteIDWhenNoSourcePrefix(t *testing.T) {
	route := testRoute("bare-route", "")
	hits := GenericCitationProvider{}.Citations(route, []NormalizedRecord{{ID: "1"}})
	if hits[0].SourceID != "bare-route:1" {
		t.Errorf("source_id: got %q want bare-route:1", hits[0].SourceID)
	}
}

func TestGenericCitationProvider_EmptyRecordsReturnsNil(t *testing.T) {
	if hits := (GenericCitationProvider{}).Citations(testRoute("x", "p"), nil); hits != nil {
		t.Errorf("expected nil, got %v", hits)
	}
}

// The seven adapters migrated in x402-research-gateway#2 must stay
// registered as later waves add providers. New adapters extend the
// registry; they never displace one.
func TestDefaultRegistry_CoversAllSixMigratedProviders(t *testing.T) {
	reg := DefaultRegistry()
	want := []string{
		"pubmed-search", "pubmed-fetch", "semantic-scholar-search",
		"openalex-works", "clinicaltrials-search", "kruse-search", "pubchem-compound",
	}
	for _, id := range want {
		if _, ok := reg[id]; !ok {
			t.Errorf("registry missing adapter for %q", id)
		}
	}
	if len(reg) < len(want) {
		t.Errorf("registry shrank to %d entries, below the %d migrated adapters", len(reg), len(want))
	}
}

// The citation-graph adapters from x402-research-gateway#6, one per
// provider per direction.
func TestDefaultRegistry_CoversCitationGraphAdapters(t *testing.T) {
	reg := DefaultRegistry()
	for _, id := range []string{
		"openalex-references", "openalex-cited-by",
		"semantic-scholar-references", "semantic-scholar-cited-by",
		"opencitations-references", "opencitations-cited-by",
		"crossref-references",
	} {
		a, ok := reg[id]
		if !ok {
			t.Fatalf("registry missing citation adapter for %q", id)
		}
		if a.CitationGraphProvider == nil {
			t.Errorf("%s has no CitationGraphProvider", id)
		}
	}
	// Crossref serves outbound references only. No cited-by adapter must
	// exist for it, so a cited-by query reports the direction unsupported
	// rather than reporting Crossref as a provider that found nothing.
	if _, ok := reg["crossref-cited-by"]; ok {
		t.Error("Crossref has no cited-by endpoint and must not be registered for that direction")
	}
}

func TestDefaultRegistry_EveryAdapterIDMatchesItsMapKey(t *testing.T) {
	for key, adapter := range DefaultRegistry() {
		if adapter.ID != key {
			t.Errorf("registry key %q maps to adapter with ID %q", key, adapter.ID)
		}
	}
}
