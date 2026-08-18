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

		// Unpaywall (x402-research-gateway#28): rights-aware asset discovery
		// over open-access locations. DOI-only lookup, no search endpoint.
		UnpaywallFetchAdapter.ID: UnpaywallFetchAdapter,

		// DBLP (x402-research-gateway#31): publication search, a first-class
		// author search preserving DBLP's own homonym disambiguation, and
		// XML-only single-record fetch, none of which the declarative path
		// can express.
		DBLPPublSearchAdapter.ID:   DBLPPublSearchAdapter,
		DBLPAuthorSearchAdapter.ID: DBLPAuthorSearchAdapter,
		DBLPFetchAdapter.ID:        DBLPFetchAdapter,

		// CORE (x402-research-gateway#8): the open-access aggregator that
		// carries full text rather than metadata, registered for its
		// per-record rights and full-text location discovery. Its routes
		// need an operator-supplied API key; without one the routes answer
		// 401 and the asset endpoint reports that status.
		CORESearchAdapter.ID: CORESearchAdapter,
		COREFetchAdapter.ID:  COREFetchAdapter,

		// zbMATH Open (x402-research-gateway#32): MSC codes with their
		// edition preserved, and review text kept apart from bibliographic
		// rights, neither of which the declarative path can express.
		ZbMATHSearchAdapter.ID: ZbMATHSearchAdapter,
		ZbMATHFetchAdapter.ID:  ZbMATHFetchAdapter,

		// ORCID (x402-research-gateway#29): the gateway's first credentialed
		// provider (client-credentials token, internal/auth). Registered for
		// external-identifier preservation, works cross-referencing, and CC0
		// rights, none of which the declarative path can express.
		ORCIDFetchAdapter.ID:  ORCIDFetchAdapter,
		ORCIDSearchAdapter.ID: ORCIDSearchAdapter,

		// DOAJ (x402-research-gateway#13, Wave 2): CC0 metadata, per-link
		// asset rights, credential-free.
		DOAJSearchAdapter.ID: DOAJSearchAdapter,
		DOAJFetchAdapter.ID:  DOAJFetchAdapter,

		// OpenAIRE (x402-research-gateway#13, Wave 2): the Graph v3 API,
		// credential-free, per-instance rights read rather than assumed.
		OpenAIRESearchAdapter.ID: OpenAIRESearchAdapter,
		OpenAIREFetchAdapter.ID:  OpenAIREFetchAdapter,

		// bioRxiv / medRxiv (x402-research-gateway#13, Wave 2): a shared
		// API with no keyword search, only date-interval listing and DOI
		// lookup, plus the preprint-to-published-work relation the
		// declarative path cannot express.
		BioRxivSearchAdapter.ID: BioRxivSearchAdapter,
		BioRxivFetchAdapter.ID:  BioRxivFetchAdapter,
		MedRxivSearchAdapter.ID: MedRxivSearchAdapter,
		MedRxivFetchAdapter.ID:  MedRxivFetchAdapter,
	}
}
