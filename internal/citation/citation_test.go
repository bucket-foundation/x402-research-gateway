package citation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

var at = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func doi(t *testing.T, raw string) identity.Identifier {
	t.Helper()
	id, ok := identity.New(identity.SchemeDOI, raw)
	if !ok {
		t.Fatalf("fixture DOI %q rejected", raw)
	}
	return id
}

func endpointDOI(t *testing.T, raw string) Endpoint {
	return Endpoint{Identifiers: []identity.Identifier{doi(t, raw)}, RawID: raw}
}

func TestEndpointKey_FallsBackToRaw(t *testing.T) {
	e := Endpoint{RawID: "Some Unstructured Reference String"}
	if got := e.Key(); got != "raw:some unstructured reference string" {
		t.Errorf("unparseable endpoint key = %q", got)
	}
	// An unparseable endpoint only ever matches itself.
	other := Endpoint{RawID: "A Different Reference"}
	if e.Key() == other.Key() {
		t.Error("two unstructured references must not share a key")
	}
}

func TestEndpointKeys_AreDeterministicAcrossSchemes(t *testing.T) {
	pmid, _ := identity.New(identity.SchemePMID, "12345")
	e := Endpoint{Identifiers: []identity.Identifier{doi(t, "10.1234/x"), pmid}}
	keys := e.Keys()
	if len(keys) != 2 {
		t.Fatalf("keys = %v", keys)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Fatal("Keys() must be sorted")
		}
	}
	if e.Key() != keys[0] {
		t.Errorf("Key() should be the lowest sorted key, got %q of %v", e.Key(), keys)
	}
}

// Two providers asserting the same citation produce two edges plus one
// equivalence. Neither edge is dropped or rewritten.
func TestEquivalences_AcrossProvidersOnly(t *testing.T) {
	src, tgt := endpointDOI(t, "10.1234/citing"), endpointDOI(t, "10.5678/cited")
	edges := []Edge{
		{Provider: "opencitations-references", Direction: DirectionReferences, Source: src, Target: tgt},
		{Provider: "crossref-references", Direction: DirectionReferences, Source: src, Target: tgt},
	}
	eq := Equivalences(edges, at)
	if len(eq) != 1 {
		t.Fatalf("expected one equivalence, got %+v", eq)
	}
	if len(eq[0].Edges) != 2 {
		t.Errorf("equivalence should list both edges, got %v", eq[0].Edges)
	}
	if len(eq[0].MatchedOn) != 1 || eq[0].MatchedOn[0] != string(identity.SchemeDOI) {
		t.Errorf("matched_on should name the DOI scheme, got %v", eq[0].MatchedOn)
	}
	if eq[0].Evidence.Kind != identity.EvidenceGatewayInferred ||
		eq[0].Evidence.Method != identity.MethodSharedIdentifier {
		t.Errorf("equivalence evidence = %+v", eq[0].Evidence)
	}
	if eq[0].Evidence.RetrievedAt == "" {
		t.Error("equivalence must carry a timestamp")
	}

	// One provider asserting the same edge twice is asserting two edges,
	// not an equivalence.
	same := []Edge{
		{Provider: "crossref-references", Source: src, Target: tgt},
		{Provider: "crossref-references", Source: src, Target: tgt},
	}
	if got := Equivalences(same, at); len(got) != 0 {
		t.Errorf("edges from one provider must not be equated, got %+v", got)
	}
}

// Unstructured endpoints never enter an equivalence: matching them would be
// a similarity claim, and equivalence is exact-identifier only.
func TestEquivalences_SkipUnstructuredEndpoints(t *testing.T) {
	edges := []Edge{
		{Provider: "a", Source: endpointDOI(t, "10.1234/x"), Target: Endpoint{RawID: "Smith 1999, some journal"}},
		{Provider: "b", Source: endpointDOI(t, "10.1234/x"), Target: Endpoint{RawID: "Smith 1999, some journal"}},
	}
	if got := Equivalences(edges, at); len(got) != 0 {
		t.Errorf("unstructured endpoints must not produce an equivalence, got %+v", got)
	}
}

