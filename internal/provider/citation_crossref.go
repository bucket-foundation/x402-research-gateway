package provider

import (
	"encoding/json"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Crossref citation-graph adapter (x402-research-gateway#6).
//
// Verified against api.crossref.org on 2026-08-17:
//
//	GET https://api.crossref.org/works/{doi} -> {"message":{"reference":[...]}}
//
// No API key. A `mailto` query parameter puts the caller in the polite
// pool and is set on the route's queryParams.
//
// Crossref serves outbound references only. There is no cited-by endpoint,
// so no cited-by adapter exists and a cited-by query reports Crossref as
// unsupported_direction rather than as a provider that found nothing.
//
// Two facts shape the reference parsing. A reference entry carries a DOI
// only when the publisher deposited one, so unstructured entries are kept
// with their raw text as the endpoint's RawID and no identifier, which
// keeps them visible without inventing a match. And the `reference` array
// is present only for publishers who deposit references openly, which is
// the exact coverage gap citation.AbsenceNotice exists to state.

type crossrefReferences struct{}

func (crossrefReferences) Direction() citation.Direction { return citation.DirectionReferences }

func (crossrefReferences) EdgeQuery(id identity.Identifier) (map[string]string, bool) {
	if id.Scheme != identity.SchemeDOI || id.Value == "" {
		return nil, false
	}
	return map[string]string{"id": id.Value}, true
}

type crossrefWorkBody struct {
	Message struct {
		DOI       string `json:"DOI"`
		Reference []struct {
			Key          string `json:"key"`
			DOI          string `json:"DOI"`
			Unstructured string `json:"unstructured"`
			ArticleTitle string `json:"article-title"`
			JournalTitle string `json:"journal-title"`
			Year         string `json:"year"`
		} `json:"reference"`
		ReferenceCount int `json:"reference-count"`
	} `json:"message"`
}

func (crossrefReferences) Edges(query identity.Identifier, body []byte, at time.Time) []citation.Edge {
	var b crossrefWorkBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil
	}
	source := citation.Endpoint{
		Identifiers:  []identity.Identifier{query},
		RawID:        firstNonEmpty(query.Raw, b.Message.DOI),
		CanonicalURL: "https://doi.org/" + query.Value,
	}
	stamp := at.UTC().Format(time.RFC3339)
	var edges []citation.Edge
	for _, ref := range b.Message.Reference {
		target := citation.Endpoint{
			RawID: firstNonEmpty(ref.DOI, ref.Unstructured, ref.ArticleTitle, ref.Key),
		}
		if ref.DOI != "" {
			target.Identifiers = appendID(target.Identifiers, identity.SchemeDOI, ref.DOI)
			target.CanonicalURL = "https://doi.org/" + ref.DOI
		}
		if target.RawID == "" {
			continue
		}
		edges = append(edges, citation.Edge{
			Direction:   citation.DirectionReferences,
			Source:      source,
			Target:      target,
			RetrievedAt: stamp,
		})
	}
	return edges
}

// EdgePagination reports "none": Crossref returns a work's whole reference
// list in the work record. Truncated compares the deposited
// `reference-count` against the entries present, which differ when
// a publisher deposits a count without the list.
func (crossrefReferences) EdgePagination(body []byte) (string, bool, string) {
	var b crossrefWorkBody
	if err := json.Unmarshal(body, &b); err != nil {
		return "none", false, ""
	}
	return "none", b.Message.ReferenceCount > len(b.Message.Reference), ""
}

// CrossrefReferencesAdapter backs route ID "crossref-references".
var CrossrefReferencesAdapter = &Adapter{
	ID:                    "crossref-references",
	Description:           "Crossref deposited reference list for a DOI.",
	CitationGraphProvider: crossrefReferences{},
}
