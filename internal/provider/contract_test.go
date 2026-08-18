package provider

import "testing"

// Contract tests (x402-research-gateway#22).
//
// Every implemented adapter's Normalizer assumes a specific response shape:
// pubmedESearchHits assumes esearchresult.idlist, CrossrefWorksNormalizer
// assumes message.items[].DOI, and so on. Each adapter's own _test.go file
// already asserts these shapes in depth (field-by-field, edge cases, malformed
// bodies); what was missing was a single, explicitly named, exhaustive list
// naming every normalizer this gateway ships as a contract, so a reviewer
// can answer "is every adapter contract-tested?" by reading one file instead
// of auditing 20.
//
// This suite runs against the same recorded fixtures the adapters' own unit
// tests use — declared once as package-level consts, referenced here by
// name — rather than inventing parallel copies that could drift from what
// the adapter tests actually exercise. No test in this file makes an
// upstream request; that is the scheduled registry.Verifier's job
// (internal/registry/verify.go), which shares no fixture with unit tests
// because it runs against live endpoints by design.
//
// A contract test failing here means: the shape this adapter's production
// parser depends on no longer round-trips through a fixture recorded from
// that provider. That is exactly the "PubMed stops returning
// esearchresult.idlist" scenario this issue was filed to catch before a
// live campaign discovers it instead.
var normalizerContracts = []struct {
	// name is the provider/route this normalizer serves, for a failure
	// message a human can act on without opening the source file.
	name string
	norm interface {
		Normalize([]byte) []NormalizedRecord
	}
	// fixture is a recorded (not hand-invented) response body: the same
	// const another test in this package already uses.
	fixture string
	// wantMinRecords is the fewest records a correctly parsed fixture must
	// yield. Every fixture referenced here is known to contain at least one
	// parseable record, so a drop to zero means the shape assumption broke.
	wantMinRecords int
	// noRawExpected marks shapes that are, by design, an id list with no
	// per-record object to preserve (PubMed ESearch returns idlist only,
	// nothing else). Everything else must round-trip its raw bytes.
	noRawExpected bool
}{
	{"pubmed/esearch", PubMedSearchNormalizer{}, `{"esearchresult":{"idlist":["38831607","34588695"]}}`, 1, true},
	{"crossref/works", CrossrefWorksNormalizer{}, crossrefListFixture, 1, false},
	{"crossref/works-single", CrossrefWorksNormalizer{}, crossrefSingleFixture, 1, false},
	{"core/search", CORENormalizer{}, coreSearchFixture, 1, false},
	{"arxiv/atom-feed", ArXivNormalizer{}, arxivFeedFixture, 1, false},
	{"datacite/dois-list", DataCiteNormalizer{}, dataciteListFixture, 1, false},
	{"datacite/dois-single", DataCiteNormalizer{}, dataciteSingleFixture, 1, false},
	{"openalex/works", OpenAlexWorksNormalizer{}, `{"results":[{"id":"https://openalex.org/W1234"}]}`, 1, false},
	{"semantic-scholar/search", SemanticScholarSearchNormalizer{}, `{"data":[{"paperId":"abc123"}]}`, 1, false},
	{"europe-pmc/search", EuropePMCNormalizer{}, epmcSearchFixture, 1, false},
	{"dblp/publ-search", DBLPPublNormalizer{}, dblpPublSearchFixture, 1, false},
	{"dblp/author-search", DBLPAuthorNormalizer{}, dblpAuthorSearchFixture, 1, false},
	{"dblp/record-xml", DBLPRecordNormalizer{}, dblpRecordXMLFixture, 1, false},
	{"orcid/record", ORCIDRecordNormalizer{}, orcidRecordFixture, 1, false},
	{"orcid/search", ORCIDSearchNormalizer{}, orcidSearchFixture, 1, false},
	{"unpaywall/doi", UnpaywallNormalizer{}, unpaywallOAFixture, 1, false},
	{"ror/search", RORNormalizer{}, rorSearchFixture, 1, false},
	{"ror/single", RORNormalizer{}, rorSingleFixture, 1, false},
	{"zbmath/search", ZbMATHNormalizer{}, zbmathSearchListFixture, 1, false},
	{"zbmath/fetch", ZbMATHNormalizer{}, zbmathFetchFixture, 1, false},
	{"zenodo/search", ZenodoNormalizer{}, zenodoSearchFixture, 1, false},
	{"doaj/search", DOAJNormalizer{}, doajSearchFixture, 1, false},
	{"doaj/fetch", DOAJNormalizer{}, doajSingleFixture, 1, false},
	{"openaire/search", OpenAIRENormalizer{}, openaireSearchFixture, 1, false},
	{"openaire/fetch", OpenAIRENormalizer{}, openaireSingleFixture, 1, false},
	{"biorxiv/listing", BioRxivNormalizer{}, biorxivFixture, 1, false},
	{"oeis/search", OEISNormalizer{}, oeisFixture, 1, false},
	{"lmfdb/search", LMFDBNormalizer{}, lmfdbFixture, 1, false},
	{"biomodels/search", BioModelsNormalizer{}, biomodelsSearchFixture, 1, false},
	{"biomodels/fetch", BioModelsNormalizer{}, biomodelsSingleFixture, 1, false},
	{"uspto/search", USPTONormalizer{}, usptoSearchFixture, 1, false},
	{"uspto/fetch", USPTONormalizer{}, usptoFetchGrantedFixture, 1, false},
}

func TestNormalizerContracts(t *testing.T) {
	for _, c := range normalizerContracts {
		c := c
		t.Run(c.name, func(t *testing.T) {
			recs := c.norm.Normalize([]byte(c.fixture))
			if len(recs) < c.wantMinRecords {
				t.Fatalf("%s: parsed %d record(s) from a fixture known to hold at least %d; "+
					"the response shape this adapter depends on may have drifted",
					c.name, len(recs), c.wantMinRecords)
			}
			for _, r := range recs {
				if r.ID == "" {
					t.Errorf("%s: a parsed record has an empty ID, which breaks citation and re-verification handles", c.name)
				}
				if len(r.Raw) == 0 && !c.noRawExpected {
					t.Errorf("%s: a parsed record dropped its raw bytes, which downstream capabilities (assets, relations) read from", c.name)
				}
			}
		})
	}
}

// TestNormalizerContracts_CoverEveryRegisteredNormalizer is the audit this
// issue exists for: it is not enough for a contract test to pass, every
// Normalizer type the provider package exports must have one. Extending
// this list is a required step of adding a new adapter's normalizer, not an
// optional follow-up.
func TestNormalizerContracts_CoverEveryRegisteredNormalizer(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range normalizerContracts {
		registered[c.name] = true
	}
	// The set of provider/shape pairs known to exist as of this session.
	// A new normalizer added without a matching contract entry above fails
	// this test, which is the point.
	want := []string{
		"pubmed/esearch", "crossref/works", "core/search", "arxiv/atom-feed",
		"datacite/dois-list", "openalex/works", "semantic-scholar/search",
		"europe-pmc/search", "dblp/publ-search", "dblp/author-search",
		"dblp/record-xml", "orcid/record", "orcid/search", "unpaywall/doi",
		"ror/search", "zbmath/search", "zenodo/search",
		"doaj/search", "doaj/fetch", "openaire/search", "openaire/fetch",
		"biorxiv/listing",
		"oeis/search", "lmfdb/search", "biomodels/search", "biomodels/fetch",
		"uspto/search", "uspto/fetch",
	}
	for _, w := range want {
		if !registered[w] {
			t.Errorf("missing contract test for %s", w)
		}
	}
}
