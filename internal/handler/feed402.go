// Package handler — feed402 protocol compliance layer.
//
// When GatewayConfig.Feed402.Enabled is true, the gateway:
//
//  1. Serves /.well-known/feed402.json with the discovery manifest
//     (SPEC §1 / §4).
//  2. Wraps every successful paid response in the feed402 envelope
//     (SPEC §3): {data, citation, receipt}.
//
// The gateway does NOT own a retrieval index; upstreams do their own
// retrieval. We therefore omit the optional §4 `index` block and the
// §3.2 retrieval-provenance fields on citations. A future revision may
// parse per-upstream response shapes (PubMed ESearch, OpenAlex, etc.) to
// extract top-hit ids and emit `chunk_id` + retrieval provenance per hit.
// For v1 compliance we emit a provider-level `source` citation per call.
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
}

type feed402RouteCit struct {
	SourcePrefix string `json:"source_prefix,omitempty"`
	ProviderURL  string `json:"provider_url,omitempty"`
	License      string `json:"license,omitempty"`
}

type feed402Manifest struct {
	Name           string                         `json:"name"`
	Version        string                         `json:"version"`
	Spec           string                         `json:"spec"`
	Chain          string                         `json:"chain"`
	Wallet         string                         `json:"wallet"`
	Tiers          map[string]feed402TierSpec     `json:"tiers"`
	CitationPolicy string                         `json:"citation_policy,omitempty"`
	CitationTypes  []string                       `json:"citation_types"`
	Contact        string                         `json:"contact,omitempty"`
	Routes         []feed402RouteEntry            `json:"routes"`
	TierRoutes     map[string][]feed402RouteEntry `json:"tier_routes"`
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
}

// feed402RetrievalProvenance mirrors SPEC §3.2 (v0.2 optional).
type feed402RetrievalProvenance struct {
	Model string  `json:"model"`
	Score float64 `json:"score"`
	Rank  int     `json:"rank"`
}

type feed402Receipt struct {
	Tier     string  `json:"tier"`
	PriceUSD float64 `json:"price_usd"`
	TX       string  `json:"tx"`
	PaidAt   string  `json:"paid_at"`
}

type feed402Envelope struct {
	Data     json.RawMessage       `json:"data"`
	Citation feed402CitationSource `json:"citation"`
	// Hits is an optional v0.2-additive field: on search-tier responses
	// the gateway extracts per-record re-verification handles
	// (source_id + canonical_url + rank) so agents can re-fetch or
	// re-cite individual results. Absent when the route isn't a search
	// or the upstream body shape isn't recognized. Spec §2.3 unknown-
	// field rule guarantees v0.1 agents ignore it safely.
	Hits    []feed402Hit   `json:"hits,omitempty"`
	Receipt feed402Receipt `json:"receipt"`
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

	return feed402Manifest{
		Name:           f.Name,
		Version:        f.Version,
		Spec:           f.Spec,
		Chain:          string(h.cfg.Network),
		Wallet:         h.cfg.RecipientAddress,
		Tiers:          tiers,
		CitationPolicy: f.CitationPolicy,
		CitationTypes:  []string{"source"},
		Contact:        f.Contact,
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

	citation := h.buildCitationFor(route, req)
	price := parsePriceUSD(route.Price)

	tx := txHash
	if tx == "" {
		// Payment was verified but settlement is async; we emit a placeholder
		// receipt. A future revision can block on settle or include the
		// verify-step "payer" address as a cryptographic anchor.
		tx = "pending:" + shortHash(payer, req.URL.Path, req.URL.RawQuery)
	}

	// Extract per-hit provenance if the route has a registered parser
	// (search-tier routes on recognized upstreams).
	var hits []feed402Hit
	if h.hitParsers != nil {
		if parser, ok := h.hitParsers[route.ID]; ok {
			hits = parser(route, upstreamBody)
		}
	}

	env := feed402Envelope{
		Data:     dataField,
		Citation: citation,
		Hits:     hits,
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
