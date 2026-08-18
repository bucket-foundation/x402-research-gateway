package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A single-organization response, the shape /v2/organizations/{id} returns.
// Trimmed from a live 2026-08-17 verification query against
// https://ror.org/03yrm5c26 (California Digital Library).
const rorSingleFixture = `{
 "id":"https://ror.org/03yrm5c26",
 "status":"active",
 "established":1997,
 "names":[
   {"lang":"en","types":["acronym"],"value":"CDL"},
   {"lang":"en","types":["ror_display","label"],"value":"California Digital Library"}
 ],
 "types":["archive"],
 "external_ids":[
   {"all":["grid.463323.3"],"preferred":"grid.463323.3","type":"grid"},
   {"all":["0000 0001 1957 5136"],"preferred":null,"type":"isni"},
   {"all":["Q5020447"],"preferred":null,"type":"wikidata"}
 ],
 "relationships":[
   {"label":"University of California Office of the President","type":"parent","id":"https://ror.org/00dmfq477"}
 ],
 "links":[{"type":"website","value":"https://cdlib.org"}],
 "admin":{"last_modified":{"date":"2025-09-22","schema_version":"2.1"}}
}`

// A search-list response, the shape /v2/organizations?query= returns.
const rorSearchFixture = `{"number_of_results":2,"time_taken":4,"items":[
 ` + rorSingleFixture + `,
 {"id":"https://ror.org/00withdrwn","status":"withdrawn","names":[{"lang":"en","types":["ror_display","label"],"value":"A withdrawn org"}],
  "relationships":[{"label":"Its successor","type":"successor","id":"https://ror.org/00succ0000"}]}
]}`

