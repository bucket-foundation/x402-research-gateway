package provider

import "encoding/json"

// SemanticScholarSearchNormalizer parses Semantic Scholar Graph API search:
//
//	{"data": [{"paperId": "abc123...", ...}, ...]}
type SemanticScholarSearchNormalizer struct{}

func (SemanticScholarSearchNormalizer) Normalize(body []byte) []NormalizedRecord {
	var parsed struct {
		Data []struct {
			PaperID string `json:"paperId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(parsed.Data))
	for _, p := range parsed.Data {
		recs = append(recs, NormalizedRecord{
			ID:           p.PaperID,
			CanonicalURL: "https://www.semanticscholar.org/paper/" + p.PaperID,
		})
	}
	return recs
}

// semanticScholarOffsetPagination implements Searcher: the Graph API pages
// search results via an `offset` query parameter.
type semanticScholarOffsetPagination struct{}

func (semanticScholarOffsetPagination) PaginationModel() string { return "offset" }

// SemanticScholarSearchAdapter backs route ID "semantic-scholar-search".
var SemanticScholarSearchAdapter = &Adapter{
	ID:               "semantic-scholar-search",
	Description:      "Semantic Scholar Graph API paper search.",
	Searcher:         semanticScholarOffsetPagination{},
	Normalizer:       SemanticScholarSearchNormalizer{},
	CitationProvider: GenericCitationProvider{},
}
