package handler

// Identity-resolution endpoint (x402-research-gateway#5).
//
// One identifier goes in. The gateway asks every provider whose adapter
// implements provider.IdentityProvider what it knows about that identifier,
// hands the answers to internal/identity's resolver, and returns the
// relation graph in a feed402 §3 envelope carrying one citation per
// contributing provider.
//
// Three properties this handler is built around:
//
//   - A provider that fails is reported as a failure. A timeout, a 500, or a
//     rate-limit never renders as "that provider found nothing." Silence and
//     absence stay distinguishable, which is the same rule the adapter layer
//     applies to capabilities.
//   - No merged canonical record is emitted. Every provider record is its
//     own node with its raw bytes attached, and the answer is the relations
//     between them.
//   - Fan-out is bounded. Concurrency and per-provider timeout are config,
//     defaulting to 4 and 10s, so a resolve call cannot fan into an
//     unbounded burst against upstreams that rate-limit.

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
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// resolveProviderFailure is the explicit record of a provider that did not
// answer. It carries no upstream URL, no headers, and no error text from a
// transport layer that could echo a credential: only the route id, a
// coarse reason, and the upstream status when there was one.
type resolveProviderFailure struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
	Status   int    `json:"upstream_status,omitempty"`
}

// Failure reasons. Coarse by design: a caller needs to know a provider was
// asked and did not answer, and the detail belongs in gateway logs.
const (
	resolveFailUpstream = "upstream_error"
	resolveFailStatus   = "upstream_status"
	resolveFailTimeout  = "timeout"
	resolveFailNoRoute  = "route_not_configured"
)

type resolveResult struct {
	records []identity.SourceRecord
	failure *resolveProviderFailure
}

