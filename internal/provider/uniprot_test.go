package provider

import "testing"

// uniprotSearchFixture and uniprotSingleFixture are trimmed from live
// rest.uniprot.org responses verified 2026-08-18 (see uniprot.go's doc
// comment).
const uniprotSearchFixture = `{"results": [{"primaryAccession": "P01308", "uniProtkbId": "INS_HUMAN", "proteinDescription": {"recommendedName": {"fullName": {"value": "Insulin"}}}, "organism": {"scientificName": "Homo sapiens"}}]}`

const uniprotSingleFixture = `{"primaryAccession": "P01308", "uniProtkbId": "INS_HUMAN", "proteinDescription": {"recommendedName": {"fullName": {"value": "Insulin"}}}, "organism": {"scientificName": "Homo sapiens"}}`

func TestUniProtNormalizer_SearchShape(t *testing.T) {
	recs := UniProtNormalizer{}.Normalize([]byte(uniprotSearchFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "P01308" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://www.uniprot.org/uniprotkb/P01308/entry" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestUniProtNormalizer_SingleRecordShape(t *testing.T) {
	recs := UniProtNormalizer{}.Normalize([]byte(uniprotSingleFixture))
	if len(recs) != 1 || recs[0].ID != "P01308" {
		t.Fatalf("single-record shape not handled: %+v", recs)
	}
}

func TestUniProtNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"results":[]}`)} {
		if recs := (UniProtNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestUniProtIdentity_Descriptor(t *testing.T) {
	recs := UniProtNormalizer{}.Normalize([]byte(uniprotSingleFixture))
	d := uniprotIdentity{}.Descriptor(recs[0])
	if d.Title != "Insulin" {
		t.Errorf("title = %q, want Insulin", d.Title)
	}
}

func TestUniProtIdentity_RecordRights(t *testing.T) {
	recs := UniProtNormalizer{}.Normalize([]byte(uniprotSingleFixture))
	rights := uniprotIdentity{}.RecordRights(recs[0])
	if !rights.Permits() {
		t.Errorf("uniprot rights should permit redistribution (CC BY 4.0), got %+v", rights)
	}
}

func TestUniProtAdapters_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["uniprot-search"] != UniProtSearchAdapter {
		t.Error("uniprot-search not wired")
	}
	if reg["uniprot-fetch"] != UniProtFetchAdapter {
		t.Error("uniprot-fetch not wired")
	}
}
