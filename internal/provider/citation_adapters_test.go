package provider

import (
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

var citAt = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func mustIdent(t *testing.T, s identity.Scheme, raw string) identity.Identifier {
	t.Helper()
	id, ok := identity.New(s, raw)
	if !ok {
		t.Fatalf("fixture identifier %q rejected under %s", raw, s)
	}
	return id
}

func endpointValue(e citation.Endpoint, s identity.Scheme) string {
	for _, id := range e.Identifiers {
		if id.Scheme == s {
			return id.Value
		}
	}
	return ""
}

// ---------- OpenAlex ----------

// The filter names invert the traversal, so the mapping is asserted rather
// than trusted: references uses cited_by, cited_by uses cites.
func TestOpenAlexCitationGraph_FilterMapping(t *testing.T) {
	id := mustIdent(t, identity.SchemeOpenAlex, "https://openalex.org/W2741809807")

	refs, ok := OpenAlexReferencesAdapter.CitationGraphProvider.EdgeQuery(id)
	if !ok {
		t.Fatal("OpenAlex should accept an OpenAlex work id")
	}
	if refs["filter"] != "cited_by:W2741809807" {
		t.Errorf("references filter = %q, want cited_by:W2741809807", refs["filter"])
	}
	cited, _ := OpenAlexCitedByAdapter.CitationGraphProvider.EdgeQuery(id)
	if cited["filter"] != "cites:W2741809807" {
		t.Errorf("cited_by filter = %q, want cites:W2741809807", cited["filter"])
	}

	// A DOI cannot drive the OpenAlex citation filters, so it reports
	// unsupported rather than being coerced into a plausible query.
	if _, ok := OpenAlexReferencesAdapter.CitationGraphProvider.EdgeQuery(
		mustIdent(t, identity.SchemeDOI, "10.1234/x")); ok {
		t.Error("a DOI must be unsupported for the OpenAlex citation filters")
	}
}

const openAlexEdgeFixture = `{"meta":{"count":2,"per_page":200,"next_cursor":"abc"},
 "results":[
   {"id":"https://openalex.org/W111","doi":"https://doi.org/10.1234/one",
    "ids":{"openalex":"https://openalex.org/W111","doi":"https://doi.org/10.1234/one","pmid":"https://pubmed.ncbi.nlm.nih.gov/999"}},
   {"id":"https://openalex.org/W222","ids":{"openalex":"https://openalex.org/W222"},"is_retracted":true}]}`

func TestOpenAlexCitationGraph_EdgeOrientation(t *testing.T) {
	query := mustIdent(t, identity.SchemeOpenAlex, "W2741809807")

	// references: the queried work cites the results, so it is the source.
	refs := OpenAlexReferencesAdapter.CitationGraphProvider.Edges(query, []byte(openAlexEdgeFixture), citAt)
	if len(refs) != 2 {
		t.Fatalf("got %d edges, want 2", len(refs))
	}
	if endpointValue(refs[0].Source, identity.SchemeOpenAlex) != "W2741809807" {
		t.Errorf("under references the queried work must be the source, got %+v", refs[0].Source)
	}
	if endpointValue(refs[0].Target, identity.SchemeOpenAlex) != "W111" {
		t.Errorf("target = %+v", refs[0].Target)
	}
	if endpointValue(refs[0].Target, identity.SchemeDOI) != "10.1234/one" {
		t.Error("the target's DOI should be carried alongside its OpenAlex id")
	}
	if refs[0].RetrievedAt != citAt.Format(time.RFC3339) {
		t.Errorf("edge timestamp = %q", refs[0].RetrievedAt)
	}
	// The provider's own retraction annotation survives onto the edge.
	if refs[1].Status != citation.EdgeStatusRetracted {
		t.Errorf("retraction annotation lost, status = %q", refs[1].Status)
	}
	if refs[0].Status != "" {
		t.Error("an unannotated edge must carry no status rather than a default")
	}

	// cited_by: the results cite the queried work, so it is the target.
	cited := OpenAlexCitedByAdapter.CitationGraphProvider.Edges(query, []byte(openAlexEdgeFixture), citAt)
	if endpointValue(cited[0].Target, identity.SchemeOpenAlex) != "W2741809807" {
		t.Errorf("under cited_by the queried work must be the target, got %+v", cited[0].Target)
	}
}

func TestOpenAlexCitationGraph_Pagination(t *testing.T) {
	model, truncated, cursor := OpenAlexReferencesAdapter.CitationGraphProvider.
		EdgePagination([]byte(`{"meta":{"count":500,"next_cursor":"c1"},"results":[{"id":"https://openalex.org/W1"}]}`))
	if model != "cursor" || !truncated || cursor != "c1" {
		t.Errorf("pagination = %q/%v/%q", model, truncated, cursor)
	}
	_, truncated, _ = OpenAlexReferencesAdapter.CitationGraphProvider.
		EdgePagination([]byte(`{"meta":{"count":1},"results":[{"id":"https://openalex.org/W1"}]}`))
	if truncated {
		t.Error("a complete page must not report truncation")
	}
}

// ---------- Semantic Scholar ----------

const s2EdgeFixture = `{"offset":0,"next":100,"data":[
  {"citedPaper":{"paperId":"aaa","externalIds":{"DOI":"10.1234/one","PubMed":"999"},"title":"One"}},
  {"citedPaper":{"paperId":"bbb","externalIds":{"ArXiv":"2101.00001"},"title":"Two"}},
  {"citedPaper":null}]}`

func TestSemanticScholarCitationGraph_EdgesAndIdentifierForms(t *testing.T) {
	query := mustIdent(t, identity.SchemeDOI, "10.7717/peerj.4375")

	params, ok := SemanticScholarReferencesAdapter.CitationGraphProvider.EdgeQuery(query)
	if !ok || params["id"] != "DOI:10.7717/peerj.4375" {
		t.Fatalf("S2 should render a DOI as a prefixed external id, got %v", params)
	}
	// S2 resolves several schemes; ROR is not one of them.
	if _, ok := SemanticScholarReferencesAdapter.CitationGraphProvider.EdgeQuery(
		mustIdent(t, identity.SchemeROR, "https://ror.org/03vek6s52")); ok {
		t.Error("an ROR id must be unsupported for a paper citation query")
	}

	edges := SemanticScholarReferencesAdapter.CitationGraphProvider.Edges(query, []byte(s2EdgeFixture), citAt)
	if len(edges) != 2 {
		t.Fatalf("a null paper must be skipped without dropping the rest, got %d edges", len(edges))
	}
	if endpointValue(edges[0].Target, identity.SchemeDOI) != "10.1234/one" ||
		endpointValue(edges[0].Target, identity.SchemePMID) != "999" {
		t.Errorf("target identifiers = %+v", edges[0].Target)
	}
	if endpointValue(edges[1].Target, identity.SchemeArXiv) != "2101.00001" {
		t.Errorf("arXiv external id lost: %+v", edges[1].Target)
	}
	if endpointValue(edges[0].Source, identity.SchemeDOI) != "10.7717/peerj.4375" {
		t.Errorf("the queried work must be the source under references, got %+v", edges[0].Source)
	}

	cited := SemanticScholarCitedByAdapter.CitationGraphProvider.Edges(query,
		[]byte(`{"data":[{"citingPaper":{"paperId":"ccc","externalIds":{"DOI":"10.9999/citer"}}}]}`), citAt)
	if len(cited) != 1 || endpointValue(cited[0].Source, identity.SchemeDOI) != "10.9999/citer" {
		t.Errorf("under cited_by the citing paper must be the source, got %+v", cited)
	}
}

func TestSemanticScholarCitationGraph_Pagination(t *testing.T) {
	model, truncated, cursor := SemanticScholarReferencesAdapter.CitationGraphProvider.
		EdgePagination([]byte(s2EdgeFixture))
	if model != "offset" || !truncated || cursor != "100" {
		t.Errorf("pagination = %q/%v/%q", model, truncated, cursor)
	}
	// No `next` means the caller has the whole set.
	_, truncated, _ = SemanticScholarReferencesAdapter.CitationGraphProvider.
		EdgePagination([]byte(`{"offset":0,"data":[]}`))
	if truncated {
		t.Error("a response with no next offset must not report truncation")
	}
}

// ---------- OpenCitations ----------

const openCitationsFixture = `[
 {"oci":"0201-0202","citing":"doi:10.1234/citing omid:br/1","cited":"doi:10.5678/cited omid:br/2",
  "creation":"2018-02","timespan":"P1Y","journal_sc":"no","author_sc":"no"},
 {"oci":"0201-0203","citing":"doi:10.1234/citing","cited":"pmid:999"},
 {"oci":"bad","citing":"","cited":"doi:10.1/x"}]`

func TestOpenCitations_EdgesPreserveBothPIDForms(t *testing.T) {
	query := mustIdent(t, identity.SchemeDOI, "10.1234/citing")

	params, ok := OpenCitationsReferencesAdapter.CitationGraphProvider.EdgeQuery(query)
	if !ok || params["id"] != "doi:10.1234/citing" {
		t.Fatalf("OpenCitations needs a prefixed PID, got %v", params)
	}
	if _, ok := OpenCitationsReferencesAdapter.CitationGraphProvider.EdgeQuery(
		mustIdent(t, identity.SchemeOpenAlex, "W1")); ok {
		t.Error("an OpenAlex id must be unsupported at OpenCitations")
	}

	edges := OpenCitationsReferencesAdapter.CitationGraphProvider.Edges(query, []byte(openCitationsFixture), citAt)
	if len(edges) != 2 {
		t.Fatalf("an edge with an empty end must be skipped, got %d edges", len(edges))
	}
	if edges[0].ProviderEdgeID != "0201-0202" {
		t.Errorf("the OCI must survive as the provider edge id, got %q", edges[0].ProviderEdgeID)
	}
	// The full space-separated PID list survives verbatim, including the
	// OMID that has no registered scheme.
	if edges[0].Target.RawID != "doi:10.5678/cited omid:br/2" {
		t.Errorf("raw PID list lost: %q", edges[0].Target.RawID)
	}
	if endpointValue(edges[0].Target, identity.SchemeDOI) != "10.5678/cited" {
		t.Errorf("target DOI = %+v", edges[0].Target)
	}
	if endpointValue(edges[1].Target, identity.SchemePMID) != "999" {
		t.Errorf("a PMID-only end must still normalize, got %+v", edges[1].Target)
	}
	model, truncated, _ := OpenCitationsReferencesAdapter.CitationGraphProvider.EdgePagination(nil)
	if model != "none" || truncated {
		t.Errorf("OpenCitations v2 has no pagination envelope, got %q/%v", model, truncated)
	}
}

// ---------- Crossref ----------

const crossrefFixture = `{"message":{"DOI":"10.1234/citing","reference-count":3,"reference":[
  {"key":"ref1","DOI":"10.5678/cited","article-title":"A cited work"},
  {"key":"ref2","unstructured":"Smith J. Some Journal. 1999;12:34-56."},
  {"key":"ref3"}]}}`

func TestCrossref_ReferencesAndUnstructuredEntries(t *testing.T) {
	query := mustIdent(t, identity.SchemeDOI, "10.1234/citing")

	if OpenAlexReferencesAdapter.CitationGraphProvider.Direction() != citation.DirectionReferences {
		t.Fatal("fixture precondition")
	}
	if CrossrefReferencesAdapter.CitationGraphProvider.Direction() != citation.DirectionReferences {
		t.Error("Crossref serves outbound references only")
	}
	if _, ok := CrossrefReferencesAdapter.CitationGraphProvider.EdgeQuery(
		mustIdent(t, identity.SchemePMID, "999")); ok {
		t.Error("Crossref works lookup takes a DOI, so a PMID must be unsupported")
	}

	edges := CrossrefReferencesAdapter.CitationGraphProvider.Edges(query, []byte(crossrefFixture), citAt)
	if len(edges) != 3 {
		t.Fatalf("got %d edges, want 3", len(edges))
	}
	if endpointValue(edges[0].Target, identity.SchemeDOI) != "10.5678/cited" {
		t.Errorf("a deposited DOI must normalize, got %+v", edges[0].Target)
	}
	// An entry with no DOI is kept with its raw text and no identifier,
	// which keeps it visible without inventing a match.
	if len(edges[1].Target.Identifiers) != 0 {
		t.Errorf("an unstructured reference must carry no identifier, got %+v", edges[1].Target)
	}
	if edges[1].Target.RawID == "" {
		t.Error("an unstructured reference must keep its text")
	}
	// A publisher depositing a count larger than the list is a truncation
	// the consumer needs told about.
	model, truncated, _ := CrossrefReferencesAdapter.CitationGraphProvider.EdgePagination([]byte(crossrefFixture))
	if model != "none" {
		t.Errorf("pagination model = %q", model)
	}
	if truncated {
		t.Error("reference-count 3 with 3 entries is complete")
	}
	_, truncated, _ = CrossrefReferencesAdapter.CitationGraphProvider.EdgePagination(
		[]byte(`{"message":{"reference-count":40,"reference":[]}}`))
	if !truncated {
		t.Error("a deposited count with no list must report truncation")
	}
}

// Malformed bodies never panic and never invent edges.
func TestCitationAdapters_MalformedBodies(t *testing.T) {
	query := mustIdent(t, identity.SchemeDOI, "10.1234/x")
	bodies := [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`[]`), []byte(`{"results":null}`)}
	for _, id := range []string{
		"openalex-references", "openalex-cited-by",
		"semantic-scholar-references", "semantic-scholar-cited-by",
		"opencitations-references", "opencitations-cited-by",
		"crossref-references",
	} {
		cg := DefaultRegistry()[id].CitationGraphProvider
		for _, body := range bodies {
			if edges := cg.Edges(query, body, citAt); len(edges) != 0 {
				t.Errorf("%s invented %d edges from %q", id, len(edges), body)
			}
			_, _, _ = cg.EdgePagination(body)
		}
	}
}

func TestCitationAdapters_CapabilityReporting(t *testing.T) {
	if !OpenAlexReferencesAdapter.Supports(CapReferences) {
		t.Error("a references adapter must report the references capability")
	}
	if OpenAlexReferencesAdapter.Supports(CapCitedBy) {
		t.Error("a references adapter must not claim cited_by")
	}
	if !OpenAlexCitedByAdapter.Supports(CapCitedBy) {
		t.Error("a cited-by adapter must report the cited_by capability")
	}
	if !CrossrefReferencesAdapter.Supports(CapReferences) || CrossrefReferencesAdapter.Supports(CapCitedBy) {
		t.Error("Crossref must report references and never cited_by")
	}
	// The search adapters gained no citation capability.
	if OpenAlexWorksAdapter.Supports(CapReferences) || OpenAlexWorksAdapter.Supports(CapCitedBy) {
		t.Error("the search adapter must not claim citation capabilities")
	}
}
