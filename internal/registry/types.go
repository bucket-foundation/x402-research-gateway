// Package registry is the machine-readable source of truth for the research
// sources this gateway knows about.
//
// RESEARCH-INDEX.md used to be that source of truth, and nothing could check
// it. The registry holds the fields a program needs; the markdown document is
// generated from it. Human judgment that has no registry field (the legal
// posture disclaimer, the priority rationale, the parser-reuse analysis) stays
// hand-written and is preserved verbatim by the generator.
//
// The registry describes sources. It does not fetch, cache, or analyze their
// content.
package registry

import "fmt"

// ProviderType records what a source *is*. A classification is not
// automatically an ontology, and a dataset repository is not a literature
// provider, so the registry does not force one taxonomy onto every source.
type ProviderType string

const (
	TypeScholarlyMetadata             ProviderType = "scholarly_metadata"
	TypeCitationGraph                 ProviderType = "citation_graph"
	TypeFullTextRepository            ProviderType = "full_text_repository"
	TypePreprintRepository            ProviderType = "preprint_repository"
	TypeOntology                      ProviderType = "ontology"
	TypeControlledVocabulary          ProviderType = "controlled_vocabulary"
	TypeThesaurus                     ProviderType = "thesaurus"
	TypeClassification                ProviderType = "classification"
	TypeNomenclature                  ProviderType = "nomenclature"
	TypeHistoricalVocabulary          ProviderType = "historical_vocabulary"
	TypeStandard                      ProviderType = "standard"
	TypeDatasetRepository             ProviderType = "dataset_repository"
	TypeSoftwareRepository            ProviderType = "software_repository"
	TypePatentDatabase                ProviderType = "patent_database"
	TypeAuthorRegistry                ProviderType = "author_registry"
	TypeOrganizationRegistry          ProviderType = "organization_registry"
	TypeChemicalDatabase              ProviderType = "chemical_database"
	TypeBiologicalDatabase            ProviderType = "biological_database"
	TypeMaterialsDatabase             ProviderType = "materials_database"
	TypeMathematicalObjectDatabase    ProviderType = "mathematical_object_database"
	TypeAstronomicalDatabase          ProviderType = "astronomical_database"
	TypeEarthScienceDatabase          ProviderType = "earth_science_database"
	TypeExperimentalRegistry          ProviderType = "experimental_registry"
	TypeDocumentStandard              ProviderType = "document_standard"
	TypeMathematicalSemanticsStandard ProviderType = "mathematical_semantics_standard"
	TypeIntegrityRegistry             ProviderType = "integrity_registry"
	TypeRetractionUpdateSource        ProviderType = "retraction_update_source"
)

// ProviderTypes is the closed set this revision recognizes. Adding a source
// class means adding it here, so an unreviewed string cannot silently enter
// the registry.
var ProviderTypes = []ProviderType{
	TypeScholarlyMetadata, TypeCitationGraph, TypeFullTextRepository,
	TypePreprintRepository, TypeOntology, TypeControlledVocabulary,
	TypeThesaurus, TypeClassification, TypeNomenclature,
	TypeHistoricalVocabulary, TypeStandard, TypeDatasetRepository,
	TypeSoftwareRepository, TypePatentDatabase, TypeAuthorRegistry,
	TypeOrganizationRegistry, TypeChemicalDatabase, TypeBiologicalDatabase,
	TypeMaterialsDatabase, TypeMathematicalObjectDatabase,
	TypeAstronomicalDatabase, TypeEarthScienceDatabase,
	TypeExperimentalRegistry, TypeDocumentStandard,
	TypeMathematicalSemanticsStandard, TypeIntegrityRegistry,
	TypeRetractionUpdateSource,
}

func (p ProviderType) Valid() bool {
	for _, t := range ProviderTypes {
		if t == p {
			return true
		}
	}
	return false
}

// Status is the provider lifecycle. The point of the registry is that a source
// can be known and described long before an adapter exists, so the backlog is
// staged rather than a demand to implement everything at once.
type Status string

