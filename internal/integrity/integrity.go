// Package integrity models the scholarly update and integrity graph
// (x402-research-gateway#9): corrections, errata, retractions, withdrawals,
// expressions of concern, and new versions.
//
// A paid gateway feeding autonomous agents that returns a retracted paper
// with the same confidence as a sound one is the most consequential failure
// available to it. This package exists so an agent can ask.
//
// Two rules shape everything here.
//
// Disagreement coexists. Providers ingest notices at different times and
// record different things. One source carrying a retraction another has not
// yet ingested is informative, so every provider's assertion is stored side
// by side. There is no single status field, no flattening, and no
// gateway-side adjudication of which provider is right.
//
// Absence is not clearance. A work with no notice from the consulted
// providers is a work those providers reported nothing about. Every
// response names who was consulted, including the ones that reported
// nothing, and restates that rule in the payload.
package integrity

import (
	"sort"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Status is what an assertion says happened to a work. The set is closed in
// this revision; a provider term that maps to none of these is carried on
// the assertion with Status empty and Recognized false rather than being
// forced into the nearest one, because calling a correction a retraction is
// worse than saying nothing.
type Status string

const (
	StatusCorrection          Status = "correction"
	StatusErratum             Status = "erratum"
	StatusRetraction          Status = "retraction"
	StatusWithdrawal          Status = "withdrawal"
	StatusExpressionOfConcern Status = "expression_of_concern"
	StatusNewVersion          Status = "new_version"
)

// Statuses is the closed set this revision recognizes.
var Statuses = []Status{
	StatusCorrection, StatusErratum, StatusRetraction, StatusWithdrawal,
	StatusExpressionOfConcern, StatusNewVersion,
}

func (s Status) Valid() bool {
	for _, x := range Statuses {
		if x == s {
			return true
		}
	}
	return false
}

// providerTerms maps a lowercased upstream term onto a status. Extended at
// runtime with RegisterTerm, which lets a provider package add its own
// vocabulary without this file knowing about the provider.
var providerTerms = map[string]Status{}

// RegisterTerm maps one provider's own term onto a status.
func RegisterTerm(providerTerm string, s Status) {
	providerTerms[strings.ToLower(strings.TrimSpace(providerTerm))] = s
}

// StatusFor resolves a provider term. ok reports whether a mapping existed.
func StatusFor(providerTerm string) (Status, bool) {
	s, ok := providerTerms[strings.ToLower(strings.TrimSpace(providerTerm))]
	return s, ok
}

func init() {
	// Crossref / Crossmark update types.
	for term, s := range map[string]Status{
		"correction": StatusCorrection, "addendum": StatusCorrection,
		"corrigendum": StatusErratum, "erratum": StatusErratum,
		"retraction": StatusRetraction, "partial_retraction": StatusRetraction,
		"withdrawal": StatusWithdrawal, "removal": StatusWithdrawal,
		"expression_of_concern": StatusExpressionOfConcern,
		"new_version":           StatusNewVersion, "new_edition": StatusNewVersion,
	} {
		RegisterTerm(term, s)
	}
	// Europe PMC commentCorrectionList types, which carry the PubMed
	// correction and retraction relations.
	for term, s := range map[string]Status{
		"erratum in": StatusErratum, "erratum for": StatusErratum,
		"corrected and republished in":   StatusCorrection,
		"corrected and republished from": StatusCorrection,
		"retraction in":                  StatusRetraction,
		"retraction of":                  StatusRetraction,
		"retracted publication":          StatusRetraction,
		"expression of concern in":       StatusExpressionOfConcern,
		"expression of concern for":      StatusExpressionOfConcern,
		"update in":                      StatusNewVersion,
		"preprint of this article":       StatusNewVersion,
	} {
		RegisterTerm(term, s)
	}
	// DataCite version relations, the repository-side version history.
	for term, s := range map[string]Status{
		"isnewversionof": StatusNewVersion, "ispreviousversionof": StatusNewVersion,
		"isobsoletedby": StatusWithdrawal,
	} {
		RegisterTerm(term, s)
	}
}

// Endpoint is one end of an assertion: the affected work or the notice.
type Endpoint struct {
	Identifiers []identity.Identifier `json:"identifiers,omitempty"`
	// RawID is the identifier string the provider used, kept even when no
	// scheme claimed it.
	RawID        string `json:"raw_id,omitempty"`
	CanonicalURL string `json:"canonical_url,omitempty"`
}

// NewEndpoint builds an endpoint from a provider's identifier string.
func NewEndpoint(rawID string) Endpoint {
	e := Endpoint{RawID: strings.TrimSpace(rawID)}
	if id, ok := identity.Parse(rawID); ok {
		e.Identifiers = []identity.Identifier{id}
	}
	return e
}

// Key is the endpoint's exact-match key: the lowest sorted identifier key,
// or the raw string when nothing parsed.
func (e Endpoint) Key() string {
	keys := make([]string, 0, len(e.Identifiers))
	for _, id := range e.Identifiers {
		if id.Value != "" {
			keys = append(keys, id.Key())
		}
	}
	if len(keys) == 0 {
		return "raw:" + strings.ToLower(e.RawID)
	}
	sort.Strings(keys)
	return keys[0]
}

// Assertion is one provider's statement about one work's integrity.
type Assertion struct {
	// Provider is the route/adapter id that asserted this.
	Provider string `json:"provider"`
	// Status is the gateway term. Empty when the provider's term maps to
	// none, in which case ProviderTerm carries the whole statement.
	Status Status `json:"status,omitempty"`
	// ProviderTerm is the upstream's own term, verbatim.
	ProviderTerm string `json:"provider_term"`
	// Recognized reports whether ProviderTerm mapped to a status.
	Recognized bool `json:"recognized"`
	// Work is the work the assertion is about.
	Work Endpoint `json:"work"`
	// Notice is the notice document, where the provider named one. A
	// provider that records a status without a notice leaves this empty,
	// which is different from the notice not existing.
	Notice Endpoint `json:"notice,omitempty"`
	// NoticeID is the provider's own identifier for the notice, e.g. the
	// notice DOI, when it published one.
	NoticeID string `json:"notice_id,omitempty"`
	// Date is the notice date the provider published, verbatim in the
	// provider's own format. Empty when the provider published none.
	Date string `json:"date,omitempty"`
	// SourceField names the upstream field this was read from.
	SourceField string `json:"source_field,omitempty"`
	RetrievedAt string `json:"retrieved_at"`
	// Annotations carry per-assertion facts with no typed field, keyed by
	// the provider's own field names.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// New builds an assertion from a provider's own term, resolving the status
// without discarding the term.
func New(provider, sourceField string, work Endpoint, providerTerm string, at time.Time) Assertion {
	a := Assertion{
		Provider:     provider,
		ProviderTerm: strings.TrimSpace(providerTerm),
		Work:         work,
		SourceField:  sourceField,
		RetrievedAt:  at.UTC().Format(time.RFC3339),
	}
	if s, ok := StatusFor(providerTerm); ok {
		a.Status, a.Recognized = s, true
	}
	return a
}

// WithNotice attaches the notice document to an assertion.
func (a Assertion) WithNotice(rawID string) Assertion {
	if strings.TrimSpace(rawID) == "" {
		return a
	}
	a.Notice = NewEndpoint(rawID)
	a.NoticeID = strings.TrimSpace(rawID)
	return a
}

// ID addresses this assertion inside a Result.
func (a Assertion) ID() string {
	return a.Provider + "|" + a.Work.Key() + "|" +
		strings.ToLower(a.ProviderTerm) + "|" + a.Notice.Key()
}

// Valid reports whether the assertion carries the attribution this package
// requires.
func (a Assertion) Valid() bool {
	return a.Provider != "" && a.ProviderTerm != "" && a.RetrievedAt != "" &&
		a.Work.Key() != "raw:"
}

// Outcome is what happened when one provider was consulted.
type Outcome string

const (
	// OutcomeOK means the provider answered. AssertionCount zero means that
	// provider published no notice for this work, which is not a
	// clearance.
	OutcomeOK                    Outcome = "ok"
	OutcomeUnsupportedIdentifier Outcome = "unsupported_identifier"
	OutcomeNotConfigured         Outcome = "not_configured"
	OutcomeUpstreamError         Outcome = "upstream_error"
	OutcomeUpstreamStatus        Outcome = "upstream_status"
	OutcomeTimeout               Outcome = "timeout"
)

// ProviderReport is the per-provider account every response carries.
type ProviderReport struct {
	Provider       string  `json:"provider"`
	Consulted      bool    `json:"consulted"`
	Outcome        Outcome `json:"outcome"`
	AssertionCount int     `json:"assertion_count"`
	UpstreamStatus int     `json:"upstream_status,omitempty"`
	// Coverage states what this provider's integrity data covers, so a
	// consumer reading a zero count knows whose view it is looking at.
	Coverage string `json:"coverage,omitempty"`
}

// StatusView groups the providers asserting one status. It is an index over
// the assertions, never a replacement for them: the assertions stay whole
// and every one of them names its own provider.
type StatusView struct {
	Status Status `json:"status,omitempty"`
	// ProviderTerm is set instead of Status for a term with no gateway
	// status, so an unrecognized notice is still visible in the summary.
	ProviderTerm string `json:"provider_term,omitempty"`
	// Providers are the providers asserting this, sorted.
	Providers []string `json:"providers"`
	// Notices are the notice identifiers published for it, sorted.
	Notices []string `json:"notices,omitempty"`
	// AssertionIDs indexes back into the assertions array.
	AssertionIDs []string `json:"assertion_ids"`
}

// AbsenceNotice is emitted verbatim in every integrity response.
const AbsenceNotice = "Absence of a notice is not a clearance. A work no consulted provider published a " +
	"notice for is a work those providers reported nothing about, which is different from a work that has " +
	"been checked and found sound. Providers ingest notices at different times and record different things, " +
	"so conflicting assertions are reported side by side and this gateway does not adjudicate between them. " +
	"Read providers_consulted for who was asked and what each one returned."

// Result is one integrity query's full answer.
type Result struct {
	Query      identity.Identifier `json:"query"`
	Assertions []Assertion         `json:"assertions"`
	// StatusSummary indexes the assertions by status. It never collapses
	// them: two providers disagreeing produce two entries or one entry with
	// one provider in it, and both readings stay available in Assertions.
	StatusSummary []StatusView `json:"status_summary,omitempty"`
	// ProvidersDisagree reports that the consulted providers that answered
	// did not assert the same set of statuses. It is a flag on the data,
	// not a judgment about which provider is right.
	ProvidersDisagree bool `json:"providers_disagree"`
	// ProvidersConsulted covers every provider considered, answered or not.
	ProvidersConsulted []ProviderReport `json:"providers_consulted"`
	AbsenceNotice      string           `json:"absence_notice"`
}

// Build assembles a Result. Assertions are sorted deterministically, exact
// duplicates from one provider collapse, and nothing across providers is
// merged.
func Build(query identity.Identifier, assertions []Assertion, reports []ProviderReport, at time.Time) Result {
	seen := map[string]bool{}
	out := make([]Assertion, 0, len(assertions))
	for _, a := range assertions {
		if !a.Valid() || seen[a.ID()] {
			continue
		}
		seen[a.ID()] = true
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].Provider < reports[j].Provider })
	if reports == nil {
		reports = []ProviderReport{}
	}

	return Result{
		Query:              query,
		Assertions:         out,
		StatusSummary:      summarize(out),
		ProvidersDisagree:  disagree(out, reports),
		ProvidersConsulted: reports,
		AbsenceNotice:      AbsenceNotice,
	}
}

