package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestVerifier builds a verifier with no inter-provider delay, so tests do
// not sleep. Unit tests never touch a live provider API.
func newTestVerifier() *Verifier {
	v := NewVerifier()
	v.Delay = 0
	v.Now = func() time.Time { return time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC) }
	return v
}

func TestVerifyMarksLiveProviderVerified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &Registry{
		File: File{Providers: []Provider{{
			ProviderID:       "live",
			Name:             "Live",
			Type:             TypeScholarlyMetadata,
			Status:           StatusResearched,
			BaseURL:          srv.URL,
			DocumentationURL: srv.URL + "/docs",
		}}},
		byID: map[string]*Provider{},
	}
	r.byID["live"] = &r.Providers[0]

	results, err := newTestVerifier().Verify(context.Background(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected the live provider to verify, got %+v", results)
	}
	p, _ := r.Get("live")
	if p.LastVerified != "2026-08-17" {
		t.Errorf("last_verified = %q, want 2026-08-17", p.LastVerified)
	}
	if p.Stale {
		t.Error("a live provider must not be flagged stale")
	}
}

// A failure flags the entry stale. It never deletes it: an upstream being
// down is not evidence that a source should be forgotten.
func TestVerifyFlagsStaleWithoutDeleting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := &Registry{
		File: File{Providers: []Provider{{
			ProviderID: "broken",
			Name:       "Broken",
			Type:       TypeScholarlyMetadata,
			Status:     StatusResearched,
			BaseURL:    srv.URL,
		}}},
		byID: map[string]*Provider{},
	}
	r.byID["broken"] = &r.Providers[0]

	before := r.Len()
	results, err := newTestVerifier().Verify(context.Background(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Fatal("a 500 should not verify")
	}
	if r.Len() != before {
		t.Fatalf("verification deleted a provider: %d -> %d", before, r.Len())
	}
	p, _ := r.Get("broken")
	if !p.Stale {
		t.Error("expected the entry to be flagged stale")
	}
	if p.StaleReason == "" {
		t.Error("expected a stale reason")
	}
	if p.LastVerified == "" {
		t.Error("last_verified should be recorded even on failure")
	}
}

// An endpoint that demands credentials still exists. 401/403 is not drift.
func TestVerifyTreatsAuthRequiredAsAlive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	v := newTestVerifier()
	res := v.check(context.Background(), "base_url", srv.URL)
	if !res.OK() {
		t.Errorf("401 should count as alive, got %+v", res)
	}
}

// Excluded and sunset sources are described but never contacted.
func TestVerifyNeverContactsExcludedSources(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	for _, status := range []Status{StatusExcluded, StatusSunset} {
		p := &Provider{
			ProviderID: "x", Name: "X", Type: TypeFullTextRepository,
			Status: status, BaseURL: srv.URL, DocumentationURL: srv.URL,
		}
		res := newTestVerifier().VerifyProvider(context.Background(), p)
		if !res.OK {
			t.Errorf("%s: skipping should not be a failure", status)
		}
	}
	if hits != 0 {
		t.Errorf("excluded/sunset sources were contacted %d time(s); they must not be", hits)
	}
}

func TestVerifySkipsUnfetchableURLs(t *testing.T) {
	v := newTestVerifier()
	for _, tc := range []struct{ url, why string }{
		{"", "not recorded"},
		{"https://example.org/{id}", "URL template"},
		{"not-a-url", "not an absolute URL"},
	} {
		res := v.check(context.Background(), "base_url", tc.url)
		if res.Skipped == "" {
			t.Errorf("%q should be skipped", tc.url)
		}
		if !res.OK() {
			t.Errorf("%q: a skipped check must not fail the provider", tc.url)
		}
	}
}

// The verifier identifies itself, so upstreams can contact us rather than
// silently block the gateway.
func TestVerifierIdentifiesItselfPolitely(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	newTestVerifier().check(context.Background(), "base_url", srv.URL)
	if !strings.Contains(ua, "x402-research-gateway") {
		t.Errorf("User-Agent %q should identify the gateway", ua)
	}
	if !strings.Contains(ua, "http") {
		t.Errorf("User-Agent %q should carry a contact URL", ua)
	}
}

// Verification is a liveness probe, not a scrape: it must not pull bodies.
func TestVerifyDoesNotDownloadBodies(t *testing.T) {
	var method, rangeHdr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		rangeHdr = r.Header.Get("Range")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	newTestVerifier().check(context.Background(), "base_url", srv.URL)
	if method != http.MethodGet {
		t.Fatalf("expected a GET fallback after HEAD was refused, got %s", method)
	}
	if rangeHdr != "bytes=0-0" {
		t.Errorf("GET fallback should request one byte, got Range=%q", rangeHdr)
	}
}

func TestVerifyOnlyFiltersProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &Registry{
		File: File{Providers: []Provider{
			{ProviderID: "a", Name: "A", Type: TypeScholarlyMetadata, Status: StatusResearched, BaseURL: srv.URL},
			{ProviderID: "b", Name: "B", Type: TypeScholarlyMetadata, Status: StatusResearched, BaseURL: srv.URL},
		}},
		byID: map[string]*Provider{},
	}
	r.byID["a"] = &r.Providers[0]
	r.byID["b"] = &r.Providers[1]

	results, err := newTestVerifier().Verify(context.Background(), r, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ProviderID != "b" {
		t.Fatalf("expected only provider b, got %+v", results)
	}
}
