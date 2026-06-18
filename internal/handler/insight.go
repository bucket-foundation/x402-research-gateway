// Package handler — feed402 insight-tier endpoint.
//
// The insight tier is the cheapest feed402 tier (SPEC §5) because it's
// where downstream agents are most willing to pay volume: no post-
// processing required, just "answer my question with citations." The
// merchant does the retrieval + summarization; the agent gets a short
// answer and a re-verifiable citation.
//
// Flow per request:
//
//  1. x402 payment verify + settle (same as handleProtectedRoute).
//  2. Fan out to a configured retrieval route (Insight.RetrievalRouteID)
//     to pull context — the gateway reuses the exact same in-process
//     proxy path, so the retrieval upstream is whatever pubmed-search /
//     semantic-scholar-search / etc. is configured to hit.
//  3. Run the summarizer (mock or OpenAI) over the top hits.
//  4. Emit a feed402 envelope with tier="insight", primary citation
//     pointing at the top hit, plus v0.2-additive `hits` array.
//
// Only the summarizer is pluggable; everything else is shared with the
// rest of the handler.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
)

// ---------- Summarizer interface ----------

type summarizer interface {
	id() string
	summarize(ctx context.Context, question string, contextSnippets []string) (string, error)
}

func newSummarizer(cfg config.InsightConfig) summarizer {
	switch cfg.Summarizer {
	case "openai":
		return &openAISummarizer{model: cfg.Model}
	default:
		return &mockSummarizer{}
	}
}

// mockSummarizer is deterministic and offline — used by default in tests
// and when OPENAI_API_KEY is not set.
type mockSummarizer struct{}

func (m *mockSummarizer) id() string { return "mock:template-v1" }
func (m *mockSummarizer) summarize(_ context.Context, q string, ctxs []string) (string, error) {
	if len(ctxs) == 0 {
		return fmt.Sprintf("No retrieval context was available for %q.", q), nil
	}
	joined := strings.Join(ctxs, " · ")
	if len(joined) > 400 {
		joined = joined[:400] + "..."
	}
	return fmt.Sprintf("Question: %s — Top context: %s", q, joined), nil
}

// openAISummarizer calls the OpenAI Chat Completions API. No SDK — plain
// HTTP so the gateway's dep list stays minimal.
type openAISummarizer struct {
	model string
}

func (o *openAISummarizer) id() string { return "openai:" + o.model }
func (o *openAISummarizer) summarize(ctx context.Context, q string, ctxs []string) (string, error) {
	payload := map[string]interface{}{
		"model": o.model,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "You are a research-summarization agent. Answer the user's question in 2-3 sentences using ONLY the provided context. If the context is insufficient say so. Never fabricate citations.",
			},
			{
				"role":    "user",
				"content": fmt.Sprintf("Question: %s\n\nContext:\n%s", q, strings.Join(ctxs, "\n---\n")),
			},
		},
		"temperature": 0.2,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai %d: %s", resp.StatusCode, string(rb))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rb, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// ---------- Handler ----------

