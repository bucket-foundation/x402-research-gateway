// Package relation models links between research objects
// (x402-research-gateway#7).
//
// A paper has an underlying dataset, the software that produced it, a trial
// it reports, a correction issued against it, and supplementary material.
// Providers publish those links and every provider uses its own vocabulary
// for them. DataCite `relationType`, Crossref update types, and
// ClinicalTrials.gov reference types do not map onto each other, so this
// package stores the provider's own term as the record and treats any
// normalized term the gateway assigns as an annotation on top of it.
//
// Three rules hold everywhere here.
//
// The type sets are open. Object types and relation terms are extensible at
// runtime through RegisterObjectTypeTerm and RegisterPredicateTerm, and a
// term nobody registered is stored and returned with Recognized false
// rather than dropped. This is feed402 SPEC §2.3's unknown-field rule
// applied to relation vocabularies.
//
// Every relation carries its asserting provider and a retrieval timestamp.
// A relation with no provider is not a relation this package will hold.
//
// Derivation goes through feed402 lineage. A provider asserting that one
// object was derived from another produces a lineage entry
// (internal/lineage, feed402 SPEC §3.7) attached to the relation rather
// than a second derivation structure beside it.
//
// Scope: relations upstream providers assert about research objects.
// Relations a downstream system computes over scientific content (a work to
// a problem, an equation, or an algorithm) belong to a different repository
// and are not represented here.
package relation

import (
	"sort"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/lineage"
)

// ObjectType names what kind of research object an endpoint is. The set is
// open: RegisterObjectTypeTerm adds provider terms, and an unmapped term
// yields TypeUnknown with the provider's own string retained.
type ObjectType string

const (
	TypeWork         ObjectType = "work"
	TypePreprint     ObjectType = "preprint"
	TypeDataset      ObjectType = "dataset"
	TypeSoftware     ObjectType = "software"
	TypePatent       ObjectType = "patent"
	TypeTrial        ObjectType = "trial"
	TypeModel        ObjectType = "model"
	TypeCorrection   ObjectType = "correction"
	TypeSupplement   ObjectType = "supplement"
	TypeOrganization ObjectType = "organization"
	TypePerson       ObjectType = "person"
	TypeCollection   ObjectType = "collection"
	// TypeUnknown is what an unmapped provider term resolves to. It is a
	// statement that the gateway has no term for what the provider said,
	// never a statement about the object.
	TypeUnknown ObjectType = "unknown"
)

// objectTypeTerms maps a lowercased provider term to a gateway object type.
var objectTypeTerms = map[string]ObjectType{}

// RegisterObjectTypeTerm maps one provider's own object-type term onto a
// gateway type. Registering a term that already exists replaces it, so a
// provider package can tighten a mapping without this file knowing about
// the provider.
func RegisterObjectTypeTerm(providerTerm string, t ObjectType) {
	objectTypeTerms[strings.ToLower(strings.TrimSpace(providerTerm))] = t
}

// ObjectTypeFor resolves a provider term. ok reports whether a mapping
// existed; an unmapped term returns TypeUnknown.
func ObjectTypeFor(providerTerm string) (ObjectType, bool) {
	t, ok := objectTypeTerms[strings.ToLower(strings.TrimSpace(providerTerm))]
	if !ok {
		return TypeUnknown, false
	}
	return t, true
}

func init() {
	// DataCite resourceTypeGeneral.
	for term, t := range map[string]ObjectType{
		"dataset": TypeDataset, "software": TypeSoftware, "text": TypeWork,
		"journalarticle": TypeWork, "preprint": TypePreprint, "model": TypeModel,
		"collection": TypeCollection, "computationalnotebook": TypeSoftware,
		"workflow": TypeSoftware, "physicalobject": TypeUnknown,
	} {
		RegisterObjectTypeTerm(term, t)
	}
	// Crossref work types that appear as relation endpoints.
	for term, t := range map[string]ObjectType{
		"journal-article": TypeWork, "posted-content": TypePreprint,
		"dataset-crossref": TypeDataset, "peer-review": TypeWork,
		"proceedings-article": TypeWork,
	} {
		RegisterObjectTypeTerm(term, t)
	}
	// ClinicalTrials.gov and the integrity vocabularies.
	RegisterObjectTypeTerm("clinical-trial", TypeTrial)
	RegisterObjectTypeTerm("interventional", TypeTrial)
	RegisterObjectTypeTerm("observational", TypeTrial)
	RegisterObjectTypeTerm("correction", TypeCorrection)
	RegisterObjectTypeTerm("retraction", TypeCorrection)
	RegisterObjectTypeTerm("patent", TypePatent)
}

