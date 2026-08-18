package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// orcidRecordFixture is a trimmed GET /v3.0/{id}/record response shaped
// from ORCID's own published documentation examples, using ORCID's
// standard demo iD 0000-0002-1825-0097 (Josiah Carberry) — safe as a test
// fixture since ORCID publishes it as its own demo record.
const orcidRecordFixture = `{
 "orcid-identifier":{"uri":"https://orcid.org/0000-0002-1825-0097","path":"0000-0002-1825-0097","host":"orcid.org"},
 "person":{
   "name":{"given-names":{"value":"Josiah"},"family-name":{"value":"Carberry"},"credit-name":null},
   "external-identifiers":{"external-identifier":[
     {"external-id-type":"Scopus Author ID","external-id-value":"7007156898",
      "external-id-url":{"value":"https://www.scopus.com/authid/detail.uri?authorId=7007156898"},
      "external-id-relationship":"self"},
     {"external-id-type":"ResearcherID","external-id-value":"A-1234-5678",
      "external-id-url":{"value":"https://www.researcherid.com/rid/A-1234-5678"},
      "external-id-relationship":"self"}
   ]}
 },
 "activities-summary":{
   "employments":{"affiliation-group":[
     {"summaries":[{"employment-summary":{"organization":{"name":"Wesleyan University",
       "address":{"city":"Middletown","country":"US"},
       "disambiguated-organization":{"disambiguated-organization-identifier":"grid.268117.b","disambiguation-source":"GRID"}}}}]}
   ]},
   "works":{"group":[
     {"work-summary":[{"title":{"title":{"value":"Toward a Unified Theory of High-Energy Metaphysics"}},
       "type":"journal-article",
       "publication-date":{"year":{"value":"2008"}},
       "external-ids":{"external-id":[{"external-id-type":"doi","external-id-value":"10.5555/12345678","external-id-relationship":"self"}]},
       "put-code":12345}],
      "external-ids":{"external-id":[{"external-id-type":"doi","external-id-value":"10.5555/12345678","external-id-relationship":"self"}]}}
   ]}
 }
}`

const orcidSearchFixture = `{
 "expanded-result":[
   {"orcid-id":"0000-0002-1825-0097","given-names":"Josiah","family-names":"Carberry",
    "credit-name":null,"other-name":null,"institution-name":["Wesleyan University"]}
 ],
 "num-found":1
}`

func TestORCIDRecordNormalizer_ParsesRecord(t *testing.T) {
	recs := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "0000-0002-1825-0097" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://orcid.org/0000-0002-1825-0097" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}
}

func TestORCIDRecordNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"orcid-identifier":{"path":""}}`)} {
		if recs := (ORCIDRecordNormalizer{}).Normalize(body); recs != nil {
			t.Errorf("Normalize(%q) = %+v, want nil", body, recs)
		}
	}
}

func TestORCIDSearchNormalizer_ParsesResults(t *testing.T) {
	recs := ORCIDSearchNormalizer{}.Normalize([]byte(orcidSearchFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "0000-0002-1825-0097" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://orcid.org/0000-0002-1825-0097" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestORCIDSearchNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"expanded-result":[]}`)} {
		if recs := (ORCIDSearchNormalizer{}).Normalize(body); recs != nil {
			t.Errorf("Normalize(%q) = %+v, want nil", body, recs)
		}
	}
}

func TestORCIDIdentity_Identifiers(t *testing.T) {
	rec := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))[0]
	ids := orcidIdentity{}.Identifiers(rec)
	if len(ids) != 1 {
		t.Fatalf("got %d identifiers, want 1", len(ids))
	}
	if ids[0].Scheme != identity.SchemeORCID || ids[0].Value != "0000-0002-1825-0097" {
		t.Errorf("identifier = %+v", ids[0])
	}
}

func TestORCIDIdentity_SearchRecordIdentifiers(t *testing.T) {
	rec := ORCIDSearchNormalizer{}.Normalize([]byte(orcidSearchFixture))[0]
	ids := orcidIdentity{}.Identifiers(rec)
	if len(ids) != 1 || ids[0].Value != "0000-0002-1825-0097" {
		t.Errorf("identifiers from a search-result record = %+v", ids)
	}
}

func TestORCIDIdentity_ExternalIdentifiers(t *testing.T) {
	rec := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))[0]
	ext := orcidIdentity{}.ExternalIdentifiers(rec)
	if len(ext) != 2 {
		t.Fatalf("got %d external identifiers, want 2", len(ext))
	}
	if ext[0].Type != "Scopus Author ID" || ext[0].Value != "7007156898" {
		t.Errorf("external identifier[0] = %+v", ext[0])
	}
	if ext[1].Type != "ResearcherID" {
		t.Errorf("external identifier[1] = %+v", ext[1])
	}
}

