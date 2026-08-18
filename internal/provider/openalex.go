package provider

import "encoding/json"

// OpenAlexWorksNormalizer parses OpenAlex /works search:
//
//	{"results": [{"id": "https://openalex.org/W1234", ...}, ...]}
//
// OpenAlex IDs are full URLs; the short form (the trailing path segment) is
// what other x402-research-gateway routes and citations key on, so this
// Normalizer derives both: NormalizedRecord.ID is the short form, and
// CanonicalURL is the full upstream URL verbatim — the one adapter in this
// package where CanonicalURL isn't built by string-concatenating the ID.
type OpenAlexWorksNormalizer struct{}

func (OpenAlexWorksNormalizer) Normalize(body []byte) []NormalizedRecord {
	// Results are captured as RawMessage as well as decoded, so each
	// record keeps its original bytes for capabilities that need more than
	// the id (IdentityProvider reads the `ids` block from them). Decoding
	// twice is cheap next to the upstream call and keeps this Normalizer's
	// existing output byte-identical.
	var parsed struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(parsed.Results))
	for _, raw := range parsed.Results {
		var r struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           shortOpenAlexID(r.ID),
			CanonicalURL: r.ID,
			Raw:          raw,
		})
	}
	return recs
}

// shortOpenAlexID returns the trailing path segment of a full OpenAlex URL
// id, e.g. "https://openalex.org/W1234" -> "W1234". Empty input returns
// empty output rather than the full string, so GenericCitationProvider's
// empty-ID skip rule still applies to a record with no id at all.
func shortOpenAlexID(fullID string) string {
	if fullID == "" {
		return ""
	}
	for i := len(fullID) - 1; i >= 0; i-- {
		if fullID[i] == '/' {
			return fullID[i+1:]
		}
	}
	return fullID
}

// openAlexPagePagination implements Searcher: OpenAlex pages via `page` and
// `per_page` query parameters.
type openAlexPagePagination struct{}

func (openAlexPagePagination) PaginationModel() string { return "page" }

// OpenAlexWorksAdapter backs route ID "openalex-works".
var OpenAlexWorksAdapter = &Adapter{
	ID:               "openalex-works",
	Description:      "OpenAlex /works search over CC0 scholarly metadata.",
	Searcher:         openAlexPagePagination{},
	Normalizer:       OpenAlexWorksNormalizer{},
	CitationProvider: GenericCitationProvider{},

	IdentityProvider:   openAlexIdentity{},
	DescriptorProvider: openAlexIdentity{},
	Paginator:          openAlexPaginator{},
}
