package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A single-DOI response, trimmed from a live 2026-08-17 verification query
// against https://api.unpaywall.org/v2/10.1038/nature12373. The email
// parameter used for that request never appears in the response body,
// which is the fact the leakage test below asserts.
const unpaywallOAFixture = `{
 "doi":"10.1038/nature12373","doi_url":"https://doi.org/10.1038/nature12373",
 "title":"Nanometre-scale thermometry in a living cell",
 "genre":"journal-article","is_oa":true,"oa_status":"bronze","year":2013,
 "journal_name":"Nature","publisher":"Springer Science and Business Media LLC",
 "best_oa_location":{"host_type":"publisher","version":"publishedVersion","license":null,
   "url":"https://www.nature.com/articles/nature12373.pdf","url_for_pdf":"https://www.nature.com/articles/nature12373.pdf",
   "url_for_landing_page":"https://doi.org/10.1038/nature12373","is_best":true},
 "oa_locations":[
   {"host_type":"publisher","version":"publishedVersion","license":null,
    "url":"https://www.nature.com/articles/nature12373.pdf","url_for_pdf":"https://www.nature.com/articles/nature12373.pdf",
    "url_for_landing_page":"https://doi.org/10.1038/nature12373","is_best":true},
   {"host_type":"repository","version":"submittedVersion","license":"cc-by",
    "url":"http://nrs.harvard.edu/urn-3:HUL.InstRepos:12285462","url_for_pdf":null,
    "url_for_landing_page":"http://nrs.harvard.edu/urn-3:HUL.InstRepos:12285462",
    "repository_institution":"Harvard University","is_best":false}
 ],
 "z_authors":[{"given":"G.","family":"Kucsko"},{"given":"P. C.","family":"Maurer"}]
}`

// A DOI with no open-access copy: is_oa false, no locations.
const unpaywallClosedFixture = `{
 "doi":"10.1234/closed","doi_url":"https://doi.org/10.1234/closed",
 "title":"A closed-access paper","is_oa":false,"oa_status":"closed","year":2020,
 "oa_locations":[]
}`

