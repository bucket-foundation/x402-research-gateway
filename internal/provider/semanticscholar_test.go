package provider

import "testing"

func TestSemanticScholarSearchAdapter_EndToEndCitations(t *testing.T) {
	body := []byte(`{"data":[{"paperId":"abc123"},{"paperId":"def456"},{"paperId":""}]}`)
	route := testRoute("semantic-scholar-search", "s2")
	recs := SemanticScholarSearchAdapter.Normalizer.Normalize(body)
	hits := SemanticScholarSearchAdapter.CitationProvider.Citations(route, recs)

	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2 (empty paperId skipped)", len(hits))
	}
	if hits[0].SourceID != "s2:abc123" {
		t.Errorf("source_id: got %q", hits[0].SourceID)
	}
	if hits[0].CanonicalURL != "https://www.semanticscholar.org/paper/abc123" {
		t.Errorf("canonical_url: got %q", hits[0].CanonicalURL)
	}
	// Original position (1-based) is preserved even though the 3rd record
	// (empty paperId) is skipped — matches the pre-#2 per-provider parser.
	if hits[0].Rank != 1 || hits[1].Rank != 2 {
		t.Errorf("ranks: got %d, %d want 1, 2", hits[0].Rank, hits[1].Rank)
	}
}

func TestSemanticScholarSearchAdapter_Capabilities(t *testing.T) {
	if !SemanticScholarSearchAdapter.Supports(CapSearch) {
		t.Error("should support search")
	}
	if SemanticScholarSearchAdapter.Searcher.PaginationModel() != "offset" {
		t.Errorf("pagination model: got %q", SemanticScholarSearchAdapter.Searcher.PaginationModel())
	}
}

func TestSemanticScholarSearchNormalizer_MalformedBodyReturnsNil(t *testing.T) {
	if recs := (SemanticScholarSearchNormalizer{}).Normalize([]byte(`{bad json`)); recs != nil {
		t.Errorf("expected nil, got %v", recs)
	}
}
