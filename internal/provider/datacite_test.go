package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A /dois list response, as api.datacite.org returns it (JSON:API shape).
const dataciteListFixture = `{"data":[
 {"id":"10.5281/zenodo.1234","attributes":{
   "doi":"10.5281/zenodo.1234","url":"https://zenodo.org/record/1234",
   "titles":[{"title":"A dataset"}],
   "creators":[{"name":"Piwowar, Heather"}],
   "publicationYear":2020,
   "types":{"resourceType":"Dataset","resourceTypeGeneral":"Dataset"},
   "rightsList":[{"rights":"Creative Commons Zero v1.0 Universal","rightsIdentifier":"cc0-1.0","rightsUri":"https://creativecommons.org/publicdomain/zero/1.0/"}],
   "relatedIdentifiers":[
     {"relatedIdentifier":"10.1234/paper","relatedIdentifierType":"DOI","relationType":"IsSupplementTo"},
     {"relatedIdentifier":"10.9999/mystery","relatedIdentifierType":"DOI","relationType":"IsCitedBy"},
     {"relatedIdentifier":"","relatedIdentifierType":"DOI","relationType":"IsSupplementTo"}
   ],
   "contentUrl":["https://zenodo.org/record/1234/files/data.csv",""]}},
 {"id":"10.5281/zenodo.5678","attributes":{
   "doi":"10.5281/zenodo.5678","titles":[{"title":"No rights, no content url"}],
   "publicationYear":2021,"types":{"resourceType":"Software","resourceTypeGeneral":"Software"}}}
]}`

// A single-DOI response, the shape /dois/{doi} returns: data is an object.
const dataciteSingleFixture = `{"data":
 {"id":"10.5281/zenodo.1234","attributes":{
   "doi":"10.5281/zenodo.1234","titles":[{"title":"A dataset"}],
   "publicationYear":2020,"types":{"resourceTypeGeneral":"Dataset"}}}}`

