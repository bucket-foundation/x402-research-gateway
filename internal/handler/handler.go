package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	x402 "github.com/coinbase/x402/go"
	x402http "github.com/coinbase/x402/go/http"
	evmserver "github.com/coinbase/x402/go/mechanisms/evm/exact/server"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/gianyrox/x402-research-gateway/internal/config"
)

// Handler handles x402-protected research API requests.
type Handler struct {
	router     *chi.Mux
	cfg        *config.GatewayConfig
	x402srv    *x402http.HTTPServer
	routeIndex map[string]*config.RouteConfig // "GET /path" -> config
	httpClient *http.Client
	hitParsers map[string]hitParser // route.ID -> per-upstream hit extractor
	summarizer summarizer           // feed402 insight-tier LLM (mock or openai)
}

// chiHTTPAdapter implements x402http.HTTPAdapter for net/http requests.
type chiHTTPAdapter struct {
	r *http.Request
}

func (a *chiHTTPAdapter) GetHeader(name string) string { return a.r.Header.Get(name) }
func (a *chiHTTPAdapter) GetMethod() string            { return a.r.Method }
func (a *chiHTTPAdapter) GetPath() string              { return a.r.URL.Path }
func (a *chiHTTPAdapter) GetURL() string               { return a.r.URL.String() }
func (a *chiHTTPAdapter) GetAcceptHeader() string      { return a.r.Header.Get("Accept") }
func (a *chiHTTPAdapter) GetUserAgent() string         { return a.r.UserAgent() }

// NewHandler creates a new research gateway handler with x402 SDK integration.
func NewHandler(cfg *config.GatewayConfig) *Handler {
	network := x402.Network(cfg.Network)

	facilitatorClient := x402http.NewFacilitatorClient(&x402http.FacilitatorConfig{
		URL: cfg.FacilitatorURL,
	})

	// Build x402 route config from YAML routes
	x402Routes := make(x402http.RoutesConfig)
	routeIndex := make(map[string]*config.RouteConfig)

	for i := range cfg.Routes {
		r := &cfg.Routes[i]
		key := r.Method + " " + r.Path
		x402Routes[key] = x402http.RouteConfig{
			Description: r.Description,
			MimeType:    r.MimeType,
			Accepts: x402http.PaymentOptions{
				{
					Scheme:            "exact",
					Network:           network,
					PayTo:             cfg.RecipientAddress,
					Price:             r.Price,
					MaxTimeoutSeconds: 60,
				},
			},
		}
		routeIndex[key] = r
	}

	// feed402 insight-tier synthetic route — registered in routeIndex +
	// x402Routes so the same payment-verify plumbing applies uniformly.
	if cfg.Feed402.Enabled && cfg.Feed402.Insight.Enabled {
		ins := cfg.Feed402.Insight
		insightRoute := &config.RouteConfig{
			ID:          "feed402-insight",
			Path:        ins.Path,
			Method:      "POST",
			Description: ins.Description,
			MimeType:    "application/json",
			Price:       ins.Price,
			Feed402Tier: "insight",
			Citation: config.RouteCitation{
				SourcePrefix: "insight",
				ProviderURL:  "",
				License:      cfg.Feed402.CitationPolicy,
			},
		}
		cfg.Routes = append(cfg.Routes, *insightRoute)
		insKey := "POST " + ins.Path
		x402Routes[insKey] = x402http.RouteConfig{
			Description: ins.Description,
			MimeType:    "application/json",
			Accepts: x402http.PaymentOptions{
				{
					Scheme:            "exact",
					Network:           network,
					PayTo:             cfg.RecipientAddress,
					Price:             ins.Price,
					MaxTimeoutSeconds: 60,
				},
			},
		}
		// Pointer into the slot we just appended.
		routeIndex[insKey] = &cfg.Routes[len(cfg.Routes)-1]
	}

	x402srv := x402http.NewServer(
		x402Routes,
		x402.WithFacilitatorClient(facilitatorClient),
		x402.WithSchemeServer(network, evmserver.NewExactEvmScheme()),
	)

	h := &Handler{
		router:     chi.NewRouter(),
		cfg:        cfg,
		x402srv:    x402srv,
		routeIndex: routeIndex,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		hitParsers: defaultHitParsers(),
	}
	if cfg.Feed402.Enabled && cfg.Feed402.Insight.Enabled {
		h.summarizer = newSummarizer(cfg.Feed402.Insight)
	}

	h.router.Use(chimw.RequestID)
	h.router.Use(chimw.RealIP)
	h.router.Use(chimw.Logger)
	h.router.Use(chimw.Recoverer)
	h.router.Use(chimw.Timeout(30 * time.Second))

	// CORS
	h.router.Use(chimw.SetHeader("Access-Control-Allow-Origin", "*"))
	h.router.Use(chimw.SetHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS"))
	h.router.Use(chimw.SetHeader("Access-Control-Allow-Headers", "Content-Type, PAYMENT-SIGNATURE, X-PAYMENT"))

	// Health endpoint (free)
	h.router.Get("/health", h.handleHealth)

	// feed402 discovery manifest (free) — only mounted when enabled.
	if cfg.Feed402.Enabled {
		h.router.Get("/.well-known/feed402.json", h.handleFeed402Manifest)
		slog.Info("feed402 compliance layer active",
			"spec", cfg.Feed402.Spec,
			"manifest", "/.well-known/feed402.json",
		)
	}

	// Register all configured research routes. The feed402 insight-tier
	// synthetic route (if enabled) gets its own handler that fans out to
	// a retrieval route + calls the configured summarizer.
	for i := range cfg.Routes {
		r := &cfg.Routes[i]
		slog.Info("Registering route", "method", r.Method, "path", r.Path, "price", r.Price)
		if r.ID == "feed402-insight" {
			h.router.Post(r.Path, h.handleInsight)
			continue
		}
		switch r.Method {
		case "GET":
			h.router.Get(r.Path, h.handleProtectedRoute)
		case "POST":
			h.router.Post(r.Path, h.handleProtectedRoute)
		}
	}

	return h
}

// Initialize calls the x402 SDK to discover facilitator capabilities.
func (h *Handler) Initialize(ctx context.Context) error {
	if err := h.x402srv.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize x402 resource server: %w", err)
	}
	return nil
}

