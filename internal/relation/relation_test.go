package relation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)

func TestNormalizePredicate_PreservesUnknownTerm(t *testing.T) {
	p := NormalizePredicate("IsObsoletedBy")
	if p.ProviderTerm != "IsObsoletedBy" {
		t.Fatalf("provider term lost: %q", p.ProviderTerm)
	}
	if p.Recognized {
		t.Fatal("unmapped term reported as recognized")
	}
	if p.Normalized != "" {
		t.Fatalf("unmapped term got a normalized value %q", p.Normalized)
	}
}

func TestNormalizePredicate_KeepsProviderSpellingAlongsideNormalized(t *testing.T) {
	p := NormalizePredicate("IsSupplementTo")
	if p.ProviderTerm != "IsSupplementTo" {
		t.Fatalf("provider spelling rewritten: %q", p.ProviderTerm)
	}
	if !p.Recognized || p.Normalized != TermSupplementTo {
		t.Fatalf("got %+v", p)
	}
}

func TestRegisterPredicateTerm_IsAdditive(t *testing.T) {
	if p := NormalizePredicate("IsFundedBy"); p.Recognized {
		t.Fatal("term recognized before registration")
	}
	RegisterPredicateTerm("IsFundedBy", RelationTerm("funded_by"))
	defer delete(predicateTerms, "isfundedby")
	p := NormalizePredicate("IsFundedBy")
	if !p.Recognized || p.Normalized != "funded_by" {
		t.Fatalf("registration did not take: %+v", p)
	}
}

func TestObjectType_OpenSet(t *testing.T) {
	o := NewObject("SomeTypeNobodyRegistered", "10.5061/dryad.1")
	if o.Type != TypeUnknown {
		t.Fatalf("unmapped type resolved to %q", o.Type)
	}
	if o.TypeRecognized {
		t.Fatal("unmapped type reported as recognized")
	}
	if o.ProviderType != "SomeTypeNobodyRegistered" {
		t.Fatalf("provider type lost: %q", o.ProviderType)
	}
	if o.Key() != "doi:10.5061/dryad.1" {
		t.Fatalf("key = %q", o.Key())
	}
}

func TestObject_UnparseableIdentifierKeepsRaw(t *testing.T) {
	o := NewObject("Dataset", "NCT01234567")
	if len(o.Identifiers) != 0 {
		t.Fatalf("unexpected identifiers: %+v", o.Identifiers)
	}
	if o.RawID != "NCT01234567" {
		t.Fatalf("raw id lost: %q", o.RawID)
	}
	if o.Key() != "raw:nct01234567" {
		t.Fatalf("key = %q", o.Key())
	}
}

func TestNew_DerivationEmitsFeed402Lineage(t *testing.T) {
	r := New("datacite", "datacite:relatedIdentifiers",
		NewObject("Dataset", "10.5061/dryad.1"), "IsDerivedFrom",
		NewObject("Dataset", "10.5061/dryad.0"), at)
	if r.Lineage == nil {
		t.Fatal("derivation produced no lineage entry")
	}
	if !r.Lineage.Valid() {
		t.Fatalf("lineage entry invalid: %+v", r.Lineage)
	}
	if r.Lineage.DerivedObject != "doi:10.5061/dryad.1" {
		t.Fatalf("derived_object = %q", r.Lineage.DerivedObject)
	}
	if got := r.Lineage.Sources[0].DerivedObject; got != "doi:10.5061/dryad.0" {
		t.Fatalf("source = %q", got)
	}
}

func TestNew_NonDerivationHasNoLineage(t *testing.T) {
	r := New("datacite", "f", NewObject("Text", "10.1/a"), "IsSupplementTo",
		NewObject("Dataset", "10.2/b"), at)
	if r.Lineage != nil {
		t.Fatalf("unexpected lineage on a non-derivation: %+v", r.Lineage)
	}
}

func TestRelation_CarriesProviderAndTimestamp(t *testing.T) {
	r := New("datacite", "f", NewObject("Text", "10.1/a"), "Describes",
		NewObject("Dataset", "10.2/b"), at)
	if r.Provider != "datacite" {
		t.Fatalf("provider = %q", r.Provider)
	}
	if r.RetrievedAt != "2026-08-17T10:00:00Z" {
		t.Fatalf("retrieved_at = %q", r.RetrievedAt)
	}
	if !r.Valid() {
		t.Fatal("well-formed relation reported invalid")
	}
}

