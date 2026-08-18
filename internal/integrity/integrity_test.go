package integrity

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

var at = time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)

func query(t *testing.T) identity.Identifier {
	t.Helper()
	id, ok := identity.Parse("10.1038/s41586-024-00001-1")
	if !ok {
		t.Fatal("fixture identifier did not parse")
	}
	return id
}

func work() Endpoint { return NewEndpoint("10.1038/s41586-024-00001-1") }

func TestNew_CarriesProviderTermAndAttribution(t *testing.T) {
	a := New("crossref", "crossref:updated-by", work(), "retraction", at).
		WithNotice("10.1038/s41586-024-09999-9")
	if a.Status != StatusRetraction || !a.Recognized {
		t.Fatalf("status = %+v", a)
	}
	if a.ProviderTerm != "retraction" {
		t.Fatalf("provider term = %q", a.ProviderTerm)
	}
	if a.NoticeID != "10.1038/s41586-024-09999-9" {
		t.Fatalf("notice id = %q", a.NoticeID)
	}
	if a.Notice.Key() != "doi:10.1038/s41586-024-09999-9" {
		t.Fatalf("notice key = %q", a.Notice.Key())
	}
	if a.RetrievedAt != "2026-08-17T10:00:00Z" || a.Provider != "crossref" {
		t.Fatalf("attribution = %q %q", a.Provider, a.RetrievedAt)
	}
	if !a.Valid() {
		t.Fatal("a complete assertion reported invalid")
	}
}

func TestNew_UnknownTermIsKeptUnrecognized(t *testing.T) {
	a := New("europepmc", "f", work(), "Republished in", at)
	if a.Recognized || a.Status != "" {
		t.Fatalf("an unmapped term was given a status: %+v", a)
	}
	if a.ProviderTerm != "Republished in" {
		t.Fatalf("provider term lost: %q", a.ProviderTerm)
	}
	set := Build(query(t), []Assertion{a}, []ProviderReport{
		{Provider: "europepmc", Consulted: true, Outcome: OutcomeOK, AssertionCount: 1},
	}, at)
	if len(set.Assertions) != 1 {
		t.Fatal("an unrecognized notice was dropped")
	}
	if len(set.StatusSummary) != 1 || set.StatusSummary[0].ProviderTerm != "Republished in" {
		t.Fatalf("summary = %+v", set.StatusSummary)
	}
}

func TestBuild_DisagreementCoexistsWithoutAdjudication(t *testing.T) {
	// Crossref carries a retraction; Europe PMC has not ingested it and
	// reports only a correction. Both survive.
	crossref := New("crossref", "crossref:updated-by", work(), "retraction", at).
		WithNotice("10.1038/retraction")
	epmc := New("europepmc", "europepmc:commentCorrectionList", work(), "Erratum in", at).
		WithNotice("pmid:29999999")

	set := Build(query(t), []Assertion{crossref, epmc}, []ProviderReport{
		{Provider: "crossref", Consulted: true, Outcome: OutcomeOK, AssertionCount: 1},
		{Provider: "europepmc", Consulted: true, Outcome: OutcomeOK, AssertionCount: 1},
	}, at)

	if len(set.Assertions) != 2 {
		t.Fatalf("want both assertions retained, got %d", len(set.Assertions))
	}
	if !set.ProvidersDisagree {
		t.Fatal("a retraction from one provider and an erratum from the other was not flagged as disagreement")
	}
	statuses := map[Status][]string{}
	for _, v := range set.StatusSummary {
		statuses[v.Status] = v.Providers
	}
	if got := statuses[StatusRetraction]; len(got) != 1 || got[0] != "crossref" {
		t.Fatalf("retraction providers = %v", got)
	}
	if got := statuses[StatusErratum]; len(got) != 1 || got[0] != "europepmc" {
		t.Fatalf("erratum providers = %v", got)
	}
	// No field anywhere collapses the two into one answer.
	blob, _ := json.Marshal(set)
	var generic map[string]any
	_ = json.Unmarshal(blob, &generic)
	for _, banned := range []string{"status", "current_status", "resolved_status", "is_retracted"} {
		if _, present := generic[banned]; present {
			t.Fatalf("result carries a flattened %q field", banned)
		}
	}
}

func TestBuild_AgreementIsNotDisagreement(t *testing.T) {
	set := Build(query(t), []Assertion{
		New("crossref", "f", work(), "retraction", at).WithNotice("10.1/r"),
		New("europepmc", "g", work(), "Retraction in", at).WithNotice("pmid:1"),
	}, []ProviderReport{
		{Provider: "crossref", Consulted: true, Outcome: OutcomeOK, AssertionCount: 1},
		{Provider: "europepmc", Consulted: true, Outcome: OutcomeOK, AssertionCount: 1},
	}, at)
	if set.ProvidersDisagree {
		t.Fatal("two providers asserting a retraction were flagged as disagreeing")
	}
	if len(set.StatusSummary) != 1 || len(set.StatusSummary[0].Providers) != 2 {
		t.Fatalf("summary = %+v", set.StatusSummary)
	}
	if len(set.StatusSummary[0].Notices) != 2 {
		t.Fatalf("both notice identifiers must survive: %v", set.StatusSummary[0].Notices)
	}
}

func TestBuild_AbsenceIsNotClearance(t *testing.T) {
	set := Build(query(t), nil, []ProviderReport{
		{Provider: "crossref", Consulted: true, Outcome: OutcomeOK},
		{Provider: "datacite", Consulted: false, Outcome: OutcomeNotConfigured},
	}, at)
	if len(set.Assertions) != 0 {
		t.Fatal("assertions invented from nothing")
	}
	if len(set.ProvidersConsulted) != 2 {
		t.Fatalf("providers dropped: %+v", set.ProvidersConsulted)
	}
	if set.AbsenceNotice == "" {
		t.Fatal("absence notice missing")
	}
	if set.ProvidersDisagree {
		t.Fatal("a single answering provider cannot disagree with anyone")
	}
}

func TestBuild_DeterministicAndDedupedPerProvider(t *testing.T) {
	a := New("crossref", "f", work(), "retraction", at).WithNotice("10.1/r")
	b := New("europepmc", "g", work(), "Retraction in", at).WithNotice("pmid:1")
	reports := []ProviderReport{
		{Provider: "crossref", Consulted: true, Outcome: OutcomeOK},
		{Provider: "europepmc", Consulted: true, Outcome: OutcomeOK},
	}
	one, _ := json.Marshal(Build(query(t), []Assertion{a, b, a}, reports, at))
	two, _ := json.Marshal(Build(query(t), []Assertion{b, a, a}, reports, at))
	if string(one) != string(two) {
		t.Fatal("Build is not order-independent")
	}
	var set Result
	_ = json.Unmarshal(one, &set)
	if len(set.Assertions) != 2 {
		t.Fatalf("duplicate from one provider survived: %d assertions", len(set.Assertions))
	}
}

func TestAssertion_InvalidWithoutAttribution(t *testing.T) {
	base := New("crossref", "f", work(), "retraction", at)
	noProvider := base
	noProvider.Provider = ""
	noTerm := base
	noTerm.ProviderTerm = ""
	noWork := New("crossref", "f", NewEndpoint(""), "retraction", at)
	for name, a := range map[string]Assertion{
		"no provider": noProvider, "no term": noTerm, "no work": noWork,
	} {
		if a.Valid() {
			t.Fatalf("%s: reported valid", name)
		}
	}
}