// Router returns the chi router.
func (h *Handler) Router() http.Handler {
	return h.router
}

// handleProtectedRoute serves any configured research route via x402 payment flow.
func (h *Handler) handleProtectedRoute(w http.ResponseWriter, r *http.Request) {
	// Accept both header names for compatibility
	paymentHeader := r.Header.Get("PAYMENT-SIGNATURE")
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-PAYMENT")
	}

	if paymentHeader == "" {
		// No payment → generate 402 response via SDK
		adapter := &chiHTTPAdapter{r: r}
		reqCtx := x402http.HTTPRequestContext{
			Adapter:       adapter,
			Path:          r.URL.Path,
			Method:        r.Method,
			PaymentHeader: "",
		}
		result := h.x402srv.ProcessHTTPRequest(r.Context(), reqCtx, nil)
		resp := result.Response
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		if resp.Body != nil {
			writeJSON(w, resp.Status, resp.Body)
		} else {
			w.WriteHeader(resp.Status)
		}
		return
	}

	// Payment header present → verify and proxy
	h.handlePaymentAndProxy(w, r, paymentHeader)
}

// handlePaymentAndProxy verifies the x402 payment via facilitator, then proxies to upstream.
func (h *Handler) handlePaymentAndProxy(w http.ResponseWriter, r *http.Request, paymentHeader string) {
	routeKey := r.Method + " " + r.URL.Path
	route, ok := h.routeIndex[routeKey]
	if !ok {
		http.Error(w, "route not found", http.StatusNotFound)
		return
	}

	v1PaymentPayload, v1Requirements, payer, ok := h.decodeAndVerifyPayment(w, r, paymentHeader, route)
	if !ok {
		return
	}
	txHash := h.settleWithTimeout(r.Context(), v1PaymentPayload, v1Requirements, 3*time.Second)

	slog.Info("Payment verified, proxying to upstream",
		"route", route.ID,
		"payer", payer,
		"upstream", route.Upstream.BaseURL+route.Upstream.Path,
	)

	// Proxy to upstream research API
	result, err := proxyToUpstream(r.Context(), h.httpClient, route, r)
	if err != nil {
		slog.Error("Upstream proxy failed", "route", route.ID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":   "upstream_error",
			"message": fmt.Sprintf("upstream API request failed: %v", err),
		})
		return
	}

	// Return upstream response with payment metadata headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Research-Route", route.ID)
	w.Header().Set("X-Research-Payer", payer)
	if txHash != "" {
		w.Header().Set("X-Research-Transaction", txHash)
	}

	// feed402 compliance: wrap the upstream body in the §3 envelope
	// (data + citation + receipt). Only on 2xx; pass through error bodies
	// unchanged so agents still see useful upstream error messages.
	respBody := result.Body
	if h.cfg.Feed402.Enabled && result.StatusCode >= 200 && result.StatusCode < 300 {
		wrapped, werr := h.wrapFeed402Envelope(route, result.Body, payer, txHash, r)
		if werr != nil {
			slog.Warn("feed402 envelope wrap failed, returning raw upstream body",
				"route", route.ID, "error", werr)
		} else {
			respBody = wrapped
			w.Header().Set("X-Feed402-Spec", h.cfg.Feed402.Spec)
		}
	}

	w.WriteHeader(result.StatusCode)
	_, _ = w.Write(respBody)
}

