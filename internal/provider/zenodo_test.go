package provider

import "testing"

// zenodoSearchFixture is trimmed from a live zenodo.org/api/records search
// response verified 2026-08-18 (see zenodo.go's doc comment).
const zenodoSearchFixture = `{"hits":{"hits":[
  {"id":14575984,"doi":"10.5281/zenodo.14575984","metadata":{"title":"Supplementary data for cytotoxicity prediction","access_right":"open"}},
  {"id":14575985,"doi":"","metadata":{"title":"A record with no DOI yet","access_right":"open"}}
]}}`

const zenodoSingleFixture = `{"id":14575984,"doi":"10.5281/zenodo.14575984","metadata":{"title":"Supplementary data"}}`

func TestZenodoNormalizer_SearchShape(t *testing.T) {
	recs := ZenodoNormalizer{}.Normalize([]byte(zenodoSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "14575984" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doi.org/10.5281/zenodo.14575984" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	// A record with no DOI yet still resolves through Zenodo's own
	// permanent record URL rather than being dropped.
	if recs[1].CanonicalURL != "https://zenodo.org/records/14575985" {
		t.Errorf("fallback canonical url = %q", recs[1].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}
}

func TestZenodoNormalizer_SingleRecordShape(t *testing.T) {
	recs := ZenodoNormalizer{}.Normalize([]byte(zenodoSingleFixture))
	if len(recs) != 1 || recs[0].ID != "14575984" {
		t.Fatalf("single-record shape not handled: %+v", recs)
	}
}

func TestZenodoNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (ZenodoNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}
