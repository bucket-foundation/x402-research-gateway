package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// sunsetSignalHeaders are the HTTP headers a well-behaved API uses to
// announce it is going away: Sunset (RFC 8594) and the draft-standard
// Deprecation header several providers (GitHub, others) have converged on
// ahead of formal standardization. Either one present is worth a human's
// attention regardless of whether the response otherwise looks healthy.
var sunsetSignalHeaders = []string{"Sunset", "Deprecation"}

// sunsetKeywords are matched, case-insensitively, against documentation body
// text as a second, weaker signal: many providers announce a migration in
// prose (the PatentsView -> USPTO ODP migration this issue was filed after)
// with no header at all.
var sunsetKeywords = []string{
	"has been discontinued", "is discontinued", "is deprecated",
	"has been sunset", "will be sunset", "migrating to", "has migrated to",
	"replaced by", "no longer supported", "no longer maintained",
	"end of life", "end-of-life",
}

// VerifyResult is the outcome of checking one provider.
type VerifyResult struct {
	ProviderID string
	Checks     []CheckResult
	// OK means every check that could fail the provider passed. A provider
	// can be OK and still carry Warnings (a sunset notice, a documentation
	// change) that deserve a human's attention without flagging the entry
	// stale.
	OK       bool
	Warnings []CheckResult
	// newDocumentationHash is the content fingerprint observed this run, set
	// whenever the documentation URL was fetched successfully. Unexported:
	// it is Verify's own bookkeeping for persisting to the registry, not
	// part of the result a caller reports.
	newDocumentationHash string
}

// CheckResult is one liveness or drift check.
type CheckResult struct {
	// Kind identifies the check: documentation_url, base_url, terms_url,
	// documentation_drift, sunset_signal.
	Kind    string
	URL     string
	Status  int
	Err     string
	Skipped string // why the check did not run
	// Warning marks a check that surfaces information without failing the
	// provider: a Sunset/Deprecation header, a documentation body that
	// changed since the last check. These do not flip OK to false and do
	// not block last_verified from being stamped.
	Warning bool
	// Detail carries the human-readable finding for a warning, e.g. the
	// Sunset header value or "documentation body changed since 2026-07-01".
	Detail string
}

func (c CheckResult) OK() bool {
	if c.Skipped != "" || c.Warning {
		return true
	}
	// Anything that answers is alive enough for a liveness check. A 401/403
	// means the endpoint exists and wants credentials, which is not drift.
	return c.Err == "" && c.Status > 0 && c.Status < 500
}

// Verifier performs cheap liveness and drift checks. It is deliberately
// minimal: one lightweight request per URL, no scraping, no bulk downloads,
// no crawling. A failure flags an entry stale; it never deletes one.
type Verifier struct {
	Client *http.Client
	// UserAgent identifies the gateway politely, so upstreams can see who is
	// checking and contact us rather than block us.
	UserAgent string
	// Delay is the pause between providers, to stay well inside rate limits.
	Delay time.Duration
	// Now supplies the verification date, injectable for tests.
	Now func() time.Time
}

// NewVerifier returns a verifier with polite defaults.
func NewVerifier() *Verifier {
	return &Verifier{
		Client:    &http.Client{Timeout: 15 * time.Second},
		UserAgent: "x402-research-gateway/registry-verify (+https://github.com/bucket-foundation/x402-research-gateway)",
		Delay:     time.Second,
		Now:       time.Now,
	}
}

// probeResult is what one HTTP round trip yields, kept separate from
// CheckResult so callers that want the headers or body (sunset detection,
// documentation-drift hashing) do not have to re-request.
type probeResult struct {
	status  int
	err     error
	headers http.Header
	// body is populated only by checks that ask for it (documentationBody),
	// so the plain liveness probe stays a HEAD/ranged-GET and never pulls a
	// full response over the wire.
	body []byte
}

// check issues one HEAD, falling back to a ranged GET for servers that reject
// HEAD. Nothing beyond the response head is read. It also surfaces a Sunset
// or Deprecation header as a warning: a provider can be perfectly reachable
// today and still be telling every client it is going away.
func (v *Verifier) check(ctx context.Context, kind, url string) CheckResult {
	res := CheckResult{Kind: kind, URL: url}
	if strings.TrimSpace(url) == "" {
		res.Skipped = "not recorded"
		return res
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		res.Skipped = "not an absolute URL"
		return res
	}
	// A template is not a fetchable URL.
	if strings.Contains(url, "{") {
		res.Skipped = "URL template"
		return res
	}

	p, err := v.probe(ctx, url, false)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	res.Status = p.status
	if sunset := sunsetHeaderValue(p.headers); sunset != "" {
		res.Detail = sunset
	}
	return res
}

