// Package citation models normalized citation edges across providers
// (x402-research-gateway#6).
//
// Every provider models citations differently, covers a different slice of
// the literature, and disagrees with the others. This package represents
// that rather than flattening it.
//
// The rule the whole package is built around: absence is not evidence. A
// provider that returns no edge for a work has no edge for that work. It
// does not follow that the citation does not exist. Coverage varies by
// discipline, by era, and by whether the publisher deposits references
// openly. Every Result therefore reports which providers were consulted and
// what each one returned, empty results included, and carries that
// statement in the response body itself.
package citation

import (
	"sort"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Direction names which way an edge is traversed from the queried work.
type Direction string

const (
	// DirectionReferences returns the works the queried work cites,
	// outbound.
	DirectionReferences Direction = "references"
	// DirectionCitedBy returns the works citing the queried work, inbound.
	DirectionCitedBy Direction = "cited_by"
)

// Valid reports whether d is a direction this package understands.
func (d Direction) Valid() bool {
	return d == DirectionReferences || d == DirectionCitedBy
}

// AbsenceNotice is emitted verbatim in every citation response. It states
// the one thing a consumer of citation data is most likely to get wrong.
const AbsenceNotice = "A provider returning no edges has no edges for this work. " +
	"It does not follow that the work is uncited or cites nothing. Coverage varies by " +
	"discipline, by era, and by whether the publisher deposits references openly. " +
	"Read providers_consulted for what each provider was asked and what it returned."

// EdgeStatus carries an annotation the provider itself puts on an edge.
// Empty means the provider said nothing, which is distinct from the
// provider saying the edge is current.
type EdgeStatus string

const (
	EdgeStatusRetracted  EdgeStatus = "retracted"
	EdgeStatusCorrected  EdgeStatus = "corrected"
	EdgeStatusSuperseded EdgeStatus = "superseded"
)

// Endpoint is one end of an edge, holding every identifier form the
// provider expressed it in. The original strings survive in
// Identifier.Raw, so an edge asserted in DOIs stays reversible to the DOIs
// the provider sent.
type Endpoint struct {
	// Identifiers are the normalized forms, each retaining its raw string.
	Identifiers []identity.Identifier `json:"identifiers,omitempty"`
	// RawID is the identifier string the provider used for this endpoint,
	// before any normalization, kept even when no scheme claimed it.
	RawID        string `json:"raw_id,omitempty"`
	CanonicalURL string `json:"canonical_url,omitempty"`
}

// Key is the endpoint's exact-match key, used to recognize two providers'
// endpoints as the same work. It is the lowest sorted identifier key, so
// the choice is deterministic. An endpoint with no parseable identifier
// falls back to its raw string, which only ever matches itself.
func (e Endpoint) Key() string {
	keys := make([]string, 0, len(e.Identifiers))
	for _, id := range e.Identifiers {
		if id.Value != "" {
			keys = append(keys, id.Key())
		}
	}
	if len(keys) == 0 {
		return "raw:" + strings.ToLower(e.RawID)
	}
	sort.Strings(keys)
	return keys[0]
}

// Keys returns every exact-match key this endpoint carries, so equivalence
// can match on any shared scheme rather than only the sorted-first one.
func (e Endpoint) Keys() []string {
	out := make([]string, 0, len(e.Identifiers))
	for _, id := range e.Identifiers {
		if id.Value != "" {
			out = append(out, id.Key())
		}
	}
	if len(out) == 0 {
		return []string{"raw:" + strings.ToLower(e.RawID)}
	}
	sort.Strings(out)
	return out
}

// Edge is one provider's assertion that Source cites Target.
//
// Nothing about an edge is fused across providers. Two providers asserting
// the same citation produce two Edges, each attributed, and the recognition
// that they describe one citation is an explicit Equivalence rather than a
// silent merge.
type Edge struct {
	// Provider is the route/adapter id that asserted this edge.
	Provider string `json:"provider"`
	// Direction is the traversal the edge was retrieved under.
	Direction Direction `json:"direction"`
	Source    Endpoint  `json:"source_work"`
	Target    Endpoint  `json:"target_work"`
	// Status is the provider's own annotation on the edge, empty when the
	// provider said nothing.
	Status EdgeStatus `json:"status,omitempty"`
	// ProviderEdgeID is the provider's identifier for the edge itself when
	// it has one, e.g. an OpenCitations OCI.
	ProviderEdgeID string `json:"provider_edge_id,omitempty"`
	// Annotations carry per-edge facts a provider publishes that this model
	// has no typed field for, e.g. OpenCitations' timespan, journal_sc, and
	// author_sc. Keys are the provider's own field names, values its own
	// strings. Dropping them would discard published data the gateway has
	// no better place for.
	Annotations map[string]string `json:"annotations,omitempty"`
	RetrievedAt string            `json:"retrieved_at"`
}

// ID addresses this edge inside a Result: provider plus both endpoint keys.
func (e Edge) ID() string {
	return e.Provider + "|" + e.Source.Key() + "->" + e.Target.Key()
}

// Equivalence records that several edges from different providers describe
// one citation. It is a statement about the edges, kept beside them rather
// than applied to them, so a consumer that distrusts the match can ignore
// it and still have every provider's original assertion.
type Equivalence struct {
	// Edges are the Edge.ID values recognized as one citation, sorted.
	Edges []string `json:"edges"`
	// MatchedOn names the identifier schemes that agreed on both ends.
	MatchedOn []string          `json:"matched_on"`
	Evidence  identity.Evidence `json:"evidence"`
}

// ProviderOutcome is what happened when one provider was consulted.
type ProviderOutcome string

const (
	// OutcomeOK means the provider answered. EdgeCount may be zero, which
	// means that provider has no edges for this work.
	OutcomeOK ProviderOutcome = "ok"
	// OutcomeUnsupportedIdentifier means the provider was not asked because
	// it cannot express a query for this identifier scheme. Distinct from
	// answering with nothing.
	OutcomeUnsupportedIdentifier ProviderOutcome = "unsupported_identifier"
	// OutcomeUnsupportedDirection means the provider does not serve this
	// traversal at all, e.g. Crossref has no cited-by.
	OutcomeUnsupportedDirection ProviderOutcome = "unsupported_direction"
	OutcomeUpstreamError        ProviderOutcome = "upstream_error"
	OutcomeUpstreamStatus       ProviderOutcome = "upstream_status"
	OutcomeTimeout              ProviderOutcome = "timeout"
)

// ProviderReport is the per-provider account every response carries. A
// provider appears here whether it answered, failed, or was never asked,
// because "consulted and returned nothing" and "never asked" are different
// facts and a consumer must be able to tell them apart.
type ProviderReport struct {
	Provider  string          `json:"provider"`
	Consulted bool            `json:"consulted"`
	Outcome   ProviderOutcome `json:"outcome"`
	EdgeCount int             `json:"edge_count"`
	// UpstreamStatus is set when Outcome is OutcomeUpstreamStatus.
	UpstreamStatus int `json:"upstream_status,omitempty"`
	// PaginationModel names how this provider pages its edge list, so a
	// consumer knows whether EdgeCount is the whole set or one page.
	PaginationModel string `json:"pagination_model,omitempty"`
	// Truncated reports that the provider has more edges than this
	// response carries.
	Truncated bool `json:"truncated,omitempty"`
	// NextCursor is the opaque handle for the next page, when the provider
	// supplied one.
	NextCursor string `json:"next_cursor,omitempty"`
	// Coverage states which collection answered and what it covers, so a
	// consumer reading edge_count knows whose view it is looking at.
	Coverage string `json:"coverage,omitempty"`
}

// Result is one citation-graph query's full answer.
type Result struct {
	Direction Direction `json:"direction"`
	// Query is the identifier the traversal started from, normalized, with
	// its raw form retained.
	Query identity.Identifier `json:"query"`
	Edges []Edge              `json:"edges"`
	// Equivalences links edges from different providers that describe one
	// citation.
	Equivalences []Equivalence `json:"equivalences,omitempty"`
	// ProvidersConsulted covers every provider in the registry for this
	// direction, answered or not.
	ProvidersConsulted []ProviderReport `json:"providers_consulted"`
	// AbsenceNotice is AbsenceNotice, restated in the payload so a consumer
	// reading only the response never has to find the documentation.
	AbsenceNotice string `json:"absence_notice"`
}

// Build assembles a Result, sorting edges deterministically and computing
// equivalences. Edges are never dropped, deduplicated, or rewritten.
func Build(direction Direction, query identity.Identifier, edges []Edge, reports []ProviderReport, at time.Time) Result {
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Provider != edges[j].Provider {
			return edges[i].Provider < edges[j].Provider
		}
		if edges[i].Source.Key() != edges[j].Source.Key() {
			return edges[i].Source.Key() < edges[j].Source.Key()
		}
		return edges[i].Target.Key() < edges[j].Target.Key()
	})
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].Provider < reports[j].Provider })
	if edges == nil {
		edges = []Edge{}
	}
	if reports == nil {
		reports = []ProviderReport{}
	}
	return Result{
		Direction:          direction,
		Query:              query,
		Edges:              edges,
		Equivalences:       Equivalences(edges, at),
		ProvidersConsulted: reports,
		AbsenceNotice:      AbsenceNotice,
	}
}

