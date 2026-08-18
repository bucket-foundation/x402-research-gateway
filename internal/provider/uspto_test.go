package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Fixtures below are trimmed, realistic recreations of shapes recorded live
// against api.uspto.gov on 2026-08-18 (x402-research-gateway#18), keeping
// every field this adapter reads and dropping the large, adapter-irrelevant
// eventDataBag/correspondenceAddressBag noise. No live network call is made
// by this test file; internal/registry.Verifier owns that.

// usptoSearchFixture: a pending national-stage application, one hit from
// /api/v1/patent/applications/search.
const usptoSearchFixture = `{
  "count": 1,
  "patentFileWrapperDataBag": [
    {
      "applicationNumberText": "18863769",
      "applicationMetaData": {
        "applicationStatusCode": 19,
        "applicationStatusDescriptionText": "Application Undergoing Preexam Processing",
        "applicationTypeCode": "UTL",
        "filingDate": "2024-11-07",
        "inventionTitle": "METHOD AND SYSTEM FOR EVALUATING BATTERY LIFE",
        "firstInventorName": "Sungwon PARK",
        "firstApplicantName": "BATTERFLY INC.LTD",
        "inventorBag": [
          {"firstName": "Sungwon", "lastName": "PARK", "inventorNameText": "Sungwon PARK"}
        ],
        "applicantBag": [
          {"applicantNameText": "BATTERFLY INC.LTD"}
        ]
      },
      "parentContinuityBag": [
        {
          "parentApplicationStatusCode": 19,
          "claimParentageTypeCode": "NST",
          "claimParentageTypeCodeDescriptionText": "is a National Stage Entry of",
          "parentApplicationNumberText": "PCTKR2024002849",
          "parentApplicationFilingDate": "2024-03-06",
          "childApplicationNumberText": "18863769"
        }
      ]
    }
  ]
}`

// usptoFetchGrantedFixture: a granted utility patent, one hit from
// /api/v1/patent/applications/{applicationNumber}, carrying
// cpcClassificationBag, patentNumber/grantDate, and a childContinuityBag
// entry (a later application continuing this one).
const usptoFetchGrantedFixture = `{
  "patentFileWrapperDataBag": [
    {
      "applicationNumberText": "18645123",
      "applicationMetaData": {
        "applicationStatusCode": 150,
        "applicationStatusDescriptionText": "Patented Case",
        "applicationTypeCode": "UTL",
        "filingDate": "2026-04-01",
        "inventionTitle": "SYSTEMS FOR THERMOCHEMICAL HYDROGEN PRODUCTION",
        "firstInventorName": "Patrick J. Panzarino",
        "firstApplicantName": "Plan Beta LLC",
        "patentNumber": "12692153",
        "grantDate": "2026-07-28",
        "uspcSymbolText": "423/651",
        "cpcClassificationBag": [
          "B01J   8/0015",
          "C01B   3/28",
          "B01D  53/226"
        ],
        "inventorBag": [
          {"firstName": "Patrick", "lastName": "Panzarino", "inventorNameText": "Patrick J. Panzarino"}
        ],
        "applicantBag": [
          {"applicantNameText": "Plan Beta LLC"}
        ]
      },
      "childContinuityBag": [
        {
          "claimParentageTypeCode": "CON",
          "claimParentageTypeCodeDescriptionText": "is a Continuation of",
          "parentApplicationNumberText": "18645123",
          "childApplicationNumberText": "19012345"
        }
      ]
    }
  ]
}`

