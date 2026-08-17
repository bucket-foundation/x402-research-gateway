package identity

import (
	"encoding/json"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func testResolver() *Resolver {
	return &Resolver{Now: func() time.Time { return fixedTime }}
}

func mustID(t *testing.T, s Scheme, raw string) Identifier {
	t.Helper()
	id, ok := New(s, raw)
	if !ok {
		t.Fatalf("fixture identifier %q rejected under %s", raw, s)
	}
	return id
}

func relBetween(g Graph, a, b string) *Relation {
	for i := range g.Relations {
		r := g.Relations[i]
		if (r.From == a && r.To == b) || (r.From == b && r.To == a) {
			return &g.Relations[i]
		}
	}
	return nil
}

// A preprint carrying its published DOI, and the published record from
// another provider. Both nodes must survive, linked by the shared DOI.
func TestResolve_PreprintWithPublishedDOI(t *testing.T) {
	doi := "10.1038/s41586-020-2649-2"
	records := []SourceRecord{
		{
			Provider:         "arxiv",
			ProviderRecordID: "2006.10256",
			CanonicalURL:     "https://arxiv.org/abs/2006.10256",
			Identifiers: []Identifier{
				mustID(t, SchemeArXiv, "arXiv:2006.10256"),
				mustID(t, SchemeDOI, "https://doi.org/"+doi),
			},
			Title: "Array programming with NumPy",
			Raw:   json.RawMessage(`{"id":"2006.10256"}`),
		},
		{
			Provider:         "crossref",
			ProviderRecordID: doi,
			CanonicalURL:     "https://doi.org/" + doi,
			Identifiers:      []Identifier{mustID(t, SchemeDOI, doi)},
			Title:            "Array programming with NumPy",
			Raw:              json.RawMessage(`{"DOI":"` + doi + `"}`),
		},
	}
	g := testResolver().Resolve(records)

	if len(g.Nodes) != 2 {
		t.Fatalf("both records must remain addressable, got %d nodes", len(g.Nodes))
	}
	rel := relBetween(g, "arxiv:2006.10256", "crossref:"+doi)
	if rel == nil {
		t.Fatal("expected a relation across the shared DOI")
	}
	if rel.Type != RelSameWork {
		t.Errorf("shared exact DOI should give same_work, got %s", rel.Type)
	}
	if rel.Evidence.Kind != EvidenceGatewayInferred || rel.Evidence.Method != MethodSharedIdentifier {
		t.Errorf("evidence should be gateway-inferred shared identifier, got %+v", rel.Evidence)
	}
	if rel.Evidence.Detail != string(SchemeDOI) {
		t.Errorf("evidence detail should name the matching scheme, got %q", rel.Evidence.Detail)
	}
	if rel.Evidence.RetrievedAt != fixedTime.Format(time.RFC3339) {
		t.Errorf("relation must carry a timestamp, got %q", rel.Evidence.RetrievedAt)
	}
	if string(g.Nodes[0].Raw) != `{"id":"2006.10256"}` {
		t.Error("raw provider record must be preserved next to the normalized identity")
	}
	if len(g.Providers) != 2 || g.Providers[0] != "arxiv" || g.Providers[1] != "crossref" {
		t.Errorf("providers should be listed sorted, got %v", g.Providers)
	}
}

// Three arXiv versions of one paper. Each version stays independently
// addressable and relates by version_of, never same_work.
func TestResolve_MultiVersionArXiv(t *testing.T) {
	var records []SourceRecord
	for _, v := range []string{"v1", "v2", "v3"} {
		records = append(records, SourceRecord{
			Provider:         "arxiv",
			ProviderRecordID: "2101.00001" + v,
			Identifiers:      []Identifier{mustID(t, SchemeArXiv, "arXiv:2101.00001"+v)},
			Title:            "On a thing",
		})
	}
	g := testResolver().Resolve(records)
	if len(g.Nodes) != 3 {
		t.Fatalf("every version must remain addressable, got %d", len(g.Nodes))
	}
	if len(g.Relations) != 3 {
		t.Fatalf("expected 3 pairwise version relations, got %d", len(g.Relations))
	}
	for _, r := range g.Relations {
		if r.Type != RelVersionOf {
			t.Errorf("version pairs must relate as version_of, got %s", r.Type)
		}
		if r.Evidence.Method != MethodArXivVersion {
			t.Errorf("expected version-suffix method, got %q", r.Evidence.Method)
		}
	}
	// version_of points from the lower version to the higher one.
	r := relBetween(g, "arxiv:2101.00001v1", "arxiv:2101.00001v3")
	if r == nil || r.From != "arxiv:2101.00001v1" {
		t.Errorf("version_of should point from v1 to v3, got %+v", r)
	}
}

// One work seen by four providers, glued by two different identifier
// schemes. Every provider record survives and the graph is fully connected.
func TestResolve_WorkAcrossFourProviders(t *testing.T) {
	doi := "10.1016/j.cell.2019.01.001"
	records := []SourceRecord{
		{Provider: "crossref", ProviderRecordID: doi,
			Identifiers: []Identifier{mustID(t, SchemeDOI, doi)}},
		{Provider: "openalex-works", ProviderRecordID: "W2741809807",
			Identifiers: []Identifier{
				mustID(t, SchemeOpenAlex, "https://openalex.org/W2741809807"),
				mustID(t, SchemeDOI, "https://doi.org/"+doi),
				mustID(t, SchemePMID, "30712867"),
			}},
		{Provider: "pubmed-search", ProviderRecordID: "30712867",
			Identifiers: []Identifier{mustID(t, SchemePMID, "30712867")}},
		{Provider: "semantic-scholar-search", ProviderRecordID: "abc",
			Identifiers: []Identifier{mustID(t, SchemeDOI, doi)}},
	}
	g := testResolver().Resolve(records)
	if len(g.Nodes) != 4 {
		t.Fatalf("got %d nodes, want 4", len(g.Nodes))
	}
	if len(g.Providers) != 4 {
		t.Fatalf("got providers %v, want 4", g.Providers)
	}
	// DOI links crossref/openalex/s2 pairwise (3 edges); PMID links
	// openalex/pubmed (1 edge).
	if len(g.Relations) != 4 {
		t.Fatalf("got %d relations, want 4: %+v", len(g.Relations), g.Relations)
	}
	if r := relBetween(g, "openalex-works:W2741809807", "pubmed-search:30712867"); r == nil ||
		r.Evidence.Detail != string(SchemePMID) {
		t.Errorf("PMID edge missing or mis-attributed: %+v", r)
	}
}

// Two different papers with near-identical titles. The gateway may flag a
// possibility, and must never call it same_work.
func TestResolve_PlausibleButWrongFuzzyMatch(t *testing.T) {
	records := []SourceRecord{
		{Provider: "a", ProviderRecordID: "1",
			Title: "Deep learning for protein structure prediction", Authors: []string{"Jane Doe"}, Year: 2019},
		{Provider: "b", ProviderRecordID: "2",
			Title: "Deep learning for protein structure prediction", Authors: []string{"Rui Chen"}, Year: 2023},
	}
	g := testResolver().Resolve(records)
	for _, r := range g.Relations {
		if r.Type == RelSameWork {
			t.Fatalf("similarity must never produce same_work: %+v", r)
		}
	}
	// Same titles, different authors, four years apart: the year gap
	// discount must keep this under the threshold entirely.
	if len(g.Relations) != 0 {
		t.Errorf("expected no relation for a year-mismatched pair, got %+v", g.Relations)
	}

	// A closer pair does get flagged, as possible_same_work only.
	records[1].Authors = []string{"Jane Doe"}
	records[1].Year = 2019
	g = testResolver().Resolve(records)
	if len(g.Relations) != 1 || g.Relations[0].Type != RelPossibleSameWork {
		t.Fatalf("expected exactly one possible_same_work, got %+v", g.Relations)
	}
	ev := g.Relations[0].Evidence
	if ev.Kind != EvidenceGatewayInferred || ev.Method != MethodTitleAuthor || ev.Score <= 0 {
		t.Errorf("fuzzy relation must be gateway-inferred with a score, got %+v", ev)
	}
}

// Graph.Add is the enforcement point: no caller can promote a similarity
// match to same_work.
func TestGraphAdd_NoAutoPromotion(t *testing.T) {
	g := Graph{}
	g.Add(Relation{
		From: "a:1", To: "b:2", Type: RelSameWork,
		Evidence: GatewayInferred(MethodTitleAuthor, "", 0.99, fixedTime),
	})
	if len(g.Relations) != 1 || g.Relations[0].Type != RelPossibleSameWork {
		t.Fatalf("similarity same_work must be demoted, got %+v", g.Relations)
	}
	// Invalid evidence is dropped rather than stored.
	g.Add(Relation{From: "a:1", To: "b:3", Type: RelSameWork, Evidence: Evidence{Kind: "invented"}})
	if len(g.Relations) != 1 {
		t.Errorf("relation with invalid evidence should be dropped, got %+v", g.Relations)
	}
	// Self-relations and duplicates are dropped.
	g.Add(Relation{From: "a:1", To: "a:1", Type: RelSameWork, Evidence: ProviderAsserted("x", fixedTime)})
	g.Add(g.Relations[0])
	if len(g.Relations) != 1 {
		t.Errorf("self and duplicate relations should be dropped, got %+v", g.Relations)
	}
}

// A provider's own assertion passes through with its attribution intact and
// is distinguishable from anything the gateway computed.
func TestResolve_ProviderAssertionsSurviveAndStayDistinguishable(t *testing.T) {
	records := []SourceRecord{
		{
			Provider: "crossref", ProviderRecordID: "10.1234/pub",
			Identifiers: []Identifier{mustID(t, SchemeDOI, "10.1234/pub")},
			AssertedRelations: []Relation{{
				From: "crossref:10.1234/pub", To: "arxiv:2101.00001",
				Type: RelPreprintOf, Evidence: ProviderAsserted("crossref", fixedTime),
			}},
		},
		{Provider: "arxiv", ProviderRecordID: "2101.00001",
			Identifiers: []Identifier{mustID(t, SchemeArXiv, "2101.00001")}},
	}
	g := testResolver().Resolve(records)
	if len(g.Relations) != 1 {
		t.Fatalf("expected the asserted relation only, got %+v", g.Relations)
	}
	r := g.Relations[0]
	if r.Type != RelPreprintOf || r.Evidence.Kind != EvidenceProviderAsserted || r.Evidence.Provider != "crossref" {
		t.Errorf("provider assertion mangled: %+v", r)
	}
	if r.Evidence.Method != "" {
		t.Error("a provider-asserted relation must not carry an inference method")
	}
}

// Disagreement is data: two providers asserting contradictory relations
// both appear, with no authority model picking a winner.
func TestResolve_DisagreementIsRetained(t *testing.T) {
	records := []SourceRecord{
		{Provider: "p1", ProviderRecordID: "x", AssertedRelations: []Relation{{
			From: "p1:x", To: "p2:y", Type: RelPreprintOf, Evidence: ProviderAsserted("p1", fixedTime)}}},
		{Provider: "p2", ProviderRecordID: "y", AssertedRelations: []Relation{{
			From: "p1:x", To: "p2:y", Type: RelTranslationOf, Evidence: ProviderAsserted("p2", fixedTime)}}},
	}
	g := testResolver().Resolve(records)
	if len(g.Relations) != 2 {
		t.Fatalf("contradicting assertions must both survive, got %+v", g.Relations)
	}
}

// Two runs over the same input must serialize identically.
func TestResolve_Deterministic(t *testing.T) {
	records := []SourceRecord{
		{Provider: "a", ProviderRecordID: "1", Identifiers: []Identifier{mustID(t, SchemeDOI, "10.1234/x")}},
		{Provider: "b", ProviderRecordID: "2", Identifiers: []Identifier{mustID(t, SchemeDOI, "10.1234/x")}},
		{Provider: "c", ProviderRecordID: "3", Identifiers: []Identifier{mustID(t, SchemeDOI, "10.1234/x")}},
	}
	first, _ := json.Marshal(testResolver().Resolve(records))
	for i := 0; i < 20; i++ {
		next, _ := json.Marshal(testResolver().Resolve(records))
		if string(first) != string(next) {
			t.Fatal("resolution output is not deterministic across runs")
		}
	}
}

func TestSimilarity_ThinMetadataScoresZero(t *testing.T) {
	if s := Similarity(SourceRecord{}, SourceRecord{Title: "something"}); s != 0 {
		t.Errorf("a record with no title must score 0, got %v", s)
	}
}