const (
	// StatusDiscovered means the source is known to exist and nothing more.
	StatusDiscovered Status = "discovered"
	// StatusResearched means its API, licence, and rights have been read.
	StatusResearched Status = "researched"
	// StatusRegistered means it has a complete registry entry.
	StatusRegistered Status = "registered"
	// StatusVerified means its endpoints and licence were checked live.
	StatusVerified Status = "verified"
	// StatusAdapterPlanned means an adapter is scheduled.
	StatusAdapterPlanned Status = "adapter_planned"
	// StatusImplemented means an adapter exists but is not serving traffic.
	StatusImplemented Status = "implemented"
	// StatusProduction means it is serving traffic through a configured route.
	StatusProduction Status = "production"
	// StatusDeprecated means it still works but is on the way out.
	StatusDeprecated Status = "deprecated"
	// StatusSunset means the upstream is gone.
	StatusSunset Status = "sunset"
	// StatusExcluded means a deliberate decision not to operate this source.
	// The research note is retained; no operational endpoints are registered.
	StatusExcluded Status = "excluded"
)

var Statuses = []Status{
	StatusDiscovered, StatusResearched, StatusRegistered, StatusVerified,
	StatusAdapterPlanned, StatusImplemented, StatusProduction,
	StatusDeprecated, StatusSunset, StatusExcluded,
}

func (s Status) Valid() bool {
	for _, x := range Statuses {
		if x == s {
			return true
		}
	}
	return false
}

// Operational reports whether the gateway may route traffic to this source.
// Excluded and sunset sources are described but never called.
func (s Status) Operational() bool {
	return s == StatusImplemented || s == StatusProduction || s == StatusDeprecated
}

