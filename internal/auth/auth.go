// Package auth mints and caches upstream bearer tokens for the credentialed
// providers behind config.UpstreamConfig.AuthTokenSource
// (x402-research-gateway#29).
//
// Every provider adapter registered so far is unauthenticated public REST.
// ORCID is the first that needs a token, via the OAuth2 client-credentials
// grant, and the pattern here is deliberately generic so the next
// credentialed provider (patents, x402-research-gateway#18; licensed
// vocabularies, x402-research-gateway#14) reuses it rather than growing its
// own token plumbing.
//
// Nothing in this package ever logs a client secret or a minted token. A
// TokenSource returns the token string to its one caller (proxyToUpstream)
// and nowhere else; errors are built without embedding request or response
// bodies, since a token endpoint's error body can itself echo the secret
// back.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// registry maps a config.UpstreamConfig.AuthTokenSource name (e.g. "orcid")
// to the TokenSource that mints its bearer token. proxyToUpstream
// (internal/handler/proxy.go) looks a route's AuthTokenSource up here; a
// route naming a source nobody registered gets no Authorization header,
// which the upstream itself then reports as unauthorized, rather than the
// gateway silently guessing at a credential.
var (
	registryMu sync.RWMutex
	registry   = map[string]TokenSource{}
)

// Register adds a TokenSource under a name a route's AuthTokenSource can
// reference. Call once at startup per credentialed provider; a second
// Register under the same name replaces the first; the last call wins.
func Register(name string, source TokenSource) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = source
}

// Get returns the TokenSource registered under name, if any.
func Get(name string) (TokenSource, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ts, ok := registry[name]
	return ts, ok
}

// orcidTokenURL and orcidScope are ORCID's own published values for the
// public-API client-credentials grant, verified 2026-08-18 against
// info.orcid.org: POST https://orcid.org/oauth/token with
// grant_type=client_credentials and scope=/read-public mints a token
// ORCID reports as valid for 631138518 seconds (20 years).
const (
	orcidTokenURL = "https://orcid.org/oauth/token"
	orcidScope    = "/read-public"
)

// RegisterFromEnv wires every credentialed provider this package knows
// about into the registry, reading each provider's client credentials from
// environment variables at call time and nowhere else. A provider whose
// env vars are unset is skipped with a warning rather than registered with
// an empty secret, so a route naming it fails loud (no token source
// registered) instead of minting a token ORCID will reject.
//
// Call once at startup, before the handler serves any request.
func RegisterFromEnv(httpClient *http.Client) {
	clientID := os.Getenv("ORCID_CLIENT_ID")
	clientSecret := os.Getenv("ORCID_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		slog.Warn("auth: ORCID_CLIENT_ID/ORCID_CLIENT_SECRET not set, orcid routes requiring auth will fail")
		return
	}
	Register("orcid", NewClientCredentialsSource(orcidTokenURL, clientID, clientSecret, orcidScope, httpClient))
	slog.Info("auth: registered orcid token source")
}

// TokenSource mints or returns a cached bearer token for one upstream.
// Implementations must be safe for concurrent use: proxyToUpstream calls
// Token from whatever goroutine is serving that request.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// expiryMargin is how long before a token's reported expiry it is treated as
// stale, so a request in flight never races a token that expires mid-call.
const expiryMargin = 5 * time.Minute

// fallbackTTL is the cache lifetime used when a token endpoint omits
// expires_in. Re-minting on this cadence is wasteful for a token as
// long-lived as ORCID's, but conservative: this package will never hold a
// token past a lifetime it was never told the token has.
const fallbackTTL = 1 * time.Hour

// ClientCredentialsSource mints a bearer token via RFC 6749 §4.4 (the
// OAuth2 client-credentials grant) and caches it until shortly before its
// reported expiry.
type ClientCredentialsSource struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scope        string
	httpClient   *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewClientCredentialsSource builds a token source. clientID and
// clientSecret are read by the caller from environment variables — this
// constructor never reads the environment itself, so a caller controls
// exactly where the secret comes from and this package never has to be
// trusted with that choice.
func NewClientCredentialsSource(tokenURL, clientID, clientSecret, scope string, httpClient *http.Client) *ClientCredentialsSource {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &ClientCredentialsSource{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scope:        scope,
		httpClient:   httpClient,
	}
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

// Token returns a cached token when one is still valid, minting a new one
// otherwise. The client secret is sent once, in the token-endpoint POST
// body, and never appears in the returned error on failure.
func (s *ClientCredentialsSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Now().Before(s.expiresAt) {
		return s.token, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
	}
	if s.scope != "" {
		form.Set("scope", s.scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("auth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		// A network error can carry the request in its string form on some
		// transports; wrap with a fixed message rather than %w'ing it
		// directly next to anything request-shaped.
		return "", fmt.Errorf("auth: token request failed: %s", classifyNetErr(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("auth: read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Never include the body: an OAuth error response has been known to
		// echo client_id and, on some providers, request parameters.
		return "", fmt.Errorf("auth: token endpoint returned status %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("auth: parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("auth: token endpoint returned no access_token")
	}

	ttl := fallbackTTL
	if tr.ExpiresIn > 0 {
		ttl = time.Duration(tr.ExpiresIn) * time.Second
		if ttl > expiryMargin {
			ttl -= expiryMargin
		}
	}

	s.token = tr.AccessToken
	s.expiresAt = time.Now().Add(ttl)
	return s.token, nil
}

// classifyNetErr reports a network failure without echoing the error's own
// message, which for some http.Client transports embeds the full request
// URL (harmless here, since the token URL has no secret in it) but, more
// cautiously, is kept generic so this call site never becomes the one place
// that regresses if a future transport adds request-body detail to its
// error text.
func classifyNetErr(err error) string {
	if err == nil {
		return "unknown error"
	}
	return "network error"
}
