// Vocabulary capability layer (x402-research-gateway#14, #15).
//
// The gateway can find papers about a concept and could not, before this,
// resolve the concept itself: what a term means, its broader/narrower
// terms, what it used to be called, which vocabulary release defined it,
// what it maps to in another vocabulary. A thesaurus, a classification
// scheme, an OWL ontology, a nomenclature, and a document-semantics
// standard are six different kinds of object with six different access
// patterns; forcing all of them into one shape would fabricate structure
// (most damagingly, coercing a thesaurus's skos:broader into an
// ontological subClassOf, which is a stronger claim than the source ever
// asserted).
//
// Concept is the common access surface: a normalized view plus the native
// serialization, untouched, alongside it. Each capability below is its own
// interface, following the Searcher/Fetcher/Paginator convention already in
// this package, so a provider implements exactly the operations its
// upstream supports and a caller learns what's unsupported from a type
// assertion, not a runtime error.
package provider

import "encoding/json"

// Concept is one vocabulary term or concept, normalized. Every field a
// vocabulary does not publish stays zero; nothing here is inferred or
// fabricated to fill the schema.
type Concept struct {
	// ID is the provider-local concept identifier, in the source's own
	// notation (a MeSH descriptor "D017209", a GO term IRI, an MSC code).
	ID        string
	PrefLabel string
	// PrefLabelLanguage is the BCP-47 language tag the source itself
	// published for PrefLabel (x402-research-gateway#21), where the
	// source states one. Empty means the source did not publish a
	// language, never "English": there is no field here that designates
	// an English form as canonical.
	PrefLabelLanguage string
	// AltLabels are synonyms currently in use, distinct from
	// HistoricalAliases, which are labels the concept was known by in a
	// prior release and no longer carries as an active synonym.
	AltLabels  []string
	Definition string

	// SourceRelease is the vocabulary edition or release that produced this
	// response (#15): MeSH's annual release year, an OBO ontology's dated
	// version, an MSC edition. Every Concept a provider returns carries
	// this, so two responses with different data are distinguishable by
	// release rather than looking like an inconsistency.
	SourceRelease string

	// Temporal / lifecycle fields (#15). A vocabulary that does not publish
	// one of these leaves it empty; an empty field is a gap in what the
	// source discloses, not evidence the concept was always current.
	ValidFrom    string // YYYY or YYYY-MM-DD, source's own precision
	ValidUntil   string
	IntroducedIn string // release identifier the concept first appeared in
	DeprecatedIn string // release identifier it was withdrawn in, if any

	// HistoricalAliases are labels this concept was indexed under in a
	// prior release (MeSH's previousIndexing: "Cell Survival (1972-1992)"
	// for what is now "Apoptosis"). A query written against the old label
	// still has somewhere to resolve.
	HistoricalAliases []string

	// SupersededBy, Predecessor, and Successor are directional pointers and
	// never imply equivalence: a term a source marks as superseded by
	// another is not necessarily the same concept, and a source publishing
	// a partial or one-to-many mapping keeps that shape here rather than
	// being collapsed into a single "equals" pointer. Each carries every
	// concept ID the source names, in the source's own cardinality.
	SupersededBy []string
	Predecessor  []string
	Successor    []string
	// Deprecated reports the source's own obsolescence flag (GO's
	// is_obsolete, MeSH's active=false), independent of whether a
	// replacement is known: a term can be deprecated with no successor
	// recorded, which is itself a fact worth keeping distinct from "still
	// current."
	Deprecated bool

	// Native preserves the source's own serialization verbatim: SKOS,
	// OWL/OBO JSON, MeSH's JSON-LD, a CIF dictionary entry. Normalization
	// above is additive; Native is never reconstructed from the normalized
	// fields; it is what the upstream actually sent.
	Native json.RawMessage
	// NativeFormat names the shape of Native: "obo-owl-json", "mesh-jsonld",
	// "skos-rdf", etc., so a caller knows how to parse it without guessing
	// from the provider ID.
	NativeFormat string

	// Labels carries every language/script-tagged label this concept's
	// source published beyond PrefLabel (x402-research-gateway#21): a
	// multilingual vocabulary (AGROVOC, UNESCO Thesaurus, MeSH
	// translations, Getty) publishes labels in many languages, and a
	// response carrying only PrefLabel has discarded most of what the
	// source stated. Kind is FormSynonym for a same-concept label in
	// another language; PrefLabel itself is never duplicated here unless
	// the source names a language/script for it this field can carry and
	// PrefLabel cannot.
	Labels []LocalizedForm
}