// Object is one end of a relation. It is addressed by whatever identifiers
// the provider expressed it with; nothing is fetched or resolved to build
// one, so an Object is exactly what the asserting provider said.
type Object struct {
	Type ObjectType `json:"type"`
	// ProviderType is the provider's own object-type term, verbatim, kept
	// whether or not a mapping existed for it.
	ProviderType string `json:"provider_type,omitempty"`
	// TypeRecognized reports whether ProviderType mapped to a gateway type.
	TypeRecognized bool                  `json:"type_recognized"`
	Identifiers    []identity.Identifier `json:"identifiers,omitempty"`
	// RawID is the identifier string the provider used, before any
	// normalization, kept even when no scheme claimed it.
	RawID        string `json:"raw_id,omitempty"`
	CanonicalURL string `json:"canonical_url,omitempty"`
}

// NewObject builds an endpoint from a provider's own type term and
// identifier string. An identifier no scheme recognizes is retained in
// RawID and carries no normalized form, which is the accurate answer rather
// than a guess.
func NewObject(providerType, rawID string) Object {
	o := Object{ProviderType: strings.TrimSpace(providerType), RawID: strings.TrimSpace(rawID)}
	o.Type, o.TypeRecognized = ObjectTypeFor(providerType)
	if id, ok := identity.Parse(rawID); ok {
		o.Identifiers = []identity.Identifier{id}
	}
	return o
}

// WithType returns a copy carrying an explicit gateway type, for a provider
// that names the object kind out of band rather than in a type field.
func (o Object) WithType(t ObjectType) Object {
	o.Type = t
	if o.ProviderType == "" {
		o.TypeRecognized = true
	}
	return o
}

// Key is the endpoint's exact-match key: the lowest sorted identifier key,
// or the raw string when no identifier parsed. Deterministic, so two
// providers naming one object by the same DOI produce the same key.
func (o Object) Key() string {
	keys := make([]string, 0, len(o.Identifiers))
	for _, id := range o.Identifiers {
		if id.Value != "" {
			keys = append(keys, id.Key())
		}
	}
	if len(keys) == 0 {
		return "raw:" + strings.ToLower(o.RawID)
	}
	sort.Strings(keys)
	return keys[0]
}

// RelationTerm is a gateway-normalized relation term. Open set, extended
// through RegisterPredicateTerm.
type RelationTerm string

const (
	TermDescribes      RelationTerm = "describes"
	TermDescribedBy    RelationTerm = "described_by"
	TermSupplementTo   RelationTerm = "supplement_to"
	TermSupplementedBy RelationTerm = "supplemented_by"
	TermDerivedFrom    RelationTerm = "derived_from"
	TermSourceOf       RelationTerm = "source_of"
	TermCorrects       RelationTerm = "corrects"
	TermCorrectedBy    RelationTerm = "corrected_by"
	TermRetracts       RelationTerm = "retracts"
	TermRetractedBy    RelationTerm = "retracted_by"
	TermWithdraws      RelationTerm = "withdraws"
	TermVersionOf      RelationTerm = "version_of"
	TermHasVersion     RelationTerm = "has_version"
	TermPreprintOf     RelationTerm = "preprint_of"
	TermHasPreprint    RelationTerm = "has_preprint"
	TermIdenticalTo    RelationTerm = "identical_to"
	TermPartOf         RelationTerm = "part_of"
	TermHasPart        RelationTerm = "has_part"
	TermDocuments      RelationTerm = "documents"
	TermCompiles       RelationTerm = "compiles"
	TermReportsTrial   RelationTerm = "reports_trial"
	TermResultsFrom    RelationTerm = "results_from"
	TermCites          RelationTerm = "cites"
	TermCitedBy        RelationTerm = "cited_by"
	TermRequires       RelationTerm = "requires"
	TermContinues      RelationTerm = "continues"
)

var predicateTerms = map[string]RelationTerm{}

// RegisterPredicateTerm maps one provider's relation term onto a gateway
// term. The provider's own string always survives on the Predicate, so a
// mapping added later never rewrites what a provider said.
func RegisterPredicateTerm(providerTerm string, t RelationTerm) {
	predicateTerms[strings.ToLower(strings.TrimSpace(providerTerm))] = t
}

