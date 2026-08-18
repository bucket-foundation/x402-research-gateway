package provider

import "testing"

// biomodelsSearchFixture and biomodelsSingleFixture are trimmed from live
// biomodels.org responses verified 2026-08-18 (see biomodels.go's doc
// comment).
const biomodelsSearchFixture = `{"matches":66,"models":[
  {"format":"SBML","id":"BIOMD0000000482","name":"Noguchi2013 - Insulin dependent glucose metabolism","submitter":"Rei Noguchi"},
  {"format":"SBML","id":"BIOMD0000000012","name":"Elowitz2000 - Repressilator","submitter":"J Cooper"}
]}`

const biomodelsSingleFixture = `{"name":"Noguchi2013 - Insulin dependent glucose metabolism","description":"...","format":"SBML","publicationId":"BIOMD0000000482"}`

func TestBioModelsNormalizer_SearchShape(t *testing.T) {
	recs := BioModelsNormalizer{}.Normalize([]byte(biomodelsSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "BIOMD0000000482" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://www.biomodels.org/BIOMD0000000482" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestBioModelsNormalizer_SingleRecordShape(t *testing.T) {
	recs := BioModelsNormalizer{}.Normalize([]byte(biomodelsSingleFixture))
	if len(recs) != 1 || recs[0].ID != "BIOMD0000000482" {
		t.Fatalf("single-record shape (publicationId) not handled: %+v", recs)
	}
}

func TestBioModelsNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (BioModelsNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestBioModelsIdentity_RecordRights_CC0(t *testing.T) {
	recs := BioModelsNormalizer{}.Normalize([]byte(biomodelsSearchFixture))
	rights := biomodelsIdentity{}.RecordRights(recs[0])
	if !rights.Permits() || rights.License != "CC0-1.0" {
		t.Errorf("rights = %+v, want CC0-1.0 permitting redistribution", rights)
	}
}
