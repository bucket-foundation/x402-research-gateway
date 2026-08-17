package provider

// DefaultRegistry returns the adapters for the six providers migrated in
// x402-research-gateway#2: PubMed (search and fetch), Semantic Scholar,
// OpenAlex, ClinicalTrials.gov, PubChem, and the Kruse search service.
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
	}
}
