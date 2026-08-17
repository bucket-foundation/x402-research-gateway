package provider

import (
	"encoding/json"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// OpenAlex citation-graph adapters (x402-research-gateway#6).
//
// OpenAlex serves both directions off /works with a filter. The filter
// names read backwards from the traversal they perform, verified against
// the OpenAlex documentation on 2026-08-17:
//
//	filter=cited_by:W123 -> the works in W123's referenced_works (outbound)
//	filter=cites:W123    -> the works whose referenced_works contains W123
//
// So DirectionReferences uses `cited_by` and DirectionCitedBy uses `cites`.
// Getting this pair backwards silently inverts the graph, which is why the
// mapping is stated here and asserted in the tests.
//
// Both filters take OpenAlex work ids. A query identifier under any other
// scheme is reported as unsupported_identifier rather than guessed at.
//
// Pagination: OpenAlex pages /works results with `per_page` plus a cursor.
// The gateway requests one page and reports `meta.count` against the page
// size so a consumer knows whether it has the whole edge set.

type openAlexCitationGraph struct {
	direction citation.Direction
	filter    string
}

func (o openAlexCitationGraph) Direction() citation.Direction { return o.direction }

func (o openAlexCitationGraph) EdgeQuery(id identity.Identifier) (map[string]string, bool) {
	if id.Scheme != identity.SchemeOpenAlex || id.Value == "" {
		return nil, false
	}
	return map[string]string{
		"filter":   o.filter + ":" + id.Value,
		"per_page": "200",
	}, true
}

type openAlexEdgeBody struct {
	Meta struct {
		Count      int    `json:"count"`
		PerPage    int    `json:"per_page"`
		NextCursor string `json:"next_cursor"`
	} `json:"meta"`
	Results []struct {
		ID  string `json:"id"`
		DOI string `json:"doi"`
		IDs struct {
			OpenAlex string `json:"openalex"`
			DOI      string `json:"doi"`
			PMID     string `json:"pmid"`
		} `json:"ids"`
		IsRetracted bool `json:"is_retracted"`
	} `json:"results"`
}

func (o openAlexCitationGraph) parse(body []byte) (openAlexEdgeBody, bool) {
	var b openAlexEdgeBody
	if err := json.Unmarshal(body, &b); err != nil {
		return b, false
	}
	return b, true
}

func (o openAlexCitationGraph) Edges(query identity.Identifier, body []byte, at time.Time) []citation.Edge {
	b, ok := o.parse(body)
	if !ok {
		return nil
	}
	queryEnd := citation.Endpoint{
		Identifiers:  []identity.Identifier{query},
		RawID:        query.Raw,
		CanonicalURL: "https://openalex.org/" + query.Value,
	}
	stamp := at.UTC().Format(time.RFC3339)
	var edges []citation.Edge
	for _, r := range b.Results {
		var ids []identity.Identifier
		ids = appendID(ids, identity.SchemeOpenAlex, firstNonEmpty(r.IDs.OpenAlex, r.ID))
		ids = appendID(ids, identity.SchemeDOI, firstNonEmpty(r.IDs.DOI, r.DOI))
		ids = appendID(ids, identity.SchemePMID, r.IDs.PMID)
		if len(ids) == 0 {
			continue
		}
		far := citation.Endpoint{
			Identifiers:  ids,
			RawID:        firstNonEmpty(r.IDs.OpenAlex, r.ID),
			CanonicalURL: firstNonEmpty(r.IDs.OpenAlex, r.ID),
		}
		// Provider is stamped by the caller, which knows the route id this
		// adapter is registered under. An adapter naming itself would drift
		// from its registry key.
		edge := citation.Edge{Direction: o.direction, RetrievedAt: stamp}
		// The queried work is the citing end under references and the cited
		// end under cited_by. Source always cites Target.
		if o.direction == citation.DirectionReferences {
			edge.Source, edge.Target = queryEnd, far
		} else {
			edge.Source, edge.Target = far, queryEnd
		}
		// OpenAlex marks retraction on the work, so the annotation lands on
		// an edge only when the far end is the retracted party.
		if r.IsRetracted {
			edge.Status = citation.EdgeStatusRetracted
		}
		edges = append(edges, edge)
	}
	return edges
}

func (o openAlexCitationGraph) EdgePagination(body []byte) (string, bool, string) {
	b, ok := o.parse(body)
	if !ok {
		return "cursor", false, ""
	}
	return "cursor", b.Meta.Count > len(b.Results), b.Meta.NextCursor
}

// OpenAlexReferencesAdapter backs route ID "openalex-references".
var OpenAlexReferencesAdapter = &Adapter{
	ID:          "openalex-references",
	Description: "OpenAlex outbound citations: the works a work references.",
	CitationGraphProvider: openAlexCitationGraph{
		direction: citation.DirectionReferences,
		filter:    "cited_by",
	},
}

// OpenAlexCitedByAdapter backs route ID "openalex-cited-by".
var OpenAlexCitedByAdapter = &Adapter{
	ID:          "openalex-cited-by",
	Description: "OpenAlex inbound citations: the works citing a work.",
	CitationGraphProvider: openAlexCitationGraph{
		direction: citation.DirectionCitedBy,
		filter:    "cites",
	},
}