// ConceptMapping is one published cross-vocabulary correspondence, kept
// exactly as narrow as the source states it: exact, close, broad, narrow,
// or related, per the SKOS mapping-relation vocabulary most sources that
// publish mappings already use. The gateway serves what a vocabulary
// publishes; it computes no mapping of its own (#14 non-goal).
type ConceptMapping struct {
	TargetVocabulary string
	TargetConceptID  string
	// MappingType is one of skos:exactMatch, closeMatch, broadMatch,
	// narrowMatch, relatedMatch, or the source's own term for the relation
	// if it does not use SKOS. Empty means the source asserted a
	// correspondence without characterizing its strength, which is
	// information too and is preserved rather than guessed at.
	MappingType string
}

// VocabularyRelease identifies one version or edition of a vocabulary.
// Returned by CurrentReleaseProvider and echoed on every Concept via
// SourceRelease, so a caller asking for a release the gateway does not hold
// gets an explicit "not available" rather than a silent substitution with
// whatever the current release happens to be.
type VocabularyRelease struct {
	Release string
	Notes   string
}

// TermSearcher looks up concepts by free-text label. release, if non-empty,
// asks for that specific vocabulary edition; empty means the provider's
// current release. A provider asked for a release it does not hold returns
// ok=false, never a silent fallback to the current one.
type TermSearcher interface {
	SearchTerms(query, release string) (concepts []Concept, ok bool)
}

// ConceptGetter fetches one concept by its provider-local ID.
type ConceptGetter interface {
	GetConcept(id, release string) (Concept, bool)
}

// BroaderNarrowerProvider reports a concept's position in its hierarchy.
// Broader and Narrower are separate methods, not one "related" call: a
// vocabulary can hold one direction and not the other (a thesaurus without
// a populated top term, an ontology root with no parent), and collapsing
// them would erase that.
type BroaderNarrowerProvider interface {
	Broader(id, release string) ([]Concept, bool)
	Narrower(id, release string) ([]Concept, bool)
}

// RelatedTermProvider reports non-hierarchical associative relations
// (SKOS related, an ontology's non-subsumption object properties).
type RelatedTermProvider interface {
	Related(id, release string) ([]Concept, bool)
}

// SynonymProvider reports a concept's current alternate labels. Kept
// separate from Concept.AltLabels being populated on GetConcept because a
// provider's search/browse endpoints and its synonym endpoint are
// routinely different upstream calls with different rate limits.
type SynonymProvider interface {
	Synonyms(id, release string) ([]string, bool)
}

// MappingProvider reports published cross-vocabulary correspondences. It
// never computes an alignment the source did not assert.
type MappingProvider interface {
	Mappings(id string) ([]ConceptMapping, bool)
}

// HistoricalTermProvider retrieves a concept as it existed in a superseded
// vocabulary or prior release. This is the core operation #15 exists for:
// PACS-coded literature stays resolvable through PhySH's historical
// terminology, a 2005 MeSH heading resolves through its historical alias,
// a deprecated GO term stays queryable with its replacement pointer.
type HistoricalTermProvider interface {
	HistoricalTerms(id string) ([]Concept, bool)
}

// DeprecatedTermProvider lists concepts a release has withdrawn, for a
// caller building a migration or auditing what changed between releases.
type DeprecatedTermProvider interface {
	DeprecatedTerms(release string) ([]Concept, bool)
}

// CurrentReleaseProvider reports which vocabulary release a provider is
// currently serving, independent of any single concept lookup.
type CurrentReleaseProvider interface {
	CurrentRelease() (VocabularyRelease, bool)
}
