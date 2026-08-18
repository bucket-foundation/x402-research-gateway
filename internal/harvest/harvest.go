// Package harvest makes a paged retrieval survivable
// (x402-research-gateway#10).
//
// An agent walking a large result set that fails on page four hundred has to
// be able to resume. The gateway runs no harvest and stores no harvest
// state: it hands the client a cursor after every page, and accepts that
// cursor back to continue from the exact position.
//
// Three rules hold everywhere here.
//
// Cursor state is inspectable and unforgeable. It travels as a signed
// envelope: a base64url JSON payload a client can read, plus an HMAC tag
// only this gateway can produce. A client reads result_count, provider, and
// retrieved_at; it cannot move the position without invalidating the tag.
//
// No credential ever enters cursor state. The provider request fingerprint
// is computed after stripping API keys, tokens, signatures, and polite-pool
// email addresses, following the feed402 SPEC §3.6 exclusion rule. The
// upstream URL itself is never carried. A test asserts this over emitted
// cursors rather than leaving it to review.
//
// A resumed harvest that crosses a provider release boundary is detectable.
// provider_release travels in the cursor, and a page whose release differs
// from the one the harvest started on is reported rather than silently
// folded into a set that was never consistent at any single moment.
package harvest

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Model names a provider's pagination scheme, using the feed402 SPEC §1.2
// PaginationModel vocabulary.
const (
	ModelNone   = "none"
	ModelOffset = "offset"
	ModelPage   = "page"
	ModelCursor = "cursor"
	ModelToken  = "token"
)

// Position is where in a provider's result set a page starts. One struct
// covers the four models the gateway proxies: Offset for PubMed and
// Semantic Scholar, Page for OpenAlex, Token for ClinicalTrials.gov and
// every cursor-shaped provider.
type Position struct {
	Offset int    `json:"offset,omitempty"`
	Page   int    `json:"page,omitempty"`
	Token  string `json:"token,omitempty"`
}

// Zero reports whether this is the start of a result set.
func (p Position) Zero() bool { return p.Offset == 0 && p.Page == 0 && p.Token == "" }

// Cursor is the client-persistable harvest state. Every field is emitted;
// none of them carries a credential or an upstream URL.
type Cursor struct {
	// RequestFingerprint identifies the upstream request this gateway
	// issued, credentials excluded, per feed402 SPEC §3.6
	// provider_request_fingerprint.
	RequestFingerprint string `json:"request_fingerprint"`
	// QueryFingerprint identifies the query without revealing it, per
	// feed402 SPEC §3.6 query_fingerprint.
	QueryFingerprint string `json:"query_fingerprint"`
	// Provider is the route id this cursor belongs to. A cursor is not
	// portable across providers and the resume path refuses one that is
	// presented to a different route.
	Provider string `json:"provider"`
	// PaginationModel is the provider's own scheme.
	PaginationModel string `json:"pagination_model"`
	// ProviderCursor is the position this page was fetched at.
	ProviderCursor Position `json:"provider_cursor"`
	// NextCursor is the position the next page starts at. Zero with
	// Exhausted true means the set is finished.
	NextCursor Position `json:"next_cursor"`
	// Exhausted reports that the provider published no further page.
	Exhausted bool `json:"exhausted"`
	// ResultCount is the running total across the harvest, so a resumed
	// client knows what it already holds.
	ResultCount int `json:"result_count"`
	// PageResultCount is how many records this page carried.
	PageResultCount int `json:"page_result_count"`
	// UpstreamRequestID is the provider's own request identifier when it
	// published one in a response header.
	UpstreamRequestID string `json:"upstream_request_id,omitempty"`
	// ResponseSHA256 is the hash of the exact upstream response body, per
	// feed402 SPEC §3.6, enabling byte-level replay.
	ResponseSHA256 string `json:"response_sha256,omitempty"`
	// RateLimitRemaining is the provider's own remaining-quota header,
	// verbatim, so a client can pace itself.
	RateLimitRemaining string `json:"rate_limit_remaining,omitempty"`
	// RetryAfter is the provider's Retry-After header, verbatim.
	RetryAfter  string `json:"retry_after,omitempty"`
	RetrievedAt string `json:"retrieved_at"`
	// ProviderRelease is the upstream API version or release train this
	// page was fetched against, per feed402 SPEC §3.6.
	ProviderRelease string `json:"provider_release,omitempty"`
	// StartedAt is when the harvest's first page was fetched, carried
	// forward across resumes.
	StartedAt string `json:"started_at,omitempty"`
	// StartedRelease is the provider release the harvest's first page was
	// fetched against, carried forward so a boundary crossing is
	// detectable.
	StartedRelease string `json:"started_release,omitempty"`
	// ReleaseChanged reports that this page was fetched against a different
	// provider release than the harvest started on. The pages are still
	// returned; what is refused is pretending the set is consistent.
	ReleaseChanged bool `json:"release_changed"`
}

// ReleaseNotice is emitted whenever ReleaseChanged is set.
const ReleaseNotice = "This page was retrieved against a different provider release than the one this " +
	"harvest started on. The pages you hold were not consistent at any single point in time. Re-run the " +
	"harvest from the start if the result set must be a snapshot."

