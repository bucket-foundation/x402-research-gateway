package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A works-list response, as api.crossref.org returns it.
const crossrefListFixture = `{"status":"ok","message-type":"work-list","message":{
 "total-results":1042,"next-cursor":"AoJw",
 "items":[
  {"DOI":"10.7717/peerj.4375","URL":"https://doi.org/10.7717/peerj.4375",
   "title":["The state of OA"],"issued":{"date-parts":[[2018,2,13]]},
   "author":[{"given":"Heather","family":"Piwowar","ORCID":"https://orcid.org/0000-0003-1613-5981"}],
   "link":[{"URL":"https://peerj.com/articles/4375.pdf","content-type":"application/pdf",
            "content-version":"vor","intended-application":"text-mining"}],
   "license":[{"URL":"https://creativecommons.org/licenses/by/4.0/","content-version":"vor","delay-in-days":0}]},
  {"DOI":"10.1234/no-url","title":["No URL here"]},
  {"title":["No DOI at all"]}]}}`

// A single-work response, the shape /works/{doi} returns.
const crossrefSingleFixture = `{"status":"ok","message-type":"work","message":{
 "DOI":"10.1234/retracted","title":["A retracted paper"],
 "updated-by":[{"DOI":"10.1234/the-retraction","type":"retraction","label":"Retraction"}],
 "update-to":[{"DOI":"10.1234/the-original","type":"correction","label":"Correction"},
              {"DOI":"10.1234/unknown-type","type":"new-edition","label":"New edition"},
              {"DOI":"","type":"retraction"}]}}`

func TestCrossrefNormalizer_ListAndSingleShapes(t *testing.T) {
	recs := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefListFixture))
	if len(recs) != 2 {
		t.Fatalf("a work with no DOI must be skipped, got %d records", len(recs))
	}
	if recs[0].ID != "10.7717/peerj.4375" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doi.org/10.7717/peerj.4375" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	// A work with no URL falls back to the DOI resolver rather than an
	// empty string.
	if recs[1].CanonicalURL != "https://doi.org/10.1234/no-url" {
		t.Errorf("fallback canonical url = %q", recs[1].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}

	// The same Normalizer handles the single-record message.
	single := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefSingleFixture))
	if len(single) != 1 || single[0].ID != "10.1234/retracted" {
		t.Fatalf("single-record shape not handled: %+v", single)
	}
}

func TestCrossrefCitations_UseTheCrossrefPrefix(t *testing.T) {
	route := &config.RouteConfig{ID: "crossref-search", Citation: config.RouteCitation{SourcePrefix: "crossref"}}
	recs := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefListFixture))
	hits := CrossrefSearchAdapter.CitationProvider.Citations(route, recs)
	if len(hits) != 2 {
		t.Fatalf("got %d hits", len(hits))
	}
	if hits[0].SourceID != "crossref:10.7717/peerj.4375" {
		t.Errorf("source id = %q", hits[0].SourceID)
	}
	if hits[0].Rank != 1 || hits[1].Rank != 2 {
		t.Errorf("ranks = %d,%d", hits[0].Rank, hits[1].Rank)
	}
}

func TestCrossrefIdentity_DOIAndDescriptor(t *testing.T) {
	rec := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefListFixture))[0]

	ids := crossrefIdentity{}.Identifiers(rec)
	if len(ids) != 1 || ids[0].Scheme != identity.SchemeDOI || ids[0].Value != "10.7717/peerj.4375" {
		t.Errorf("identifiers = %+v", ids)
	}
	d := crossrefIdentity{}.Descriptor(rec)
	if d.Title != "The state of OA" || d.Year != 2018 {
		t.Errorf("descriptor = %+v", d)
	}
	if len(d.Authors) != 1 || d.Authors[0] != "Heather Piwowar" {
		t.Errorf("authors = %v", d.Authors)
	}
}

// Crossmark update relations feed the integrity graph, with the direction
// each field implies and only the update types the mapping knows.
func TestCrossrefIdentity_UpdateRelations(t *testing.T) {
	rec := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefSingleFixture))[0]
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	node := "crossref-fetch:10.1234/retracted"

	rels := crossrefIdentity{}.AssertedRelations(node, rec, at)
	if len(rels) != 2 {
		t.Fatalf("an unknown update type and an empty DOI must both be dropped, got %+v", rels)
	}
	byType := map[identity.RelationType]identity.Relation{}
	for _, r := range rels {
		byType[r.Type] = r
		if r.Evidence.Kind != identity.EvidenceProviderAsserted || r.Evidence.Provider != "crossref" {
			t.Errorf("update relations are Crossref's own assertions, got %+v", r.Evidence)
		}
		if r.Evidence.Method != "" {
			t.Error("a provider assertion must carry no inference method")
		}
		if r.Evidence.RetrievedAt != at.Format(time.RFC3339) {
			t.Errorf("relation timestamp = %q", r.Evidence.RetrievedAt)
		}
	}
	// update-to: this record corrects the named work, so it is the source.
	if r := byType[identity.RelCorrects]; r.From != node || r.To != "doi:10.1234/the-original" {
		t.Errorf("update-to direction wrong: %+v", r)
	}
	// updated-by: the named work retracts this record, so it is the source.
	if r := byType[identity.RelRetracts]; r.From != "doi:10.1234/the-retraction" || r.To != node {
		t.Errorf("updated-by direction wrong: %+v", r)
	}
}