// probe performs the HEAD-then-ranged-GET liveness request, optionally
// pulling the body (withBody) for checks that need to inspect content, e.g.
// documentation-drift hashing or sunset-keyword scanning.
func (v *Verifier) probe(ctx context.Context, url string, withBody bool) (probeResult, error) {
	do := func(method string) (probeResult, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return probeResult{}, err
		}
		req.Header.Set("User-Agent", v.UserAgent)
		if method == http.MethodGet && !withBody {
			// Ask for the first bytes only; this is a liveness probe.
			req.Header.Set("Range", "bytes=0-0")
		}
		resp, err := v.Client.Do(req)
		if err != nil {
			return probeResult{}, err
		}
		defer resp.Body.Close()
		out := probeResult{status: resp.StatusCode, headers: resp.Header}
		if withBody {
			// Cap what we read: this is a drift check on a documentation
			// page, not a mirror. 2MiB comfortably covers a rendered docs
			// page while bounding memory against a misbehaving server.
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if readErr != nil {
				return out, readErr
			}
			out.body = body
		}
		return out, nil
	}

	method := http.MethodHead
	if withBody {
		method = http.MethodGet
	}
	res, err := do(method)
	if err != nil || res.status == http.StatusMethodNotAllowed || res.status == http.StatusNotImplemented {
		res, err = do(http.MethodGet)
	}
	return res, err
}

// sunsetHeaderValue returns the first sunset-signal header present, so a
// caller can surface it without caring which of the two conventions a
// provider used.
func sunsetHeaderValue(h http.Header) string {
	if h == nil {
		return ""
	}
	for _, name := range sunsetSignalHeaders {
		if v := h.Get(name); v != "" {
			return fmt.Sprintf("%s: %s", name, v)
		}
	}
	return ""
}

// hashBody returns a stable content fingerprint for documentation-drift
// detection. A hash, not the body itself, is stored on the registry entry:
// the point is "has this changed", not mirroring the provider's docs.
func hashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// containsSunsetKeyword reports whether documentation prose announces a
// migration or discontinuation, the weaker signal for providers that do not
// set a Sunset/Deprecation header (most don't).
func containsSunsetKeyword(body []byte) string {
	lower := strings.ToLower(string(body))
	for _, kw := range sunsetKeywords {
		if strings.Contains(lower, kw) {
			return kw
		}
	}
	return ""
}

// VerifyProvider runs the cheap checks for one provider. Excluded and sunset
// sources are not contacted at all: we have decided not to operate them, so
// probing them would be pointless and, for the excluded ones, unwise.
func (v *Verifier) VerifyProvider(ctx context.Context, p *Provider) VerifyResult {
	out := VerifyResult{ProviderID: p.ProviderID, OK: true}

	if p.Status == StatusExcluded || p.Status == StatusSunset {
		out.Checks = append(out.Checks, CheckResult{
			Kind:    "skipped",
			Skipped: fmt.Sprintf("provider is %s; not contacted", p.Status),
		})
		return out
	}

	for _, c := range []struct{ kind, url string }{
		{"documentation_url", p.DocumentationURL},
		{"base_url", p.BaseURL},
		{"terms_url", p.Rights.TermsURL},
	} {
		r := v.check(ctx, c.kind, c.url)
		out.Checks = append(out.Checks, r)
		if r.Detail != "" {
			// A sunset/deprecation header on any of these URLs is a warning,
			// not a failure: the endpoint answered, it is telling us it
			// won't for much longer.
			out.Warnings = append(out.Warnings, CheckResult{
				Kind: c.kind + "_sunset_signal", URL: c.url, Warning: true, Detail: r.Detail,
			})
		}
		if !r.OK() {
			out.OK = false
		}
	}

	if d, newHash, ok := v.checkDocumentationDrift(ctx, p); ok {
		out.Warnings = append(out.Warnings, d)
		out.newDocumentationHash = newHash
	} else if newHash != "" {
		out.newDocumentationHash = newHash
	}

	return out
}