// summarize indexes assertions by status, keeping an unrecognized provider
// term visible under its own key.
func summarize(assertions []Assertion) []StatusView {
	type acc struct {
		status    Status
		term      string
		providers map[string]bool
		notices   map[string]bool
		ids       []string
	}
	groups := map[string]*acc{}
	order := []string{}
	for _, a := range assertions {
		key := string(a.Status)
		if !a.Recognized {
			key = "term:" + strings.ToLower(a.ProviderTerm)
		}
		g, ok := groups[key]
		if !ok {
			g = &acc{providers: map[string]bool{}, notices: map[string]bool{}}
			if a.Recognized {
				g.status = a.Status
			} else {
				g.term = a.ProviderTerm
			}
			groups[key] = g
			order = append(order, key)
		}
		g.providers[a.Provider] = true
		if a.NoticeID != "" {
			g.notices[a.NoticeID] = true
		}
		g.ids = append(g.ids, a.ID())
	}
	sort.Strings(order)
	var out []StatusView
	for _, k := range order {
		g := groups[k]
		ids := append([]string(nil), g.ids...)
		sort.Strings(ids)
		out = append(out, StatusView{
			Status:       g.status,
			ProviderTerm: g.term,
			Providers:    sortedKeys(g.providers),
			Notices:      sortedKeys(g.notices),
			AssertionIDs: ids,
		})
	}
	return out
}

// disagree reports whether the providers that answered asserted different
// status sets. A single answering provider cannot disagree with anyone.
func disagree(assertions []Assertion, reports []ProviderReport) bool {
	answered := []string{}
	for _, r := range reports {
		if r.Outcome == OutcomeOK {
			answered = append(answered, r.Provider)
		}
	}
	if len(answered) < 2 {
		return false
	}
	byProvider := map[string]map[string]bool{}
	for _, p := range answered {
		byProvider[p] = map[string]bool{}
	}
	for _, a := range assertions {
		if set, ok := byProvider[a.Provider]; ok {
			key := string(a.Status)
			if !a.Recognized {
				key = "term:" + strings.ToLower(a.ProviderTerm)
			}
			set[key] = true
		}
	}
	var first []string
	for _, p := range answered {
		got := sortedKeys(byProvider[p])
		if first == nil {
			first = got
			continue
		}
		if strings.Join(got, "|") != strings.Join(first, "|") {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
