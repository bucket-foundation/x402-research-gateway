package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A search response, as the Europe PMC RESTful web service returns it.
const epmcSearchFixture = `{"hitCount":2,"nextCursorMark":"AoJw",
 "resultList":{"result":[
  {"id":"33333333","source":"MED","pmid":"33333333","pmcid":"PMC1234567",
   "doi":"10.1234/epmc-example","title":"A biomedical paper","authorString":"Smith J, Doe J.",
   "pubYear":"2021","isOpenAccess":"Y","inEPMC":"Y","inPMC":"Y",
   "license":"CC BY","pubType":"research-article",
   "commentCorrectionList":{"commentCorrection":[
     {"id":"11111111","type":"Preprint"},
     {"id":"22222222","type":"a type this gateway has no mapping for"}
   ]}},
  {"id":"44444444","source":"MED","title":"No pmcid, no doi, no licence","pubYear":"2019"}
 ]}}`

func TestEuropePMCNormalizer_ParsesResults(t *testing.T) {
	recs := EuropePMCNormalizer{}.Normalize([]byte(epmcSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "MED:33333333" {
		t.Errorf("record id = %q, want source:id", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://europepmc.org/article/MED/33333333" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}
}

func TestEuropePMCNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"resultList":{}}`)} {
		if recs := (EuropePMCNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestEuropePMCIdentity_IdentifiersAndDescriptor(t *testing.T) {
	recs := EuropePMCNormalizer{}.Normalize([]byte(epmcSearchFixture))
	rec := recs[0]

	ids := epmcIdentity{}.Identifiers(rec)
	byScheme := map[identity.Scheme]string{}
	for _, id := range ids {
		byScheme[id.Scheme] = id.Value
	}
	if byScheme[identity.SchemePMID] != "33333333" {
		t.Errorf("pmid = %q", byScheme[identity.SchemePMID])
	}
	if byScheme[identity.SchemePMCID] != "PMC1234567" {
		t.Errorf("pmcid = %q", byScheme[identity.SchemePMCID])
	}
	if byScheme[identity.SchemeDOI] != "10.1234/epmc-example" {
		t.Errorf("doi = %q", byScheme[identity.SchemeDOI])
	}

	d := epmcIdentity{}.Descriptor(rec)
	if d.Title != "A biomedical paper" || d.Year != 2021 {
		t.Errorf("descriptor = %+v", d)
	}
	if len(d.Authors) != 2 || d.Authors[0] != "Smith J" || d.Authors[1] != "Doe J" {
		t.Errorf("authors = %v", d.Authors)
	}
}

// Only the commentCorrectionList types this mapping knows become typed
// relations; an unmapped type is dropped rather than guessed.
func TestEuropePMCIdentity_AssertedRelations(t *testing.T) {
	recs := EuropePMCNormalizer{}.Normalize([]byte(epmcSearchFixture))
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	node := "epmc-fetch:MED:33333333"

	rels := epmcIdentity{}.AssertedRelations(node, recs[0], at)
	if len(rels) != 1 {
		t.Fatalf("got %d relations, want 1 (unmapped type dropped)", len(rels))
	}
	r := rels[0]
	if r.Type != identity.RelPreprintOf || r.To != "pmid:11111111" {
		t.Errorf("relation = %+v", r)
	}
	if r.Evidence.Kind != identity.EvidenceProviderAsserted || r.Evidence.Provider != "europepmc" {
		t.Errorf("evidence = %+v", r.Evidence)
	}

	if rels := (epmcIdentity{}).AssertedRelations("n", recs[1], at); len(rels) != 0 {
		t.Errorf("a record with no commentCorrectionList must assert no relations, got %+v", rels)
	}
}

// Free to read and open to redistribute are different facts: an
// open-access, in-EPMC record with a declared licence permits
// redistribution, while a record with no licence reports unknown even
// though it is readable.
func TestEuropePMCIdentity_RecordRights(t *testing.T) {
	recs := EuropePMCNormalizer{}.Normalize([]byte(epmcSearchFixture))

	ccBy := epmcIdentity{}.RecordRights(recs[0])
	if ccBy.Redistribution != RedistributionAllowed {
		t.Errorf("CC BY record must permit redistribution, got %q", ccBy.Redistribution)
	}
	if !ccBy.FreeToRead {
		t.Error("isOpenAccess=Y must report free to read")
	}

	noLicense := epmcIdentity{}.RecordRights(recs[1])
	if noLicense.Redistribution != RedistributionUnknown {
		t.Errorf("no declared licence must report unknown, got %q", noLicense.Redistribution)
	}
	if noLicense.FreeToRead {
		t.Error("a record with no isOpenAccess/inEPMC flag must not report free to read")
	}
	if noLicense.Permits() {
		t.Error("unknown must never permit redistribution")
	}
}

func TestEuropePMCIdentity_Assets(t *testing.T) {
	recs := EuropePMCNormalizer{}.Normalize([]byte(epmcSearchFixture))

	assets := epmcIdentity{}.Assets(recs[0])
	var haveAbstract, haveFullText, havePDF bool
	for _, a := range assets {
		switch a.AssetID {
		case "epmc:MED:33333333#abstract":
			haveAbstract = true
		case "epmc:MED:33333333#fulltext-xml":
			haveFullText = true
			if a.Representation != "application/xml; role=full-text; schema=JATS" {
				t.Errorf("full-text representation = %q", a.Representation)
			}
		case "epmc:MED:33333333#pdf":
			havePDF = true
		}
	}
	if !haveAbstract || !haveFullText || !havePDF {
		t.Errorf("missing an expected asset: abstract=%v fulltext=%v pdf=%v", haveAbstract, haveFullText, havePDF)
	}

	// A record with no PMCID and not in EPMC/PMC yields only the abstract.
	minimal := epmcIdentity{}.Assets(recs[1])
	if len(minimal) != 1 || minimal[0].AssetID != "epmc:MED:44444444#abstract" {
		t.Errorf("minimal record assets = %+v, want only the abstract", minimal)
	}
}

func TestEuropePMCAdapters_CapabilitiesAndSync(t *testing.T) {
	if EuropePMCSearchAdapter.Searcher.PaginationModel() != "cursor" {
		t.Errorf("pagination model = %q, want cursor", EuropePMCSearchAdapter.Searcher.PaginationModel())
	}
	schemes := EuropePMCFetchAdapter.Fetcher.IdentifierSchemes()
	if len(schemes) != 3 {
		t.Errorf("fetch identifier schemes = %v, want 3", schemes)
	}
	sc := EuropePMCSearchAdapter.SyncProvider.SyncCapability()
	if !sc.Bulk || !sc.Incremental {
		t.Errorf("europe pmc supports both bulk and incremental, got %+v", sc)
	}
	if !EuropePMCSearchAdapter.Supports(CapAssets) || !EuropePMCSearchAdapter.Supports(CapIdentityResolution) {
		t.Error("epmc-search should support assets and identity resolution")
	}
	if !EuropePMCFetchAdapter.Supports(CapFetch) {
		t.Error("epmc-fetch should support fetch")
	}
	if !EuropePMCReferencesAdapter.Supports(CapReferences) {
		t.Error("epmc-references should support references")
	}
	if !EuropePMCCitedByAdapter.Supports(CapCitedBy) {
		t.Error("epmc-cited-by should support cited-by")
	}
}

// ---------- Citation graph ----------

func TestEuropePMCCitationGraph_EdgeQuery(t *testing.T) {
	refs := epmcCitationGraph{direction: citation.DirectionReferences}

	pmid := mustIdent(t, identity.SchemePMID, "33333333")
	q, ok := refs.EdgeQuery(pmid)
	if !ok || q["source"] != "MED" || q["id"] != "33333333" {
		t.Errorf("pmid edge query = %+v/%v", q, ok)
	}

	pmcid := mustIdent(t, identity.SchemePMCID, "PMC1234567")
	q2, ok := refs.EdgeQuery(pmcid)
	if !ok || q2["source"] != "PMC" || q2["id"] != "1234567" {
		t.Errorf("pmcid edge query = %+v/%v, want PMC prefix stripped", q2, ok)
	}

	doi := mustIdent(t, identity.SchemeDOI, "10.1234/x")
	if _, ok := refs.EdgeQuery(doi); ok {
		t.Error("europe pmc article endpoints take pmid or pmcid, never doi; edge query must fail")
	}
}

const epmcEdgeFixture = `{"hitCount":2,
 "citationList":{"citation":[{"id":"55555555","source":"MED","doi":"10.1234/citing-a"}]},
 "referenceList":{"reference":[{"id":"66666666","source":"MED","doi":"10.1234/cited-a"}]}}`

func TestEuropePMCCitationGraph_EdgesBothDirections(t *testing.T) {
	source := mustIdent(t, identity.SchemePMID, "33333333")
	refs := epmcCitationGraph{direction: citation.DirectionReferences}
	edges := refs.Edges(source, []byte(epmcEdgeFixture), citAt)
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2 (citations + references both surfaced)", len(edges))
	}
	for _, e := range edges {
		if e.Direction != citation.DirectionReferences {
			t.Errorf("edge direction = %q, want references (the adapter's own direction on every edge)", e.Direction)
		}
		if endpointValue(e.Source, identity.SchemePMID) != "33333333" {
			t.Errorf("source endpoint = %+v", e.Source)
		}
		if e.RetrievedAt != citAt.UTC().Format(time.RFC3339) {
			t.Errorf("retrieved at = %q", e.RetrievedAt)
		}
	}

	cited := epmcCitationGraph{direction: citation.DirectionCitedBy}
	citedEdges := cited.Edges(source, []byte(epmcEdgeFixture), citAt)
	for _, e := range citedEdges {
		if endpointValue(e.Target, identity.SchemePMID) != "33333333" {
			t.Errorf("cited-by target endpoint = %+v", e.Target)
		}
	}
}

func TestEuropePMCCitationGraph_EdgesMalformedBody(t *testing.T) {
	refs := epmcCitationGraph{direction: citation.DirectionReferences}
	source := mustIdent(t, identity.SchemePMID, "1")
	if edges := refs.Edges(source, []byte(`not json`), citAt); len(edges) != 0 {
		t.Errorf("invented %d edges from malformed body", len(edges))
	}
}

func TestEuropePMCCitationGraph_EdgePagination(t *testing.T) {
	refs := epmcCitationGraph{direction: citation.DirectionReferences}
	model, more, _ := refs.EdgePagination([]byte(epmcEdgeFixture))
	if model != "page" {
		t.Errorf("pagination model = %q, want page", model)
	}
	if more {
		t.Error("got=2 == hitCount=2, so there must be no more pages")
	}
}

func TestEuropePMCIdentity_MalformedRecord(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (epmcIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if rels := (epmcIdentity{}).AssertedRelations("n", bad, time.Now()); len(rels) != 0 {
		t.Errorf("invented relations: %+v", rels)
	}
	if assets := (epmcIdentity{}).Assets(bad); len(assets) != 0 {
		t.Errorf("invented assets: %+v", assets)
	}
	r := (epmcIdentity{}).RecordRights(bad)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("unparseable record must report unknown rights, got %q", r.Redistribution)
	}
}