func TestDataCiteNormalizer_ListAndSingleShapes(t *testing.T) {
	recs := DataCiteNormalizer{}.Normalize([]byte(dataciteListFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "10.5281/zenodo.1234" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doi.org/10.5281/zenodo.1234" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}

	single := DataCiteNormalizer{}.Normalize([]byte(dataciteSingleFixture))
	if len(single) != 1 || single[0].ID != "10.5281/zenodo.1234" {
		t.Fatalf("single-record shape not handled: %+v", single)
	}
}

func TestDataCiteNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"data":null}`)} {
		if recs := (DataCiteNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestDataCiteIdentity_IdentifiersAndDescriptor(t *testing.T) {
	rec := DataCiteNormalizer{}.Normalize([]byte(dataciteListFixture))[0]

	ids := dataciteIdentity{}.Identifiers(rec)
	if len(ids) != 1 || ids[0].Scheme != identity.SchemeDOI || ids[0].Value != "10.5281/zenodo.1234" {
		t.Errorf("identifiers = %+v", ids)
	}

	d := dataciteIdentity{}.Descriptor(rec)
	if d.Title != "A dataset" || d.Year != 2020 {
		t.Errorf("descriptor = %+v", d)
	}
	if len(d.Authors) != 1 || d.Authors[0] != "Piwowar, Heather" {
		t.Errorf("authors = %v", d.Authors)
	}
}

func TestDataCiteRelationType_MappedAndUnmapped(t *testing.T) {
	for raw, want := range map[string]identity.RelationType{
		"IsSupplementTo":      identity.RelSupplementTo,
		"IsIdenticalTo":       identity.RelSameWork,
		"IsPreviousVersionOf": identity.RelVersionOf,
		"IsNewVersionOf":      identity.RelVersionOf,
		"HasVersion":          identity.RelVersionOf,
		"IsPreprintOf":        identity.RelPreprintOf,
		"IsObsoletedBy":       identity.RelWithdraws,
	} {
		got, ok := dataciteRelationType(raw)
		if !ok || got != want {
			t.Errorf("dataciteRelationType(%q) = %q/%v, want %q", raw, got, ok, want)
		}
	}
	// IsCitedBy and IsDerivedFrom describe a different fact than any
	// identity relation; coercing them would assert something DataCite did
	// not say.
	if _, ok := dataciteRelationType("IsCitedBy"); ok {
		t.Error("IsCitedBy must not resolve to an identity relation")
	}
}

// The mapped subset becomes typed relations with provider-asserted
// evidence; an empty related identifier and an unmapped type are both
// dropped from AssertedRelations but preserved verbatim in
// ResourceRelations.
func TestDataCiteIdentity_AssertedRelationsAndResourceRelations(t *testing.T) {
	rec := DataCiteNormalizer{}.Normalize([]byte(dataciteListFixture))[0]
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	node := "datacite-fetch:10.5281/zenodo.1234"

	rels := dataciteIdentity{}.AssertedRelations(node, rec, at)
	if len(rels) != 1 {
		t.Fatalf("got %d relations, want 1 (IsCitedBy unmapped, empty identifier dropped)", len(rels))
	}
	if rels[0].Type != identity.RelSupplementTo || rels[0].To != "doi:10.1234/paper" {
		t.Errorf("relation = %+v", rels[0])
	}
	if rels[0].Evidence.Kind != identity.EvidenceProviderAsserted || rels[0].Evidence.Provider != "datacite" {
		t.Errorf("evidence = %+v", rels[0].Evidence)
	}

	resourceRels := dataciteIdentity{}.ResourceRelations(rec)
	if len(resourceRels) != 3 {
		t.Fatalf("resource relations must preserve every entry DataCite published, got %d", len(resourceRels))
	}
	var sawUnmapped bool
	for _, r := range resourceRels {
		if r.ProviderRelationType == "IsCitedBy" {
			sawUnmapped = true
			if r.NormalizedRelationType != "" {
				t.Errorf("an unmapped relation type must gain no normalized term, got %q", r.NormalizedRelationType)
			}
		}
	}
	if !sawUnmapped {
		t.Error("IsCitedBy must survive in ResourceRelations even though AssertedRelations drops it")
	}
}

func TestDataCiteIdentity_ResourceType(t *testing.T) {
	rec := DataCiteNormalizer{}.Normalize([]byte(dataciteListFixture))[0]
	general, specific := dataciteIdentity{}.ResourceType(rec)
	if general != "Dataset" || specific != "Dataset" {
		t.Errorf("resource type = %q/%q", general, specific)
	}
}

// CC0 metadata says nothing about the deposited object; RecordRights reads
// rightsList instead of the provider-level metadata licence.
func TestDataCiteIdentity_RecordRights(t *testing.T) {
	recs := DataCiteNormalizer{}.Normalize([]byte(dataciteListFixture))

	cc0 := dataciteIdentity{}.RecordRights(recs[0])
	if cc0.Redistribution != RedistributionAllowed || !cc0.FreeToRead {
		t.Errorf("CC0 rightsList entry must permit redistribution, got %+v", cc0)
	}

	noRights := dataciteIdentity{}.RecordRights(recs[1])
	if noRights.Redistribution != RedistributionUnknown {
		t.Errorf("absent rightsList must report unknown, got %q", noRights.Redistribution)
	}
	if noRights.Permits() {
		t.Error("unknown must never permit redistribution")
	}
}

func TestDataCiteIdentity_Assets(t *testing.T) {
	recs := DataCiteNormalizer{}.Normalize([]byte(dataciteListFixture))

	assets := dataciteIdentity{}.Assets(recs[0])
	var haveLanding, haveContent bool
	for _, a := range assets {
		if a.CanonicalURL == "https://zenodo.org/record/1234" {
			haveLanding = true
		}
		if a.CanonicalURL == "https://zenodo.org/record/1234/files/data.csv" {
			haveContent = true
		}
		// Empty content URLs must not produce empty-URL assets.
		if a.CanonicalURL == "" {
			t.Error("an asset must never carry an empty canonical URL")
		}
		if a.Rights.Redistribution != RedistributionAllowed {
			t.Errorf("asset %q rights = %+v, want allowed (CC0 record)", a.AssetID, a.Rights)
		}
	}
	if !haveLanding || !haveContent {
		t.Errorf("missing an expected asset: landing=%v content=%v", haveLanding, haveContent)
	}

	// A record with no url and no contentUrl yields no assets.
	if got := (dataciteIdentity{}).Assets(recs[1]); len(got) != 0 {
		t.Errorf("a record with no url/contentUrl must yield no assets, got %+v", got)
	}
}

func TestDataCiteAdapters_CapabilitiesAndSync(t *testing.T) {
	if DataCiteSearchAdapter.Searcher.PaginationModel() != "cursor" {
		t.Errorf("pagination model = %q, want cursor", DataCiteSearchAdapter.Searcher.PaginationModel())
	}
	if schemes := DataCiteFetchAdapter.Fetcher.IdentifierSchemes(); len(schemes) != 1 || schemes[0] != "doi" {
		t.Errorf("fetch identifier schemes = %v", schemes)
	}
	sc := DataCiteSearchAdapter.SyncProvider.SyncCapability()
	if sc.Bulk {
		t.Error("DataCite has no published bulk export this gateway uses; bulk must be false")
	}
	if !sc.Incremental {
		t.Error("cursor paging supports incremental harvest")
	}
	if !DataCiteSearchAdapter.Supports(CapAssets) || !DataCiteSearchAdapter.Supports(CapIdentityResolution) {
		t.Error("datacite-search should support assets and identity resolution")
	}
	if !DataCiteFetchAdapter.Supports(CapFetch) {
		t.Error("datacite-fetch should support fetch")
	}
}

func TestDataCiteIdentity_MalformedRecord(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (dataciteIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if rels := (dataciteIdentity{}).AssertedRelations("n", bad, time.Now()); len(rels) != 0 {
		t.Errorf("invented relations: %+v", rels)
	}
	if assets := (dataciteIdentity{}).Assets(bad); len(assets) != 0 {
		t.Errorf("invented assets: %+v", assets)
	}
	r := (dataciteIdentity{}).RecordRights(bad)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("unparseable record must report unknown rights, got %q", r.Redistribution)
	}
}
