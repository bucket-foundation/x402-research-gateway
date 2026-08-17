// Package handler — feed402 protocol compliance layer.
//
// When GatewayConfig.Feed402.Enabled is true, the gateway:
//
//  1. Serves /.well-known/feed402.json with the discovery manifest
//     (SPEC §1 / §1.2 / §4).
//  2. Wraps every successful paid response in the feed402 envelope
//     (SPEC §3): {data, citation, receipt}, citation always an array.
//
// The gateway does NOT own a retrieval index; upstreams do their own
// retrieval. We therefore omit the optional §4 `index` block and the
// §3.2 retrieval-provenance fields on citations.
//
// # feed402/0.3 migration (x402-research-gateway#1)
//
// The gateway now speaks canonical feed402/0.3. `citation` is an array on
// every route, including insight. `citation_legacy` (a copy of
// `citation[0]`) and the pre-0.3 `data.hits`/`routes`/`tier_routes` fields
// are retained for the deprecation window per feed402 SPEC §7.2 — sunset at
// feed402/0.5. See DEPRECATIONS.md for the full field-by-field mapping and
// the conformance rationale (why the array has one entry for single-record
// and insight responses, and one-plus-N entries for search-tier responses
// with adapter-derived hits).
package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

// ---------- Manifest types (mirror feed402 SPEC §1) ----------

type feed402TierSpec struct {
	Path     string  `json:"path"`
	PriceUSD float64 `json:"price_usd"`
	Unit     string  `json:"unit"`
}

type feed402RouteEntry struct {
	ID          string          `json:"id"`
	Path        string          `json:"path"`
	Method      string          `json:"method"`
	Tier        string          `json:"tier"`
	Description string          `json:"description,omitempty"`
	Price       feed402TierSpec `json:"price"`
	Citation    feed402RouteCit `json:"citation,omitempty"`
	// Capabilities is additive, v0.3-shaped groundwork (x402-research-
	// gateway#2 / #1): the capability vocabulary this route's adapter
	// implements, computed from internal/provider.Adapter.Capabilities().
	// A route with no registered adapter — the declarative-only proxy path
	// — omits this field entirely rather than reporting an empty array, so
	// "no adapter" and "adapter implements nothing" stay distinguishable.
	Capabilities []string `json:"capabilities,omitempty"`
}

type feed402RouteCit struct {
	SourcePrefix string `json:"source_prefix,omitempty"`
	ProviderURL  string `json:"provider_url,omitempty"`
	License      string `json:"license,omitempty"`
}

// feed402Operation mirrors feed402 SPEC §1.2's OperationSpec — the standard
// replacement for the gateway's private `routes`/`tier_routes` enumeration
// (SPEC §1.3, §7.2). Fields the gateway cannot populate honestly (schemas,
// content_types, canonical_identifier) are left empty rather than guessed.
type feed402Operation struct {
	OperationID       string   `json:"operation_id"`
	Capability        string   `json:"capability"`
	Path              string   `json:"path"`
	Method            string   `json:"method,omitempty"`
	Tier              string   `json:"tier,omitempty"`
	Description       string   `json:"description,omitempty"`
	PaginationModel   string   `json:"pagination_model,omitempty"`
	IdentifierSchemes []string `json:"identifier_schemes,omitempty"`
}

