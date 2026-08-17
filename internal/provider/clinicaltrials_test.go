package provider

import "testing"

func TestClinicalTrialsSearchAdapter_EndToEndCitations(t *testing.T) {
	body := []byte(`{"studies":[
		{"protocolSection":{"identificationModule":{"nctId":"NCT01234567"}}},
		{"protocolSection":{"identificationModule":{"nctId":"NCT07654321"}}}
	]}`)
	route := testRoute("clinicaltrials-search", "nct")
	recs := ClinicalTrialsSearchAdapter.Normalizer.Normalize(body)
	hits := ClinicalTrialsSearchAdapter.CitationProvider.Citations(route, recs)

	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].SourceID != "nct:NCT01234567" {
		t.Errorf("source_id: got %q", hits[0].SourceID)
	}
	if hits[0].CanonicalURL != "https://clinicaltrials.gov/study/NCT01234567" {
		t.Errorf("canonical_url: got %q", hits[0].CanonicalURL)
	}
}

func TestClinicalTrialsSearchAdapter_Capabilities(t *testing.T) {
	if ClinicalTrialsSearchAdapter.Searcher.PaginationModel() != "token" {
		t.Errorf("pagination model: got %q want token", ClinicalTrialsSearchAdapter.Searcher.PaginationModel())
	}
}