func TestCrossrefUpdateType_UnknownTypesAreDropped(t *testing.T) {
	for raw, want := range map[string]identity.RelationType{
		"retraction":  identity.RelRetracts,
		"Correction":  identity.RelCorrects,
		"corrigendum": identity.RelCorrects,
		"erratum":     identity.RelCorrects,
		"withdrawal":  identity.RelWithdraws,
	} {
		got, ok := crossrefUpdateType(raw)
		if !ok || got != want {
			t.Errorf("crossrefUpdateType(%q) = %q/%v, want %q", raw, got, ok, want)
		}
	}
	// An unmapped type is dropped rather than coerced into the nearest
	// relation: calling a new edition a retraction would be worse than
	// saying nothing.
	if _, ok := crossrefUpdateType("new-edition"); ok {
		t.Error("an unmapped update type must not resolve to a relation")
	}
}

// A link is a locator. Asset discovery reports it and never treats it as
// permission.
func TestCrossrefAssets_LocatorsNotPermission(t *testing.T) {
	rec := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefListFixture))[0]
	assets := crossrefIdentity{}.Assets(rec)
	if len(assets) != 1 {
		t.Fatalf("got %d assets, want 1", len(assets))
	}
	a := assets[0]
	if a.CanonicalURL != "https://peerj.com/articles/4375.pdf" {
		t.Errorf("asset url = %q", a.CanonicalURL)
	}
	// The representation carries the content type plus the qualifiers
	// Crossref publishes, so a consumer can tell a text-mining VOR PDF from
	// an accepted manuscript.
	for _, want := range []string{"application/pdf", "intended-application=text-mining", "content-version=vor"} {
		if !strings.Contains(a.Representation, want) {
			t.Errorf("representation %q missing %q", a.Representation, want)
		}
	}
	// A work with no links yields no assets rather than an empty
	// placeholder that could read as "nothing available".
	noLinks := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefListFixture))[1]
	if got := (crossrefIdentity{}).Assets(noLinks); len(got) != 0 {
		t.Errorf("a work with no link metadata must yield no assets, got %+v", got)
	}
}

// Bulk is reported false: whole-corpus dumps are the paid Metadata Plus
// tier this gateway does not subscribe to, and claiming a capability it
// cannot exercise is a lie an agent could act on.
func TestCrossrefSync_ReportsWhatItCanActuallyDo(t *testing.T) {
	sc := CrossrefSearchAdapter.SyncProvider.SyncCapability()
	if sc.Bulk {
		t.Error("Crossref bulk dumps are the paid tier; bulk must be reported false")
	}
	if !sc.Incremental {
		t.Error("from-index-date plus cursor paging is open, so incremental must be true")
	}
	if CrossrefSearchAdapter.Supports(CapBulk) {
		t.Error("the adapter must not advertise the bulk capability")
	}
	if !CrossrefSearchAdapter.Supports(CapIncrementalSync) {
		t.Error("the adapter should advertise incremental sync")
	}
}

func TestCrossrefAdapters_Capabilities(t *testing.T) {
	for _, c := range []Capability{CapSearch, CapPagination, CapAssets, CapIdentityResolution, CapIncrementalSync} {
		if !CrossrefSearchAdapter.Supports(c) {
			t.Errorf("crossref-search should support %q", c)
		}
	}
	if !CrossrefFetchAdapter.Supports(CapFetch) {
		t.Error("crossref-fetch should support fetch")
	}
	if CrossrefSearchAdapter.Searcher.PaginationModel() != "cursor" {
		t.Errorf("pagination model = %q", CrossrefSearchAdapter.Searcher.PaginationModel())
	}
	if schemes := CrossrefFetchAdapter.Fetcher.IdentifierSchemes(); len(schemes) != 1 || schemes[0] != "doi" {
		t.Errorf("fetch identifier schemes = %v", schemes)
	}
	// Crossref has no cited-by, so the search adapter must not claim it.
	if CrossrefSearchAdapter.Supports(CapCitedBy) {
		t.Error("Crossref has no cited-by and must not claim it")
	}
}

func TestCrossrefAdapters_MalformedBodies(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"message":{}}`), []byte(`[]`)} {
		if recs := (CrossrefWorksNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (crossrefIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if rels := (crossrefIdentity{}).AssertedRelations("n", bad, time.Now()); len(rels) != 0 {
		t.Errorf("invented relations: %+v", rels)
	}
	if assets := (crossrefIdentity{}).Assets(bad); len(assets) != 0 {
		t.Errorf("invented assets: %+v", assets)
	}
	_ = crossrefIdentity{}.Descriptor(bad)
}
