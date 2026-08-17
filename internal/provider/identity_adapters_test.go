package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

func idValues(ids []identity.Identifier) map[identity.Scheme]string {
	out := map[identity.Scheme]string{}
	for _, id := range ids {
		out[id.Scheme] = id.Value
	}
	return out
}

func TestOpenAlexIdentity_ReadsIDsBlock(t *testing.T) {
	body := []byte(`{"results":[{
	  "id":"https://openalex.org/W2741809807",
	  "ids":{"openalex":"https://openalex.org/W2741809807",
	         "doi":"https://doi.org/10.7717/PeerJ.4375",
	         "pmid":"https://pubmed.ncbi.nlm.nih.gov/29456894",
	         "pmcid":"https://www.ncbi.nlm.nih.gov/pmc/articles/PMC5815332"},
	  "title":"The state of OA","publication_year":2018,
	  "authorships":[{"author":{"display_name":"Heather Piwowar"}}]}]}`)

	recs := OpenAlexWorksNormalizer{}.Normalize(body)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	// Pre-existing fields are unchanged by the raw-preservation change.
	if recs[0].ID != "W2741809807" || recs[0].CanonicalURL != "https://openalex.org/W2741809807" {
		t.Errorf("normalizer output changed: %+v", recs[0])
	}
	if len(recs[0].Raw) == 0 {
		t.Fatal("raw record bytes should be preserved")
	}

	got := idValues(openAlexIdentity{}.Identifiers(recs[0]))
	want := map[identity.Scheme]string{
		identity.SchemeOpenAlex: "W2741809807",
		identity.SchemeDOI:      "10.7717/peerj.4375",
		identity.SchemePMID:     "29456894",
		identity.SchemePMCID:    "PMC5815332",
	}
	for scheme, v := range want {
		if got[scheme] != v {
			t.Errorf("%s = %q, want %q", scheme, got[scheme], v)
		}
	}
	d := openAlexIdentity{}.Descriptor(recs[0])
	if d.Title != "The state of OA" || d.Year != 2018 || len(d.Authors) != 1 {
		t.Errorf("descriptor = %+v", d)
	}
	// The adapter asserts identifiers, never relations. Inference belongs
	// to the resolver.
	if rels := (openAlexIdentity{}).AssertedRelations("openalex-works:W1", recs[0], time.Now()); rels != nil {
		t.Errorf("OpenAlex asserts no typed relations, got %+v", rels)
	}
}

func TestSemanticScholarIdentity_ReadsExternalIDs(t *testing.T) {
	body := []byte(`{"data":[{"paperId":"649def34f8be52c8b66281af98ae884c09aef38b",
	  "externalIds":{"DOI":"10.7717/peerj.4375","PubMed":"29456894","ArXiv":"1802.01234",
	                 "DBLP":"journals/pj/Piwowar18"},
	  "title":"The state of OA","year":2018,"authors":[{"name":"Heather Piwowar"}]}]}`)

	recs := SemanticScholarSearchNormalizer{}.Normalize(body)
	if len(recs) != 1 || recs[0].ID != "649def34f8be52c8b66281af98ae884c09aef38b" {
		t.Fatalf("normalizer output changed: %+v", recs)
	}
	got := idValues(semanticScholarIdentity{}.Identifiers(recs[0]))
	for scheme, want := range map[identity.Scheme]string{
		identity.SchemeSemanticScholar: "649def34f8be52c8b66281af98ae884c09aef38b",
		identity.SchemeDOI:             "10.7717/peerj.4375",
		identity.SchemePMID:            "29456894",
		identity.SchemeArXiv:           "1802.01234",
		identity.SchemeDBLP:            "journals/pj/Piwowar18",
	} {
		if got[scheme] != want {
			t.Errorf("%s = %q, want %q", scheme, got[scheme], want)
		}
	}
}

func TestPubMedIdentity_PMIDOnly(t *testing.T) {
	recs := PubMedSearchNormalizer{}.Normalize([]byte(`{"esearchresult":{"idlist":["38831607"]}}`))
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	ids := pubMedIdentity{}.Identifiers(recs[0])
	if len(ids) != 1 || ids[0].Scheme != identity.SchemePMID || ids[0].Value != "38831607" {
		t.Errorf("ESearch asserts the PMID and nothing else, got %+v", ids)
	}
}

// Malformed and empty bodies must never panic and must never invent an
// identifier.
func TestIdentityAdapters_MalformedInputIsSilentNotFatal(t *testing.T) {
	bad := []NormalizedRecord{
		{},
		{ID: "x", Raw: json.RawMessage(`not json`)},
		{ID: "x", Raw: json.RawMessage(`{"ids":{"doi":"nonsense"}}`)},
		{ID: "x", Raw: json.RawMessage(`[]`)},
	}
	for _, rec := range bad {
		if ids := (openAlexIdentity{}).Identifiers(rec); len(ids) != 0 {
			t.Errorf("openalex invented identifiers from %s: %+v", rec.Raw, ids)
		}
		if ids := (semanticScholarIdentity{}).Identifiers(rec); len(ids) != 0 {
			t.Errorf("s2 invented identifiers from %s: %+v", rec.Raw, ids)
		}
		_ = openAlexIdentity{}.Descriptor(rec)
		_ = semanticScholarIdentity{}.Descriptor(rec)
	}
}

func TestAdapterCapabilities_ReportIdentityResolution(t *testing.T) {
	if !OpenAlexWorksAdapter.Supports(CapIdentityResolution) {
		t.Error("OpenAlex adapter should report identity_resolution")
	}
	if !SemanticScholarSearchAdapter.Supports(CapIdentityResolution) {
		t.Error("Semantic Scholar adapter should report identity_resolution")
	}
	if !PubMedSearchAdapter.Supports(CapIdentityResolution) {
		t.Error("PubMed search adapter should report identity_resolution")
	}
	// An adapter without an IdentityProvider must report the capability as
	// unsupported rather than silently absent.
	if PubMedFetchAdapter.Supports(CapIdentityResolution) {
		t.Error("PubMed fetch adapter has no IdentityProvider and must not claim the capability")
	}
	// Pre-existing capability reporting is unchanged.
	if !OpenAlexWorksAdapter.Supports(CapSearch) || !PubMedFetchAdapter.Supports(CapFetch) {
		t.Error("existing capability reporting regressed")
	}
}