// identityRouteIDs lists the route ids that participate in resolution:
// every route with a registered adapter implementing IdentityProvider,
// intersected with the configured allowlist when one is set. Sorted, so
// fan-out order and the resulting graph are stable across runs.
func (h *Handler) identityRouteIDs() []string {
	allow := map[string]bool{}
	for _, id := range h.cfg.Feed402.Resolve.ProviderRouteIDs {
		allow[id] = true
	}
	var out []string
	for id, adapter := range h.providers {
		if adapter == nil || adapter.IdentityProvider == nil {
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

// handleResolve serves the identity-resolution endpoint. Payment verify and
// settle mirror handleInsight exactly, so pricing and receipt plumbing stay
// in one place.
func (h *Handler) handleResolve(w http.ResponseWriter, r *http.Request) {
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}
	if paymentHeader == "" {
		h.returnPaymentError(w, r, "payment required for resolve")
		return
	}
	resolveRoute, ok := h.routeIndex[r.Method+" "+r.URL.Path]
	if !ok {
		http.Error(w, "resolve route not registered", http.StatusNotFound)
		return
	}
	payer, txHash, ok := h.verifyAndSettle(w, r, paymentHeader, resolveRoute)
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
			"error": "resolve requires a non-empty `identifier` field",
		})
		return
	}

	// Normalize the query identifier. An identifier no scheme claims is
	// still resolvable: it goes upstream as a plain query string, so parse
	// failure is reported rather than rejected.
	queryID, parsed := identity.Parse(body.Identifier)

	records, failures := h.fanOutIdentity(r.Context(), body.Identifier)
	g := (&identity.Resolver{
		SimilarityThreshold: h.cfg.Feed402.Resolve.SimilarityThreshold,
	}).Resolve(records)

	citations := h.resolveCitations(resolveRoute, g)
	if txHash == "" {
		txHash = "pending:" + shortHash(payer, resolveRoute.Path, body.Identifier)
	}

	dataMap := map[string]any{
		"identifier": map[string]any{
			"raw":        body.Identifier,
			"scheme":     string(queryID.Scheme),
			"normalized": queryID.Value,
			"recognized": parsed,
		},
		"graph": g,
		// providers_failed is always present, empty array included, so a
		// consumer never has to distinguish "no failures" from "this
		// gateway does not report failures."
		"providers_failed":    failures,
		"providers_attempted": h.identityRouteIDs(),
	}
	dataBytes, _ := json.Marshal(dataMap)

	env := feed402Envelope{
		Data:     dataBytes,
		Citation: citations,
		Receipt: feed402Receipt{
			Tier:     resolveRoute.Feed402Tier,
			PriceUSD: parsePriceUSD(resolveRoute.Price),
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
	w.Header().Set("X-Research-Route", resolveRoute.ID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// fanOutIdentity queries every identity-capable provider for the
// identifier, bounded by the configured concurrency. Results come back in
// route-id order regardless of which provider answered first, so the graph
// is deterministic.
func (h *Handler) fanOutIdentity(ctx context.Context, identifier string) ([]identity.SourceRecord, []resolveProviderFailure) {
	routeIDs := h.identityRouteIDs()
	results := make([]resolveResult, len(routeIDs))

	max := h.cfg.Feed402.Resolve.MaxConcurrency
	if max < 1 {
		max = 4
	}
	sem := make(chan struct{}, max)
	var wg sync.WaitGroup
	for i, id := range routeIDs {
		wg.Add(1)
		go func(i int, routeID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = h.identityFromProvider(ctx, routeID, identifier)
		}(i, id)
	}
	wg.Wait()

	var records []identity.SourceRecord
	failures := []resolveProviderFailure{}
	for _, res := range results {
		records = append(records, res.records...)
		if res.failure != nil {
			failures = append(failures, *res.failure)
		}
	}
	return records, failures
}

// identityFromProvider calls one provider and turns its response into
// SourceRecords. Every exit path returns either records or an explicit
// failure, never an empty-and-silent result for a call that went wrong.
func (h *Handler) identityFromProvider(ctx context.Context, routeID, identifier string) resolveResult {
	adapter := h.providers[routeID]
	route := h.findRouteByID(routeID)
	if route == nil || adapter == nil || adapter.Normalizer == nil {
		return resolveResult{failure: &resolveProviderFailure{Provider: routeID, Reason: resolveFailNoRoute}}
	}

	timeout := time.Duration(h.cfg.Feed402.Resolve.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req := resolveUpstreamRequest(callCtx, route, identifier)
	upstream, err := proxyToUpstream(callCtx, h.httpClient, route, req)
	if err != nil {
		reason := resolveFailUpstream
		if callCtx.Err() != nil {
			reason = resolveFailTimeout
		}
		// The error text can carry the upstream URL, which for a
		// key-bearing provider carries the key. It goes to the log at debug
		// level and never into the response body.
		slog.Warn("resolve: provider call failed", "provider", routeID, "reason", reason)
		return resolveResult{failure: &resolveProviderFailure{Provider: routeID, Reason: reason}}
	}
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		slog.Warn("resolve: provider returned non-2xx",
			"provider", routeID, "status", upstream.StatusCode)
		return resolveResult{failure: &resolveProviderFailure{
			Provider: routeID, Reason: resolveFailStatus, Status: upstream.StatusCode,
		}}
	}

	normalized := adapter.Normalizer.Normalize(upstream.Body)
	at := time.Now()
	var out []identity.SourceRecord
	for _, rec := range normalized {
		if rec.ID == "" {
			continue
		}
		src := identity.SourceRecord{
			Provider:         routeID,
			ProviderRecordID: rec.ID,
			CanonicalURL:     rec.CanonicalURL,
			Raw:              rec.Raw,
			Identifiers:      adapter.IdentityProvider.Identifiers(rec),
		}
		nodeID := identity.NodeID(routeID, rec.ID)
		src.AssertedRelations = adapter.IdentityProvider.AssertedRelations(nodeID, rec, at)
		if adapter.DescriptorProvider != nil {
			desc := adapter.DescriptorProvider.Descriptor(rec)
			src.Title, src.Authors, src.Year = desc.Title, desc.Authors, desc.Year
		}
		out = append(out, src)
	}
	return resolveResult{records: out}
}

// resolveUpstreamRequest builds the synthetic request the declarative proxy
// consumes, filling the route's declared passthrough params with the
// identifier. Same construction as cloneForRetrieval, with the identifier
// as the query term.
func resolveUpstreamRequest(ctx context.Context, route *config.RouteConfig, identifier string) *http.Request {
	q := url.Values{}
	for _, p := range route.Upstream.PassThrough {
		q.Set(p, identifier)
	}
	for _, alias := range []string{"term", "query", "q", "search", "id"} {
		if q.Get(alias) == "" {
			q.Set(alias, identifier)
		}
	}
	u := &url.URL{Path: route.Path, RawQuery: q.Encode()}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	req.URL = u
	return req
}

// resolveCitations emits one feed402 §3 citation per contributing provider,
// each bound by result_index to the graph nodes that provider supplied.
// A resolution nobody answered still carries one citation for the gateway
// itself, because §3 requires a non-empty citation array.
func (h *Handler) resolveCitations(resolveRoute *config.RouteConfig, g identity.Graph) []feed402CitationSource {
	providerName := h.cfg.Feed402.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	indices := map[string][]int{}
	for i, n := range g.Nodes {
		indices[n.Provider] = append(indices[n.Provider], i)
	}

	var out []feed402CitationSource
	for _, p := range g.Providers {
		route := h.findRouteByID(p)
		prefix, providerURL, license := p, "", h.cfg.Feed402.CitationPolicy
		if route != nil {
			if route.Citation.SourcePrefix != "" {
				prefix = route.Citation.SourcePrefix
			}
			providerURL = route.Citation.ProviderURL
			license = licenseFor(route, &h.cfg.Feed402)
		}
		out = append(out, feed402CitationSource{
			Type:         "source",
			SourceID:     prefix + ":resolve:" + shortHash(p, resolveRoute.Path),
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
			SourceID:    "resolve:" + shortHash(resolveRoute.Path),
			Provider:    providerName,
			RetrievedAt: now,
			License:     h.cfg.Feed402.CitationPolicy,
		})
	}
	return out
}
