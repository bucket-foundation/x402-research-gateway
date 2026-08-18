package harvest

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const secret = "test-cursor-secret"

// The credential values a cursor must never carry. Fixture strings, not
// credentials.
const (
	fixtureAPIKey = "SECRET-UPSTREAM-KEY-do-not-leak"
	fixtureEmail  = "research@viatika.ai"
)

func TestSanitizeURL_StripsCredentials(t *testing.T) {
	raw := "https://api.example.org/v2/search?q=mitochondria&api_key=" + fixtureAPIKey +
		"&email=" + fixtureEmail + "&tool=x402&signature=abc&page=3"
	got := SanitizeURL(raw)
	for _, banned := range []string{fixtureAPIKey, fixtureEmail, "api_key", "email", "tool", "signature"} {
		if strings.Contains(got, banned) {
			t.Fatalf("sanitized URL still carries %q: %s", banned, got)
		}
	}
	for _, kept := range []string{"q=mitochondria", "page=3", "api.example.org"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("sanitized URL dropped %q: %s", kept, got)
		}
	}
}

func TestSanitizeURL_StripsUserinfo(t *testing.T) {
	got := SanitizeURL("https://user:" + fixtureAPIKey + "@api.example.org/v2?q=x")
	if strings.Contains(got, fixtureAPIKey) || strings.Contains(got, "user:") {
		t.Fatalf("userinfo survived: %s", got)
	}
}

func TestFingerprints_AreKeyedAndStable(t *testing.T) {
	s := NewSigner(secret)
	other := NewSigner("a different secret")

	q1 := s.QueryFingerprint("Mitochondria ")
	q2 := s.QueryFingerprint("mitochondria")
	if q1 != q2 {
		t.Fatal("the same normalized query produced two fingerprints")
	}
	if q1 == other.QueryFingerprint("mitochondria") {
		t.Fatal("fingerprints are not keyed; two gateways produced the same digest")
	}
	if strings.Contains(q1, "mitochondria") {
		t.Fatal("the query appears in its own fingerprint")
	}
	if !strings.HasPrefix(q1, "hmac-sha256:") {
		t.Fatalf("fingerprint = %q; feed402 §3.6 forbids an unsalted digest", q1)
	}
}

// The provider request fingerprint must be computed after the credential
// exclusion, so the digest cannot be brute-forced back to a live key.
func TestProviderRequestFingerprint_ExcludesCredentialsBeforeHashing(t *testing.T) {
	s := NewSigner(secret)
	withKey := "https://api.example.org/v2?q=x&api_key=" + fixtureAPIKey + "&email=" + fixtureEmail
	without := "https://api.example.org/v2?q=x"
	if s.ProviderRequestFingerprint("GET", withKey) != s.ProviderRequestFingerprint("GET", without) {
		t.Fatal("the fingerprint changes with the credential, so it was hashed before exclusion")
	}
	if s.ProviderRequestFingerprint("GET", withKey) ==
		s.ProviderRequestFingerprint("GET", "https://api.example.org/v2?q=y") {
		t.Fatal("the fingerprint does not distinguish two different queries")
	}
}

func cursorFixture(s *Signer) Cursor {
	return Stamp(Cursor{
		RequestFingerprint: s.ProviderRequestFingerprint("GET",
			"https://api.example.org/v2?q=x&api_key="+fixtureAPIKey+"&email="+fixtureEmail),
		QueryFingerprint:   s.QueryFingerprint("mitochondria"),
		Provider:           "pubmed-search",
		PaginationModel:    ModelOffset,
		ProviderCursor:     Position{Offset: 0},
		NextCursor:         Position{Offset: 25},
		PageResultCount:    25,
		ResponseSHA256:     ResponseSHA256([]byte(`{"esearchresult":{}}`)),
		RateLimitRemaining: "9",
		ProviderRelease:    "eutils-2026-07",
	}, time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC))
}

