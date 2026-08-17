package provider

import "testing"

func TestOpenAlexWorksAdapter_EndToEndCitations(t *testing.T) {
	body := []byte(`{"results":[{"id":"https://openalex.org/W1234"},{"id":"https://openalex.org/W5678"}]}`)
	route := testRoute("openalex-works", "openalex")
	recs := OpenAlexWorksAdapter.Normalizer.Normalize(body)
	hits := OpenAlexWorksAdapter.CitationProvider.Citations(route, recs)

	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].SourceID != "openalex:W1234" {
		t.Errorf("source_id should use the shortened id: got %q", hits[0].SourceID)
	}
	// CanonicalURL is the full upstream URL verbatim, not reconstructed.
	if hits[0].CanonicalURL != "https://openalex.org/W1234" {
		t.Errorf("canonical_url: got %q", hits[0].CanonicalURL)
	}
}

func TestShortOpenAlexID(t *testing.T) {
	cases := map[string]string{
		"https://openalex.org/W1234": "W1234",
		"":                           "",
		"no-slash-here":              "no-slash-here",
	}
	for in, want := range cases {
		if got := shortOpenAlexID(in); got != want {
			t.Errorf("shortOpenAlexID(%q): got %q want %q", in, got, want)
		}
	}
}

func TestOpenAlexWorksAdapter_Capabilities(t *testing.T) {
	if !OpenAlexWorksAdapter.Supports(CapSearch) {
		t.Error("should support search")
	}
	if OpenAlexWorksAdapter.Searcher.PaginationModel() != "page" {
		t.Errorf("pagination model: got %q want page", OpenAlexWorksAdapter.Searcher.PaginationModel())
	}
}
