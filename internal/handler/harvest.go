package handler

// Resumable harvest endpoint (x402-research-gateway#10).
//
// POST /research/harvest with {route, query, page_size} returns one page
// plus a signed cursor. Present that cursor back and the next page comes
// from the exact position the last one ended at, so an interrupted walk of
// a large result set resumes instead of restarting.
//
// The gateway runs no harvest and stores no harvest state. It hands the
// client everything needed to run its own, and every page is its own paid
// call, so a cursor is a position rather than an entitlement: presenting
// one without payment buys nothing, and forging one buys nothing either.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/harvest"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

const (
	defaultHarvestPageSize = 25
	maxHarvestPageSize     = 200
)

// Harvest failure reasons. Coarse by design: a client needs to know the page
// did not arrive, and the detail belongs in gateway logs where it cannot
// carry an upstream URL to a caller.
var (
	errHarvestUpstream = errors.New("upstream_error")
	errHarvestStatus   = errors.New("upstream_status")
	errHarvestTimeout  = errors.New("timeout")
)

// harvestRouteIDs lists every configured route backed by an adapter that
// implements Paginator, in sorted order.
func (h *Handler) harvestRouteIDs() []string {
	allow := map[string]bool{}
	for _, id := range h.cfg.Feed402.Harvest.ProviderRouteIDs {
		allow[id] = true
	}
	var out []string
	for id, adapter := range h.providers {
		if adapter == nil || adapter.Paginator == nil {
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

type harvestRequest struct {
	Route    string `json:"route"`
	Query    string `json:"query"`
	PageSize int    `json:"page_size"`
	Cursor   string `json:"cursor"`
}

// handleHarvest serves one page of a resumable harvest.
func (h *Handler) handleHarvest(w http.ResponseWriter, r *http.Request) {
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}
	if paymentHeader == "" {
		h.returnPaymentError(w, r, "payment required for harvest")
		return
	}
	route, ok := h.routeIndex[r.Method+" "+r.URL.Path]
	if !ok {
		http.Error(w, "harvest route not registered", http.StatusNotFound)
		return
	}
	payer, txHash, ok := h.verifyAndSettle(w, r, paymentHeader, route)
	if !ok {
		return
	}

	var body harvestRequest
	if r.Body != nil {
		defer r.Body.Close()
		buf, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(buf, &body)
	}
	if body.Route == "" {
		body.Route = r.URL.Query().Get("route")
	}
	if body.Query == "" {
		body.Query = r.URL.Query().Get("query")
	}
	if body.Cursor == "" {
		body.Cursor = r.URL.Query().Get("cursor")
	}

	// A presented cursor carries the route and the query fingerprint, so a
	// resume needs nothing else. It is verified before it is read.
	var prev harvest.Cursor
	resuming := false
	if body.Cursor != "" {
		decoded, err := h.harvestSigner.Decode(body.Cursor)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "cursor is not one this gateway issued",
			})
			return
		}
		prev, resuming = decoded, true
		if body.Route == "" {
			body.Route = prev.Provider
		}
		if body.Route != prev.Provider {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":           "cursor belongs to a different provider",
				"cursor_provider": prev.Provider,
				"route_requested": body.Route,
			})
			return
		}
		if prev.Exhausted {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":        "this harvest is finished; the provider published no page after the last one",
				"result_count": prev.ResultCount,
			})
			return
		}
	}
	if body.Route == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":              "harvest requires a `route`, or a `cursor` naming one",
			"harvestable_routes": h.harvestRouteIDs(),
		})
		return
	}

	adapter := h.providers[body.Route]
	target := h.findRouteByID(body.Route)
	if adapter == nil || adapter.Paginator == nil || target == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":              "route is not harvestable on this deployment",
			"route_requested":    body.Route,
			"harvestable_routes": h.harvestRouteIDs(),
		})
		return
	}
	if body.Query == "" && !resuming {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "harvest requires a non-empty `query` on the first page",
		})
		return
	}

	pageSize := body.PageSize
	if pageSize <= 0 {
		pageSize = defaultHarvestPageSize
	}
	if pageSize > maxHarvestPageSize {
		pageSize = maxHarvestPageSize
	}

	page, err := h.harvestPage(r.Context(), body.Route, target, adapter, body.Query, prev.NextCursor, pageSize)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":    "the provider did not serve this page",
			"outcome":  err.Error(),
			"provider": body.Route,
			// The last good cursor is echoed back so a client can retry the
			// same position rather than losing its place on one failure.
			"cursor": body.Cursor,
		})
		return
	}

	cursor := page.cursor
	if resuming {
		cursor = prev.Continue(cursor)
	} else {
		cursor.ResultCount = cursor.PageResultCount
		cursor.StartedAt = cursor.RetrievedAt
		cursor.StartedRelease = cursor.ProviderRelease
	}
	// A query fingerprint identifies the query without revealing it. On a
	// resume the presented one is carried forward, so a client can prove
	// two pages belong to one harvest.
	cursor.QueryFingerprint = h.harvestSigner.QueryFingerprint(body.Query)
	if resuming && body.Query == "" {
		cursor.QueryFingerprint = prev.QueryFingerprint
	}

	token, err := h.harvestSigner.Encode(cursor)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not issue a cursor"})
		return
	}

	dataMap := map[string]any{
		"provider":     body.Route,
		"page_size":    pageSize,
		"records":      page.hits,
		"cursor":       token,
		"cursor_state": cursor,
		// cursor_ephemeral says whether outstanding cursors survive a
		// gateway restart on this deployment, so a client harvesting over
		// hours knows what it is holding.
		"cursor_ephemeral": h.harvestSigner.Ephemeral(),
	}
	if cursor.ReleaseChanged {
		dataMap["release_notice"] = harvest.ReleaseNotice
	}
	dataBytes, _ := json.Marshal(dataMap)

	if txHash == "" {
		txHash = "pending:" + shortHash(payer, route.Path, body.Route, body.Query)
	}
	citations := h.harvestCitations(route, target, cursor, len(page.hits))
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

