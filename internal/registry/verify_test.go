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
	// #22: last_verified records the last SUCCESSFUL check, so an outage
	// cannot masquerade as a fresh verification. LastChecked records the
	// attempt regardless of outcome.
	if p.LastVerified != "" {
		t.Error("last_verified should stay empty: this provider has never passed a check")
	}
	if p.LastChecked == "" {
		t.Error("last_checked should be recorded even on failure")
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

// TestVerifyPreservesLastKnownGoodOnOutage is the behavior this issue exists
// for: a provider that was working, then goes down, must not lose the record
// of when it last worked. Flipping last_verified to "today" on a failed
// check would make a five-year-reliable source look identical to one that
// has never once succeeded, which destroys exactly the information a
// campaign relying on reproducibility needs.
func TestVerifyPreservesLastKnownGoodOnOutage(t *testing.T) {
	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if up {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := &Registry{
		File: File{Providers: []Provider{{
			ProviderID: "flaky", Name: "Flaky", Type: TypeScholarlyMetadata,
			Status: StatusResearched, BaseURL: srv.URL,
		}}},
		byID: map[string]*Provider{},
	}
	r.byID["flaky"] = &r.Providers[0]

	v := newTestVerifier()
	if _, err := v.Verify(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := r.Get("flaky")
	goodDate := p.LastVerified
	if goodDate == "" {
		t.Fatal("expected the healthy pass to record last_verified")
	}

	// Move the clock and take the provider down.
	v.Now = func() time.Time { return time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC) }
	up = false
	if _, err := v.Verify(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}

	p, _ = r.Get("flaky")
	if p.LastVerified != goodDate {
		t.Errorf("last_verified should still read %q from the last success, got %q", goodDate, p.LastVerified)
	}
	if !p.Stale {
		t.Error("expected the entry flagged stale after the outage")
	}
	if p.LastChecked != "2026-08-20" {
		t.Errorf("last_checked should advance to the failed attempt's date, got %q", p.LastChecked)
	}
}

// TestVerifySurfacesSunsetHeaderAsWarningNotFailure covers RFC 8594 Sunset
// and the Deprecation convention several APIs use ahead of it: an endpoint
// that answers 200 while announcing its own retirement is not "down," but a
// human deciding whether to plan a migration needs to see it.
func TestVerifySurfacesSunsetHeaderAsWarningNotFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Sunset", "Sat, 31 Oct 2026 23:59:59 GMT")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &Registry{
		File: File{Providers: []Provider{{
			ProviderID: "sunsetting", Name: "Sunsetting", Type: TypeScholarlyMetadata,
			Status: StatusResearched, BaseURL: srv.URL,
		}}},
		byID: map[string]*Provider{},
	}
	r.byID["sunsetting"] = &r.Providers[0]

	results, err := newTestVerifier().Verify(context.Background(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Fatal("a Sunset header on an otherwise healthy endpoint must not fail the check")
	}
	if len(results[0].Warnings) == 0 {
		t.Fatal("expected the Sunset header surfaced as a warning")
	}
	p, _ := r.Get("sunsetting")
	if p.LastVerified == "" {
		t.Error("a warning-only result should still count as verified")
	}
	if len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "Sunset") {
		t.Errorf("expected a persisted Sunset warning, got %v", p.Warnings)
	}
}

// TestVerifyDetectsDocumentationDrift covers the acceptance criterion that
// documentation-URL content changing materially since last_verified is
// surfaced. The first run establishes the baseline hash; the second, against
// changed content, must warn without failing the check.
func TestVerifyDetectsDocumentationDrift(t *testing.T) {
	body := "these are the docs, v1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	r := &Registry{
		File: File{Providers: []Provider{{
			ProviderID: "docdrift", Name: "DocDrift", Type: TypeScholarlyMetadata,
			Status: StatusResearched, BaseURL: srv.URL, DocumentationURL: srv.URL + "/docs",
		}}},
		byID: map[string]*Provider{},
	}
	r.byID["docdrift"] = &r.Providers[0]

	v := newTestVerifier()
	if _, err := v.Verify(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := r.Get("docdrift")
	if p.DocumentationContentHash == "" {
		t.Fatal("expected a baseline documentation hash after the first run")
	}
	if len(p.Warnings) != 0 {
		t.Fatalf("first run has nothing to compare against, expected no drift warning, got %v", p.Warnings)
	}

	body = "these are the docs, v2, everything moved"
	results, err := v.Verify(context.Background(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Fatal("documentation drift must warn, not fail")
	}
	p, _ = r.Get("docdrift")
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "documentation_drift") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a documentation_drift warning after content changed, got %v", p.Warnings)
	}
}

// TestVerifyDetectsSunsetKeywordInDocumentation covers the weaker,
// prose-based signal: most providers announce a migration in the docs body
// with no Sunset header at all, which is exactly how the PatentsView -> USPTO
// ODP migration this issue references would have shown up.
func TestVerifyDetectsSunsetKeywordInDocumentation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("This API has been discontinued. Please use the new portal instead."))
	}))
	defer srv.Close()

	r := &Registry{
		File: File{Providers: []Provider{{
			ProviderID: "migrating", Name: "Migrating", Type: TypeScholarlyMetadata,
			Status: StatusResearched, BaseURL: srv.URL, DocumentationURL: srv.URL + "/docs",
		}}},
		byID: map[string]*Provider{},
	}
	r.byID["migrating"] = &r.Providers[0]

	if _, err := newTestVerifier().Verify(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := r.Get("migrating")
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "documentation_sunset_keyword") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a documentation_sunset_keyword warning, got %v", p.Warnings)
	}
}
