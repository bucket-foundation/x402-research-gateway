package provider

import "testing"

// doajSearchFixture is trimmed from a live doaj.org/api/search/articles
// response verified 2026-08-18 (see doaj.go's doc comment).
const doajSearchFixture = `{"total":195623,"page":1,"pageSize":2,"results":[
  {"id":"000122f776cb4f27b0f575971a4bed38","bibjson":{"title":"A feature selection and scoring scheme for dimensionality reduction in a machine learning task","year":"2025","identifier":[{"id":"10.46481/jnsps.2025.2273","type":"doi"},{"id":"2714-2817","type":"pissn"}],"author":[{"name":"PHILEMON UTEN EMMOH"}],"link":[{"content_type":"HTML","type":"fulltext","url":"https://journal.nsps.org.ng/index.php/jnsps/article/view/2273"}]}},
  {"id":"00099999999999999999999999999999","bibjson":{"title":"A record with no DOI","year":"2024","identifier":[{"id":"1234-5678","type":"pissn"}],"author":[]}}
]}`

const doajSingleFixture = `{"id":"000122f776cb4f27b0f575971a4bed38","bibjson":{"title":"A feature selection and scoring scheme","year":"2025","identifier":[{"id":"10.46481/jnsps.2025.2273","type":"doi"}]}}`

func TestDOAJNormalizer_SearchShape(t *testing.T) {
	recs := DOAJNormalizer{}.Normalize([]byte(doajSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "000122f776cb4f27b0f575971a4bed38" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doaj.org/article/000122f776cb4f27b0f575971a4bed38" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}
}

func TestDOAJNormalizer_SingleRecordShape(t *testing.T) {
	recs := DOAJNormalizer{}.Normalize([]byte(doajSingleFixture))
	if len(recs) != 1 || recs[0].ID != "000122f776cb4f27b0f575971a4bed38" {
		t.Fatalf("single-record shape not handled: %+v", recs)
	}
}

func TestDOAJNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (DOAJNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestDOAJIdentity_Identifiers(t *testing.T) {
	recs := DOAJNormalizer{}.Normalize([]byte(doajSearchFixture))
	ids := doajIdentity{}.Identifiers(recs[0])
	var sawDOI bool
	for _, id := range ids {
		if id.Scheme == "doi" && id.Raw == "10.46481/jnsps.2025.2273" {
			sawDOI = true
		}
	}
	if !sawDOI {
		t.Errorf("expected a DOI identifier in %+v", ids)
	}
}

func TestDOAJIdentity_RecordRights_CC0(t *testing.T) {
	recs := DOAJNormalizer{}.Normalize([]byte(doajSingleFixture))
	rights := doajIdentity{}.RecordRights(recs[0])
	if !rights.Permits() {
		t.Errorf("DOAJ metadata rights should permit redistribution (CC0), got %+v", rights)
	}
	if rights.License != "CC0-1.0" {
		t.Errorf("license = %q, want CC0-1.0", rights.License)
	}
}

func TestDOAJIdentity_Assets(t *testing.T) {
	recs := DOAJNormalizer{}.Normalize([]byte(doajSearchFixture))
	assets := doajIdentity{}.Assets(recs[0])
	if len(assets) != 1 {
		t.Fatalf("got %d assets, want 1", len(assets))
	}
	if assets[0].CanonicalURL != "https://journal.nsps.org.ng/index.php/jnsps/article/view/2273" {
		t.Errorf("asset url = %q", assets[0].CanonicalURL)
	}
	// A DOAJ-vetted link is free to read but redistribution is never
	// inferred from journal-level openness alone.
	if assets[0].Rights.Redistribution != RedistributionUnknown {
		t.Errorf("asset redistribution = %q, want unknown", assets[0].Rights.Redistribution)
	}
}