func init() {
	// DataCite relationType, the fullest vocabulary of the three seeded
	// here. Terms with no gateway equivalent stay unmapped on purpose:
	// IsObsoletedBy is not a withdrawal and IsCitedBy is a citation edge
	// that internal/citation already models with its own provenance.
	for term, t := range map[string]RelationTerm{
		"describes": TermDescribes, "isdescribedby": TermDescribedBy,
		"issupplementto": TermSupplementTo, "issupplementedby": TermSupplementedBy,
		"isderivedfrom": TermDerivedFrom, "issourceof": TermSourceOf,
		"isidenticalto": TermIdenticalTo,
		"isversionof":   TermVersionOf, "hasversion": TermHasVersion,
		"ispreviousversionof": TermHasVersion, "isnewversionof": TermVersionOf,
		"ispreprintof": TermPreprintOf,
		"ispartof":     TermPartOf, "haspart": TermHasPart,
		"documents": TermDocuments, "isdocumentedby": TermDescribedBy,
		"compiles": TermCompiles, "iscompiledby": TermResultsFrom,
		"requires": TermRequires, "iscontinuedby": TermContinues,
		"cites": TermCites, "iscitedby": TermCitedBy,
	} {
		RegisterPredicateTerm(term, t)
	}
	// Crossref update types, the integrity vocabulary.
	for term, t := range map[string]RelationTerm{
		"correction": TermCorrects, "corrigendum": TermCorrects,
		"erratum": TermCorrects, "addendum": TermCorrects,
		"retraction": TermRetracts, "withdrawal": TermWithdraws,
		"removal": TermWithdraws,
	} {
		RegisterPredicateTerm(term, t)
	}
	// Crossref relation-block terms, which are hyphenated rather than
	// camel-cased, so they are separate keys from the DataCite spellings.
	for term, t := range map[string]RelationTerm{
		"is-supplement-to": TermSupplementTo, "has-preprint": TermHasPreprint,
		"is-preprint-of": TermPreprintOf, "is-derived-from": TermDerivedFrom,
		"has-related-material": TermDescribes, "is-part-of": TermPartOf,
		"has-part": TermHasPart, "is-same-as": TermIdenticalTo,
	} {
		RegisterPredicateTerm(term, t)
	}
	// ClinicalTrials.gov reference types.
	RegisterPredicateTerm("result", TermResultsFrom)
	RegisterPredicateTerm("derived", TermResultsFrom)
	RegisterPredicateTerm("background", TermCites)
}

// Predicate is the relation term itself: what the provider called it, plus
// whatever the gateway could normalize it to. ProviderTerm is the record;
// Normalized is the annotation.
type Predicate struct {
	ProviderTerm string       `json:"provider_term"`
	Normalized   RelationTerm `json:"normalized_term,omitempty"`
	Recognized   bool         `json:"recognized"`
}

// NormalizePredicate builds a Predicate from a provider's own term. An
// unrecognized term produces a Predicate that carries the term and reports
// Recognized false, which is what a consumer needs to decide for itself.
func NormalizePredicate(providerTerm string) Predicate {
	p := Predicate{ProviderTerm: strings.TrimSpace(providerTerm)}
	if t, ok := predicateTerms[strings.ToLower(p.ProviderTerm)]; ok {
		p.Normalized, p.Recognized = t, true
	}
	return p
}

// Relation is one provider's assertion that Subject stands in some relation
// to Object.
type Relation struct {
	Subject   Object    `json:"subject"`
	Predicate Predicate `json:"predicate"`
	Object    Object    `json:"object"`
	// Provider is the route/adapter id that asserted this relation.
	Provider string `json:"provider"`
	// SourceField names the upstream field the assertion was read out of,
	// e.g. "datacite:relatedIdentifiers", so a consumer can audit it.
	SourceField string `json:"source_field,omitempty"`
	RetrievedAt string `json:"retrieved_at"`
	// Annotations carry per-relation facts a provider publishes that this
	// model has no typed field for. Keys are the provider's own field
	// names.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Lineage is set when the provider asserted a derivation. It is a
	// feed402 SPEC §3.7 entry rather than a second derivation model.
	Lineage *lineage.Entry `json:"lineage,omitempty"`
}