// Continue builds the cursor for the next page from the one just served,
// carrying the harvest-level facts forward and detecting a release
// boundary.
func (c Cursor) Continue(next Cursor) Cursor {
	next.ResultCount = c.ResultCount + next.PageResultCount
	next.StartedAt = firstNonEmpty(c.StartedAt, c.RetrievedAt, next.RetrievedAt)
	next.StartedRelease = firstNonEmpty(c.StartedRelease, c.ProviderRelease)
	if next.StartedRelease != "" && next.ProviderRelease != "" &&
		next.StartedRelease != next.ProviderRelease {
		next.ReleaseChanged = true
	}
	if c.ReleaseChanged {
		next.ReleaseChanged = true
	}
	return next
}

// ErrCursor covers every rejection of a presented cursor. The reasons are
// deliberately indistinguishable to the client: a forged tag and a
// truncated payload are both "this cursor is not one I issued."
var ErrCursor = errors.New("cursor is not valid for this gateway")

// Signer encodes and decodes cursors, and computes the feed402 §3.6
// fingerprints. One key does both, and it never leaves the process.
type Signer struct {
	key       []byte
	ephemeral bool
}

// NewSigner builds a signer from an operator-supplied secret. An empty
// secret produces a random per-process key, which keeps the gateway working
// without configuration at the cost of invalidating outstanding cursors on
// restart. That tradeoff is stated rather than hidden: a deployment that
// wants cursors to survive a restart configures a secret.
func NewSigner(secret string) *Signer {
	if secret != "" {
		sum := sha256.Sum256([]byte("x402-research-gateway/harvest/" + secret))
		return &Signer{key: sum[:]}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// A gateway that cannot get randomness cannot issue unforgeable
		// cursors, and a predictable fallback would be worse than the
		// panic: it would look like it worked.
		panic("harvest: no source of randomness for the cursor key")
	}
	return &Signer{key: key, ephemeral: true}
}

// Ephemeral reports whether this signer's key is per-process, which means
// outstanding cursors stop verifying when the gateway restarts.
func (s *Signer) Ephemeral() bool { return s.ephemeral }

// QueryFingerprint is feed402 SPEC §3.6 query_fingerprint: a keyed digest
// over the normalized query. Two calls carrying the same query produce the
// same fingerprint within this gateway, and a holder cannot recover the
// query, which unsalted SHA-256 would not achieve.
func (s *Signer) QueryFingerprint(query string) string {
	return "hmac-sha256:" + s.mac("query\x00"+strings.ToLower(strings.TrimSpace(query)))
}

// ProviderRequestFingerprint is feed402 SPEC §3.6
// provider_request_fingerprint: a keyed digest over the upstream request
// this gateway issued, computed after the credential exclusion below. The
// exclusion happens before hashing on purpose: hashing the raw request
// would produce a digest a determined holder could brute-force back to a
// live credential.
func (s *Signer) ProviderRequestFingerprint(method, rawURL string) string {
	return "hmac-sha256:" + s.mac("request\x00"+strings.ToUpper(method)+"\x00"+SanitizeURL(rawURL))
}

func (s *Signer) mac(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

// CredentialParams are the query parameters stripped before fingerprinting.
// Same list feed402's conformance validator enforces against published
// URLs: keys, tokens, signatures, and the polite-pool identifiers that name
// a person.
var CredentialParams = map[string]bool{
	"api_key": true, "apikey": true, "api-key": true, "key": true,
	"token": true, "access_token": true, "auth": true, "auth_token": true,
	"bearer": true, "password": true, "secret": true, "client_secret": true,
	"signature": true, "sig": true, "session": true, "sessionid": true,
	"email": true, "mailto": true, "tool": true, "user": true, "username": true,
}

// SanitizeURL removes credentials from a URL: userinfo, and every query
// parameter named in CredentialParams. A URL that does not parse is
// reduced to its scheme and host when those survive, and to the empty
// string when they do not, because a fingerprint over an unparsed string
// could carry a key.
func SanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	q := u.Query()
	for k := range q {
		if CredentialParams[strings.ToLower(k)] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String()
}

// ResponseSHA256 hashes an upstream response body, per feed402 SPEC §3.6.
func ResponseSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Encode renders a cursor as the opaque-but-readable token a client
// persists: base64url(payload).base64url(tag).
func (s *Signer) Encode(c Cursor) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	tag := base64.RawURLEncoding.EncodeToString(s.tag(body))
	return body + "." + tag, nil
}

// Decode verifies and parses a presented cursor. A tampered payload, a bad
// tag, or a malformed token all return ErrCursor.
func (s *Signer) Decode(token string) (Cursor, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Cursor{}, ErrCursor
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(want, s.tag(parts[0])) {
		return Cursor{}, ErrCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Cursor{}, ErrCursor
	}
	var c Cursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return Cursor{}, ErrCursor
	}
	return c, nil
}

// Inspect parses a cursor's readable fields without verifying the tag. It
// exists for a client that wants to read its own state; the gateway itself
// always uses Decode, so an unverified cursor can never set a position.
func Inspect(token string) (Cursor, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) == 0 || parts[0] == "" {
		return Cursor{}, ErrCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Cursor{}, ErrCursor
	}
	var c Cursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return Cursor{}, ErrCursor
	}
	return c, nil
}

func (s *Signer) tag(body string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("cursor\x00" + body))
	return m.Sum(nil)
}

// Stamp fills the timestamp on a cursor.
func Stamp(c Cursor, at time.Time) Cursor {
	c.RetrievedAt = at.UTC().Format(time.RFC3339)
	return c
}

// Itoa is the small integer conversion the pagination params need, kept
// here so provider adapters do not each import strconv for one call.
func Itoa(n int) string { return strconv.Itoa(n) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
