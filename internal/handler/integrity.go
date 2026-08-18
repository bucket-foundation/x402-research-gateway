package handler

// Scholarly integrity and update endpoint (x402-research-gateway#9).
//
// One identifier goes in. The gateway asks every provider whose adapter
// implements provider.IntegrityProvider what notices it publishes about
// that work, and returns every provider's assertions side by side.
//
// It does not adjudicate. Two providers disagreeing produce two sets of
// assertions and a flag saying they differ, never a single status the
// gateway picked. A provider that published nothing is reported as having
// published nothing, which is not a clearance, and the response says so.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/integrity"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

type integrityFanResult struct {
	assertions []integrity.Assertion
	report     integrity.ProviderReport
}

// integrityRouteIDs lists every configured route backed by an adapter that
// implements IntegrityProvider, in sorted order.
func (h *Handler) integrityRouteIDs() []string {
	allow := map[string]bool{}
	for _, id := range h.cfg.Feed402.Integrity.ProviderRouteIDs {
		allow[id] = true
	}
	var out []string
	for id, adapter := range h.providers {
		if adapter == nil || adapter.IntegrityProvider == nil {
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

// handleIntegrity serves the integrity endpoint.
func (h *Handler) handleIntegrity(w http.ResponseWriter, r *http.Request) {
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}
	if paymentHeader == "" {
		h.returnPaymentError(w, r, "payment required for integrity")
		return
	}
	route, ok := h.routeIndex[r.Method+" "+r.URL.Path]
	if !ok {
		http.Error(w, "integrity route not registered", http.StatusNotFound)
		return
	}
	payer, txHash, ok := h.verifyAndSettle(w, r, paymentHeader, route)
	if !ok {
		return
	}

	var body struct {
		Identifier string `json:"identifier"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		buf, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(buf, &body)
	}
	if body.Identifier == "" {
		body.Identifier = r.URL.Query().Get("identifier")
	}
	if body.Identifier == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "integrity requires a non-empty `identifier` field",
		})
		return
	}
	queryID, recognized := identity.Parse(body.Identifier)
	if !recognized {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":          "identifier matched no known scheme, so no provider can express an integrity lookup for it",
			"identifier":     body.Identifier,
			"known_schemes":  schemeNames(),
			"absence_notice": integrity.AbsenceNotice,
		})
		return
	}

	assertions, reports := h.fanOutIntegrity(r.Context(), body.Identifier)
	result := integrity.Build(queryID, assertions, reports, time.Now())

	citations := h.integrityCitations(route, result)
	if txHash == "" {
		txHash = "pending:" + shortHash(payer, route.Path, body.Identifier)
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

// fanOutIntegrity queries every integrity-capable provider, bounded by the
// configured concurrency. Every provider considered produces a report.
func (h *Handler) fanOutIntegrity(ctx context.Context, identifier string) ([]integrity.Assertion, []integrity.ProviderReport) {
	routeIDs := h.integrityRouteIDs()
	results := make([]integrityFanResult, len(routeIDs))

	maxConc := h.cfg.Feed402.Integrity.MaxConcurrency
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
			results[i] = h.integrityFromProvider(ctx, routeID, identifier)
		}(i, id)
	}
	wg.Wait()

	var assertions []integrity.Assertion
	reports := make([]integrity.ProviderReport, 0, len(results))
	for _, res := range results {
		assertions = append(assertions, res.assertions...)
		reports = append(reports, res.report)
	}
	return assertions, reports
}

// integrityFromProvider calls one provider. Every exit path produces a
// report; none produces silence, because silence would read as clearance.
func (h *Handler) integrityFromProvider(ctx context.Context, routeID, identifier string) integrityFanResult {
	report := integrity.ProviderReport{Provider: routeID}
	adapter := h.providers[routeID]
	route := h.findRouteByID(routeID)
	if adapter == nil || route == nil || adapter.Normalizer == nil || adapter.IntegrityProvider == nil {
		report.Outcome = integrity.OutcomeNotConfigured
		return integrityFanResult{report: report}
	}
	if cr, ok := adapter.IntegrityProvider.(provider.CoverageReporter); ok {
		report.Coverage = cr.Coverage()
	}

	timeout := time.Duration(h.cfg.Feed402.Integrity.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	report.Consulted = true
	req := resolveUpstreamRequest(callCtx, route, identifier)
	upstream, err := proxyToUpstream(callCtx, h.httpClient, route, req)
	if err != nil {
		report.Outcome = integrity.OutcomeUpstreamError
		if callCtx.Err() != nil {
			report.Outcome = integrity.OutcomeTimeout
		}
		// The error text can carry the upstream URL, which for a
		// key-bearing provider carries the key. It stays in the log.
		slog.Warn("integrity: provider call failed", "provider", routeID, "outcome", report.Outcome)
		return integrityFanResult{report: report}
	}
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		slog.Warn("integrity: provider returned non-2xx", "provider", routeID, "status", upstream.StatusCode)
		report.Outcome = integrity.OutcomeUpstreamStatus
		report.UpstreamStatus = upstream.StatusCode
		return integrityFanResult{report: report}
	}

	report.Outcome = integrity.OutcomeOK
	at := time.Now()
	var out []integrity.Assertion
	for _, rec := range adapter.Normalizer.Normalize(upstream.Body) {
		for _, a := range adapter.IntegrityProvider.IntegrityAssertions(rec, at) {
			// Attribution is the route id, which is what the provider
			// reports and the feed402 citations are keyed on. An adapter
			// names its upstream ("crossref"); the route names the
			// configured way this deployment reached it, and the two must
			// match for a consumer to line an assertion up with the report
			// of who was asked.
			a.Provider = routeID
			out = append(out, a)
		}
	}
	report.AssertionCount = len(out)
	return integrityFanResult{assertions: out, report: report}
}

// integrityCitations emits one feed402 §3 citation per contributing
// provider, bound by result_index to that provider's assertions.
func (h *Handler) integrityCitations(route *config.RouteConfig, result integrity.Result) []feed402CitationSource {
	providerName := h.cfg.Feed402.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	indices := map[string][]int{}
	order := []string{}
	for i, a := range result.Assertions {
		if _, seen := indices[a.Provider]; !seen {
			order = append(order, a.Provider)
		}
		indices[a.Provider] = append(indices[a.Provider], i)
	}
	sort.Strings(order)

	var out []feed402CitationSource
	for _, p := range order {
		pr := h.findRouteByID(p)
		prefix, providerURL, license := p, "", h.cfg.Feed402.CitationPolicy
		if pr != nil {
			if pr.Citation.SourcePrefix != "" {
				prefix = pr.Citation.SourcePrefix
			}
			providerURL = pr.Citation.ProviderURL
			license = licenseFor(pr, &h.cfg.Feed402)
		}
		out = append(out, feed402CitationSource{
			Type:         "source",
			SourceID:     prefix + ":integrity:" + shortHash(p, route.Path),
			Provider:     providerName,
			RetrievedAt:  now,
			License:      license,
			CanonicalURL: providerURL,
			ResultIndex:  indices[p],
		})
	}
	if len(out) == 0 {
		// A work nobody published a notice for is still a paid answer, and
		// §3 requires a non-empty citation array.
		out = append(out, feed402CitationSource{
			Type:        "source",
			SourceID:    "integrity:" + shortHash(route.Path),
			Provider:    providerName,
			RetrievedAt: now,
			License:     h.cfg.Feed402.CitationPolicy,
		})
	}
	return out
}