// ID addresses this relation inside a Set.
func (r Relation) ID() string {
	return r.Provider + "|" + r.Subject.Key() + "|" + strings.ToLower(r.Predicate.ProviderTerm) + "|" + r.Object.Key()
}

// Valid reports whether the relation carries the attribution this package
// requires: both endpoints addressable, a provider term, an asserting
// provider, and a retrieval timestamp.
func (r Relation) Valid() bool {
	if r.Provider == "" || r.RetrievedAt == "" || r.Predicate.ProviderTerm == "" {
		return false
	}
	subj, obj := r.Subject.Key(), r.Object.Key()
	return subj != "raw:" && obj != "raw:" && subj != obj
}

// derivationTerms are the normalized terms that mean "this object was
// produced from that one." A relation carrying one of them gets a feed402
// lineage entry.
var derivationTerms = map[RelationTerm]bool{
	TermDerivedFrom: true, TermResultsFrom: true, TermVersionOf: true,
	TermCompiles: true,
}

// New builds a relation, stamping the retrieval time and attaching a
// feed402 lineage entry when the provider's term asserts a derivation.
func New(provider, sourceField string, subject Object, providerTerm string, object Object, at time.Time) Relation {
	r := Relation{
		Subject:     subject,
		Predicate:   NormalizePredicate(providerTerm),
		Object:      object,
		Provider:    provider,
		SourceField: sourceField,
		RetrievedAt: at.UTC().Format(time.RFC3339),
	}
	if derivationTerms[r.Predicate.Normalized] {
		e := lineage.Stamp(lineage.Entry{
			DerivedObject:  subject.Key(),
			Sources:        []lineage.Source{lineage.ObjectSource(object.Key())},
			Transformation: lineage.TransformProviderAssertedDerivation,
			Software:       provider,
			Notes: "derivation asserted by " + provider + " as " +
				r.Predicate.ProviderTerm + "; the gateway performed no transformation",
		}, at)
		r.Lineage = &e
	}
	return r
}

// AbsenceNotice is emitted in every relation response. A relation set is
// what the consulted providers published, and no more than that.
const AbsenceNotice = "A relation absent from this set is a relation no consulted provider published. " +
	"It does not follow that the relation does not exist. Provider relation vocabularies differ and " +
	"coverage of dataset, software, trial, and correction links is uneven. Provider terms are carried " +
	"verbatim in predicate.provider_term; normalized_term is this gateway's annotation and is absent " +
	"when it has no term for what the provider said."

// Set is a relation query's answer.
type Set struct {
	// Subject is the object the query started from.
	Subject   Object     `json:"subject"`
	Relations []Relation `json:"relations"`
	// Providers lists the contributing providers, sorted.
	Providers []string `json:"providers"`
	// UnrecognizedTerms lists the provider terms in this set that the
	// gateway has no normalized term for, sorted. They are present in the
	// relations; this is the index of them, so a consumer can see at a
	// glance where its own vocabulary work is needed.
	UnrecognizedTerms []string `json:"unrecognized_terms,omitempty"`
	// Lineage collects every derivation step in this set, renumbered, in
	// the feed402 §3.7 array form an envelope carries at top level.
	Lineage       []lineage.Entry `json:"lineage,omitempty"`
	AbsenceNotice string          `json:"absence_notice"`
}

// Build assembles a Set. Invalid relations are dropped, exact duplicates
// from one provider collapse, and two providers asserting the same link
// both survive as separate relations, because a provider agreement is a
// fact about two providers rather than one relation.
func Build(subject Object, rels []Relation) Set {
	seen := map[string]bool{}
	out := make([]Relation, 0, len(rels))
	providerSet, unknownSet := map[string]bool{}, map[string]bool{}
	for _, r := range rels {
		if !r.Valid() || seen[r.ID()] {
			continue
		}
		seen[r.ID()] = true
		out = append(out, r)
		providerSet[r.Provider] = true
		if !r.Predicate.Recognized {
			unknownSet[r.Predicate.ProviderTerm] = true
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })

	var steps []lineage.Entry
	for _, r := range out {
		if r.Lineage != nil {
			steps = append(steps, *r.Lineage)
		}
	}

	return Set{
		Subject:           subject,
		Relations:         out,
		Providers:         sortedKeys(providerSet),
		UnrecognizedTerms: sortedKeys(unknownSet),
		Lineage:           lineage.Number(steps),
		AbsenceNotice:     AbsenceNotice,
	}
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
