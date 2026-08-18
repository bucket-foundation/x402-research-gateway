package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A single-document record, trimmed from a live 2026-08-17 verification
// query against api.zbmath.org/v1/document/_search (Minsky & Papert,
// "Perceptrons", expanded ed., zbl 0794.68104). The review text is
// shortened from the live response; a link entry with a DOI type was
// added to exercise DOI extraction, since the sampled record carried none.
const zbmathDocFixture = `{"contributors":{"authors":[
 {"codes":["minsky.marvin-l"],"name":"Minsky, Marvin L."},
 {"codes":["papert.seymour-a"],"name":"Papert, Seymour A."}
]},"editorial_contributions":[
 {"language":"English","reviewer":{"sign":"S. P. Smith (Los Angeles, CA)"},
  "text":"This book is a reprint of the classic 1969 treatise on perceptrons.",
  "contribution_type":"review"}
],"id":46318,"identifier":"0794.68104",
"license":[],
"links":[{"type":"doi","url":"https://doi.org/10.7551/mitpress/6270.001.0001"},
         {"type":"publisher","url":"https://mitpress.mit.edu/9780262631112"}],
"msc":[
 {"code":"68Q45","scheme":"msc2020","text":"Formal languages and automata"},
 {"code":"68T10","scheme":"msc2020","text":"Pattern recognition, speech recognition"}
],
"title":{"addition":"expanded ed.","title":"Perceptrons."},
"year":"1988","zbmath_url":"https://zbmath.org/46318"}`

// A search-list response wrapping two documents (the second minimal, no
// review, no msc, no links) to exercise the array-result shape.
const zbmathSearchListFixture = `{"result":[` + zbmathDocFixture + `,
 {"contributors":{"authors":[]},"id":1,"identifier":"0001.00001",
  "license":[],"msc":[],"year":"1950","zbmath_url":"https://zbmath.org/1"}
]}`

// A single-document fetch response: result is one object rather than an array.
const zbmathFetchFixture = `{"result":` + zbmathDocFixture + `}`

