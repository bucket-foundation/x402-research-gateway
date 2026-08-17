package registry

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// VerifyResult is the outcome of checking one provider.
type VerifyResult struct {
	ProviderID string
	Checks     []CheckResult
	OK         bool
}

// CheckResult is one liveness or drift check.
type CheckResult struct {
	Kind    string // documentation_url, base_url, terms_url
	URL     string
	Status  int
	Err     string
	Skipped string // why the check did not run
}

func (c CheckResult) OK() bool {
	if c.Skipped != "" {
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

// check issues one HEAD, falling back to a ranged GET for servers that reject
// HEAD. Nothing beyond the response head is read.
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

	do := func(method string) (int, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", v.UserAgent)
		if method == http.MethodGet {
			// Ask for the first bytes only; this is a liveness probe.
			req.Header.Set("Range", "bytes=0-0")
		}
		resp, err := v.Client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}

	status, err := do(http.MethodHead)
	if err != nil || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented {
		status, err = do(http.MethodGet)
	}
	if err != nil {
		res.Err = err.Error()
		return res
	}
	res.Status = status
	return res
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
		if !r.OK() {
			out.OK = false
		}
	}
	return out
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

		p.LastVerified = today
		if res.OK {
			p.Stale = false
			p.StaleReason = ""
		} else {
			p.Stale = true
			p.StaleReason = summarize(res)
		}

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