// handleInsight serves the feed402 insight-tier endpoint. Payment-verify
// + settle mirror handleProtectedRoute; after that we fan out to the
// configured retrieval route, summarize, and wrap in an envelope.
func (h *Handler) handleInsight(w http.ResponseWriter, r *http.Request) {
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}
	if paymentHeader == "" {
		// Reuse the generic 402 challenge path; the x402 SDK already
		// knows about this route because we registered it into routeIndex.
		h.returnPaymentError(w, r, "payment required for insight tier")
		return
	}

	// Payment + settle — copy the relevant bits from handlePaymentAndProxy
	// with the insight route as the "route" config.
	routeKey := r.Method + " " + r.URL.Path
	insightRoute, ok := h.routeIndex[routeKey]
	if !ok {
		http.Error(w, "insight route not registered", http.StatusNotFound)
		return
	}
	payer, txHash, ok := h.verifyAndSettle(w, r, paymentHeader, insightRoute)
	if !ok {
		return
	}

	// Parse the question.
	var body struct {
		Question string `json:"question"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		buf, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(buf, &body)
	}
	if body.Question == "" {
		body.Question = r.URL.Query().Get("question")
	}
	if body.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "insight requires a non-empty `question` field",
		})
		return
	}

	// Fan-out to retrieval route.
	cfg := h.cfg.Feed402.Insight
	retrievalRoute := h.findRouteByID(cfg.RetrievalRouteID)
	if retrievalRoute == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("insight: retrieval route %q not configured", cfg.RetrievalRouteID),
		})
		return
	}
	retrievalReq := cloneForRetrieval(r, retrievalRoute, body.Question)
	upstream, err := proxyToUpstream(r.Context(), h.httpClient, retrievalRoute, retrievalReq)
	if err != nil {
		slog.Error("insight: retrieval failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "retrieval_failed"})
		return
	}

	// Build hits + context snippets.
	hits := []feed402Hit{}
	if parser, ok := h.hitParsers[retrievalRoute.ID]; ok {
		hits = parser(retrievalRoute, upstream.Body)
	}
	snippets := extractSnippets(upstream.Body, cfg.MaxContextChars)

	// Summarize.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	summary, err := h.summarizer.summarize(ctx, body.Question, snippets)
	if err != nil {
		slog.Warn("insight: summarizer failed, returning bare hits", "error", err)
		summary = "(summarization unavailable; consult the hits list)"
	}

	// Build envelope. Primary citation points at the top hit when we have
	// one; otherwise falls back to a provider-level synthetic source_id.
	citation := h.buildInsightCitation(insightRoute, retrievalRoute, hits, body.Question, r)
	citation.Retrieval = &feed402RetrievalProvenance{
		Model: h.summarizer.id(),
		Score: 1.0,
		Rank:  0,
	}

	if txHash == "" {
		txHash = "pending:" + shortHash(payer, insightRoute.Path, body.Question)
	}

	dataBytes, _ := json.Marshal(map[string]interface{}{
		"question":  body.Question,
		"summary":   summary,
		"retrieval": retrievalRoute.ID,
	})
	env := feed402Envelope{
		Data:     dataBytes,
		Citation: citation,
		Hits:     hits,
		Receipt: feed402Receipt{
			Tier:     "insight",
			PriceUSD: parsePriceUSD(insightRoute.Price),
			TX:       txHash,
			PaidAt:   time.Now().UTC().Format(time.RFC3339),
		},
	}
	out, _ := json.Marshal(env)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Feed402-Spec", h.cfg.Feed402.Spec)
	w.Header().Set("X-Research-Route", insightRoute.ID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func (h *Handler) findRouteByID(id string) *config.RouteConfig {
	for i := range h.cfg.Routes {
		if h.cfg.Routes[i].ID == id {
			return &h.cfg.Routes[i]
		}
	}
	return nil
}

// cloneForRetrieval builds a synthetic request that the existing upstream
// proxy can handle. The retrieval route's PassThrough params are filled
// from the question under a conventional query key ("term", "query", "q").
func cloneForRetrieval(orig *http.Request, route *config.RouteConfig, question string) *http.Request {
	q := url.Values{}
	// Set every declared PassThrough param to the question so whichever
	// query key the upstream expects ("term" for ESearch, "query" for S2,
	// "search" for OpenAlex, etc.) gets populated. Upstream ignores extras.
	for _, p := range route.Upstream.PassThrough {
		q.Set(p, question)
	}
	// Also fill common aliases.
	for _, alias := range []string{"term", "query", "q", "search"} {
		if q.Get(alias) == "" {
			q.Set(alias, question)
		}
	}
	u := &url.URL{Path: route.Path, RawQuery: q.Encode()}
	req, _ := http.NewRequestWithContext(orig.Context(), "GET", u.String(), nil)
	req.Host = orig.Host
	req.URL = u
	return req
}

// extractSnippets pulls short textual snippets from an arbitrary upstream
// JSON body. It walks the parsed tree looking for string fields with
// plausible length (>40 chars) — title, abstract, description, summary
// fields all match this heuristic across PubMed / S2 / OpenAlex /
// ClinicalTrials. Capped at maxChars total.
func extractSnippets(body []byte, maxChars int) []string {
	var root interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil
	}
	var out []string
	total := 0
	var walk func(v interface{})
	walk = func(v interface{}) {
		if total >= maxChars {
			return
		}
		switch t := v.(type) {
		case map[string]interface{}:
			for _, v2 := range t {
				walk(v2)
			}
		case []interface{}:
			for _, v2 := range t {
				walk(v2)
			}
		case string:
			if len(t) >= 40 {
				out = append(out, t)
				total += len(t)
			}
		}
	}
	walk(root)
	return out
}

// buildInsightCitation emits the primary §3 citation for the insight
// envelope. If the top hit has a canonical URL we use it verbatim;
// otherwise we fall back to a provider-level synthetic source_id.
func (h *Handler) buildInsightCitation(
	insightRoute, retrievalRoute *config.RouteConfig,
	hits []feed402Hit,
	question string,
	req *http.Request,
) feed402CitationSource {
	providerName := h.cfg.Feed402.Name
	if providerName == "" {
		providerName = "x402-research-gateway"
	}
	if len(hits) > 0 {
		return feed402CitationSource{
			Type:         "source",
			SourceID:     hits[0].SourceID,
			Provider:     providerName,
			RetrievedAt:  time.Now().UTC().Format(time.RFC3339),
			License:      licenseFor(retrievalRoute, &h.cfg.Feed402),
			CanonicalURL: hits[0].CanonicalURL,
		}
	}
	prefix := retrievalRoute.Citation.SourcePrefix
	if prefix == "" {
		prefix = retrievalRoute.ID
	}
	return feed402CitationSource{
		Type:         "source",
		SourceID:     prefix + ":insight:" + shortHash(question),
		Provider:     providerName,
		RetrievedAt:  time.Now().UTC().Format(time.RFC3339),
		License:      licenseFor(retrievalRoute, &h.cfg.Feed402),
		CanonicalURL: retrievalRoute.Citation.ProviderURL,
	}
}

// ---------- Shared settle path ----------

// verifyAndSettle wraps decodeAndVerifyPayment (handler.go) plus
// settleWithTimeout into one call returning payer + tx.
func (h *Handler) verifyAndSettle(
	w http.ResponseWriter, r *http.Request, paymentHeader string, route *config.RouteConfig,
) (payer, txHash string, ok bool) {
	pay, reqs, p, okDecode := h.decodeAndVerifyPayment(w, r, paymentHeader, route)
	if !okDecode {
		return "", "", false
	}
	tx := h.settleWithTimeout(r.Context(), pay, reqs, 3*time.Second)
	return p, tx, true
}