func TestRelation_InvalidWithoutAttribution(t *testing.T) {
	base := New("datacite", "f", NewObject("Text", "10.1/a"), "Describes",
		NewObject("Dataset", "10.2/b"), at)
	noProvider := base
	noProvider.Provider = ""
	noTime := base
	noTime.RetrievedAt = ""
	noTerm := base
	noTerm.Predicate.ProviderTerm = ""
	selfLink := New("datacite", "f", NewObject("Text", "10.1/a"), "Describes",
		NewObject("Text", "10.1/a"), at)
	for name, r := range map[string]Relation{
		"no provider": noProvider, "no timestamp": noTime,
		"no provider term": noTerm, "self link": selfLink,
	} {
		if r.Valid() {
			t.Fatalf("%s: reported valid", name)
		}
	}
}

func TestBuild_DeterministicAndDedupedPerProvider(t *testing.T) {
	subject := NewObject("Text", "10.1/a")
	r1 := New("datacite", "f", subject, "IsSupplementTo", NewObject("Dataset", "10.2/b"), at)
	r2 := New("crossref", "g", subject, "is-supplement-to", NewObject("", "10.2/b"), at)
	dup := r1

	first := Build(subject, []Relation{r1, r2, dup})
	second := Build(subject, []Relation{dup, r2, r1})
	if len(first.Relations) != 2 {
		t.Fatalf("want 2 relations after per-provider dedupe, got %d", len(first.Relations))
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("Build is not order-independent")
	}
	if len(first.Providers) != 2 {
		t.Fatalf("providers = %v", first.Providers)
	}
	if first.AbsenceNotice == "" {
		t.Fatal("absence notice missing")
	}
}

func TestBuild_IndexesUnrecognizedTerms(t *testing.T) {
	subject := NewObject("Text", "10.1/a")
	set := Build(subject, []Relation{
		New("datacite", "f", subject, "IsObsoletedBy", NewObject("Text", "10.2/b"), at),
		New("datacite", "f", subject, "IsSupplementTo", NewObject("Dataset", "10.3/c"), at),
	})
	if len(set.Relations) != 2 {
		t.Fatalf("an unrecognized term was dropped: %d relations", len(set.Relations))
	}
	if len(set.UnrecognizedTerms) != 1 || set.UnrecognizedTerms[0] != "IsObsoletedBy" {
		t.Fatalf("unrecognized_terms = %v", set.UnrecognizedTerms)
	}
}

func TestBuild_CollectsLineageInFeed402Order(t *testing.T) {
	subject := NewObject("Dataset", "10.1/a")
	set := Build(subject, []Relation{
		New("datacite", "f", subject, "IsDerivedFrom", NewObject("Dataset", "10.2/b"), at),
		New("datacite", "f", subject, "IsDerivedFrom", NewObject("Dataset", "10.3/c"), at),
		New("datacite", "f", subject, "Describes", NewObject("Text", "10.4/d"), at),
	})
	if len(set.Lineage) != 2 {
		t.Fatalf("want 2 lineage steps, got %d", len(set.Lineage))
	}
	for i, e := range set.Lineage {
		if e.Step != i {
			t.Fatalf("step %d numbered %d", i, e.Step)
		}
	}
}

// The gateway carries relations upstream providers assert about research
// objects. Relations over scientific content belong to a different
// repository, so no term here may name one.
func TestNoScientificDiscoveryVocabulary(t *testing.T) {
	banned := []string{"problem", "equation", "algorithm", "conjecture",
		"theorem", "quantum", "embedding", "similarity", "cluster"}
	for term := range predicateTerms {
		for _, b := range banned {
			if strings.Contains(term, b) {
				t.Fatalf("relation vocabulary contains discovery term %q", term)
			}
		}
	}
	for term := range objectTypeTerms {
		for _, b := range banned {
			if strings.Contains(term, b) {
				t.Fatalf("object-type vocabulary contains discovery term %q", term)
			}
		}
	}
}
