package provider

import "encoding/json"

// PubMedSearchNormalizer parses NCBI E-utils ESearch JSON:
//
//	{"esearchresult": {"idlist": ["38831607", "34588695", ...]}}
type PubMedSearchNormalizer struct{}

func (PubMedSearchNormalizer) Normalize(body []byte) []NormalizedRecord {
	var parsed struct {
		ESearchResult struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(parsed.ESearchResult.IDList))
	for _, id := range parsed.ESearchResult.IDList {
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://pubmed.ncbi.nlm.nih.gov/" + id + "/",
		})
	}
	return recs
}

// pubMedSearchOffsetPagination implements Searcher for pubmed-search:
// NCBI E-utils ESearch pages via retstart, an offset in result units.
type pubMedSearchOffsetPagination struct{}

func (pubMedSearchOffsetPagination) PaginationModel() string { return "offset" }

// pubMedFetchByPMID implements Fetcher for pubmed-fetch: a single PMID in,
// a single abstract out.
type pubMedFetchByPMID struct{}

func (pubMedFetchByPMID) IdentifierSchemes() []string { return []string{"pmid"} }

// PubMedSearchAdapter backs route ID "pubmed-search".
var PubMedSearchAdapter = &Adapter{
	ID:               "pubmed-search",
	Description:      "NCBI E-utils ESearch over PubMed biomedical literature.",
	Searcher:         pubMedSearchOffsetPagination{},
	Normalizer:       PubMedSearchNormalizer{},
	CitationProvider: GenericCitationProvider{},
}

// PubMedFetchAdapter backs route ID "pubmed-fetch". No Normalizer or
// CitationProvider: a raw-tier single-record fetch has nothing to
// enumerate into a hits array, matching pre-#2 behavior where fetch routes
// never had a registered hit parser.
var PubMedFetchAdapter = &Adapter{
	ID:          "pubmed-fetch",
	Description: "NCBI E-utils EFetch — a single PubMed abstract by PMID.",
	Fetcher:     pubMedFetchByPMID{},
}