type feed402Manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Spec    string `json:"spec"`
	Chain   string `json:"chain"`
	Wallet  string `json:"wallet"`
	// Capabilities (SPEC §1.1, v0.3, optional) — the union of capability
	// values declared across Operations, so an agent can skip this merchant
	// without reading every operation. SHOULD equal the distinct
	// `capability` values in Operations; where they disagree Operations is
	// authoritative, per spec.
	Capabilities   []string                   `json:"capabilities,omitempty"`
	Tiers          map[string]feed402TierSpec `json:"tiers"`
	CitationPolicy string                     `json:"citation_policy,omitempty"`
	CitationTypes  []string                   `json:"citation_types"`
	Contact        string                     `json:"contact,omitempty"`
	// Operations (SPEC §1.2, v0.3, optional) — the canonical replacement for
	// Routes/TierRoutes below.
	Operations []feed402Operation `json:"operations,omitempty"`
	// Routes / TierRoutes: pre-0.3 private enumeration fields. Never a spec
	// field (SPEC §7.2 lists them as such), retained during the deprecation
	// window (sunset feed402/0.5) because the reference gateway published
	// them and agents may be reading them. New consumers should read
	// Operations, or manifestOperations() equivalent, instead.
	//
	// Deprecated: use Operations.
	Routes []feed402RouteEntry `json:"routes"`
	// Deprecated: use Operations, grouped by Tier.
	TierRoutes map[string][]feed402RouteEntry `json:"tier_routes"`
}

// ---------- Envelope types (mirror feed402 SPEC §3) ----------

type feed402CitationSource struct {
	Type         string                      `json:"type"` // "source"
	SourceID     string                      `json:"source_id"`
	Provider     string                      `json:"provider"`
	RetrievedAt  string                      `json:"retrieved_at"`
	License      string                      `json:"license,omitempty"`
	CanonicalURL string                      `json:"canonical_url,omitempty"`
	ChunkID      string                      `json:"chunk_id,omitempty"`
	Retrieval    *feed402RetrievalProvenance `json:"retrieval,omitempty"`
	// ResultIndex (SPEC §3.3, v0.3, optional) — zero-based indices into the
	// envelope's result list (`data.rows`) that this citation grounds.
	// Explicit binding: when a search-tier response carries per-hit
	// citations alongside the provider-level query citation, every citation
	// in the array carries this field per SPEC §3.3 rule 3.
	ResultIndex []int `json:"result_index,omitempty"`
}

// feed402RetrievalProvenance mirrors SPEC §3.2 (v0.2 optional).
type feed402RetrievalProvenance struct {
	Model string  `json:"model"`
	Score float64 `json:"score"`
	Rank  int     `json:"rank"`
}

// feed402Hit is the per-hit re-verification handle emitted on search-tier
// envelopes. It is intentionally minimal — agents re-fetch the full record
// via the canonical URL or a sibling raw-tier route. Populated from
// internal/provider.Hit, computed by a route's registered adapter
// (Normalizer + CitationProvider); a route with no adapter, or whose
// adapter implements neither, emits no `hits` array at all, which is
// spec-valid.
type feed402Hit struct {
	SourceID     string `json:"source_id"`
	CanonicalURL string `json:"canonical_url,omitempty"`
	Rank         int    `json:"rank"`
}

// hitsForRoute extracts per-hit citation handles for a search-tier response
// via the route's registered adapter, when one implements both Normalizer
// and CitationProvider. Returns nil for a route with no adapter, or whose
// adapter implements neither capability — the same "no hits array" outcome
// as before x402-research-gateway#2, now routed through the adapter
// registry instead of a hardcoded per-route parser map.
func (h *Handler) hitsForRoute(routeID string, body []byte) []feed402Hit {
	if h.providers == nil {
		return nil
	}
	adapter, ok := h.providers[routeID]
	if !ok || adapter.Normalizer == nil || adapter.CitationProvider == nil {
		return nil
	}
	route := h.findRouteByID(routeID)
	if route == nil {
		return nil
	}
	records := adapter.Normalizer.Normalize(body)
	providerHits := adapter.CitationProvider.Citations(route, records)
	if len(providerHits) == 0 {
		return nil
	}
	hits := make([]feed402Hit, len(providerHits))
	for i, ph := range providerHits {
		hits[i] = feed402Hit{SourceID: ph.SourceID, CanonicalURL: ph.CanonicalURL, Rank: ph.Rank}
	}
	return hits
}