type harvestPageResult struct {
	hits   []feed402Hit
	cursor harvest.Cursor
}

// harvestPage fetches one page and builds the cursor describing it.
func (h *Handler) harvestPage(
	ctx context.Context, routeID string, route *config.RouteConfig,
	adapter *provider.Adapter, query string, pos harvest.Position, pageSize int,
) (harvestPageResult, error) {
	timeout := time.Duration(h.cfg.Feed402.Harvest.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The declarative proxy forwards only a route's declared passthrough
	// params, so the paginator's own parameters are added to a copy of the
	// route for this call. The configured route is left untouched, and the
	// copy adds nothing else: no header, no credential, no base URL change.
	pageParams := adapter.Paginator.PageParams(pos, pageSize)
	paged := *route
	paged.Upstream.PassThrough = append(append([]string{}, route.Upstream.PassThrough...),
		sortedParamNames(pageParams)...)

	req := harvestUpstreamRequest(callCtx, route, pageParams, query)
	upstream, err := proxyToUpstream(callCtx, h.httpClient, &paged, req)
	if err != nil {
		if callCtx.Err() != nil {
			return harvestPageResult{}, errHarvestTimeout
		}
		// The error text can carry the upstream URL, which for a
		// key-bearing provider carries the key. It stays in the log.
		slog.Warn("harvest: provider call failed", "provider", routeID)
		return harvestPageResult{}, errHarvestUpstream
	}
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		slog.Warn("harvest: provider returned non-2xx", "provider", routeID, "status", upstream.StatusCode)
		return harvestPageResult{}, errHarvestStatus
	}

	var records []provider.NormalizedRecord
	if adapter.Normalizer != nil {
		records = adapter.Normalizer.Normalize(upstream.Body)
	}
	var hits []feed402Hit
	if adapter.CitationProvider != nil {
		for _, hit := range adapter.CitationProvider.Citations(route, records) {
			hits = append(hits, feed402Hit{
				SourceID:     hit.SourceID,
				CanonicalURL: hit.CanonicalURL,
				Rank:         hit.Rank,
			})
		}
	}

	next, more := adapter.Paginator.NextPosition(upstream.Body, pos, pageSize)
	cursor := harvest.Stamp(harvest.Cursor{
		// The fingerprint is computed over the sanitized upstream URL, so a
		// key-bearing or mailto-bearing request never reaches a hash a
		// holder could brute-force back to the credential.
		RequestFingerprint: h.harvestSigner.ProviderRequestFingerprint(
			firstNonEmptyString(route.Upstream.Method, "GET"), upstream.RequestURL),
		Provider:           routeID,
		PaginationModel:    adapter.Paginator.Model(),
		ProviderCursor:     pos,
		NextCursor:         next,
		Exhausted:          !more,
		PageResultCount:    len(records),
		UpstreamRequestID:  upstreamRequestID(upstream.Header),
		ResponseSHA256:     harvest.ResponseSHA256(upstream.Body),
		RateLimitRemaining: rateLimitRemaining(upstream.Header),
		RetryAfter:         upstream.Header.Get("Retry-After"),
	}, time.Now())
	if adapter.ReleaseReporter != nil {
		cursor.ProviderRelease = adapter.ReleaseReporter.ProviderRelease(upstream.Body)
	}
	if cursor.ProviderRelease == "" {
		cursor.ProviderRelease = upstream.Header.Get("X-API-Version")
	}
	return harvestPageResult{hits: hits, cursor: cursor}, nil
}

