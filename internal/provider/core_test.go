package provider

import (
	"strings"
	"testing"
)

// coreSearchFixture is a CORE v3 /search/works response: one record with a
// CORE-hosted copy, a repository copy, and a CC-BY licence, and one record
// CORE holds with no licence field at all.
const coreSearchFixture = `{
  "totalHits": 2,
  "results": [
    {"id": 12345678, "doi": "10.7717/peerj.4375", "title": "The state of OA",
     "yearPublished": 2018, "license": "https://creativecommons.org/licenses/by/4.0/",
     "authors": [{"name": "Heather Piwowar"}],
     "downloadUrl": "https://core.ac.uk/download/pdf/12345678.pdf",
     "sourceFulltextUrls": ["https://repo.example.edu/bitstream/1/oa.pdf"]},
    {"id": "87654321", "doi": "10.1000/no-licence", "title": "Unlicensed deposit",
     "yearPublished": 2020,
     "downloadUrl": "https://core.ac.uk/download/pdf/87654321.pdf"}
  ]
}`

func coreRecords(t *testing.T) []NormalizedRecord {
	t.Helper()
	recs := CORENormalizer{}.Normalize([]byte(coreSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	return recs
}

func TestCORENormalizer_HandlesNumericAndStringIDs(t *testing.T) {
	recs := coreRecords(t)
	if recs[0].ID != "12345678" || recs[1].ID != "87654321" {
		t.Fatalf("ids = %q %q", recs[0].ID, recs[1].ID)
	}
	if recs[0].CanonicalURL != "https://core.ac.uk/works/12345678" {
		t.Fatalf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestCOREAssets_PerAssetRights(t *testing.T) {
	recs := coreRecords(t)
	assets := (coreIdentity{}).Assets(recs[0])
	if len(assets) != 2 {
		t.Fatalf("want 2 assets, got %d", len(assets))
	}
	for _, a := range assets {
		if !a.Rights.Permits() {
			t.Fatalf("%s: a CC-BY record was not reported as redistributable", a.AssetID)
		}
		if !a.Rights.FreeToRead {
			t.Fatalf("%s: not marked free to read", a.AssetID)
		}
	}
	if !strings.Contains(assets[0].Representation, "host=core") {
		t.Fatalf("representation = %q", assets[0].Representation)
	}
	if !strings.Contains(assets[1].Representation, "host=repository") {
		t.Fatalf("representation = %q", assets[1].Representation)
	}
}

func TestCORERights_AbsentLicenceIsNotPermission(t *testing.T) {
	recs := coreRecords(t)
	rights := (coreIdentity{}).RecordRights(recs[1])
	if rights.Permits() {
		t.Fatal("a record with no licence field was reported as redistributable")
	}
	if rights.Redistribution != RedistributionUnknown {
		t.Fatalf("redistribution = %q", rights.Redistribution)
	}
	// CORE holding a downloadable copy means readable, which is a
	// different fact and must survive.
	if !rights.FreeToRead {
		t.Fatal("a CORE-hosted copy was not reported as free to read")
	}
	for _, a := range (coreIdentity{}).Assets(recs[1]) {
		if a.Rights.Permits() {
			t.Fatalf("%s: unknown rights rendered as permitted", a.AssetID)
		}
	}
}

func TestCOREAvailability(t *testing.T) {
	recs := coreRecords(t)
	if got := (coreIdentity{}).Availability(recs[0]); got != AvailabilityRetrievable {
		t.Fatalf("availability = %q", got)
	}
	// A record CORE holds without publishing any location is restricted,
	// which is not the same answer as absent.
	held := NormalizedRecord{ID: "1", Raw: []byte(`{"id":1,"fullText":"text held by CORE"}`)}
	if got := (coreIdentity{}).Availability(held); got != AvailabilityRestricted {
		t.Fatalf("held-without-location availability = %q", got)
	}
	none := NormalizedRecord{ID: "1", Raw: []byte(`{"id":1}`)}
	if got := (coreIdentity{}).Availability(none); got != AvailabilityAbsent {
		t.Fatalf("no-copy availability = %q", got)
	}
}

func TestCORENeverReservesFullText(t *testing.T) {
	// A CORE response can carry the full text inline. No asset may carry
	// that text: an asset is a location.
	rec := NormalizedRecord{ID: "1", Raw: []byte(
		`{"id":1,"fullText":"COPYRIGHTED BODY TEXT","downloadUrl":"https://core.ac.uk/download/pdf/1.pdf"}`)}
	for _, a := range (coreIdentity{}).Assets(rec) {
		if strings.Contains(a.AssetID+a.Representation+a.CanonicalURL, "COPYRIGHTED BODY TEXT") {
			t.Fatal("full text leaked into an asset")
		}
	}
}

func TestCOREAdapters_CapabilitiesAndNoEmbeddedKey(t *testing.T) {
	for _, a := range []*Adapter{CORESearchAdapter, COREFetchAdapter} {
		if !a.Supports(CapAssets) {
			t.Fatalf("%s does not report the assets capability", a.ID)
		}
		if a.Normalizer == nil || a.AssetProvider == nil || a.RecordRightsProvider == nil {
			t.Fatalf("%s is missing an asset-discovery seam", a.ID)
		}
	}
	if _, ok := DefaultRegistry()["core-search"]; !ok {
		t.Fatal("core-search is not in the default registry")
	}
}
