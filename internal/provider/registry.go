package provider

// DefaultRegistry returns every registered adapter: the six providers
// migrated in x402-research-gateway#2 (PubMed search and fetch, Semantic
// Scholar, OpenAlex, ClinicalTrials.gov, PubChem, Kruse), the citation-graph
// adapters added in x402-research-gateway#6, Crossref and OpenCitations
// (x402-research-gateway#23, #27), and arXiv, DataCite, and Europe PMC
// (x402-research-gateway#25, #24, #26).
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
		CrossrefSearchAdapter.ID:        CrossrefSearchAdapter,
		CrossrefFetchAdapter.ID:         CrossrefFetchAdapter,

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

		// arXiv (x402-research-gateway#25): Atom XML, which the declarative
		// proxy path cannot parse, so both routes need the adapter.
		ArXivSearchAdapter.ID: ArXivSearchAdapter,
		ArXivFetchAdapter.ID:  ArXivFetchAdapter,

		// DataCite (x402-research-gateway#24): JSON:API, registered for its
		// per-record object rights, resource-type distinction, and relation
		// mapping, none of which the declarative path can express.
		DataCiteSearchAdapter.ID: DataCiteSearchAdapter,
		DataCiteFetchAdapter.ID:  DataCiteFetchAdapter,

		// Europe PMC (x402-research-gateway#26): per-record rights,
		// structured full-text asset discovery, and a citation graph in
		// both directions.
		EuropePMCSearchAdapter.ID:     EuropePMCSearchAdapter,
		EuropePMCFetchAdapter.ID:      EuropePMCFetchAdapter,
		EuropePMCReferencesAdapter.ID: EuropePMCReferencesAdapter,
		EuropePMCCitedByAdapter.ID:    EuropePMCCitedByAdapter,

		// ROR (x402-research-gateway#30): organizational identity, external
		// identifier mappings, name variants, and hierarchy/successor
		// relations, none of which the declarative path can express.
		RORSearchAdapter.ID: RORSearchAdapter,
		RORFetchAdapter.ID:  RORFetchAdapter,
	}
}
