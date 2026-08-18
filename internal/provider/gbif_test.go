package provider

import "testing"

// gbifSearchFixture and gbifNoLicenseFixture are trimmed from live
// api.gbif.org responses verified 2026-08-18 (see gbif.go's doc comment).
const gbifSearchFixture = `{"offset":0,"limit":1,"endOfRecords":false,"count":29392,"results":[{"key":5938145577,"license":"http://creativecommons.org/licenses/by-nc/4.0/legalcode","scientificName":"Puma concolor (Linnaeus, 1771)","year":2024}]}`

const gbifCC0Fixture = `{"key":1234567,"license":"http://creativecommons.org/publicdomain/zero/1.0/legalcode","scientificName":"Panthera leo","year":2020}`

const gbifNoLicenseFixture = `{"key":7654321,"scientificName":"Unclassified sp.","year":2019}`

func TestGBIFNormalizer_SearchShape(t *testing.T) {
	recs := GBIFNormalizer{}.Normalize([]byte(gbifSearchFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "5938145577" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://www.gbif.org/occurrence/5938145577" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestGBIFNormalizer_SingleRecordShape(t *testing.T) {
	recs := GBIFNormalizer{}.Normalize([]byte(gbifCC0Fixture))
	if len(recs) != 1 || recs[0].ID != "1234567" {
		t.Fatalf("single-record shape not handled: %+v", recs)
	}
}

func TestGBIFNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"results":[]}`)} {
		if recs := (GBIFNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestGBIFIdentity_RecordRights_CC0Allowed(t *testing.T) {
	recs := GBIFNormalizer{}.Normalize([]byte(gbifCC0Fixture))
	rights := gbifIdentity{}.RecordRights(recs[0])
	if !rights.Permits() {
		t.Errorf("CC0 occurrence should permit redistribution, got %+v", rights)
	}
}

func TestGBIFIdentity_RecordRights_CCBYNCNotAllowed(t *testing.T) {
	recs := GBIFNormalizer{}.Normalize([]byte(gbifSearchFixture))
	rights := gbifIdentity{}.RecordRights(recs[0])
	if rights.Permits() {
		t.Errorf("CC-BY-NC occurrence must not report unconditional redistribution, got %+v", rights)
	}
	if rights.License == "" {
		t.Error("license string should still be recorded even when not allowed")
	}
}

func TestGBIFIdentity_RecordRights_AbsentLicenseUnknown(t *testing.T) {
	recs := GBIFNormalizer{}.Normalize([]byte(gbifNoLicenseFixture))
	rights := gbifIdentity{}.RecordRights(recs[0])
	if rights.Redistribution != RedistributionUnknown {
		t.Errorf("absent license should report unknown, got %q", rights.Redistribution)
	}
}

func TestGBIFAdapters_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["gbif-occurrence-search"] != GBIFOccurrenceSearchAdapter {
		t.Error("gbif-occurrence-search not wired")
	}
	if reg["gbif-occurrence-fetch"] != GBIFOccurrenceFetchAdapter {
		t.Error("gbif-occurrence-fetch not wired")
	}
}