// returnPaymentError sends a 402 with fresh payment requirements.
func (h *Handler) returnPaymentError(w http.ResponseWriter, r *http.Request, _ string) {
	reqCtx := x402http.HTTPRequestContext{
		Adapter:       &chiHTTPAdapter{r: r},
		Path:          r.URL.Path,
		Method:        r.Method,
		PaymentHeader: "",
	}
	result := h.x402srv.ProcessHTTPRequest(context.Background(), reqCtx, nil)
	if result.Response != nil {
		for k, v := range result.Response.Headers {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(http.StatusPaymentRequired)
}

// decodeAndVerifyPayment parses the x402 v2 payment envelope, calls the
// facilitator /verify endpoint, and returns the inner v1 payment payload +
// requirements + payer on success. On any failure it writes an error
// response to w and returns ok=false. Shared by handlePaymentAndProxy
// (for regular proxied routes) and handleInsight (for the synthetic
// feed402 insight-tier route).
func (h *Handler) decodeAndVerifyPayment(
	w http.ResponseWriter, r *http.Request, paymentHeader string, route *config.RouteConfig,
) (v1Payload map[string]interface{}, v1Requirements map[string]interface{}, payer string, ok bool) {
	paymentBytes, err := base64.StdEncoding.DecodeString(paymentHeader)
	if err != nil {
		paymentBytes, err = base64.URLEncoding.DecodeString(paymentHeader)
		if err != nil {
			h.returnPaymentError(w, r, "invalid payment header encoding")
			return nil, nil, "", false
		}
	}
	var v2Payload struct {
		X402Version int                    `json:"x402Version"`
		Payload     map[string]interface{} `json:"payload"`
		Accepted    struct {
			Scheme            string                 `json:"scheme"`
			Network           string                 `json:"network"`
			Asset             string                 `json:"asset"`
			Amount            string                 `json:"amount"`
			PayTo             string                 `json:"payTo"`
			MaxTimeoutSeconds int                    `json:"maxTimeoutSeconds"`
			Extra             map[string]interface{} `json:"extra,omitempty"`
		} `json:"accepted"`
	}
	if err := json.Unmarshal(paymentBytes, &v2Payload); err != nil {
		h.returnPaymentError(w, r, fmt.Sprintf("invalid payment payload: %v", err))
		return nil, nil, "", false
	}

	v1Payload = map[string]interface{}{
		"x402Version": 1,
		"scheme":      v2Payload.Accepted.Scheme,
		"network":     v2Payload.Accepted.Network,
		"payload":     v2Payload.Payload,
	}

	resourceURL := fmt.Sprintf("https://%s%s", r.Host, r.URL.Path)
	maxAmount := v2Payload.Accepted.Amount
	if authMap, ok2 := v2Payload.Payload["authorization"].(map[string]interface{}); ok2 {
		if val, ok3 := authMap["value"].(string); ok3 {
			maxAmount = val
		}
	}
	extraJSON, err := json.Marshal(v2Payload.Accepted.Extra)
	if err != nil {
		h.returnPaymentError(w, r, "failed to process payment extra data")
		return nil, nil, "", false
	}
	v1Requirements = map[string]interface{}{
		"scheme":            v2Payload.Accepted.Scheme,
		"network":           v2Payload.Accepted.Network,
		"maxAmountRequired": maxAmount,
		"resource":          resourceURL,
		"description":       route.Description,
		"mimeType":          route.MimeType,
		"payTo":             v2Payload.Accepted.PayTo,
		"maxTimeoutSeconds": v2Payload.Accepted.MaxTimeoutSeconds,
		"asset":             v2Payload.Accepted.Asset,
		"extra":             json.RawMessage(extraJSON),
	}

	verifyReq := map[string]interface{}{
		"x402Version":         1,
		"paymentPayload":      v1Payload,
		"paymentRequirements": v1Requirements,
	}
	verifyBody, err := json.Marshal(verifyReq)
	if err != nil {
		h.returnPaymentError(w, r, "internal error")
		return nil, nil, "", false
	}
	verifyResp, err := http.Post(h.cfg.FacilitatorURL+"/verify", "application/json", bytes.NewReader(verifyBody))
	if err != nil {
		h.returnPaymentError(w, r, "facilitator unavailable")
		return nil, nil, "", false
	}
	defer verifyResp.Body.Close()
	verifyRespBody, err := io.ReadAll(verifyResp.Body)
	if err != nil {
		h.returnPaymentError(w, r, "facilitator response unreadable")
		return nil, nil, "", false
	}
	var verifyResult struct {
		IsValid        bool   `json:"isValid"`
		InvalidReason  string `json:"invalidReason,omitempty"`
		InvalidMessage string `json:"invalidMessage,omitempty"`
		Payer          string `json:"payer,omitempty"`
	}
	if err := json.Unmarshal(verifyRespBody, &verifyResult); err != nil {
		h.returnPaymentError(w, r, "invalid facilitator response")
		return nil, nil, "", false
	}
	if verifyResp.StatusCode != http.StatusOK || !verifyResult.IsValid {
		reason := verifyResult.InvalidReason
		if reason == "" {
			reason = "payment_invalid"
		}
		h.returnPaymentError(w, r, fmt.Sprintf("verification failed: %s - %s", reason, verifyResult.InvalidMessage))
		return nil, nil, "", false
	}
	return v1Payload, v1Requirements, payer, true
}

// settleWithTimeout calls the facilitator /settle endpoint with a bounded
// deadline and returns the on-chain tx hash on success. On timeout or any
// failure we kick the settle call into the background (best-effort) and
// return "" — the caller will then emit a placeholder receipt.tx.
func (h *Handler) settleWithTimeout(
	parent context.Context,
	paymentPayload map[string]interface{},
	requirements map[string]interface{},
	budget time.Duration,
) string {
	settleReq := map[string]interface{}{
		"x402Version":         1,
		"paymentPayload":      paymentPayload,
		"paymentRequirements": requirements,
	}
	settleBody, err := json.Marshal(settleReq)
	if err != nil {
		slog.Error("Failed to marshal settle request", "error", err)
		return ""
	}

	type settleOutcome struct {
		status int
		body   []byte
		err    error
	}
	resultCh := make(chan settleOutcome, 1)

	// Detach from the request context so the background fallback survives
	// past the response being written.
	bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Second)

	go func() {
		defer bgCancel()
		req, rerr := http.NewRequestWithContext(bgCtx, "POST",
			h.cfg.FacilitatorURL+"/settle", bytes.NewReader(settleBody))
		if rerr != nil {
			resultCh <- settleOutcome{err: rerr}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, rerr := http.DefaultClient.Do(req)
		if rerr != nil {
			resultCh <- settleOutcome{err: rerr}
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		resultCh <- settleOutcome{status: resp.StatusCode, body: body}
	}()

	waitCtx, waitCancel := context.WithTimeout(parent, budget)
	defer waitCancel()

	select {
	case out := <-resultCh:
		if out.err != nil {
			slog.Warn("Facilitator settle failed", "error", out.err)
			return ""
		}
		slog.Info("Facilitator settle response", "status", out.status, "body", string(out.body))
		return extractSettleTxHash(out.body)
	case <-waitCtx.Done():
		// Settle still in flight — let it complete in the background and
		// log the outcome for operator observability.
		go func() {
			out := <-resultCh
			if out.err != nil {
				slog.Warn("Facilitator settle failed (background)", "error", out.err)
				return
			}
			slog.Info("Facilitator settle response (background)",
				"status", out.status, "body", string(out.body))
		}()
		slog.Warn("Facilitator settle exceeded budget; response will carry placeholder receipt.tx",
			"budget", budget.String())
		return ""
	}
}

// extractSettleTxHash pulls the on-chain tx hash out of the facilitator
// /settle response. The x402 facilitator spec isn't strict about the field
// name in practice, so we accept the common variants ("transaction",
// "txHash", "tx_hash") and ignore anything else.
func extractSettleTxHash(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	// Only treat as success if the response self-identifies as successful;
	// otherwise callers get a misleading "tx" that never landed.
	if s, ok := m["success"].(bool); ok && !s {
		return ""
	}
	for _, k := range []string{"transaction", "txHash", "tx_hash", "tx"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("Failed to encode JSON response", "error", err)
	}
}