func TestUnpaywallNormalizer_ParsesSingleDOI(t *testing.T) {
	recs := UnpaywallNormalizer{}.Normalize([]byte(unpaywallOAFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1 (Unpaywall answers one DOI per request)", len(recs))
	}
	if recs[0].ID != "10.1038/nature12373" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doi.org/10.1038/nature12373" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}
}

func TestUnpaywallNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"doi":""}`)} {
		if recs := (UnpaywallNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestUnpaywallIdentity_IdentifiersAndDescriptor(t *testing.T) {
	rec := UnpaywallNormalizer{}.Normalize([]byte(unpaywallOAFixture))[0]

	ids := unpaywallIdentity{}.Identifiers(rec)
	if len(ids) != 1 || ids[0].Scheme != identity.SchemeDOI || ids[0].Value != "10.1038/nature12373" {
		t.Errorf("identifiers = %+v", ids)
	}

	d := unpaywallIdentity{}.Descriptor(rec)
	if d.Title != "Nanometre-scale thermometry in a living cell" || d.Year != 2013 {
		t.Errorf("descriptor = %+v", d)
	}
	if len(d.Authors) != 2 || d.Authors[0] != "G. Kucsko" {
		t.Errorf("authors = %v", d.Authors)
	}
}

func TestUnpaywallIdentity_NoRelations(t *testing.T) {
	rec := UnpaywallNormalizer{}.Normalize([]byte(unpaywallOAFixture))[0]
	if rels := (unpaywallIdentity{}).AssertedRelations("n", rec, time.Now()); len(rels) != 0 {
		t.Errorf("Unpaywall asserts no relations, got %+v", rels)
	}
}

// Availability distinguishes retrievable, restricted, and absent, so a
// DOI with no OA copy is an explicit negative answer rather than an empty
// result indistinguishable from "not checked."
func TestUnpaywallIdentity_Availability(t *testing.T) {
	oa := UnpaywallNormalizer{}.Normalize([]byte(unpaywallOAFixture))[0]
	if got := (unpaywallIdentity{}).Availability(oa); got != AvailabilityRetrievable {
		t.Errorf("availability = %q, want retrievable", got)
	}

	closed := UnpaywallNormalizer{}.Normalize([]byte(unpaywallClosedFixture))[0]
	if got := (unpaywallIdentity{}).Availability(closed); got != AvailabilityAbsent {
		t.Errorf("availability = %q, want absent", got)
	}

	inconsistent := UnpaywallNormalizer{}.Normalize([]byte(
		`{"doi":"10.1/x","is_oa":true,"oa_locations":[]}`))[0]
	if got := (unpaywallIdentity{}).Availability(inconsistent); got != AvailabilityRestricted {
		t.Errorf("availability = %q, want restricted (is_oa true, no locations)", got)
	}
}

// oa_status is a coverage classification, never a licence: RecordRights
// must always report unknown at the record level, even for a bronze
// article Unpaywall marked open access.
func TestUnpaywallIdentity_RecordRightsIsAlwaysUnknown(t *testing.T) {
	oa := UnpaywallNormalizer{}.Normalize([]byte(unpaywallOAFixture))[0]
	r := unpaywallIdentity{}.RecordRights(oa)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("record-level rights = %q, want unknown regardless of oa_status", r.Redistribution)
	}
	if r.Permits() {
		t.Error("record-level rights must never permit redistribution")
	}
}

// Per-location rights: a location with a declared open licence permits
// redistribution, and a location with no license value reports unknown
// even when it is the best OA location.
func TestUnpaywallIdentity_Assets(t *testing.T) {
	rec := UnpaywallNormalizer{}.Normalize([]byte(unpaywallOAFixture))[0]
	assets := unpaywallIdentity{}.Assets(rec)
	if len(assets) != 2 {
		t.Fatalf("got %d assets, want 2 (one per oa_locations entry)", len(assets))
	}

	best := assets[0]
	if best.CanonicalURL != "https://www.nature.com/articles/nature12373.pdf" {
		t.Errorf("best location url = %q", best.CanonicalURL)
	}
	if best.Rights.Redistribution != RedistributionUnknown {
		t.Errorf("best-oa-location has no license field, must report unknown, got %q", best.Rights.Redistribution)
	}
	if !best.Rights.FreeToRead {
		t.Error("an open-access location is free to read even with an unknown licence")
	}
	if !strings.Contains(best.Representation, "role=best-oa-location") {
		t.Errorf("representation = %q, missing best-oa-location role", best.Representation)
	}

	ccBy := assets[1]
	if ccBy.Rights.Redistribution != RedistributionAllowed {
		t.Errorf("cc-by location must permit redistribution, got %+v", ccBy.Rights)
	}
	if !strings.Contains(ccBy.Representation, "host=repository") {
		t.Errorf("representation = %q, missing host type", ccBy.Representation)
	}

	// A DOI with no OA locations yields no assets, distinct from a fetch
	// that failed to parse.
	closed := UnpaywallNormalizer{}.Normalize([]byte(unpaywallClosedFixture))[0]
	if got := (unpaywallIdentity{}).Assets(closed); len(got) != 0 {
		t.Errorf("a closed-access DOI must yield no assets, got %+v", got)
	}
}

func TestUnpaywallAdapter_CapabilitiesAndSync(t *testing.T) {
	if schemes := UnpaywallFetchAdapter.Fetcher.IdentifierSchemes(); len(schemes) != 1 || schemes[0] != "doi" {
		t.Errorf("fetch identifier schemes = %v", schemes)
	}
	sc := UnpaywallFetchAdapter.SyncProvider.SyncCapability()
	if sc.Bulk || sc.Incremental {
		t.Errorf("Unpaywall's free API has neither bulk nor incremental sync, got %+v", sc)
	}
	if !UnpaywallFetchAdapter.Supports(CapFetch) || !UnpaywallFetchAdapter.Supports(CapAssets) {
		t.Error("unpaywall-fetch should support fetch and assets")
	}
	if UnpaywallFetchAdapter.Supports(CapSearch) {
		t.Error("Unpaywall has no search endpoint and must not claim search")
	}
}

// The caller-identifying email query parameter is set at the route-config
// level, never read or stored by this adapter, so it can never appear in
// a record, an asset, or a relation this file emits.
func TestUnpaywallIdentity_NoEmailLeakage(t *testing.T) {
	rec := UnpaywallNormalizer{}.Normalize([]byte(unpaywallOAFixture))[0]
	assets := unpaywallIdentity{}.Assets(rec)
	d := unpaywallIdentity{}.Descriptor(rec)
	ids := unpaywallIdentity{}.Identifiers(rec)

	for _, a := range assets {
		if strings.Contains(strings.ToLower(a.AssetID+a.CanonicalURL+a.Representation), "@") {
			t.Errorf("asset carries what looks like an email address: %+v", a)
		}
	}
	if strings.Contains(strings.ToLower(d.Title), "@") {
		t.Errorf("descriptor carries what looks like an email address: %+v", d)
	}
	for _, id := range ids {
		if strings.Contains(id.Raw, "@") || strings.Contains(id.Value, "@") {
			t.Errorf("identifier carries what looks like an email address: %+v", id)
		}
	}
}

func TestUnpaywallIdentity_MalformedRecord(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (unpaywallIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if assets := (unpaywallIdentity{}).Assets(bad); len(assets) != 0 {
		t.Errorf("invented assets: %+v", assets)
	}
	r := (unpaywallIdentity{}).RecordRights(bad)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("unparseable record must report unknown rights, got %q", r.Redistribution)
	}
	if got := (unpaywallIdentity{}).Availability(bad); got != AvailabilityAbsent {
		t.Errorf("unparseable record availability = %q, want absent", got)
	}
}