// capabilitiesForRoute reports the capability vocabulary a route's adapter
// implements, or nil when the route has no adapter (pure declarative proxy).
func (h *Handler) capabilitiesForRoute(routeID string) []string {
	adapter, ok := h.providers[routeID]
	if !ok {
		return nil
	}
	caps := adapter.Capabilities()
	if len(caps) == 0 {
		return nil
	}
	out := make([]string, len(caps))
	for i, c := range caps {
		out[i] = string(c)
	}
	return out
}

// searchPaginationFallback mirrors feed402 types.ts's
// inferCapabilityFromRoute(): a lossy heuristic for declarative-only routes
// with no adapter, distinguishing only search from fetch by name.
func searchPaginationFallback(id, path string) string {
	haystack := strings.ToLower(id + " " + path)
	if strings.Contains(haystack, "search") || strings.Contains(haystack, "query") {
		return "search"
	}
	return "fetch"
}

// buildOperationFor emits the SPEC §1.2 operation entry for one route,
// reading pagination model / identifier schemes from its adapter when one
// is registered.
func (h *Handler) buildOperationFor(r *config.RouteConfig) feed402Operation {
	op := feed402Operation{
		OperationID: r.ID,
		Path:        r.Path,
		Method:      r.Method,
		Tier:        r.Feed402Tier,
		Description: r.Description,
	}
	// The identity-resolution route is gateway-native rather than a proxy
	// to one upstream, so it has no adapter to read a capability from.
	// It declares the extension capability directly and lists every
	// identifier scheme the resolver can normalize, so an agent can tell
	// before paying whether its identifier is one this gateway handles.
	if r.ID == "feed402-resolve" {
		op.Capability = string(provider.CapIdentityResolution)
		for _, s := range identity.Schemes() {
			op.IdentifierSchemes = append(op.IdentifierSchemes, string(s))
		}
		return op
	}
	adapter, ok := h.providers[r.ID]
	switch {
	case ok && adapter.Searcher != nil:
		op.Capability = "search"
		op.PaginationModel = adapter.Searcher.PaginationModel()
	case ok && adapter.Fetcher != nil:
		op.Capability = "fetch"
		op.IdentifierSchemes = adapter.Fetcher.IdentifierSchemes()
	default:
		op.Capability = searchPaginationFallback(r.ID, r.Path)
	}
	return op
}