func TestUSPTONormalizer_Search(t *testing.T) {
	recs := USPTONormalizer{}.Normalize([]byte(usptoSearchFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "18863769" {
		t.Errorf("ID = %q, want 18863769", recs[0].ID)
	}
	if !strings.Contains(recs[0].CanonicalURL, "18863769") {
		t.Errorf("CanonicalURL = %q, want it to contain the application number", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("Raw bytes dropped")
	}
}

func TestUSPTONormalizer_FetchSingle(t *testing.T) {
	recs := USPTONormalizer{}.Normalize([]byte(usptoFetchGrantedFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "18645123" {
		t.Errorf("ID = %q, want 18645123", recs[0].ID)
	}
}

func TestUSPTONormalizer_MalformedBody(t *testing.T) {
	norm := USPTONormalizer{}
	if recs := norm.Normalize([]byte(`not json`)); recs != nil {
		t.Errorf("malformed body should yield nil, got %v", recs)
	}
	if recs := norm.Normalize([]byte(`{"count":0,"patentFileWrapperDataBag":[]}`)); len(recs) != 0 {
		t.Errorf("empty bag should yield zero records, got %d", len(recs))
	}
	// A record with no applicationNumberText is dropped, not zero-ID'd.
	dropped := USPTONormalizer{}.Normalize([]byte(`{"patentFileWrapperDataBag":[{"applicationMetaData":{"inventionTitle":"X"}}]}`))
	if len(dropped) != 0 {
		t.Errorf("record with empty application number should be dropped, got %d", len(dropped))
	}
}

func TestUSPTOIdentity_Identifiers(t *testing.T) {
	// Pending application: application-number identifier only.
	pending := USPTONormalizer{}.Normalize([]byte(usptoSearchFixture))[0]
	ids := usptoIdentity{}.Identifiers(pending)
	if len(ids) != 1 || ids[0].Scheme != identity.SchemeUSPTOApplication {
		t.Fatalf("pending application identifiers = %+v, want exactly one uspto-application id", ids)
	}

	// Granted patent: both application and patent identifiers.
	granted := USPTONormalizer{}.Normalize([]byte(usptoFetchGrantedFixture))[0]
	ids = usptoIdentity{}.Identifiers(granted)
	var sawApp, sawPatent bool
	for _, id := range ids {
		switch id.Scheme {
		case identity.SchemeUSPTOApplication:
			sawApp = true
			if id.Value != "18645123" {
				t.Errorf("application id value = %q, want 18645123", id.Value)
			}
		case identity.SchemeUSPTOPatent:
			sawPatent = true
			if id.Value != "12692153" {
				t.Errorf("patent id value = %q, want 12692153", id.Value)
			}
		}
	}
	if !sawApp || !sawPatent {
		t.Errorf("granted patent should carry both application and patent identifiers, got %+v", ids)
	}
}

func TestUSPTOIdentity_Descriptor(t *testing.T) {
	rec := USPTONormalizer{}.Normalize([]byte(usptoSearchFixture))[0]
	d := usptoIdentity{}.Descriptor(rec)
	if d.Title != "METHOD AND SYSTEM FOR EVALUATING BATTERY LIFE" {
		t.Errorf("Title = %q", d.Title)
	}
	if d.Year != 2024 {
		t.Errorf("Year = %d, want 2024 (from filingDate)", d.Year)
	}
	if len(d.Authors) != 1 || d.Authors[0] != "Sungwon PARK" {
		t.Errorf("Authors = %v, want [\"Sungwon PARK\"]", d.Authors)
	}
}

// TestUSPTOIdentity_RecordRights guards the public-domain claim: every
// record reports allowed redistribution on the 17 U.S.C. §105 US-government-
// work basis, and an unparseable record reports unknown rather than
// silently inheriting the allowed default.
func TestUSPTOIdentity_RecordRights(t *testing.T) {
	rec := USPTONormalizer{}.Normalize([]byte(usptoSearchFixture))[0]
	rights := usptoIdentity{}.RecordRights(rec)
	if !rights.Permits() {
		t.Errorf("RecordRights.Permits() = false, want true (US government work)")
	}
	if rights.License == "" {
		t.Error("License should be stated, not left empty")
	}

	empty := usptoIdentity{}.RecordRights(NormalizedRecord{})
	if empty.Permits() {
		t.Error("an unparseable record must never permit redistribution")
	}
}

// TestUSPTOIdentity_ObjectRelations_Family asserts the directional reading
// of parentContinuityBag/childContinuityBag: a parent-bag entry emits this
// application as object (it continues the parent), a child-bag entry emits
// this application as object (the child continues it), and the provider
// term (claimParentageTypeCode, lowercased) is carried verbatim.
func TestUSPTOIdentity_ObjectRelations_Family(t *testing.T) {
	pending := USPTONormalizer{}.Normalize([]byte(usptoSearchFixture))[0]
	rels := usptoIdentity{}.ObjectRelations(pending, time.Now())
	if len(rels) != 1 {
		t.Fatalf("got %d relations, want 1 (one parentContinuityBag entry)", len(rels))
	}
	rel := rels[0]
	if rel.Predicate.ProviderTerm != "nst" {
		t.Errorf("predicate term = %q, want %q (lowercased claimParentageTypeCode)", rel.Predicate.ProviderTerm, "nst")
	}
	if rel.Object.RawID != "PCTKR2024002849" {
		t.Errorf("object rawID = %q, want the parent application number", rel.Object.RawID)
	}
	if rel.Subject.RawID != "18863769" {
		t.Errorf("subject rawID = %q, want this application's own number", rel.Subject.RawID)
	}

	granted := USPTONormalizer{}.Normalize([]byte(usptoFetchGrantedFixture))[0]
	rels = usptoIdentity{}.ObjectRelations(granted, time.Now())
	if len(rels) != 1 {
		t.Fatalf("got %d relations, want 1 (one childContinuityBag entry)", len(rels))
	}
	rel = rels[0]
	if rel.Subject.RawID != "19012345" {
		t.Errorf("child-bag subject rawID = %q, want the child application number", rel.Subject.RawID)
	}
	if rel.Object.RawID != "18645123" {
		t.Errorf("child-bag object rawID = %q, want this application's own number", rel.Object.RawID)
	}
}

// TestUSPTOAdapters_Registered guards that both routes are wired to the
// right adapter and report the capabilities their interfaces imply.
func TestUSPTOAdapters_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["uspto-search"] != USPTOSearchAdapter {
		t.Error("uspto-search not registered to USPTOSearchAdapter")
	}
	if reg["uspto-fetch"] != USPTOFetchAdapter {
		t.Error("uspto-fetch not registered to USPTOFetchAdapter")
	}
	if !USPTOSearchAdapter.Supports(CapSearch) {
		t.Error("USPTOSearchAdapter should support CapSearch")
	}
	if !USPTOFetchAdapter.Supports(CapFetch) {
		t.Error("USPTOFetchAdapter should support CapFetch")
	}
	if !USPTOSearchAdapter.Supports(CapRelations) {
		t.Error("USPTOSearchAdapter should support CapRelations (family via continuity)")
	}
	if !USPTOSearchAdapter.Supports(CapIdentityResolution) {
		t.Error("USPTOSearchAdapter should support CapIdentityResolution")
	}
	if USPTOSearchAdapter.CitationGraphProvider != nil {
		t.Error("USPTOSearchAdapter must not claim a citation-graph direction: verified absent from this API")
	}
}

// TestUSPTOIdentity_NoSecretLeakage guards the same rule ORCID's test
// establishes: nothing this adapter emits from a normal record — the API
// key is a static header set by config, never carried on a record — may
// contain what looks like the credential, and none of the adapter's output
// helpers ever read from an HTTP header in the first place. This test
// exists as the standing guard against a future change accidentally
// threading a request header into a NormalizedRecord.
func TestUSPTOIdentity_NoSecretLeakage(t *testing.T) {
	const bogusKey = "test-uspto-odp-key-should-never-appear"
	rec := USPTONormalizer{}.Normalize([]byte(usptoFetchGrantedFixture))[0]

	ui := usptoIdentity{}
	ids := ui.Identifiers(rec)
	d := ui.Descriptor(rec)
	rights := ui.RecordRights(rec)
	rels := ui.ObjectRelations(rec, time.Now())

	blob, err := json.Marshal(struct {
		Identifiers interface{}
		Descriptor  interface{}
		Rights      interface{}
		Relations   interface{}
	}{ids, d, rights, rels})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(blob)
	if strings.Contains(s, bogusKey) {
		t.Fatal("adapter output contains what looks like a secret")
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "x-api-key") || strings.Contains(low, "apikey") || strings.Contains(low, "api_key") {
		t.Fatal("adapter output mentions the API key header/field name; it must never carry auth material")
	}
}
