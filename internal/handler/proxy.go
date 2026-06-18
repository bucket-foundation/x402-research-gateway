package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
)

// ProxyResult holds the upstream API response.
type ProxyResult struct {
	StatusCode int
	Body       []byte
	LatencyMs  int64
}

// proxyToUpstream calls the upstream research API and returns the response.
func proxyToUpstream(ctx context.Context, client *http.Client, route *config.RouteConfig, originalReq *http.Request) (*ProxyResult, error) {
	upstream := &route.Upstream
	start := time.Now()

	// Build upstream URL
	upstreamURL, err := buildUpstreamURL(upstream, originalReq)
	if err != nil {
		return nil, fmt.Errorf("build upstream URL: %w", err)
	}

	slog.Info("Proxying to upstream",
		"route", route.ID,
		"url", upstreamURL,
	)

	// Create upstream request
	timeout := time.Duration(upstream.Timeout) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, upstream.Method, upstreamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	// Inject configured headers
	for k, v := range upstream.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}

	latency := time.Since(start).Milliseconds()
	slog.Info("Upstream response",
		"route", route.ID,
		"status", resp.StatusCode,
		"bytes", len(body),
		"latency_ms", latency,
	)

	return &ProxyResult{
		StatusCode: resp.StatusCode,
		Body:       body,
		LatencyMs:  latency,
	}, nil
}

// buildUpstreamURL constructs the full upstream URL with query parameters.
func buildUpstreamURL(upstream *config.UpstreamConfig, originalReq *http.Request) (string, error) {
	clientQuery := originalReq.URL.Query()

	// If pathTemplate is set, substitute {param} placeholders from query params
	path := upstream.Path
	if upstream.PathTemplate != "" {
		path = upstream.PathTemplate
		for _, param := range upstream.PassThrough {
			placeholder := "{" + param + "}"
			if val := clientQuery.Get(param); val != "" && strings.Contains(path, placeholder) {
				path = strings.ReplaceAll(path, placeholder, url.PathEscape(val))
			}
		}
	}

	u, err := url.Parse(upstream.BaseURL + path)
	if err != nil {
		return "", fmt.Errorf("parse upstream base URL: %w", err)
	}

	q := u.Query()

	// Add default query params from config
	for k, v := range upstream.QueryParams {
		q.Set(k, v)
	}

	// Forward allowed query params from client request (skip those already used in path template)
	for _, param := range upstream.PassThrough {
		if upstream.PathTemplate != "" && strings.Contains(upstream.PathTemplate, "{"+param+"}") {
			continue // Already substituted into the path
		}
		if val := clientQuery.Get(param); val != "" {
			q.Set(param, val)
		}
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}