func TestZbMATHNormalizer_SearchListShape(t *testing.T) {
	recs := ZbMATHNormalizer{}.Normalize([]byte(zbmathSearchListFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "0794.68104" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://zbmath.org/46318" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}
}

func TestZbMATHNormalizer_SingleFetchShape(t *testing.T) {
	recs := ZbMATHNormalizer{}.Normalize([]byte(zbmathFetchFixture))
	if len(recs) != 1 || recs[0].ID != "0794.68104" {
		t.Fatalf("single-document shape not handled: %+v", recs)
	}
}

func TestZbMATHNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"result":null}`), []byte(`{"result":404}`)} {
		if recs := (ZbMATHNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestZbMATHIdentity_IdentifiersAndDescriptor(t *testing.T) {
	rec := ZbMATHNormalizer{}.Normalize([]byte(zbmathFetchFixture))[0]

	ids := zbmathIdentity{}.Identifiers(rec)
	byScheme := map[identity.Scheme]string{}
	for _, id := range ids {
		byScheme[id.Scheme] = id.Value
	}
	if byScheme[identity.SchemeZbMATH] != "0794.68104" {
		t.Errorf("zbmath id = %q", byScheme[identity.SchemeZbMATH])
	}
	if byScheme[identity.SchemeDOI] != "10.7551/mitpress/6270.001.0001" {
		t.Errorf("doi = %q", byScheme[identity.SchemeDOI])
	}

	d := zbmathIdentity{}.Descriptor(rec)
	if d.Title != "Perceptrons." || d.Year != 1988 {
		t.Errorf("descriptor = %+v", d)
	}
	if len(d.Authors) != 2 || d.Authors[0] != "Minsky, Marvin L." {
		t.Errorf("authors = %v", d.Authors)
	}
}

// A publisher-type link must not be mistaken for a DOI.
func TestZbMATHIdentity_OnlyDOITypeLinksBecomeDOIIdentifiers(t *testing.T) {
	recs := ZbMATHNormalizer{}.Normalize([]byte(zbmathSearchListFixture))
	minimal := recs[1] // no links at all
	ids := zbmathIdentity{}.Identifiers(minimal)
	for _, id := range ids {
		if id.Scheme == identity.SchemeDOI {
			t.Errorf("a record with no links must carry no DOI identifier: %+v", id)
		}
	}
}

// MSC codes carry their edition; the same numeric code means different
// things across editions, so scheme must never be dropped.
func TestZbMATHIdentity_MSCCodesPreserveScheme(t *testing.T) {
	rec := ZbMATHNormalizer{}.Normalize([]byte(zbmathFetchFixture))[0]
	codes := zbmathIdentity{}.MSCCodes(rec)
	if len(codes) != 2 {
		t.Fatalf("got %d MSC codes, want 2", len(codes))
	}
	for _, c := range codes {
		if c.Scheme != "msc2020" {
			t.Errorf("MSC code %q lost its scheme: %+v", c.Code, c)
		}
	}
}

// Review text is a distinct authored work, never covered by any
// bibliographic-metadata rights statement.
func TestZbMATHIdentity_ReviewsAreDistinctFromMetadata(t *testing.T) {
	rec := ZbMATHNormalizer{}.Normalize([]byte(zbmathFetchFixture))[0]
	reviews := zbmathIdentity{}.Reviews(rec)
	if len(reviews) != 1 {
		t.Fatalf("got %d reviews, want 1", len(reviews))
	}
	r := reviews[0]
	if r.ContributionType != "review" || r.ReviewerSign != "S. P. Smith (Los Angeles, CA)" {
		t.Errorf("review = %+v", r)
	}

	// A record with no editorial contributions yields no reviews.
	recs := ZbMATHNormalizer{}.Normalize([]byte(zbmathSearchListFixture))
	if got := (zbmathIdentity{}).Reviews(recs[1]); len(got) != 0 {
		t.Errorf("a record with no editorial contributions must yield no reviews, got %+v", got)
	}

	// Reviews never appear in Assets: a review is prose with its own
	// authorship, standing apart from any locator for the underlying document.
	assets := zbmathIdentity{}.Assets(rec)
	for _, a := range assets {
		if a.CanonicalURL == "" {
			t.Errorf("no asset should reference review text: %+v", a)
		}
	}
}

// The API's license field was empty on every record sampled during
// verification; RecordRights must report unknown rather than assuming
// the prior (unverifiable) CC-BY claim.
func TestZbMATHIdentity_RecordRightsIsUnknown(t *testing.T) {
	rec := ZbMATHNormalizer{}.Normalize([]byte(zbmathFetchFixture))[0]
	r := zbmathIdentity{}.RecordRights(rec)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("rights = %q, want unknown (empty license field, unverified)", r.Redistribution)
	}
	if r.Permits() {
		t.Error("unknown must never permit redistribution")
	}
}

func TestZbMATHIdentity_Assets(t *testing.T) {
	rec := ZbMATHNormalizer{}.Normalize([]byte(zbmathFetchFixture))[0]
	assets := zbmathIdentity{}.Assets(rec)
	if len(assets) != 2 {
		t.Fatalf("got %d assets, want 2 (one per link)", len(assets))
	}
	var haveDOI, havePublisher bool
	for _, a := range assets {
		if a.CanonicalURL == "https://doi.org/10.7551/mitpress/6270.001.0001" {
			haveDOI = true
		}
		if a.CanonicalURL == "https://mitpress.mit.edu/9780262631112" {
			havePublisher = true
		}
		if a.Rights.Redistribution != RedistributionUnknown {
			t.Errorf("asset rights must be unknown (empty license field), got %+v", a.Rights)
		}
	}
	if !haveDOI || !havePublisher {
		t.Errorf("missing an expected asset: doi=%v publisher=%v", haveDOI, havePublisher)
	}
}

func TestZbMATHAdapters_CapabilitiesAndSync(t *testing.T) {
	if ZbMATHSearchAdapter.Searcher.PaginationModel() != "page" {
		t.Errorf("pagination model = %q, want page", ZbMATHSearchAdapter.Searcher.PaginationModel())
	}
	if schemes := ZbMATHFetchAdapter.Fetcher.IdentifierSchemes(); len(schemes) != 1 || schemes[0] != "zbmath" {
		t.Errorf("fetch identifier schemes = %v", schemes)
	}
	sc := ZbMATHSearchAdapter.SyncProvider.SyncCapability()
	if sc.Bulk {
		t.Error("no full-corpus bulk export was found for unauthenticated use; bulk must be false")
	}
	if !sc.Incremental {
		t.Error("the OAI-PMH endpoint answered live during verification; incremental must be true")
	}
	if !ZbMATHSearchAdapter.Supports(CapAssets) || !ZbMATHSearchAdapter.Supports(CapIdentityResolution) {
		t.Error("zbmath-search should support assets and identity resolution")
	}
	if !ZbMATHFetchAdapter.Supports(CapFetch) {
		t.Error("zbmath-fetch should support fetch")
	}
}

func TestZbMATHIdentity_MalformedRecord(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (zbmathIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if rels := (zbmathIdentity{}).AssertedRelations("n", bad, time.Now()); len(rels) != 0 {
		t.Errorf("invented relations: %+v", rels)
	}
	if assets := (zbmathIdentity{}).Assets(bad); len(assets) != 0 {
		t.Errorf("invented assets: %+v", assets)
	}
	if codes := (zbmathIdentity{}).MSCCodes(bad); len(codes) != 0 {
		t.Errorf("invented MSC codes: %+v", codes)
	}
	if reviews := (zbmathIdentity{}).Reviews(bad); len(reviews) != 0 {
		t.Errorf("invented reviews: %+v", reviews)
	}
	r := (zbmathIdentity{}).RecordRights(bad)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("unparseable record must report unknown rights, got %q", r.Redistribution)
	}
}
