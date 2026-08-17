package provider

import "testing"

func TestPubMedSearchNormalizer_ExtractsIDsAndURLs(t *testing.T) {
	body := []byte(`{"esearchresult":{"idlist":["38831607","34588695","11111111"]}}`)
	recs := PubMedSearchNormalizer{}.Normalize(body)
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	if recs[0].ID != "38831607" {
		t.Errorf("id: got %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/38831607/" {
		t.Errorf("canonical_url: got %q", recs[0].CanonicalURL)
	}
}

func TestPubMedSearchNormalizer_MalformedBodyReturnsNil(t *testing.T) {
	if recs := (PubMedSearchNormalizer{}).Normalize([]byte(`not json`)); recs != nil {
		t.Errorf("expected nil for malformed body, got %v", recs)
	}
	if recs := (PubMedSearchNormalizer{}).Normalize([]byte(`{}`)); len(recs) != 0 {
		t.Errorf("expected zero records for a body with no idlist, got %v", recs)
	}
}

func TestPubMedSearchAdapter_EndToEndCitations(t *testing.T) {
	body := []byte(`{"esearchresult":{"idlist":["38831607","34588695","11111111"]}}`)
	route := testRoute("pubmed-search", "pubmed")
	recs := PubMedSearchAdapter.Normalizer.Normalize(body)
	hits := PubMedSearchAdapter.CitationProvider.Citations(route, recs)
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3", len(hits))
	}
	if hits[0].SourceID != "pubmed:38831607" {
		t.Errorf("source_id: got %q", hits[0].SourceID)
	}
	if hits[0].CanonicalURL != "https://pubmed.ncbi.nlm.nih.gov/38831607/" {
		t.Errorf("canonical_url: got %q", hits[0].CanonicalURL)
	}
	if hits[0].Rank != 1 || hits[2].Rank != 3 {
		t.Errorf("ranks should be 1..N; got %d, %d", hits[0].Rank, hits[2].Rank)
	}
}

func TestPubMedSearchAdapter_Capabilities(t *testing.T) {
	if !PubMedSearchAdapter.Supports(CapSearch) {
		t.Error("pubmed-search should support search")
	}
	if !PubMedSearchAdapter.Supports(CapPagination) {
		t.Error("pubmed-search should support pagination")
	}
	if PubMedSearchAdapter.Supports(CapFetch) {
		t.Error("pubmed-search should not report fetch")
	}
	if PubMedSearchAdapter.Searcher.PaginationModel() != "offset" {
		t.Errorf("pagination model: got %q want offset", PubMedSearchAdapter.Searcher.PaginationModel())
	}
}

func TestPubMedFetchAdapter_FetchOnlyNoHitsCapability(t *testing.T) {
	if !PubMedFetchAdapter.Supports(CapFetch) {
		t.Error("pubmed-fetch should support fetch")
	}
	if PubMedFetchAdapter.Supports(CapSearch) {
		t.Error("pubmed-fetch should not report search")
	}
	if PubMedFetchAdapter.Normalizer != nil || PubMedFetchAdapter.CitationProvider != nil {
		t.Error("pubmed-fetch is a single-record fetch; it has nothing to enumerate into hits")
	}
	if got := PubMedFetchAdapter.Fetcher.IdentifierSchemes(); len(got) != 1 || got[0] != "pmid" {
		t.Errorf("identifier_schemes: got %v want [pmid]", got)
	}
}
