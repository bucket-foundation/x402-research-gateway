package handler

// Rights-aware asset discovery endpoint (x402-research-gateway#8).
//
// One identifier goes in. The gateway asks every provider whose adapter
// implements provider.AssetProvider what representations it publishes for
// that work, and returns the locations with rights stated per
// representation.
//
// It discovers. It does not fetch, mirror, cache, or re-serve content. The
// only upstream calls this endpoint makes are to the configured provider
// metadata routes; no asset URL is ever dereferenced, under any
// configuration, and a test asserts a host that only serves asset content
// receives zero requests.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/asset"
	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

type assetFanResult struct {
	assets []asset.Asset
	report asset.ProviderReport
}

// assetRouteIDs lists every configured route backed by an adapter that
// implements AssetProvider, in sorted order.
func (h *Handler) assetRouteIDs() []string {
	allow := map[string]bool{}
	for _, id := range h.cfg.Feed402.Assets.ProviderRouteIDs {
		allow[id] = true
	}
	var out []string
	for id, adapter := range h.providers {
		if adapter == nil || adapter.AssetProvider == nil {
			continue
		}
		if len(allow) > 0 && !allow[id] {
			continue
		}
		// A route that is not configured on this deployment is not
		// consulted. CORE is the live case: without an operator-supplied
		// key its route is absent, and the report says not_configured
		// rather than pretending the provider had nothing.
		if h.findRouteByID(id) == nil {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// handleAssets serves the asset-discovery endpoint.
func (h *Handler) handleAssets(w http.ResponseWriter, r *http.Request) {
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}
	if paymentHeader == "" {
		h.returnPaymentError(w, r, "payment required for assets")
		return
	}
	route, ok := h.routeIndex[r.Method+" "+r.URL.Path]
	if !ok {
		http.Error(w, "assets route not registered", http.StatusNotFound)
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
			"error": "assets requires a non-empty `identifier` field",
		})
		return
	}
	queryID, recognized := identity.Parse(body.Identifier)
	if !recognized {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":            "identifier matched no known scheme, so no provider can express an asset lookup for it",
			"identifier":       body.Identifier,
			"known_schemes":    schemeNames(),
			"discovery_notice": asset.DiscoveryNotice,
		})
		return
	}

	assets, reports := h.fanOutAssets(r.Context(), body.Identifier)
	result := asset.Build(queryID, assets, reports, time.Now())

	citations := h.assetCitations(route, result)
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

// fanOutAssets queries every asset-capable provider, bounded by the
// configured concurrency. Every provider considered produces a report.
func (h *Handler) fanOutAssets(ctx context.Context, identifier string) ([]asset.Asset, []asset.ProviderReport) {
	routeIDs := h.assetRouteIDs()
	results := make([]assetFanResult, len(routeIDs))

	maxConc := h.cfg.Feed402.Assets.MaxConcurrency
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
			results[i] = h.assetsFromProvider(ctx, routeID, identifier)
		}(i, id)
	}
	wg.Wait()

	var assets []asset.Asset
	reports := make([]asset.ProviderReport, 0, len(results))
	for _, res := range results {
		assets = append(assets, res.assets...)
		reports = append(reports, res.report)
	}
	return assets, reports
}

// assetsFromProvider calls one provider's metadata route and reads the
// locations out of the records it returned. It never calls a location.
func (h *Handler) assetsFromProvider(ctx context.Context, routeID, identifier string) assetFanResult {
	report := asset.ProviderReport{Provider: routeID}
	adapter := h.providers[routeID]
	route := h.findRouteByID(routeID)
	if adapter == nil || route == nil || adapter.Normalizer == nil || adapter.AssetProvider == nil {
		report.Outcome = asset.OutcomeNotConfigured
		return assetFanResult{report: report}
	}
	report.MetadataRights = metadataRightsFor(route)

	timeout := time.Duration(h.cfg.Feed402.Assets.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	report.Consulted = true
	req := resolveUpstreamRequest(callCtx, route, identifier)
	upstream, err := proxyToUpstream(callCtx, h.httpClient, route, req)
	if err != nil {
		report.Outcome = asset.OutcomeUpstreamError
		if callCtx.Err() != nil {
			report.Outcome = asset.OutcomeTimeout
		}
		// The error text can carry the upstream URL, which for a
		// key-bearing provider carries the key. It stays in the log.
		slog.Warn("assets: provider call failed", "provider", routeID, "outcome", report.Outcome)
		return assetFanResult{report: report}
	}
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		slog.Warn("assets: provider returned non-2xx", "provider", routeID, "status", upstream.StatusCode)
		report.Outcome = asset.OutcomeUpstreamStatus
		report.UpstreamStatus = upstream.StatusCode
		return assetFanResult{report: report}
	}

	report.Outcome = asset.OutcomeOK
	at := time.Now().UTC().Format(time.RFC3339)
	var out []asset.Asset
	for _, rec := range adapter.Normalizer.Normalize(upstream.Body) {
		availability := asset.AvailabilityRetrievable
		if adapter.AvailabilityReporter != nil {
			availability = mapAvailability(adapter.AvailabilityReporter.Availability(rec))
		}
		for _, a := range adapter.AssetProvider.Assets(rec) {
			if a.CanonicalURL == "" {
				continue
			}
			out = append(out, asset.Asset{
				Provider:       routeID,
				AssetID:        a.AssetID,
				Representation: a.Representation,
				CanonicalURL:   a.CanonicalURL,
				Rights:         mapRights(a.Rights, route),
				Availability:   availability,
				RetrievedAt:    at,
			})
		}
	}
	report.AssetCount = len(out)
	return assetFanResult{assets: out, report: report}
}