// checkDocumentationDrift fetches the provider's documentation body and
// compares it against the hash recorded from the last time this check ran.
// A change is a warning, never a failure: prose changing is common and often
// unrelated to the API contract, but a human deciding whether to re-review
// the source needs to know it happened. The hash is only ever compared and
// updated when the fetch succeeds, so a transient outage cannot manufacture
// a false "documentation changed" signal.
//
// It returns the freshly computed hash whenever the fetch succeeded, drift
// or not, so Verify can persist "what we last saw" even on a quiet check.
func (v *Verifier) checkDocumentationDrift(ctx context.Context, p *Provider) (res CheckResult, newHash string, warned bool) {
	url := strings.TrimSpace(p.DocumentationURL)
	if url == "" || strings.Contains(url, "{") ||
		(!strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://")) {
		return CheckResult{}, "", false
	}

	probed, err := v.probe(ctx, url, true)
	if err != nil {
		// The plain liveness check above already flagged this as a failure
		// if it matters; a drift check that cannot fetch has nothing to
		// compare and stays silent rather than double-reporting.
		return CheckResult{}, "", false
	}

	sum := hashBody(probed.body)
	warn := CheckResult{Kind: "documentation_drift", URL: url, Warning: true, Status: probed.status}

	if kw := containsSunsetKeyword(probed.body); kw != "" {
		warn.Kind = "documentation_sunset_keyword"
		warn.Detail = fmt.Sprintf("documentation body contains %q", kw)
		return warn, sum, true
	}

	if p.DocumentationContentHash != "" && p.DocumentationContentHash != sum {
		warn.Detail = fmt.Sprintf("documentation body changed since last check (%s -> %s)",
			p.DocumentationContentHash[:12], sum[:12])
		return warn, sum, true
	}
	return CheckResult{}, sum, false
}

// Verify checks every provider and records the outcome on the registry.
// It mutates LastVerified / Stale / StaleReason in memory; the caller decides
// whether to write the registry back.
//
// Verification never removes a provider. A failure flags the entry stale so a
// human can look, which is the whole point: an upstream being briefly down is
// not evidence that a source should be forgotten.
func (v *Verifier) Verify(ctx context.Context, r *Registry, only []string) ([]VerifyResult, error) {
	want := map[string]bool{}
	for _, id := range only {
		want[id] = true
	}

	today := v.Now().UTC().Format("2006-01-02")
	var results []VerifyResult

	for i := range r.Providers {
		p := &r.Providers[i]
		if len(want) > 0 && !want[p.ProviderID] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return results, err
		}

		res := v.VerifyProvider(ctx, p)
		results = append(results, res)

		// last_verified records the last SUCCESSFUL check, per #22's
		// acceptance criteria. An outage must never look like a fresh
		// verification: stamping today's date on a failed check would
		// erase the one fact worth keeping, which is when the source was
		// last known to actually work. LastChecked, by contrast, is
		// stamped every attempt, so "we tried and it failed" is still
		// distinguishable from "we never checked."
		p.LastChecked = today
		if res.OK {
			p.LastVerified = today
			p.Stale = false
			p.StaleReason = ""
		} else {
			p.Stale = true
			p.StaleReason = summarize(res)
		}
		if res.newDocumentationHash != "" {
			p.DocumentationContentHash = res.newDocumentationHash
		}
		p.Warnings = warningStrings(res.Warnings)

		if v.Delay > 0 {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			case <-time.After(v.Delay):
			}
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].ProviderID < results[j].ProviderID })
	return results, nil
}

// warningStrings renders a run's warnings for storage on the registry entry.
// An empty result clears any previous warnings, matching Verify's contract
// that every field it owns reflects only the most recent check.
func warningStrings(warnings []CheckResult) []string {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, fmt.Sprintf("%s: %s", w.Kind, w.Detail))
	}
	return out
}

func summarize(res VerifyResult) string {
	var parts []string
	for _, c := range res.Checks {
		if c.OK() {
			continue
		}
		if c.Err != "" {
			parts = append(parts, fmt.Sprintf("%s unreachable: %s", c.Kind, c.Err))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s returned %d", c.Kind, c.Status))
	}
	return strings.Join(parts, "; ")
}