// buildManifestCapabilities returns the union of every route's adapter
// capabilities, for the manifest-level §1.1 summary field. Deterministic
// order: iterates h.cfg.Routes, not the registry map.
func (h *Handler) buildManifestCapabilities() []string {
	seen := map[string]bool{}
	var out []string
	for i := range h.cfg.Routes {
		for _, c := range h.capabilitiesForRoute(h.cfg.Routes[i].ID) {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// searchCitationsAndData builds the v0.3 citation array and the augmented
// `data` payload for a search-tier response whose route has adapter-derived
// hits. Returns (nil, nil) when there are no hits, signaling the caller to
// fall back to the single-citation, unaugmented-data path.
//
// Design (DEPRECATIONS.md has the full rationale): the provider-level query
// citation stays first, carrying an explicit ResultIndex spanning every
// result so it keeps grounding the response as a whole; one citation per
// hit follows, each grounding only its own result. `data` gains a `rows`
// array (so SPEC §3.3's resultList() recognizes a multi-record response and
// the array is genuinely conformant, not just array-shaped) and a
// deprecated `hits` alias carrying the identical content under its pre-0.3
// name and field spelling, per SPEC §7.2's field mapping table.
func (h *Handler) searchCitationsAndData(
	primary feed402CitationSource,
	hits []feed402Hit,
	rawData json.RawMessage,
) ([]feed402CitationSource, json.RawMessage) {
	if len(hits) == 0 {
		return nil, nil
	}

	allIdx := make([]int, len(hits))
	for i := range allIdx {
		allIdx[i] = i
	}
	primary.ResultIndex = allIdx
	citations := make([]feed402CitationSource, 0, len(hits)+1)
	citations = append(citations, primary)
	for i, hit := range hits {
		citations = append(citations, feed402CitationSource{
			Type:         "source",
			SourceID:     hit.SourceID,
			Provider:     primary.Provider,
			RetrievedAt:  primary.RetrievedAt,
			License:      primary.License,
			CanonicalURL: hit.CanonicalURL,
			ResultIndex:  []int{i},
		})
	}

	augmented, err := augmentDataWithRowsAndHits(rawData, hits)
	if err != nil {
		// Upstream body wasn't a JSON object we can safely augment (rare —
		// none of the adapter-backed upstreams emit a bare array or scalar
		// today). Fall back to the pre-array single-citation shape rather
		// than publish a citation array SPEC §3.3 can't validate against an
		// un-augmented `data`.
		slog.Warn("feed402: could not augment data with rows/hits; falling back to single citation", "error", err)
		return nil, nil
	}
	return citations, augmented
}

// augmentDataWithRowsAndHits adds `rows` (the SPEC §3.3 resultList marker)
// and `hits` (the deprecated pre-0.3 alias, SPEC §7.2) to a JSON object
// body, preserving every existing key. Errors when rawData isn't a JSON
// object — the two new keys would have nowhere sensible to attach.
func augmentDataWithRowsAndHits(rawData json.RawMessage, hits []feed402Hit) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(rawData, &obj); err != nil {
		return nil, fmt.Errorf("upstream body is not a JSON object: %w", err)
	}
	hitsJSON, err := json.Marshal(hits)
	if err != nil {
		return nil, fmt.Errorf("marshal hits: %w", err)
	}
	obj["rows"] = hitsJSON
	obj["hits"] = hitsJSON
	return json.Marshal(obj)
}

type feed402Receipt struct {
	Tier     string  `json:"tier"`
	PriceUSD float64 `json:"price_usd"`
	TX       string  `json:"tx"`
	PaidAt   string  `json:"paid_at"`
}

type feed402Envelope struct {
	Data json.RawMessage `json:"data"`
	// Citation (SPEC §3, v0.3 BREAKING) — always an array, length >= 1 on
	// success. A single-record or insight response carries exactly one
	// entry: the provider-level citation from buildCitationFor(). A
	// search-tier response whose route has an adapter-derived hit list
	// carries the provider-level query citation FIRST (grounding every
	// result via ResultIndex), followed by one citation per hit (grounding
	// its own result). See DEPRECATIONS.md.
	Citation []feed402CitationSource `json:"citation"`
	// CitationLegacy (SPEC §7.1, deprecated, sunset feed402/0.5) — a copy of
	// Citation[0] for consumers still reading the pre-0.3 singular shape.
	// Advisory only: a 0.3 consumer MUST NOT require it.
	CitationLegacy *feed402CitationSource `json:"citation_legacy,omitempty"`
	Receipt        feed402Receipt         `json:"receipt"`
}

// ---------- Manifest builder ----------

// buildFeed402Manifest generates the discovery manifest from the gateway
// configuration. Each configured route becomes a feed402 route entry;
// per-tier aggregates are computed for convenience.
//
// Because the gateway has heterogeneous routes (not a single /raw, /query,
// /insight triplet), the manifest emits both:
//   - `tiers`: the canonical feed402 tier map, keyed to the LOWEST-priced
//     route of each tier (so an agent can pick the cheapest tier-conformant
//     path) — this matches the SPEC §1 shape.
//   - `routes` + `tier_routes`: the full enumeration of concrete paths at
//     each tier, so agents can pick a specific dataset.
func (h *Handler) buildFeed402Manifest() feed402Manifest {
	f := h.cfg.Feed402
	tiers := map[string]feed402TierSpec{}
	tierRoutes := map[string][]feed402RouteEntry{}
	routes := make([]feed402RouteEntry, 0, len(h.cfg.Routes))

	for i := range h.cfg.Routes {
		r := &h.cfg.Routes[i]
		price := parsePriceUSD(r.Price)
		entry := feed402RouteEntry{
			ID:          r.ID,
			Path:        r.Path,
			Method:      r.Method,
			Tier:        r.Feed402Tier,
			Description: r.Description,
			Price: feed402TierSpec{
				Path:     r.Path,
				PriceUSD: price,
				Unit:     "call",
			},
			Citation: feed402RouteCit{
				SourcePrefix: r.Citation.SourcePrefix,
				ProviderURL:  r.Citation.ProviderURL,
				License:      licenseFor(r, &f),
			},
			Capabilities: h.capabilitiesForRoute(r.ID),
		}
		routes = append(routes, entry)
		tierRoutes[r.Feed402Tier] = append(tierRoutes[r.Feed402Tier], entry)

		// Keep the cheapest route of each tier as the canonical tier entry.
		existing, ok := tiers[r.Feed402Tier]
		if !ok || price < existing.PriceUSD {
			tiers[r.Feed402Tier] = feed402TierSpec{
				Path:     r.Path,
				PriceUSD: price,
				Unit:     "call",
			}
		}
	}

	operations := make([]feed402Operation, 0, len(h.cfg.Routes))
	for i := range h.cfg.Routes {
		operations = append(operations, h.buildOperationFor(&h.cfg.Routes[i]))
	}

	return feed402Manifest{
		Name:           f.Name,
		Version:        f.Version,
		Spec:           f.Spec,
		Chain:          string(h.cfg.Network),
		Wallet:         h.cfg.RecipientAddress,
		Capabilities:   h.buildManifestCapabilities(),
		Tiers:          tiers,
		CitationPolicy: f.CitationPolicy,
		CitationTypes:  []string{"source"},
		Contact:        f.Contact,
		Operations:     operations,
		Routes:         routes,
		TierRoutes:     tierRoutes,
	}
}

// handleFeed402Manifest serves the /.well-known/feed402.json discovery
// manifest. Free endpoint — no payment required per SPEC §1.
func (h *Handler) handleFeed402Manifest(w http.ResponseWriter, _ *http.Request) {
	m := h.buildFeed402Manifest()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		slog.Warn("failed to encode feed402 manifest", "error", err)
	}
}

// ---------- Envelope wrapping ----------

// wrapFeed402Envelope wraps an upstream response body in the feed402 §3
// envelope. Returns the marshaled envelope bytes, or (nil, err) on failure
// (caller should fall back to returning the raw body on error — spec
// compliance is a nice-to-have, a broken response is worse).
//
// `upstreamBody` is the raw upstream payload (may be JSON or any bytes).
// For non-JSON payloads we still place them under `data` as a raw string —
// agents can inspect `mimeType` in the manifest to know the shape.
func (h *Handler) wrapFeed402Envelope(
	route *config.RouteConfig,
	upstreamBody []byte,
	payer string,
	txHash string,
	req *http.Request,
) ([]byte, error) {
	// Marshal the upstream body into `data`. If it's valid JSON, embed as
	// JSON; otherwise, stringify. Keeps the envelope shape stable.
	var dataField json.RawMessage
	if json.Valid(upstreamBody) {
		dataField = upstreamBody
	} else {
		s, err := json.Marshal(string(upstreamBody))
		if err != nil {
			return nil, fmt.Errorf("marshal non-json upstream body: %w", err)
		}
		dataField = s
	}

	primary := h.buildCitationFor(route, req)
	price := parsePriceUSD(route.Price)

	tx := txHash
	if tx == "" {
		// Payment was verified but settlement is async; we emit a placeholder
		// receipt. A future revision can block on settle or include the
		// verify-step "payer" address as a cryptographic anchor.
		tx = "pending:" + shortHash(payer, req.URL.Path, req.URL.RawQuery)
	}

	// Extract per-hit provenance if the route has a registered adapter
	// implementing both Normalizer and CitationProvider (search-tier routes
	// on recognized upstreams), then fold it into a SPEC §3.3-conformant
	// citation array plus an augmented `data` carrying the resultList marker
	// (`rows`) and the deprecated `hits` alias, side by side.
	hits := h.hitsForRoute(route.ID, upstreamBody)
	citations, augmentedData := h.searchCitationsAndData(primary, hits, dataField)
	if citations == nil {
		// No hits, or the upstream body couldn't be safely augmented: the
		// single-record shape from before x402-research-gateway#1, now as a
		// one-element array.
		citations = []feed402CitationSource{primary}
	} else {
		dataField = augmentedData
	}

	env := feed402Envelope{
		Data:           dataField,
		Citation:       citations,
		CitationLegacy: &citations[0],
		Receipt: feed402Receipt{
			Tier:     route.Feed402Tier,
			PriceUSD: price,
			TX:       tx,
			PaidAt:   time.Now().UTC().Format(time.RFC3339),
		},
	}
	return json.Marshal(env)
}

// buildCitationFor constructs the §3 `source` citation for a route response.
// For search-tier responses we synthesize `source_prefix:query:<hash>` so
// the citation is stable per query; for id-bearing routes we use the
// canonical url template if populated with a known passthrough param.
func (h *Handler) buildCitationFor(route *config.RouteConfig, req *http.Request) feed402CitationSource {
	cit := route.Citation
	f := h.cfg.Feed402

	providerName := f.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}

	// Try to extract an id from a single-record-looking route.
	var canonicalURL string
	var sourceID string
	prefix := cit.SourcePrefix
	if prefix == "" {
		prefix = route.ID
	}

	// Look for a passthrough param that feeds a {id}-style template.
	if cit.CanonicalURLTemplate != "" {
		canonicalURL = cit.CanonicalURLTemplate
		for _, p := range route.Upstream.PassThrough {
			v := req.URL.Query().Get(p)
			if v == "" {
				continue
			}
			canonicalURL = strings.ReplaceAll(canonicalURL, "{"+p+"}", v)
			if strings.Contains(cit.CanonicalURLTemplate, "{"+p+"}") && sourceID == "" {
				sourceID = prefix + ":" + v
			}
		}
		// If the template still has unresolved placeholders, treat this as
		// a search call rather than a single-record fetch.
		if strings.Contains(canonicalURL, "{") {
			canonicalURL = cit.ProviderURL
			sourceID = ""
		}
	}

	if sourceID == "" {
		// Search / query case: hash the querystring so agents re-calling
		// with the same params get the same source_id.
		q := req.URL.RawQuery
		sourceID = prefix + ":query:" + shortHash(q)
		if canonicalURL == "" {
			canonicalURL = cit.ProviderURL
		}
	}

	return feed402CitationSource{
		Type:         "source",
		SourceID:     sourceID,
		Provider:     providerName,
		RetrievedAt:  time.Now().UTC().Format(time.RFC3339),
		License:      licenseFor(route, &f),
		CanonicalURL: canonicalURL,
	}
}

// ---------- Helpers ----------

// parsePriceUSD converts the config's string price ("0.001") to a float.
// Falls back to 0 on parse error; the manifest will still render.
func parsePriceUSD(s string) float64 {
	var v float64
	_, err := fmt.Sscanf(s, "%f", &v)
	if err != nil {
		return 0
	}
	return v
}

// licenseFor returns the per-route license if set, otherwise the
// provider-level citation policy.
func licenseFor(r *config.RouteConfig, f *config.Feed402Config) string {
	if r.Citation.License != "" {
		return r.Citation.License
	}
	return f.CitationPolicy
}

// shortHash returns a short, stable hex digest of its inputs for use in
// synthetic source_ids and placeholder tx hashes.
func shortHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
