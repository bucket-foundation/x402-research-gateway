package provider

import "encoding/json"

// SemanticScholarSearchNormalizer parses Semantic Scholar Graph API search:
//
//	{"data": [{"paperId": "abc123...", ...}, ...]}
type SemanticScholarSearchNormalizer struct{}

func (SemanticScholarSearchNormalizer) Normalize(body []byte) []NormalizedRecord {
	// Per-record raw bytes are retained so IdentityProvider can read the
	// `externalIds` block without a second upstream call. Existing output
	// fields are unchanged.
	var parsed struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(parsed.Data))
	for _, raw := range parsed.Data {
		var p struct {
			PaperID string `json:"paperId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           p.PaperID,
			CanonicalURL: "https://www.semanticscholar.org/paper/" + p.PaperID,
			Raw:          raw,
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

	IdentityProvider:   semanticScholarIdentity{},
	DescriptorProvider: semanticScholarIdentity{},
	Paginator:          semanticScholarPaginator{},
}