// The whole point of the exclusion rules: nothing sensitive reaches a client
// through cursor state. Same shape as TestUnpaywallIdentity_NoEmailLeakage.
func TestCursorState_NoCredentialLeakage(t *testing.T) {
	s := NewSigner(secret)
	c := cursorFixture(s)
	token, err := s.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(c)
	decoded, err := Inspect(token)
	if err != nil {
		t.Fatal(err)
	}
	inspected, _ := json.Marshal(decoded)

	for _, surface := range []string{string(blob), string(inspected), token} {
		for _, banned := range []string{
			fixtureAPIKey, fixtureEmail, secret, "api_key", "Bearer",
			"api.example.org", // the upstream URL itself never travels
		} {
			if strings.Contains(surface, banned) {
				t.Fatalf("cursor state carries %q", banned)
			}
		}
	}
	// The base64 payload is readable, so the check above has to hold over
	// the decoded form too, which it does. Confirm the readable fields a
	// client needs did survive.
	if decoded.ResultCount != c.ResultCount || decoded.Provider != "pubmed-search" ||
		decoded.RetrievedAt != c.RetrievedAt {
		t.Fatalf("inspectable fields lost: %+v", decoded)
	}
}

func TestCursor_RoundTrip(t *testing.T) {
	s := NewSigner(secret)
	c := cursorFixture(s)
	token, err := s.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := s.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	if back.NextCursor != c.NextCursor || back.PaginationModel != c.PaginationModel {
		t.Fatalf("round trip lost the position: %+v", back)
	}
}

func TestCursor_ForgeryRejected(t *testing.T) {
	s := NewSigner(secret)
	token, _ := s.Encode(cursorFixture(s))
	parts := strings.SplitN(token, ".", 2)

	forged := cursorFixture(s)
	forged.NextCursor = Position{Offset: 100000}
	payload, _ := json.Marshal(forged)
	repacked := base64Raw(payload) + "." + parts[1]

	for name, bad := range map[string]string{
		"repacked payload":  repacked,
		"truncated":         parts[0],
		"empty":             "",
		"garbage":           "not-a-cursor",
		"wrong tag":         parts[0] + ".AAAA",
		"other gateway key": mustEncode(t, NewSigner("another gateway"), cursorFixture(s)),
	} {
		if _, err := s.Decode(bad); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestContinue_DetectsReleaseBoundary(t *testing.T) {
	s := NewSigner(secret)
	first := cursorFixture(s)
	first.ResultCount = first.PageResultCount
	first.StartedAt = first.RetrievedAt
	first.StartedRelease = first.ProviderRelease

	same := cursorFixture(s)
	same.PageResultCount = 25
	if got := first.Continue(same); got.ReleaseChanged {
		t.Fatal("a page from the same release was flagged as a boundary crossing")
	}

	moved := cursorFixture(s)
	moved.PageResultCount = 25
	moved.ProviderRelease = "eutils-2026-08"
	got := first.Continue(moved)
	if !got.ReleaseChanged {
		t.Fatal("a release change across a resume was not detected")
	}
	if got.ResultCount != 50 {
		t.Fatalf("result_count = %d, want the running total 50", got.ResultCount)
	}
	if got.StartedRelease != "eutils-2026-07" {
		t.Fatalf("started_release = %q", got.StartedRelease)
	}
	// Once flagged, the flag survives further pages.
	if !first.Continue(got).Continue(same).ReleaseChanged {
		t.Fatal("the boundary flag was cleared by a later page")
	}
}

func TestSigner_EphemeralIsReported(t *testing.T) {
	if NewSigner(secret).Ephemeral() {
		t.Fatal("a configured signer reported itself ephemeral")
	}
	if !NewSigner("").Ephemeral() {
		t.Fatal("an unconfigured signer did not report itself ephemeral")
	}
}

func base64Raw(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func mustEncode(t *testing.T, s *Signer, c Cursor) string {
	t.Helper()
	token, err := s.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
