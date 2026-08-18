package provider

import "testing"

// osfSearchFixture and osfSingleFixture are trimmed from live api.osf.io
// responses verified 2026-08-18 (see osf.go's doc comment).
const osfSearchFixture = `{"data":[{"id":"bqhrn","type":"nodes","attributes":{"title":"Puffin population age structure","description":"Digital version of poster.","public":true},"links":{"html":"https://osf.io/bqhrn/"}}],"links":{"meta":{"total":636187}}}`

const osfSingleFixture = `{"data":{"id":"bqhrn","type":"nodes","attributes":{"title":"Puffin population age structure","description":"Digital version of poster.","public":true},"links":{"html":"https://osf.io/bqhrn/"}}}`

func TestOSFNormalizer_SearchShape(t *testing.T) {
	recs := OSFNormalizer{}.Normalize([]byte(osfSearchFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "bqhrn" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://osf.io/bqhrn/" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestOSFNormalizer_SingleRecordShape(t *testing.T) {
	recs := OSFNormalizer{}.Normalize([]byte(osfSingleFixture))
	if len(recs) != 1 || recs[0].ID != "bqhrn" {
		t.Fatalf("single-record shape not handled: %+v", recs)
	}
}

func TestOSFNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"data":[]}`)} {
		if recs := (OSFNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestOSFIdentity_Descriptor(t *testing.T) {
	recs := OSFNormalizer{}.Normalize([]byte(osfSingleFixture))
	d := osfIdentity{}.Descriptor(recs[0])
	if d.Title != "Puffin population age structure" {
		t.Errorf("title = %q", d.Title)
	}
}

func TestOSFIdentity_RecordRights_Unknown(t *testing.T) {
	recs := OSFNormalizer{}.Normalize([]byte(osfSingleFixture))
	rights := osfIdentity{}.RecordRights(recs[0])
	if rights.Redistribution != RedistributionUnknown {
		t.Errorf("OSF per-node license is not dereferenced by this adapter, redistribution should be unknown, got %q", rights.Redistribution)
	}
}

func TestOSFAdapters_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["osf-node-search"] != OSFNodeSearchAdapter {
		t.Error("osf-node-search not wired")
	}
	if reg["osf-node-fetch"] != OSFNodeFetchAdapter {
		t.Error("osf-node-fetch not wired")
	}
}