// Endpoint is one callable URL exposed by a provider.
type Endpoint struct {
	ID          string `yaml:"id" json:"id"`
	URL         string `yaml:"url" json:"url"`
	Method      string `yaml:"method,omitempty" json:"method,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Capability is the feed402 capability this endpoint fulfills.
	Capability string `yaml:"capability,omitempty" json:"capability,omitempty"`
}

// Rights separates the licence of the metadata from the rights over the
// content, which are routinely different. UNKNOWN is never ALLOWED: an empty
// or "unknown" rights block means the gateway has not established permission,
// not that permission exists.
type Rights struct {
	// MetadataLicense covers the records the gateway would redistribute.
	MetadataLicense string `yaml:"metadata_license,omitempty" json:"metadata_license,omitempty"`
	// ContentLicense covers full text or binary assets, which is usually
	// narrower than the metadata licence and often absent entirely.
	ContentLicense string `yaml:"content_license,omitempty" json:"content_license,omitempty"`
	// TermsURL is the authoritative terms document.
	TermsURL string `yaml:"terms_url,omitempty" json:"terms_url,omitempty"`
	// VerifiedOn is when a human last read those terms (YYYY-MM-DD).
	VerifiedOn string `yaml:"verified_on,omitempty" json:"verified_on,omitempty"`
	// Redistribution states whether the gateway may serve the records on.
	// Defaults to "unknown", which forbids redistribution.
	Redistribution string `yaml:"redistribution,omitempty" json:"redistribution,omitempty"`
	// Notes records the reasoning, especially for a refusal.
	Notes string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// RedistributionAllowed is deliberately strict. Anything other than an
// explicit "allowed" is treated as not allowed, so an unfilled registry entry
// cannot be read as permission.
func (r Rights) RedistributionAllowed() bool {
	return r.Redistribution == "allowed"
}

// Provider is one research source.
type Provider struct {
	ProviderID string       `yaml:"provider_id" json:"provider_id"`
	Name       string       `yaml:"name" json:"name"`
	Type       ProviderType `yaml:"provider_type" json:"provider_type"`

	// Fields and Subfields are the domains the source covers, e.g.
	// "mathematics" / "number_theory".
	Fields    []string `yaml:"fields,omitempty" json:"fields,omitempty"`
	Subfields []string `yaml:"subfields,omitempty" json:"subfields,omitempty"`

	// Coverage is the prose description of what the source holds, carried
	// over from the "Domain coverage" column of the original index.
	Coverage string `yaml:"coverage,omitempty" json:"coverage,omitempty"`

	// Capabilities uses the feed402 capability vocabulary.
	Capabilities []string   `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Endpoints    []Endpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`

	DocumentationURL string `yaml:"documentation_url,omitempty" json:"documentation_url,omitempty"`
	BaseURL          string `yaml:"base_url,omitempty" json:"base_url,omitempty"`

	// Auth is the access model: none, email-header, api-key, oauth, paid.
	Auth      string `yaml:"auth,omitempty" json:"auth,omitempty"`
	RateLimit string `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`

	// License is the short metadata-licence label kept for continuity with the
	// original index. Rights carries the full picture.
	License string `yaml:"license,omitempty" json:"license,omitempty"`
	Rights  Rights `yaml:"rights,omitempty" json:"rights,omitempty"`

	IdentifierSchemes []string `yaml:"identifier_schemes,omitempty" json:"identifier_schemes,omitempty"`
	// SourcePrefix is the feed402 citation source prefix.
	SourcePrefix string `yaml:"source_prefix,omitempty" json:"source_prefix,omitempty"`
	// CanonicalURL is a template containing an {id} placeholder.
	CanonicalURL string `yaml:"canonical_url,omitempty" json:"canonical_url,omitempty"`

	QueryLanguage string `yaml:"query_language,omitempty" json:"query_language,omitempty"`
	Pagination    string `yaml:"pagination,omitempty" json:"pagination,omitempty"`

	BulkAccess         bool `yaml:"bulk_access,omitempty" json:"bulk_access,omitempty"`
	IncrementalUpdates bool `yaml:"incremental_updates,omitempty" json:"incremental_updates,omitempty"`
	FulltextAccess     bool `yaml:"fulltext_access,omitempty" json:"fulltext_access,omitempty"`
	StructuredFulltext bool `yaml:"structured_fulltext,omitempty" json:"structured_fulltext,omitempty"`

	Formats     []string `yaml:"formats,omitempty" json:"formats,omitempty"`
	Version     string   `yaml:"version,omitempty" json:"version,omitempty"`
	ReleaseDate string   `yaml:"release_date,omitempty" json:"release_date,omitempty"`

	Status Status `yaml:"status" json:"status"`
	// LastVerified is stamped by `make verify-providers` (YYYY-MM-DD).
	LastVerified string `yaml:"last_verified,omitempty" json:"last_verified,omitempty"`
	// Stale is set when verification failed. Verification never deletes an
	// entry; it flags it.
	Stale       bool   `yaml:"stale,omitempty" json:"stale,omitempty"`
	StaleReason string `yaml:"stale_reason,omitempty" json:"stale_reason,omitempty"`

	// HistoricalSuccessor / HistoricalPredecessor carry real migrations:
	// OpenAlex succeeding Microsoft Academic Graph, PatentsView migrating to
	// the USPTO Open Data Portal, PACS superseded by PhySH. A predecessor
	// entry is never deleted.
	HistoricalSuccessor   string `yaml:"historical_successor,omitempty" json:"historical_successor,omitempty"`
	HistoricalPredecessor string `yaml:"historical_predecessor,omitempty" json:"historical_predecessor,omitempty"`

	// CorpusSize is prose, because upstreams report it inconsistently.
	CorpusSize string `yaml:"corpus_size,omitempty" json:"corpus_size,omitempty"`
	// TierFit records which feed402 tiers suit the source.
	TierFit string `yaml:"tier_fit,omitempty" json:"tier_fit,omitempty"`
	// RouteIDs links to config/routes.yaml entries serving this provider.
	RouteIDs []string `yaml:"route_ids,omitempty" json:"route_ids,omitempty"`
	// Section is the RESEARCH-INDEX.md section this entry renders into.
	Section string `yaml:"section,omitempty" json:"section,omitempty"`
	Notes   string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Validate checks one entry. It reports every problem rather than the first,
// so a maintainer fixes an entry in one pass.
func (p Provider) Validate() []error {
	var errs []error
	if p.ProviderID == "" {
		errs = append(errs, fmt.Errorf("provider_id is required"))
	}
	if p.Name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", p.ProviderID))
	}
	if !p.Type.Valid() {
		errs = append(errs, fmt.Errorf("%s: unknown provider_type %q", p.ProviderID, p.Type))
	}
	if !p.Status.Valid() {
		errs = append(errs, fmt.Errorf("%s: unknown status %q", p.ProviderID, p.Status))
	}

	// A source we have decided not to operate must not carry callable
	// endpoints, so no amount of later code can route to it by accident.
	if p.Status == StatusExcluded && len(p.Endpoints) > 0 {
		errs = append(errs, fmt.Errorf("%s: excluded providers must register no endpoints", p.ProviderID))
	}
	if p.Status == StatusExcluded && p.Notes == "" {
		errs = append(errs, fmt.Errorf("%s: excluded providers must retain a research note", p.ProviderID))
	}

	// A provider serving traffic has to be reachable and attributable.
	if p.Status.Operational() {
		if p.BaseURL == "" {
			errs = append(errs, fmt.Errorf("%s: operational providers need a base_url", p.ProviderID))
		}
		if p.SourcePrefix == "" {
			errs = append(errs, fmt.Errorf("%s: operational providers need a source_prefix", p.ProviderID))
		}
	}
	return errs
}