// mapAvailability converts the adapter-layer statement to the wire model.
// An adapter value this handler does not recognize becomes unknown rather
// than a guess.
func mapAvailability(a provider.Availability) asset.Availability {
	switch a {
	case provider.AvailabilityRetrievable:
		return asset.AvailabilityRetrievable
	case provider.AvailabilityRestricted:
		return asset.AvailabilityRestricted
	case provider.AvailabilityAbsent:
		return asset.AvailabilityAbsent
	default:
		return asset.AvailabilityUnknown
	}
}

// licenseAllowsResearchActions recognizes only licenses that provide
// sufficiently broad rights for automated research acquisition. Restricted,
// absent, or ambiguous licenses remain unknown.
func licenseAllowsResearchActions(license, licenseURL string) bool {
	name := strings.ToLower(strings.TrimSpace(license))
	ref := strings.ToLower(strings.TrimSpace(licenseURL))
	combined := name + " " + ref

	// Do not convert conditional or no-derivatives licenses into an
	// unconditional machine-action permission.
	if strings.Contains(combined, "by-nc") ||
		strings.Contains(combined, "by-nd") {
		return false
	}

	if name == "cc0" ||
		strings.HasPrefix(name, "cc0-") ||
		strings.Contains(combined, "public-domain") ||
		strings.Contains(combined, "publicdomain/") {
		return true
	}

	if name == "cc-by" ||
		strings.HasPrefix(name, "cc-by-") ||
		strings.Contains(ref, "/licenses/by/") ||
		strings.Contains(ref, "/licenses/by-sa/") {
		return true
	}

	return false
}

// mapRights converts an adapter rights statement to the wire model. Only an
// explicit allowance carries across as an allowance; every other value,
// including an empty one, becomes unknown.
func mapRights(r provider.Rights, route *config.RouteConfig) asset.Rights {
	out := asset.Rights{
		License:        r.License,
		LicenseURL:     r.LicenseURL,
		Redistribution: asset.RedistributionUnknown,
		TDM:            asset.PermissionUnknown,
		Retention:      asset.PermissionUnknown,
		Source:         r.Source,
		FreeToRead:     r.FreeToRead,
	}
	switch r.Redistribution {
	case provider.RedistributionAllowed:
		out.Redistribution = asset.RedistributionAllowed
	case provider.RedistributionProhibited:
		out.Redistribution = asset.RedistributionProhibited
	}
	if licenseAllowsResearchActions(r.License, r.LicenseURL) {
		out.TDM = asset.PermissionAllowed
		out.Retention = asset.PermissionAllowed
	}
	if route != nil {
		out.TermsURL = route.Citation.ProviderURL
	}
	return out.Normalize()
}

// metadataRightsFor states the provider's licence over the records it
// serves, from route config. It is reported apart from every asset's
// content rights and never substitutes for one.
func metadataRightsFor(route *config.RouteConfig) asset.Rights {
	if route == nil {
		return asset.Rights{Redistribution: asset.RedistributionUnknown}.Normalize()
	}
	return asset.Rights{
		License:        route.Citation.License,
		Redistribution: asset.RedistributionUnknown,
		Source:         "route citation policy for " + route.ID + " (metadata only)",
		TermsURL:       route.Citation.ProviderURL,
	}.Normalize()
}

// assetCitations emits one feed402 §3 citation per contributing provider,
// bound by result_index to that provider's assets.
func (h *Handler) assetCitations(route *config.RouteConfig, set asset.Set) []feed402CitationSource {
	providerName := h.cfg.Feed402.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	indices := map[string][]int{}
	order := []string{}
	for i, a := range set.Assets {
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
			SourceID:     prefix + ":assets:" + shortHash(p, route.Path),
			Provider:     providerName,
			RetrievedAt:  now,
			License:      license,
			CanonicalURL: providerURL,
			ResultIndex:  indices[p],
		})
	}
	if len(out) == 0 {
		// A negative answer is still a paid answer and §3 requires a
		// non-empty citation array.
		out = append(out, feed402CitationSource{
			Type:        "source",
			SourceID:    "assets:" + shortHash(route.Path),
			Provider:    providerName,
			RetrievedAt: now,
			License:     h.cfg.Feed402.CitationPolicy,
		})
	}
	return out
}