// harvestUpstreamRequest builds the synthetic request the declarative proxy
// consumes: the route's declared passthrough params filled with the query,
// plus the paginator's own position parameters.
func harvestUpstreamRequest(
	ctx context.Context, route *config.RouteConfig,
	pageParams map[string]string, query string,
) *http.Request {
	q := url.Values{}
	for _, name := range route.Upstream.PassThrough {
		q.Set(name, query)
	}
	for _, alias := range []string{"term", "query", "q", "search"} {
		if q.Get(alias) == "" {
			q.Set(alias, query)
		}
	}
	for k, v := range pageParams {
		q.Set(k, v)
	}
	u := &url.URL{Path: route.Path, RawQuery: q.Encode()}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	req.URL = u
	return req
}

// harvestCitations emits the feed402 §3 citation for the page, carrying the
// execution provenance the cursor already computed so the two agree.
func (h *Handler) harvestCitations(
	harvestRoute, target *config.RouteConfig, cursor harvest.Cursor, hitCount int,
) []feed402CitationSource {
	providerName := h.cfg.Feed402.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}
	prefix, providerURL, license := cursor.Provider, "", h.cfg.Feed402.CitationPolicy
	if target != nil {
		if target.Citation.SourcePrefix != "" {
			prefix = target.Citation.SourcePrefix
		}
		providerURL = target.Citation.ProviderURL
		license = licenseFor(target, &h.cfg.Feed402)
	}
	indices := make([]int, 0, hitCount)
	for i := 0; i < hitCount; i++ {
		indices = append(indices, i)
	}
	return []feed402CitationSource{{
		Type:         "source",
		SourceID:     prefix + ":harvest:" + shortHash(cursor.Provider, harvestRoute.Path, cursor.RequestFingerprint),
		Provider:     providerName,
		RetrievedAt:  cursor.RetrievedAt,
		License:      license,
		CanonicalURL: providerURL,
		ResultIndex:  indices,
	}}
}

// rateLimitRemaining reads whichever remaining-quota header the provider
// publishes, verbatim. The header names differ per provider and none is
// standardized, so the first one present wins and the value is not
// reinterpreted.
func rateLimitRemaining(hdr http.Header) string {
	for _, name := range []string{
		"X-RateLimit-Remaining", "X-Rate-Limit-Remaining", "RateLimit-Remaining",
		"X-RateLimit-Remaining-Day", "X-Ratelimit-Remaining",
	} {
		if v := hdr.Get(name); v != "" {
			return v
		}
	}
	return ""
}

// upstreamRequestID reads the provider's own request identifier where it
// publishes one.
func upstreamRequestID(hdr http.Header) string {
	for _, name := range []string{"X-Request-Id", "X-Amzn-Requestid", "Request-Id", "X-Correlation-Id"} {
		if v := hdr.Get(name); v != "" {
			return v
		}
	}
	return ""
}

// sortedParamNames returns the parameter names in a stable order, so the
// passthrough list a page builds is deterministic.
func sortedParamNames(params map[string]string) []string {
	out := make([]string, 0, len(params))
	for k := range params {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
