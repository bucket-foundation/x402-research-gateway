package handler

// Citation-graph endpoint (x402-research-gateway#6).
//
// One identifier and a direction go in. The gateway asks every provider
// that serves that direction, normalizes each provider's edges without
// fusing them, and returns them with a per-provider account of what
// happened.
//
// The account is the point. A provider that answered with nothing, a
// provider that could not express a query for the caller's identifier
// scheme, a provider that does not serve the direction at all, and a
// provider that timed out are four different facts, and a consumer must be
// able to tell them apart. Reading "zero edges from OpenCitations" as "this
// paper is uncited" is the failure mode this endpoint is shaped to prevent,
// so every response also restates that absence is not evidence.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

type citationFanResult struct {
	edges  []citation.Edge
	report citation.ProviderReport
}

// citationRouteIDs lists every route backed by an adapter that serves the
// citation graph, in sorted order. Direction filtering happens later, so a
// provider that does not serve the requested direction can still be
// reported rather than silently omitted.
func (h *Handler) citationRouteIDs() []string {
	allow := map[string]bool{}
	for _, id := range h.cfg.Feed402.Citations.ProviderRouteIDs {
		allow[id] = true
	}
	var out []string
	for id, adapter := range h.providers {
		if adapter == nil || adapter.CitationGraphProvider == nil {
			continue
		}
		if len(allow) > 0 && !allow[id] {
			continue
		}
		if h.findRouteByID(id) == nil {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// handleCitations serves the citation-graph endpoint.
func (h *Handler) handleCitations(w http.ResponseWriter, r *http.Request) {
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}
	if paymentHeader == "" {
		h.returnPaymentError(w, r, "payment required for citations")
		return
	}
	route, ok := h.routeIndex[r.Method+" "+r.URL.Path]
	if !ok {
		http.Error(w, "citations route not registered", http.StatusNotFound)
		return
	}
	payer, txHash, ok := h.verifyAndSettle(w, r, paymentHeader, route)
	if !ok {
		return
	}

	var body struct {
		Identifier string `json:"identifier"`
		Direction  string `json:"direction"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		buf, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(buf, &body)
	}
	if body.Identifier == "" {
		body.Identifier = r.URL.Query().Get("identifier")
	}
	if body.Direction == "" {
		body.Direction = r.URL.Query().Get("direction")
	}
	if body.Direction == "" {
		body.Direction = string(citation.DirectionReferences)
	}
	if body.Identifier == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "citations requires a non-empty `identifier` field",
		})
		return
	}
	direction := citation.Direction(body.Direction)
	if !direction.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":              "unknown direction",
			"valid_directions":   []string{string(citation.DirectionReferences), string(citation.DirectionCitedBy)},
			"direction_received": body.Direction,
		})
		return
	}

	// The traversal needs a normalized identifier: every provider renders
	// it into its own accepted form from the scheme and value. An
	// identifier no scheme claims cannot be rendered by any provider, so it
	// is refused here rather than sent upstream as a bare string that would
	// produce a plausible-looking wrong answer.
	queryID, recognized := identity.Parse(body.Identifier)
	if !recognized {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":          "identifier matched no known scheme, so no provider can express a citation query for it",
			"identifier":     body.Identifier,
			"known_schemes":  schemeNames(),
			"absence_notice": citation.AbsenceNotice,
		})
		return
	}

	edges, reports := h.fanOutCitations(r.Context(), direction, queryID)
	result := citation.Build(direction, queryID, edges, reports, time.Now())

	citations := h.citationEnvelopeCitations(route, result)
	if txHash == "" {
		txHash = "pending:" + shortHash(payer, route.Path, body.Identifier, body.Direction)
	}
	dataBytes, _ := json.Marshal(result)
	env := feed402Envelope{
		Data:     dataBytes,
		Citation: citations,
		Receipt: feed402Receipt{
			Tier:     route.Feed402Tier,
			PriceUSD: parsePriceUSD(route.Price),
			TX:       txHash,
			PaidAt:   time.Now().UTC().Format(time.RFC3339),
		},
	}
	if len(citations) > 0 {
		first := citations[0]
		env.CitationLegacy = &first
	}
	out, _ := json.Marshal(env)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Feed402-Spec", h.cfg.Feed402.Spec)
	w.Header().Set("X-Research-Route", route.ID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func schemeNames() []string {
	schemes := identity.Schemes()
	out := make([]string, len(schemes))
	for i, s := range schemes {
		out[i] = string(s)
	}
	return out
}

// fanOutCitations queries every citation-capable provider, bounded by the
// configured concurrency. Every provider in the registry produces a report,
// including the ones that were never called.
func (h *Handler) fanOutCitations(
	ctx context.Context, direction citation.Direction, queryID identity.Identifier,
) ([]citation.Edge, []citation.ProviderReport) {
	routeIDs := h.citationRouteIDs()
	results := make([]citationFanResult, len(routeIDs))

	maxConc := h.cfg.Feed402.Citations.MaxConcurrency
	if maxConc < 1 {
		maxConc = 4
	}
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	for i, id := range routeIDs {
		wg.Add(1)
		go func(i int, routeID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = h.citationsFromProvider(ctx, routeID, direction, queryID)
		}(i, id)
	}
	wg.Wait()

	var edges []citation.Edge
	reports := make([]citation.ProviderReport, 0, len(results))
	for _, res := range results {
		edges = append(edges, res.edges...)
		reports = append(reports, res.report)
	}
	return edges, reports
}

// citationsFromProvider calls one provider. Every exit path produces a
// report; none produces silence.
func (h *Handler) citationsFromProvider(
	ctx context.Context, routeID string, direction citation.Direction, queryID identity.Identifier,
) citationFanResult {
	report := citation.ProviderReport{Provider: routeID}
	adapter := h.providers[routeID]
	route := h.findRouteByID(routeID)
	if adapter == nil || route == nil || adapter.CitationGraphProvider == nil {
		report.Outcome = citation.OutcomeUpstreamError
		return citationFanResult{report: report}
	}
	cg := adapter.CitationGraphProvider

	// Coverage is stated whatever the outcome, so a consumer reading
	// "unsupported_identifier" still knows what it would have been asking.
	if cr, ok := cg.(provider.CoverageReporter); ok {
		report.Coverage = cr.Coverage()
	}
	if cg.Direction() != direction {
		report.Outcome = citation.OutcomeUnsupportedDirection
		return citationFanResult{report: report}
	}
	params, ok := cg.EdgeQuery(queryID)
	if !ok {
		report.Outcome = citation.OutcomeUnsupportedIdentifier
		return citationFanResult{report: report}
	}

	timeout := time.Duration(h.cfg.Feed402.Citations.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	report.Consulted = true
	req := citationUpstreamRequest(callCtx, route, params)
	upstream, err := proxyToUpstream(callCtx, h.httpClient, route, req)
	if err != nil {
		report.Outcome = citation.OutcomeUpstreamError
		if callCtx.Err() != nil {
			report.Outcome = citation.OutcomeTimeout
		}
		// The error text can carry the upstream URL, which for a
		// token-bearing provider carries the token. It stays in the log.
		slog.Warn("citations: provider call failed", "provider", routeID, "outcome", report.Outcome)
		return citationFanResult{report: report}
	}
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		report.Outcome = citation.OutcomeUpstreamStatus
		report.UpstreamStatus = upstream.StatusCode
		slog.Warn("citations: provider returned non-2xx", "provider", routeID, "status", upstream.StatusCode)
		return citationFanResult{report: report}
	}

	edges := cg.Edges(queryID, upstream.Body, time.Now())
	for i := range edges {
		edges[i].Provider = routeID
	}
	model, truncated, cursor := cg.EdgePagination(upstream.Body)
	report.Outcome = citation.OutcomeOK
	report.EdgeCount = len(edges)
	report.PaginationModel = model
	report.Truncated = truncated
	report.NextCursor = cursor
	return citationFanResult{edges: edges, report: report}
}

// citationUpstreamRequest builds the synthetic request the declarative
// proxy consumes. The adapter supplied the parameter names and values, so
// nothing here guesses at a provider's query shape.
func citationUpstreamRequest(ctx context.Context, route *config.RouteConfig, params map[string]string) *http.Request {
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	u := &url.URL{Path: route.Path, RawQuery: q.Encode()}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	req.URL = u
	return req
}

// citationEnvelopeCitations emits one feed402 §3 citation per provider that
// contributed edges, bound by result_index to the edges it asserted. A
// provider consulted that returned nothing gets no citation, because it
// grounded nothing; its report in the payload is where its answer lives.
func (h *Handler) citationEnvelopeCitations(route *config.RouteConfig, result citation.Result) []feed402CitationSource {
	providerName := h.cfg.Feed402.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	indices := map[string][]int{}
	var order []string
	for i, e := range result.Edges {
		if _, seen := indices[e.Provider]; !seen {
			order = append(order, e.Provider)
		}
		indices[e.Provider] = append(indices[e.Provider], i)
	}
	sort.Strings(order)

	var out []feed402CitationSource
	for _, p := range order {
		upstreamRoute := h.findRouteByID(p)
		prefix, providerURL, license := p, "", h.cfg.Feed402.CitationPolicy
		if upstreamRoute != nil {
			if upstreamRoute.Citation.SourcePrefix != "" {
				prefix = upstreamRoute.Citation.SourcePrefix
			}
			providerURL = upstreamRoute.Citation.ProviderURL
			license = licenseFor(upstreamRoute, &h.cfg.Feed402)
		}
		out = append(out, feed402CitationSource{
			Type:         "source",
			SourceID:     prefix + ":citations:" + shortHash(p, string(result.Direction), result.Query.String()),
			Provider:     providerName,
			RetrievedAt:  now,
			License:      license,
			CanonicalURL: providerURL,
			ResultIndex:  indices[p],
		})
	}
	if len(out) == 0 {
		out = append(out, feed402CitationSource{
			Type:        "source",
			SourceID:    "citations:" + shortHash(route.Path, string(result.Direction), result.Query.String()),
			Provider:    providerName,
			RetrievedAt: now,
			License:     h.cfg.Feed402.CitationPolicy,
		})
	}
	return out
}
