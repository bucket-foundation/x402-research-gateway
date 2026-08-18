package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tokenServer starts an httptest server standing in for
// https://orcid.org/oauth/token. It never talks to live ORCID; every test
// in this file uses it instead. It records how many times it was hit so
// the caching test can assert Token() mints once and reuses thereafter.
func tokenServer(t *testing.T, expiresIn int64, accessToken string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type = %q, want client_credentials", r.PostForm.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": accessToken,
			"token_type":   "bearer",
			"expires_in":   expiresIn,
			"scope":        "/read-public",
		})
	}))
	return srv, &hits
}

func TestClientCredentialsSource_MintsAndCaches(t *testing.T) {
	srv, hits := tokenServer(t, 631138518, "cached-token-value")
	defer srv.Close()

	src := NewClientCredentialsSource(srv.URL, "client-id", "client-secret", "/read-public", nil)

	for i := 0; i < 5; i++ {
		tok, err := src.Token(context.Background())
		if err != nil {
			t.Fatalf("Token() call %d: %v", i, err)
		}
		if tok != "cached-token-value" {
			t.Errorf("Token() = %q", tok)
		}
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("token endpoint hit %d times, want 1 (a ~20-year token should mint once)", got)
	}
}

func TestClientCredentialsSource_RemintsAfterExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping a >1s expiry-wait test in -short mode")
	}
	// expires_in below expiryMargin forces the token's ttl to stay at its
	// literal 1-second value (the margin subtraction only applies once
	// ttl exceeds the margin), so waiting past that second forces the
	// next call to re-mint rather than reuse the cached token.
	srv, hits := tokenServer(t, 1, "short-lived-token")
	defer srv.Close()

	src := NewClientCredentialsSource(srv.URL, "id", "secret", "", nil)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("first Token(): %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("second Token(): %v", err)
	}
	if got := atomic.LoadInt32(hits); got < 2 {
		t.Errorf("token endpoint hit %d times, want >=2 for a token this short-lived", got)
	}
}

func TestClientCredentialsSource_NoAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"token_type": "bearer"})
	}))
	defer srv.Close()

	src := NewClientCredentialsSource(srv.URL, "id", "secret", "", nil)
	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("expected an error for a response with no access_token")
	}
}

func TestClientCredentialsSource_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"client-secret-should-not-leak"}`))
	}))
	defer srv.Close()

	src := NewClientCredentialsSource(srv.URL, "id", "secret", "", nil)
	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("expected an error for a non-200 token response")
	}
	if strings.Contains(err.Error(), "client-secret-should-not-leak") {
		t.Fatalf("error echoes the token endpoint's response body: %v", err)
	}
	if strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("error echoes the token endpoint's response body: %v", err)
	}
}

// TestClientCredentialsSource_NoSecretLeakage is the pattern this package
// mirrors from provider.TestUnpaywallIdentity_NoEmailLeakage: the client
// secret must never surface anywhere this package hands back to a
// caller — not the minted token's own value (an equality check would be
// wrong; the token is legitimately returned), but every error path, and
// the fact that the constructor never echoes it back either.
func TestClientCredentialsSource_NoSecretLeakage(t *testing.T) {
	const secret = "s3cr3t-orcid-client-credential-do-not-leak"

	// A network-unreachable token URL: the transport error must not embed
	// the secret (it was sent in the POST body, never the URL, but this
	// guards the invariant explicitly).
	src := NewClientCredentialsSource("http://127.0.0.1:1/dead", "client-id", secret, "/read-public", nil)
	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("expected a network error dialing a closed port")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("network-failure error leaks the secret: %v", err)
	}

	// A malformed-JSON response: the parse error must not embed the
	// request body (which contains the secret) or the response body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()
	src2 := NewClientCredentialsSource(srv.URL, "client-id", secret, "/read-public", nil)
	_, err = src2.Token(context.Background())
	if err == nil {
		t.Fatal("expected a parse error for malformed JSON")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("parse error leaks the secret: %v", err)
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	name := fmt.Sprintf("test-source-%p", t)
	src := NewClientCredentialsSource("http://example.invalid", "id", "secret", "", nil)
	Register(name, src)

	got, ok := Get(name)
	if !ok {
		t.Fatal("Get() after Register() should find the source")
	}
	if got != TokenSource(src) {
		t.Error("Get() returned a different TokenSource than was registered")
	}

	if _, ok := Get("no-such-source-was-ever-registered"); ok {
		t.Error("Get() on an unregistered name should report not-found")
	}
}