// Equivalences groups edges from different providers that share an exact
// identifier on both ends. Only exact identifier agreement counts: there is
// no similarity path into an equivalence, so a consumer acting on one is
// acting on identifiers the providers themselves published.
//
// Edges from a single provider are never grouped with each other; a
// provider asserting two edges is asserting two edges.
func Equivalences(edges []Edge, at time.Time) []Equivalence {
	type group struct {
		ids       map[string]bool
		providers map[string]bool
		schemes   map[string]bool
	}
	groups := map[string]*group{}
	order := []string{}

	for i := range edges {
		for _, sk := range edges[i].Source.Keys() {
			for _, tk := range edges[i].Target.Keys() {
				if strings.HasPrefix(sk, "raw:") || strings.HasPrefix(tk, "raw:") {
					continue
				}
				if schemeOf(sk) != schemeOf(tk) {
					continue
				}
				pairKey := sk + "->" + tk
				g, ok := groups[pairKey]
				if !ok {
					g = &group{ids: map[string]bool{}, providers: map[string]bool{}, schemes: map[string]bool{}}
					groups[pairKey] = g
					order = append(order, pairKey)
				}
				g.ids[edges[i].ID()] = true
				g.providers[edges[i].Provider] = true
				g.schemes[schemeOf(sk)] = true
			}
		}
	}

	sort.Strings(order)
	var out []Equivalence
	seen := map[string]bool{}
	for _, pairKey := range order {
		g := groups[pairKey]
		if len(g.providers) < 2 {
			continue
		}
		ids := sortedKeys(g.ids)
		fingerprint := strings.Join(ids, "|")
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		out = append(out, Equivalence{
			Edges:     ids,
			MatchedOn: sortedKeys(g.schemes),
			Evidence: identity.GatewayInferred(
				identity.MethodSharedIdentifier, strings.Join(sortedKeys(g.schemes), ","), 0, at),
		})
	}
	return out
}

func schemeOf(key string) string {
	if i := strings.Index(key, ":"); i > 0 {
		return key[:i]
	}
	return key
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