func TestRORNormalizer_SingleAndListShapes(t *testing.T) {
	single := RORNormalizer{}.Normalize([]byte(rorSingleFixture))
	if len(single) != 1 {
		t.Fatalf("got %d records, want 1", len(single))
	}
	if single[0].ID != "https://ror.org/03yrm5c26" {
		t.Errorf("record id = %q", single[0].ID)
	}
	if single[0].CanonicalURL != "https://ror.org/03yrm5c26" {
		t.Errorf("canonical url = %q", single[0].CanonicalURL)
	}
	if len(single[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}

	list := RORNormalizer{}.Normalize([]byte(rorSearchFixture))
	if len(list) != 2 {
		t.Fatalf("got %d records from search, want 2", len(list))
	}
}

func TestRORNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (RORNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestRORIdentity_Identifiers(t *testing.T) {
	rec := RORNormalizer{}.Normalize([]byte(rorSingleFixture))[0]
	ids := rorIdentity{}.Identifiers(rec)
	if len(ids) != 1 || ids[0].Scheme != identity.SchemeROR {
		t.Fatalf("identifiers = %+v", ids)
	}
	if ids[0].Value != "03yrm5c26" {
		t.Errorf("ror identifier value = %q", ids[0].Value)
	}
}

// External identifiers under schemes the identity graph has no node type
// for (GRID, ISNI, Wikidata) are preserved verbatim rather than dropped or
// coerced.
func TestRORIdentity_ExternalIdentifiers(t *testing.T) {
	rec := RORNormalizer{}.Normalize([]byte(rorSingleFixture))[0]
	ext := rorIdentity{}.ExternalIdentifiers(rec)
	if len(ext) != 3 {
		t.Fatalf("got %d external identifiers, want 3", len(ext))
	}
	byType := map[string]ExternalOrgIdentifier{}
	for _, e := range ext {
		byType[e.Type] = e
	}
	if byType["grid"].Preferred != "grid.463323.3" {
		t.Errorf("grid preferred = %q", byType["grid"].Preferred)
	}
	if len(byType["wikidata"].All) != 1 || byType["wikidata"].All[0] != "Q5020447" {
		t.Errorf("wikidata all = %v", byType["wikidata"].All)
	}
}

// Name variants are preserved rather than reduced to one display string.
func TestRORIdentity_NameVariants(t *testing.T) {
	rec := RORNormalizer{}.Normalize([]byte(rorSingleFixture))[0]
	names := rorIdentity{}.NameVariants(rec)
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
	var haveAcronym, haveDisplay bool
	for _, n := range names {
		for _, ty := range n.Types {
			if ty == "acronym" && n.Value == "CDL" {
				haveAcronym = true
			}
			if ty == "ror_display" && n.Value == "California Digital Library" {
				haveDisplay = true
			}
		}
	}
	if !haveAcronym || !haveDisplay {
		t.Errorf("missing a name variant: acronym=%v display=%v (%+v)", haveAcronym, haveDisplay, names)
	}
}

// ROR's relationship.type names what the FAR side is to this record, so
// the edge direction the adapter emits is the inverse of a literal read: a
// far side labeled "parent" makes THIS record the child.
func TestRORIdentity_AssertedRelations_DirectionInverted(t *testing.T) {
	rec := RORNormalizer{}.Normalize([]byte(rorSingleFixture))[0]
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	node := "ror-fetch:https://ror.org/03yrm5c26"

	rels := rorIdentity{}.AssertedRelations(node, rec, at)
	if len(rels) != 1 {
		t.Fatalf("got %d relations, want 1", len(rels))
	}
	r := rels[0]
	if r.Type != identity.RelChildOf {
		t.Errorf("relation type = %q, want child_of (far side is labeled parent)", r.Type)
	}
	if r.To != "https://ror.org/00dmfq477" {
		t.Errorf("relation target = %q", r.To)
	}
	if r.Evidence.Kind != identity.EvidenceProviderAsserted || r.Evidence.Provider != "ror" {
		t.Errorf("evidence = %+v", r.Evidence)
	}

	// A withdrawn record's successor relation follows the same inversion:
	// a far side labeled "successor" makes THIS record the predecessor.
	list := RORNormalizer{}.Normalize([]byte(rorSearchFixture))
	withdrawn := list[1]
	wrels := rorIdentity{}.AssertedRelations("n", withdrawn, at)
	if len(wrels) != 1 || wrels[0].Type != identity.RelPredecessorOf {
		t.Errorf("withdrawn org relations = %+v, want predecessor_of", wrels)
	}
}

func TestRORIdentity_RecordRights(t *testing.T) {
	rec := RORNormalizer{}.Normalize([]byte(rorSingleFixture))[0]
	r := rorIdentity{}.RecordRights(rec)
	if r.Redistribution != RedistributionAllowed {
		t.Errorf("ROR metadata is CC0, must permit redistribution, got %q", r.Redistribution)
	}
	if !r.FreeToRead {
		t.Error("a ROR record is free to read")
	}

	bad := NormalizedRecord{}
	badRights := rorIdentity{}.RecordRights(bad)
	if badRights.Redistribution != RedistributionUnknown {
		t.Errorf("an unparseable record must report unknown, got %q", badRights.Redistribution)
	}
}

func TestRORAdapters_CapabilitiesAndSync(t *testing.T) {
	if RORSearchAdapter.Searcher.PaginationModel() != "page" {
		t.Errorf("pagination model = %q, want page", RORSearchAdapter.Searcher.PaginationModel())
	}
	if schemes := RORFetchAdapter.Fetcher.IdentifierSchemes(); len(schemes) != 1 || schemes[0] != "ror" {
		t.Errorf("fetch identifier schemes = %v", schemes)
	}
	sc := RORSearchAdapter.SyncProvider.SyncCapability()
	if sc.Bulk {
		t.Error("this adapter does not fetch the versioned bulk data dump; bulk must be false")
	}
	if !sc.Incremental {
		t.Error("page-based search supports incremental harvest")
	}
	if !RORSearchAdapter.Supports(CapIdentityResolution) {
		t.Error("ror-search should support identity resolution")
	}
	if !RORFetchAdapter.Supports(CapFetch) {
		t.Error("ror-fetch should support fetch")
	}
}

func TestRORIdentity_MalformedRecord(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (rorIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if rels := (rorIdentity{}).AssertedRelations("n", bad, time.Now()); len(rels) != 0 {
		t.Errorf("invented relations: %+v", rels)
	}
	if ext := (rorIdentity{}).ExternalIdentifiers(bad); len(ext) != 0 {
		t.Errorf("invented external identifiers: %+v", ext)
	}
	if names := (rorIdentity{}).NameVariants(bad); len(names) != 0 {
		t.Errorf("invented name variants: %+v", names)
	}
}