// Providers disagreeing on the citation set: both views survive whole, with
// no winner and no fused set.
func TestBuild_ProviderDisagreementHasNoWinner(t *testing.T) {
	citing := endpointDOI(t, "10.1234/citing")
	shared := endpointDOI(t, "10.5678/shared")
	onlyOC := endpointDOI(t, "10.9999/only-opencitations")
	onlyCR := endpointDOI(t, "10.1111/only-crossref")

	edges := []Edge{
		{Provider: "opencitations-references", Direction: DirectionReferences, Source: citing, Target: shared},
		{Provider: "opencitations-references", Direction: DirectionReferences, Source: citing, Target: onlyOC},
		{Provider: "crossref-references", Direction: DirectionReferences, Source: citing, Target: shared},
		{Provider: "crossref-references", Direction: DirectionReferences, Source: citing, Target: onlyCR},
	}
	reports := []ProviderReport{
		{Provider: "opencitations-references", Consulted: true, Outcome: OutcomeOK, EdgeCount: 2},
		{Provider: "crossref-references", Consulted: true, Outcome: OutcomeOK, EdgeCount: 2},
		{Provider: "semantic-scholar-references", Consulted: true, Outcome: OutcomeOK, EdgeCount: 0},
		{Provider: "openalex-references", Outcome: OutcomeUnsupportedIdentifier},
	}
	res := Build(DirectionReferences, doi(t, "10.1234/citing"), edges, reports, at)

	if len(res.Edges) != 4 {
		t.Fatalf("every provider's edges must survive, got %d", len(res.Edges))
	}
	if len(res.Equivalences) != 1 {
		t.Fatalf("only the shared edge should be equated, got %+v", res.Equivalences)
	}
	// The edges each provider holds alone stay attributed and unpaired.
	for _, want := range []string{"10.9999/only-opencitations", "10.1111/only-crossref"} {
		found := false
		for _, e := range res.Edges {
			if e.Target.Identifiers[0].Value == want {
				found = true
			}
		}
		if !found {
			t.Errorf("edge unique to one provider was lost: %s", want)
		}
	}
	// A provider that answered with nothing and a provider that was never
	// asked are both reported, distinguishably.
	byProvider := map[string]ProviderReport{}
	for _, r := range res.ProvidersConsulted {
		byProvider[r.Provider] = r
	}
	if r := byProvider["semantic-scholar-references"]; !r.Consulted || r.Outcome != OutcomeOK || r.EdgeCount != 0 {
		t.Errorf("a consulted provider with no edges must read as ok/0, got %+v", r)
	}
	if r := byProvider["openalex-references"]; r.Consulted || r.Outcome != OutcomeUnsupportedIdentifier {
		t.Errorf("an unasked provider must read as not consulted, got %+v", r)
	}
	if res.AbsenceNotice != AbsenceNotice {
		t.Error("every result must restate that absence is not evidence")
	}
}

// Identifiers on both ends survive back to the forms the providers sent.
func TestBuild_EdgesReversibleToOriginalIdentifierForms(t *testing.T) {
	rawCiting := "https://doi.org/10.1234/CITING"
	rawCited := "doi:10.5678/Cited"
	citing, _ := identity.New(identity.SchemeDOI, rawCiting)
	cited, _ := identity.New(identity.SchemeDOI, rawCited)
	edges := []Edge{{
		Provider: "opencitations-references",
		Source:   Endpoint{Identifiers: []identity.Identifier{citing}, RawID: rawCiting},
		Target:   Endpoint{Identifiers: []identity.Identifier{cited}, RawID: rawCited},
	}}
	res := Build(DirectionReferences, citing, edges, nil, at)
	e := res.Edges[0]
	if e.Source.Identifiers[0].Raw != rawCiting || e.Target.Identifiers[0].Raw != rawCited {
		t.Errorf("original identifier forms lost: %+v", e)
	}
	if e.Source.RawID != rawCiting || e.Target.RawID != rawCited {
		t.Errorf("raw endpoint strings lost: %+v", e)
	}
	// Normalized forms agree across the case difference, which is what
	// makes the equivalence pass work.
	if e.Source.Identifiers[0].Value != "10.1234/citing" {
		t.Errorf("normalized value = %q", e.Source.Identifiers[0].Value)
	}
}

func TestBuild_DeterministicSerialization(t *testing.T) {
	edges := []Edge{
		{Provider: "z", Source: endpointDOI(t, "10.1234/b"), Target: endpointDOI(t, "10.1234/c")},
		{Provider: "a", Source: endpointDOI(t, "10.1234/b"), Target: endpointDOI(t, "10.1234/c")},
		{Provider: "m", Source: endpointDOI(t, "10.1234/b"), Target: endpointDOI(t, "10.1234/a")},
	}
	reports := []ProviderReport{{Provider: "z"}, {Provider: "a"}, {Provider: "m"}}
	first, _ := json.Marshal(Build(DirectionReferences, doi(t, "10.1234/b"), edges, reports, at))
	for i := 0; i < 20; i++ {
		next, _ := json.Marshal(Build(DirectionReferences, doi(t, "10.1234/b"), edges, reports, at))
		if string(first) != string(next) {
			t.Fatal("Build output is not deterministic")
		}
	}
	if !strings.Contains(string(first), `"providers_consulted"`) {
		t.Error("providers_consulted must always be serialized")
	}
}

func TestBuild_EmptyResultStillReportsProvidersAndNotice(t *testing.T) {
	res := Build(DirectionCitedBy, doi(t, "10.1234/x"), nil,
		[]ProviderReport{{Provider: "crossref-references", Outcome: OutcomeUnsupportedDirection}}, at)
	out, _ := json.Marshal(res)
	if !strings.Contains(string(out), `"edges":[]`) {
		t.Errorf("edges must serialize as an empty array, got %s", out)
	}
	if !strings.Contains(string(out), string(OutcomeUnsupportedDirection)) {
		t.Error("a provider not serving the direction must be reported as such")
	}
	if res.AbsenceNotice == "" {
		t.Error("absence notice missing from an empty result")
	}
}

func TestDirectionValid(t *testing.T) {
	if !DirectionReferences.Valid() || !DirectionCitedBy.Valid() {
		t.Error("both directions must validate")
	}
	if Direction("influences").Valid() {
		t.Error("an unknown direction must not validate")
	}
}
