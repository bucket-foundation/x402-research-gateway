package handler

// Federated search endpoint (x402-research-gateway#4).
//
// One query fans out to every provider declaring the requested capability
// and returns a merged result set that keeps each provider's own view
// intact. Direct single-provider routes are untouched: federation is an
// operation alongside them.
//
// Cost is knowable before payment. GET on the same path is free and returns
// the estimate, so an agent sees what a fan-out costs and can set a cap
// before it pays for one.

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

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/federate"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

type federatedFanResult struct {
	results []federate.Result
	report  federate.ProviderReport
}

// federatedCandidates lists the routes eligible for a fan-out under one
// capability, sorted. Selection is by declared capability from the adapter
// registry, so asking for a capability a provider does not implement never
// reaches that provider.
func (h *Handler) federatedCandidates(capability provider.Capability) []string {
	allow := map[string]bool{}
	for _, id := range h.cfg.Feed402.Federated.ProviderRouteIDs {
		allow[id] = true
	}
	var out []string
	for id, adapter := range h.providers {
		if adapter == nil || !adapter.Supports(capability) {
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

// federatedPrices maps each candidate route to its declared price.
func (h *Handler) federatedPrices(routeIDs []string) map[string]float64 {
	prices := make(map[string]float64, len(routeIDs))
	for _, id := range routeIDs {
		if route := h.findRouteByID(id); route != nil {
			prices[id] = parsePriceUSD(route.Price)
		}
	}
	return prices
}

type federatedRequest struct {
	Query      string  `json:"query"`
	Capability string  `json:"capability"`
	MaxCostUSD float64 `json:"max_cost_usd"`
}

func (h *Handler) parseFederatedRequest(r *http.Request) federatedRequest {
	var req federatedRequest
	if r.Body != nil {
		defer r.Body.Close()
		buf, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(buf, &req)
	}
	q := r.URL.Query()
	if req.Query == "" {
		req.Query = firstNonEmptyStr(q.Get("query"), q.Get("q"), q.Get("term"))
	}
	if req.Capability == "" {
		req.Capability = q.Get("capability")
	}
	if req.Capability == "" {
		req.Capability = string(provider.CapSearch)
	}
	if req.MaxCostUSD == 0 {
		if v := q.Get("max_cost_usd"); v != "" {
			req.MaxCostUSD = parsePriceUSD(v)
		}
	}
	return req
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// handleFederatedEstimate serves the free pre-payment cost estimate. It
// makes no upstream call and returns no results, so an agent can price a
// fan-out and choose a cap without paying for one.
func (h *Handler) handleFederatedEstimate(w http.ResponseWriter, r *http.Request) {
	req := h.parseFederatedRequest(r)
	capability := provider.Capability(req.Capability)
	candidates := h.federatedCandidates(capability)
	est := federate.Estimate(req.Capability, h.federatedPrices(candidates), req.MaxCostUSD)
	writeJSON(w, http.StatusOK, map[string]any{
		"capability":     req.Capability,
		"cost":           est,
		"providers":      candidates,
		"fusion_method":  "reciprocal_rank_fusion",
		"fusion_note":    federate.FusionNote,
		"payment_method": "POST to this path with an x402 payment header",
	})
}

// handleFederated serves the paid federated search.
func (h *Handler) handleFederated(w http.ResponseWriter, r *http.Request) {
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}
	if paymentHeader == "" {
		h.returnPaymentError(w, r, "payment required for federated search")
		return
	}
	route, ok := h.routeIndex[r.Method+" "+r.URL.Path]
	if !ok {
		http.Error(w, "federated route not registered", http.StatusNotFound)
		return
	}
	payer, txHash, ok := h.verifyAndSettle(w, r, paymentHeader, route)
	if !ok {
		return
	}

	req := h.parseFederatedRequest(r)
	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "federated search requires a non-empty `query` field",
		})
		return
	}
	capability := provider.Capability(req.Capability)

	candidates := h.federatedCandidates(capability)
	est := federate.Estimate(req.Capability, h.federatedPrices(candidates), req.MaxCostUSD)
	selected := est.Included()

	results, reports := h.fanOutFederated(r.Context(), selected, req.Query)

	// Every candidate the cap excluded is still reported, so a caller sees
	// which providers its own cap kept out rather than inferring silence.
	inSelected := map[string]bool{}
	for _, id := range selected {
		inSelected[id] = true
	}
	for _, id := range candidates {
		if inSelected[id] {
			continue
		}
		reports = append(reports, federate.ProviderReport{
			Provider: id,
			Outcome:  federate.OutcomeCostCapExceeded,
			PriceUSD: h.federatedPrices([]string{id})[id],
		})
	}

	resp := federate.Merge(req.Query, req.Capability, results, reports, est, time.Now())
	citations := h.federatedCitations(route, resp)
	if txHash == "" {
		txHash = "pending:" + shortHash(payer, route.Path, req.Query, req.Capability)
	}
	dataBytes, _ := json.Marshal(resp)
	env := feed402Envelope{
		Data:     dataBytes,
		Citation: citations,
		Receipt: feed402Receipt{
			Tier:     route.Feed402Tier,
			PriceUSD: resp.Cost.TotalUSD,
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

// fanOutFederated queries the selected providers concurrently, bounded, and
// assembles results in route-id order so the output does not depend on
// which upstream answered first.
func (h *Handler) fanOutFederated(ctx context.Context, routeIDs []string, query string) ([]federate.Result, []federate.ProviderReport) {
	results := make([]federatedFanResult, len(routeIDs))
	maxConc := h.cfg.Feed402.Federated.MaxConcurrency
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
			results[i] = h.federatedFromProvider(ctx, routeID, query)
		}(i, id)
	}
	wg.Wait()

	var merged []federate.Result
	reports := make([]federate.ProviderReport, 0, len(results))
	for _, res := range results {
		merged = append(merged, res.results...)
		reports = append(reports, res.report)
	}
	return merged, reports
}

// federatedFromProvider calls one provider under its own deadline. One slow
// upstream cannot extend the request past that deadline, and its failure is
// recorded rather than rendered as an empty result.
func (h *Handler) federatedFromProvider(ctx context.Context, routeID, query string) federatedFanResult {
	route := h.findRouteByID(routeID)
	adapter := h.providers[routeID]
	report := federate.ProviderReport{Provider: routeID}
	if route == nil || adapter == nil {
		report.Outcome = federate.OutcomeUpstreamError
		return federatedFanResult{report: report}
	}
	report.PriceUSD = parsePriceUSD(route.Price)

	// Per-provider deadline: the route's own timeoutSeconds when set,
	// otherwise the federated default. This is the granularity the routes
	// already declare, so a provider known to be slow is configured once
	// and honored everywhere.
	timeout := time.Duration(route.Upstream.Timeout) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(h.cfg.Feed402.Federated.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Charged tracks whether this provider's price counts toward the call.
	// A provider that was called counts, whatever the upstream then did,
	// because the gateway spent the call. A provider excluded by the cost
	// cap or by capability was never called and never counts.
	report.Consulted = true
	report.Charged = true
	req := federatedUpstreamRequest(callCtx, route, query)
	upstream, err := proxyToUpstream(callCtx, h.httpClient, route, req)
	if err != nil {
		report.Outcome = federate.OutcomeUpstreamError
		if callCtx.Err() != nil {
			report.Outcome = federate.OutcomeTimeout
		}
		// The error text can carry the upstream URL, which for a
		// key-bearing provider carries the key. It stays in the log.
		slog.Warn("federated: provider call failed", "provider", routeID, "outcome", report.Outcome)
		return federatedFanResult{report: report}
	}
	report.LatencyMs = upstream.LatencyMs
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		report.Outcome = federate.OutcomeUpstreamStatus
		report.UpstreamStatus = upstream.StatusCode
		slog.Warn("federated: provider returned non-2xx", "provider", routeID, "status", upstream.StatusCode)
		return federatedFanResult{report: report}
	}

	report.Outcome = federate.OutcomeOK
	if adapter.Normalizer == nil || adapter.CitationProvider == nil {
		// The provider answered but has no normalizer, so there is nothing
		// to merge. Reporting ok with zero results is the accurate answer.
		return federatedFanResult{report: report}
	}
	records := adapter.Normalizer.Normalize(upstream.Body)
	hits := adapter.CitationProvider.Citations(route, records)

	byID := map[string]provider.NormalizedRecord{}
	for _, rec := range records {
		byID[rec.ID] = rec
	}
	out := make([]federate.Result, 0, len(hits))
	for _, hit := range hits {
		res := federate.Result{
			Provider:     routeID,
			SourceID:     hit.SourceID,
			CanonicalURL: hit.CanonicalURL,
			ProviderRank: hit.Rank,
		}
		// hit.SourceID is prefix:id, and the record map is keyed on the
		// bare id, so the suffix after the first colon is the lookup key.
		if rec, ok := byID[bareRecordID(hit.SourceID)]; ok {
			res.Raw = rec.Raw
			if adapter.IdentityProvider != nil {
				res.Identifiers = adapter.IdentityProvider.Identifiers(rec)
			}
		}
		out = append(out, res)
	}
	report.ResultCount = len(out)
	return federatedFanResult{results: out, report: report}
}

// bareRecordID strips the source prefix a CitationProvider added, returning
// the provider-local record id.
func bareRecordID(sourceID string) string {
	for i := 0; i < len(sourceID); i++ {
		if sourceID[i] == ':' {
			return sourceID[i+1:]
		}
	}
	return sourceID
}

// federatedUpstreamRequest fills every parameter name a route declares, plus
// the common aliases, with the query. Same construction as the insight
// tier's retrieval fan-out.
func federatedUpstreamRequest(ctx context.Context, route *config.RouteConfig, query string) *http.Request {
	q := url.Values{}
	for _, p := range route.Upstream.PassThrough {
		q.Set(p, query)
	}
	for _, alias := range []string{"term", "query", "q", "search"} {
		if q.Get(alias) == "" {
			q.Set(alias, query)
		}
	}
	u := &url.URL{Path: route.Path, RawQuery: q.Encode()}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	req.URL = u
	return req
}

// federatedCitations emits one feed402 §3 citation per provider that
// contributed results, bound by result_index to the merged positions of
// that provider's results.
func (h *Handler) federatedCitations(route *config.RouteConfig, resp federate.Response) []feed402CitationSource {
	providerName := h.cfg.Feed402.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	indices := map[string][]int{}
	var order []string
	for i, res := range resp.Results {
		if _, seen := indices[res.Provider]; !seen {
			order = append(order, res.Provider)
		}
		indices[res.Provider] = append(indices[res.Provider], i)
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
			SourceID:     prefix + ":federated:" + shortHash(p, resp.Query, resp.Capability),
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
			SourceID:    "federated:" + shortHash(route.Path, resp.Query, resp.Capability),
			Provider:    providerName,
			RetrievedAt: now,
			License:     h.cfg.Feed402.CitationPolicy,
		})
	}
	return out
}
