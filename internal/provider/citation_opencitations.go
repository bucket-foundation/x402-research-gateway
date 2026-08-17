package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// OpenCitations Index citation-graph adapters (x402-research-gateway#6).
//
// Verified against api.opencitations.net on 2026-08-17:
//
//	GET https://api.opencitations.net/index/v2/references/{id}
//	GET https://api.opencitations.net/index/v2/citations/{id}
//
// {id} must carry a scheme prefix: `doi:10.x`, `pmid:123`, or `omid:br/N`.
// Each response is a flat array of citation objects with fields oci,
// citing, cited, creation, timespan, journal_sc, author_sc. `citing` and
// `cited` are space-separated PID lists, so one endpoint routinely carries
// both a DOI and an OMID, and both are retained.
//
// Authorization is optional. OpenCitations asks that applications send an
// access token in the Authorization header, and rate-limits to 180
// requests/minute per IP without one. The token, when an operator has one,
// is supplied through the route's headers and never appears in a response.
//
// The v2 responses carry no pagination envelope, so this reports the "none"
// pagination model and never claims truncation it cannot observe.

type openCitationsGraph struct {
	direction citation.Direction
}

func (o openCitationsGraph) Direction() citation.Direction { return o.direction }

// openCitationsPID renders an identifier in the prefixed form the Index
// accepts. OpenCitations resolves DOIs, PMIDs, and its own OMIDs. Any other
// scheme is unsupported rather than approximated.
func openCitationsPID(id identity.Identifier) (string, bool) {
	switch id.Scheme {
	case identity.SchemeDOI:
		return "doi:" + id.Value, true
	case identity.SchemePMID:
		return "pmid:" + id.Value, true
	default:
		return "", false
	}
}

func (o openCitationsGraph) EdgeQuery(id identity.Identifier) (map[string]string, bool) {
	pid, ok := openCitationsPID(id)
	if !ok || id.Value == "" {
		return nil, false
	}
	return map[string]string{"id": pid}, true
}

type openCitationsEdge struct {
	OCI       string `json:"oci"`
	Citing    string `json:"citing"`
	Cited     string `json:"cited"`
	Creation  string `json:"creation"`
	Timespan  string `json:"timespan"`
	JournalSC string `json:"journal_sc"`
	AuthorSC  string `json:"author_sc"`
}

// openCitationsEndpoint parses a space-separated PID list into an endpoint,
// keeping the original string as RawID so the edge stays reversible to what
// OpenCitations sent.
func openCitationsEndpoint(pids string) (citation.Endpoint, bool) {
	pids = strings.TrimSpace(pids)
	if pids == "" {
		return citation.Endpoint{}, false
	}
	end := citation.Endpoint{RawID: pids}
	for _, pid := range strings.Fields(pids) {
		switch {
		case strings.HasPrefix(pid, "doi:"):
			end.Identifiers = appendID(end.Identifiers, identity.SchemeDOI, pid)
			if end.CanonicalURL == "" {
				end.CanonicalURL = "https://doi.org/" + strings.TrimPrefix(pid, "doi:")
			}
		case strings.HasPrefix(pid, "pmid:"):
			end.Identifiers = appendID(end.Identifiers, identity.SchemePMID, strings.TrimPrefix(pid, "pmid:"))
		}
		// An `omid:` PID has no registered scheme in internal/identity, so
		// it survives only in RawID. Losing it from matching is correct;
		// losing it from the record would not be.
	}
	return end, true
}

func (o openCitationsGraph) Edges(query identity.Identifier, body []byte, at time.Time) []citation.Edge {
	var raw []openCitationsEdge
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	stamp := at.UTC().Format(time.RFC3339)
	var edges []citation.Edge
	for _, r := range raw {
		source, okS := openCitationsEndpoint(r.Citing)
		target, okT := openCitationsEndpoint(r.Cited)
		if !okS || !okT {
			continue
		}
		edge := citation.Edge{
			Direction:      o.direction,
			Source:         source,
			Target:         target,
			ProviderEdgeID: r.OCI,
			RetrievedAt:    stamp,
		}
		// creation, timespan, journal_sc, and author_sc are published per
		// edge and have no typed field in the edge model, so they are kept
		// under the provider's own field names.
		for k, v := range map[string]string{
			"creation": r.Creation, "timespan": r.Timespan,
			"journal_sc": r.JournalSC, "author_sc": r.AuthorSC,
		} {
			if v == "" {
				continue
			}
			if edge.Annotations == nil {
				edge.Annotations = map[string]string{}
			}
			edge.Annotations[k] = v
		}
		edges = append(edges, edge)
	}
	return edges
}

func (o openCitationsGraph) EdgePagination([]byte) (string, bool, string) {
	return "none", false, ""
}

// Coverage states which collection answered. OpenCitations reorganized its
// collections: the per-source indexes (COCI over Crossref, DOCI over
// DataCite, POCI over PubMed) are served through one unified Index at
// api.opencitations.net/index/v2, and the separate COCI v1 endpoint the
// registry used to name is retired. Coverage still depends on publishers
// depositing open references, which is uneven by publisher, discipline, and
// era, so an empty answer is this collection having no edge on record.
func (openCitationsGraph) Coverage() string {
	return "OpenCitations Index v2, the unified index over Crossref, DataCite, and PubMed " +
		"deposited open references. Coverage is uneven by publisher, discipline, and era. " +
		"Verified 2026-08-17."
}

// OpenCitationsReferencesAdapter backs route ID "opencitations-references".
var OpenCitationsReferencesAdapter = &Adapter{
	ID:                    "opencitations-references",
	Description:           "OpenCitations Index outbound references for a DOI or PMID.",
	CitationGraphProvider: openCitationsGraph{direction: citation.DirectionReferences},
}

// OpenCitationsCitedByAdapter backs route ID "opencitations-cited-by".
var OpenCitationsCitedByAdapter = &Adapter{
	ID:                    "opencitations-cited-by",
	Description:           "OpenCitations Index inbound citations for a DOI or PMID.",
	CitationGraphProvider: openCitationsGraph{direction: citation.DirectionCitedBy},
}
