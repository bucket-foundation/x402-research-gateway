package provider

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Semantic Scholar citation-graph adapters (x402-research-gateway#6).
//
// Verified against the Academic Graph API on 2026-08-17:
//
//	GET /graph/v1/paper/{id}/references -> {"offset":N,"next":N,"data":[{"citedPaper":{...}}]}
//	GET /graph/v1/paper/{id}/citations  -> {"offset":N,"next":N,"data":[{"citingPaper":{...}}]}
//
// {id} accepts a bare S2 paper id or a prefixed external id: `DOI:10.x`,
// `PMID:123`, `ARXIV:2101.00001`. The route's pathTemplate substitutes it.
// No API key is required; a key raises rate limits and is supplied through
// route headers when an operator has one.
//
// Pagination is offset-based. The response's `next` field is present only
// when more results exist, which is what Truncated reports.

type s2CitationGraph struct {
	direction citation.Direction
}

func (s s2CitationGraph) Direction() citation.Direction { return s.direction }

// s2ExternalID renders an identifier in the prefixed form the paper
// endpoints accept. Schemes S2 does not resolve return ok=false.
func s2ExternalID(id identity.Identifier) (string, bool) {
	switch id.Scheme {
	case identity.SchemeSemanticScholar:
		return id.Value, true
	case identity.SchemeDOI:
		return "DOI:" + id.Value, true
	case identity.SchemePMID:
		return "PMID:" + id.Value, true
	case identity.SchemePMCID:
		return "PMCID:" + id.Value, true
	case identity.SchemeArXiv:
		return "ARXIV:" + id.Value, true
	default:
		return "", false
	}
}

func (s s2CitationGraph) EdgeQuery(id identity.Identifier) (map[string]string, bool) {
	ext, ok := s2ExternalID(id)
	if !ok || id.Value == "" {
		return nil, false
	}
	return map[string]string{
		"id":     ext,
		"fields": "paperId,externalIds,title,year",
		"limit":  "100",
	}, true
}

type s2EdgeBody struct {
	Offset int  `json:"offset"`
	Next   *int `json:"next"`
	Data   []struct {
		IsInfluential bool     `json:"isInfluential"`
		CitedPaper    *s2Paper `json:"citedPaper"`
		CitingPaper   *s2Paper `json:"citingPaper"`
	} `json:"data"`
}

func (s s2CitationGraph) parse(body []byte) (s2EdgeBody, bool) {
	var b s2EdgeBody
	if err := json.Unmarshal(body, &b); err != nil {
		return b, false
	}
	return b, true
}

func s2Endpoint(p *s2Paper) (citation.Endpoint, bool) {
	if p == nil {
		return citation.Endpoint{}, false
	}
	var ids []identity.Identifier
	ids = appendID(ids, identity.SchemeSemanticScholar, p.PaperID)
	ids = appendID(ids, identity.SchemeDOI, p.ExternalIDs.DOI)
	ids = appendID(ids, identity.SchemePMID, p.ExternalIDs.PubMed)
	ids = appendID(ids, identity.SchemePMCID, p.ExternalIDs.PubMedCentral)
	ids = appendID(ids, identity.SchemeArXiv, p.ExternalIDs.ArXiv)
	if len(ids) == 0 {
		return citation.Endpoint{}, false
	}
	return citation.Endpoint{
		Identifiers:  ids,
		RawID:        p.PaperID,
		CanonicalURL: "https://www.semanticscholar.org/paper/" + p.PaperID,
	}, true
}

func (s s2CitationGraph) Edges(query identity.Identifier, body []byte, at time.Time) []citation.Edge {
	b, ok := s.parse(body)
	if !ok {
		return nil
	}
	ext, _ := s2ExternalID(query)
	queryEnd := citation.Endpoint{
		Identifiers: []identity.Identifier{query},
		RawID:       firstNonEmpty(query.Raw, ext),
	}
	stamp := at.UTC().Format(time.RFC3339)
	var edges []citation.Edge
	for _, item := range b.Data {
		far, ok := s2Endpoint(firstPaper(item.CitedPaper, item.CitingPaper))
		if !ok {
			continue
		}
		edge := citation.Edge{Direction: s.direction, RetrievedAt: stamp}
		if s.direction == citation.DirectionReferences {
			edge.Source, edge.Target = queryEnd, far
		} else {
			edge.Source, edge.Target = far, queryEnd
		}
		edges = append(edges, edge)
	}
	return edges
}

func firstPaper(ps ...*s2Paper) *s2Paper {
	for _, p := range ps {
		if p != nil {
			return p
		}
	}
	return nil
}

func (s s2CitationGraph) EdgePagination(body []byte) (string, bool, string) {
	b, ok := s.parse(body)
	if !ok || b.Next == nil {
		return "offset", false, ""
	}
	return "offset", true, strconv.Itoa(*b.Next)
}

// SemanticScholarReferencesAdapter backs route ID "semantic-scholar-references".
var SemanticScholarReferencesAdapter = &Adapter{
	ID:                    "semantic-scholar-references",
	Description:           "Semantic Scholar outbound citations for a paper.",
	CitationGraphProvider: s2CitationGraph{direction: citation.DirectionReferences},
}

// SemanticScholarCitedByAdapter backs route ID "semantic-scholar-cited-by".
var SemanticScholarCitedByAdapter = &Adapter{
	ID:                    "semantic-scholar-cited-by",
	Description:           "Semantic Scholar inbound citations for a paper.",
	CitationGraphProvider: s2CitationGraph{direction: citation.DirectionCitedBy},
}
