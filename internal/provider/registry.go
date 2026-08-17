package provider

// DefaultRegistry returns every registered adapter: the six providers
// migrated in x402-research-gateway#2 (PubMed search and fetch, Semantic
// Scholar, OpenAlex, ClinicalTrials.gov, PubChem, Kruse) plus the
// citation-graph adapters added in x402-research-gateway#6.
//
// A config.RouteConfig.ID with no entry here is served entirely by the
// declarative config-driven proxy path (internal/handler/proxy.go), which
// remains fully supported — most simple REST upstreams need no adapter.
func DefaultRegistry() Registry {
	return Registry{
		PubMedSearchAdapter.ID:          PubMedSearchAdapter,
		PubMedFetchAdapter.ID:           PubMedFetchAdapter,
		SemanticScholarSearchAdapter.ID: SemanticScholarSearchAdapter,
		OpenAlexWorksAdapter.ID:         OpenAlexWorksAdapter,
		ClinicalTrialsSearchAdapter.ID:  ClinicalTrialsSearchAdapter,
		KruseSearchAdapter.ID:           KruseSearchAdapter,
		PubChemCompoundAdapter.ID:       PubChemCompoundAdapter,

		// Citation-graph adapters (x402-research-gateway#6). One adapter
		// per provider per direction, so a provider that serves only one
		// direction reports the other as unsupported rather than empty.
		OpenAlexReferencesAdapter.ID:        OpenAlexReferencesAdapter,
		OpenAlexCitedByAdapter.ID:           OpenAlexCitedByAdapter,
		SemanticScholarReferencesAdapter.ID: SemanticScholarReferencesAdapter,
		SemanticScholarCitedByAdapter.ID:    SemanticScholarCitedByAdapter,
		OpenCitationsReferencesAdapter.ID:   OpenCitationsReferencesAdapter,
		OpenCitationsCitedByAdapter.ID:      OpenCitationsCitedByAdapter,
		CrossrefReferencesAdapter.ID:        CrossrefReferencesAdapter,
	}
}