func TestORCIDIdentity_Works(t *testing.T) {
	rec := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))[0]
	works := orcidIdentity{}.Works(rec)
	if len(works) != 1 {
		t.Fatalf("got %d works, want 1", len(works))
	}
	if works[0].DOI != "10.5555/12345678" {
		t.Errorf("work DOI = %q", works[0].DOI)
	}
	if works[0].Year != "2008" {
		t.Errorf("work year = %q", works[0].Year)
	}
	if !strings.Contains(works[0].Title, "Metaphysics") {
		t.Errorf("work title = %q", works[0].Title)
	}
}

func TestORCIDIdentity_Works_SearchRecordHasNone(t *testing.T) {
	rec := ORCIDSearchNormalizer{}.Normalize([]byte(orcidSearchFixture))[0]
	if works := (orcidIdentity{}).Works(rec); works != nil {
		t.Errorf("a search-result record has no activities-summary; got %+v", works)
	}
}

func TestORCIDIdentity_Descriptor(t *testing.T) {
	rec := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))[0]
	d := orcidIdentity{}.Descriptor(rec)
	if len(d.Authors) != 1 || d.Authors[0] != "Josiah Carberry" {
		t.Errorf("descriptor authors = %+v", d.Authors)
	}
}

func TestORCIDIdentity_RecordRights(t *testing.T) {
	rec := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))[0]
	r := orcidIdentity{}.RecordRights(rec)
	if r.Redistribution != RedistributionAllowed {
		t.Errorf("redistribution = %v, want allowed", r.Redistribution)
	}
	if r.License != "CC0-1.0" {
		t.Errorf("license = %q", r.License)
	}
	if !r.FreeToRead {
		t.Error("FreeToRead should be true")
	}
}

func TestORCIDIdentity_RecordRights_Malformed(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: nil}
	r := orcidIdentity{}.RecordRights(bad)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("redistribution on an unparseable record = %v, want unknown", r.Redistribution)
	}
}

func TestORCIDIdentity_AssertedRelationsIsNil(t *testing.T) {
	rec := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))[0]
	if rel := (orcidIdentity{}).AssertedRelations("orcid:0000-0002-1825-0097", rec, time.Now()); rel != nil {
		t.Errorf("AssertedRelations = %+v, want nil (no ROR-resolvable edges asserted)", rel)
	}
}

func TestORCIDAdapters_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["orcid-fetch"] != ORCIDFetchAdapter {
		t.Error("orcid-fetch not registered to ORCIDFetchAdapter")
	}
	if reg["orcid-search"] != ORCIDSearchAdapter {
		t.Error("orcid-search not registered to ORCIDSearchAdapter")
	}
	if !ORCIDFetchAdapter.Supports(CapFetch) {
		t.Error("ORCIDFetchAdapter should support CapFetch")
	}
	if !ORCIDSearchAdapter.Supports(CapSearch) {
		t.Error("ORCIDSearchAdapter should support CapSearch")
	}
}

// TestORCIDIdentity_NoSecretLeakage guards feed402 SPEC §3.4's rule that a
// provenance/citation block never carries a credential: nothing this
// adapter emits (identifiers, descriptor, rights, external identifiers,
// works) may contain the bearer token or client secret used to fetch it.
// The fixture never contains one either, so this also catches a future
// change that accidentally threads the request's Authorization header
// into a NormalizedRecord.
func TestORCIDIdentity_NoSecretLeakage(t *testing.T) {
	const bogusSecret = "test-client-secret-should-never-appear"
	rec := ORCIDRecordNormalizer{}.Normalize([]byte(orcidRecordFixture))[0]

	oi := orcidIdentity{}
	ids := oi.Identifiers(rec)
	ext := oi.ExternalIdentifiers(rec)
	works := oi.Works(rec)
	d := oi.Descriptor(rec)
	rights := oi.RecordRights(rec)

	blob, err := json.Marshal(struct {
		Identifiers interface{}
		External    interface{}
		Works       interface{}
		Descriptor  interface{}
		Rights      interface{}
	}{ids, ext, works, d, rights})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), bogusSecret) {
		t.Fatal("adapter output contains what looks like a secret")
	}
	if strings.Contains(strings.ToLower(string(blob)), "bearer") || strings.Contains(strings.ToLower(string(blob)), "authorization") {
		t.Fatal("adapter output mentions a bearer token or Authorization header; it must never carry auth material")
	}
}
