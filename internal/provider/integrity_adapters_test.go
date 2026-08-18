package provider

import (
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/integrity"
)

var integAt = time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)

// crossrefRetractedFixture is a retracted work as Crossref serves it: the
// Crossmark `updated-by` block naming the retraction notice and its date.
const crossrefRetractedFixture = `{"message": {
  "DOI": "10.1016/j.example.2019.01.001",
  "URL": "https://doi.org/10.1016/j.example.2019.01.001",
  "type": "journal-article",
  "title": ["A result that did not hold"],
  "update-policy": "https://doi.org/10.1016/elsevier_cm_policy",
  "updated-by": [
    {"DOI": "10.1016/j.example.2021.06.010", "type": "retraction",
     "label": "Retraction", "updated": {"date-parts": [[2021, 6, 15]]}}
  ]
}}`

func crossrefIntegrity(t *testing.T, body string) []integrity.Assertion {
	t.Helper()
	recs := CrossrefWorksNormalizer{}.Normalize([]byte(body))
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	return (crossrefIdentity{}).IntegrityAssertions(recs[0], integAt)
}

func TestCrossrefIntegrity_RetractedWork(t *testing.T) {
	got := crossrefIntegrity(t, crossrefRetractedFixture)
	if len(got) != 1 {
		t.Fatalf("want 1 assertion, got %d", len(got))
	}
	a := got[0]
	if a.Status != integrity.StatusRetraction || !a.Recognized {
		t.Fatalf("status = %+v", a)
	}
	if a.Work.Key() != "doi:10.1016/j.example.2019.01.001" {
		t.Fatalf("work = %q", a.Work.Key())
	}
	if a.NoticeID != "10.1016/j.example.2021.06.010" {
		t.Fatalf("notice = %q", a.NoticeID)
	}
	if a.Date != "2021-06-15" {
		t.Fatalf("date = %q", a.Date)
	}
	if a.Provider != "crossref" || a.RetrievedAt != "2026-08-17T10:00:00Z" {
		t.Fatalf("attribution = %q %q", a.Provider, a.RetrievedAt)
	}
	if a.Annotations["label"] != "Retraction" {
		t.Fatalf("annotations = %v", a.Annotations)
	}
	if a.Annotations["update-policy"] == "" {
		t.Fatal("the publisher update policy was dropped")
	}
}

// A notice record names the work it retracts through `update-to`. The
// assertion is about that work, and the notice is the queried record.
const crossrefNoticeFixture = `{"message": {
  "DOI": "10.1016/j.example.2021.06.010",
  "type": "journal-article",
  "title": ["Retraction notice"],
  "update-to": [
    {"DOI": "10.1016/j.example.2019.01.001", "type": "retraction",
     "updated": {"date-parts": [[2021, 6, 15]]}}
  ]
}}`

func TestCrossrefIntegrity_NoticeRecordNamesTheAffectedWork(t *testing.T) {
	got := crossrefIntegrity(t, crossrefNoticeFixture)
	if len(got) != 1 {
		t.Fatalf("want 1 assertion, got %d", len(got))
	}
	if got[0].Work.Key() != "doi:10.1016/j.example.2019.01.001" {
		t.Fatalf("affected work = %q", got[0].Work.Key())
	}
	if got[0].NoticeID != "10.1016/j.example.2021.06.010" {
		t.Fatalf("notice = %q", got[0].NoticeID)
	}
	if got[0].Annotations["notice_is_queried_record"] != "true" {
		t.Fatalf("annotations = %v", got[0].Annotations)
	}
}

// europePMCConcernFixture carries an expression of concern and a type this
// gateway has no status for, which must survive unrecognized.
const europePMCConcernFixture = `{"resultList": {"result": [
  {"id": "31234567", "source": "MED", "pmid": "31234567",
   "doi": "10.1016/j.example.2019.01.001", "title": "A result that did not hold",
   "commentCorrectionList": {"commentCorrection": [
     {"id": "33000001", "type": "Expression of concern in"},
     {"id": "33000002", "type": "Republished in"}
   ]}}
]}}`

func epmcIntegrity(t *testing.T) []integrity.Assertion {
	t.Helper()
	recs := EuropePMCNormalizer{}.Normalize([]byte(europePMCConcernFixture))
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	return (epmcIdentity{}).IntegrityAssertions(recs[0], integAt)
}

func TestEuropePMCIntegrity_ConcernAndUnknownTerm(t *testing.T) {
	got := epmcIntegrity(t)
	if len(got) != 2 {
		t.Fatalf("want 2 assertions, got %d", len(got))
	}
	var concern, unknown *integrity.Assertion
	for i := range got {
		switch got[i].ProviderTerm {
		case "Expression of concern in":
			concern = &got[i]
		case "Republished in":
			unknown = &got[i]
		}
	}
	if concern == nil || concern.Status != integrity.StatusExpressionOfConcern {
		t.Fatalf("expression of concern missing or mistyped: %+v", concern)
	}
	if concern.NoticeID != "33000001" {
		t.Fatalf("notice = %q", concern.NoticeID)
	}
	if unknown == nil {
		t.Fatal("a term the gateway does not recognize was dropped")
	}
	if unknown.Recognized || unknown.Status != "" {
		t.Fatalf("unrecognized term got a status: %+v", unknown)
	}
}

// Two providers over one work, one asserting a retraction the other has not
// ingested. Both survive and the disagreement is flagged.
func TestIntegrityProvidersDisagree(t *testing.T) {
	assertions := append(crossrefIntegrity(t, crossrefRetractedFixture), epmcIntegrity(t)...)
	ids := (crossrefIdentity{}).Identifiers(
		CrossrefWorksNormalizer{}.Normalize([]byte(crossrefRetractedFixture))[0])
	set := integrity.Build(ids[0], assertions, []integrity.ProviderReport{
		{Provider: "crossref", Consulted: true, Outcome: integrity.OutcomeOK, AssertionCount: 1},
		{Provider: "europepmc", Consulted: true, Outcome: integrity.OutcomeOK, AssertionCount: 2},
	}, integAt)

	if len(set.Assertions) != 3 {
		t.Fatalf("want 3 assertions across both providers, got %d", len(set.Assertions))
	}
	if !set.ProvidersDisagree {
		t.Fatal("Crossref carrying a retraction Europe PMC does not was not flagged")
	}
	if set.AbsenceNotice == "" {
		t.Fatal("absence notice missing")
	}
}

// dataciteVersionFixture carries the repository-side version history.
const dataciteVersionFixture = `{"data": {
  "id": "10.5281/zenodo.100",
  "attributes": {
    "doi": "10.5281/zenodo.100",
    "types": {"resourceTypeGeneral": "Dataset"},
    "relatedIdentifiers": [
      {"relatedIdentifier": "10.5281/zenodo.99", "relatedIdentifierType": "DOI",
       "relationType": "IsNewVersionOf", "resourceTypeGeneral": "Dataset"},
      {"relatedIdentifier": "10.5281/zenodo.101", "relatedIdentifierType": "DOI",
       "relationType": "IsObsoletedBy", "resourceTypeGeneral": "Dataset"},
      {"relatedIdentifier": "10.1038/article", "relatedIdentifierType": "DOI",
       "relationType": "IsSupplementTo", "resourceTypeGeneral": "JournalArticle"}
    ]
  }
}}`

func TestDataCiteIntegrity_VersionHistoryOnly(t *testing.T) {
	recs := DataCiteNormalizer{}.Normalize([]byte(dataciteVersionFixture))
	got := (dataciteIdentity{}).IntegrityAssertions(recs[0], integAt)
	if len(got) != 2 {
		t.Fatalf("want 2 integrity assertions (version + obsoletion), got %d", len(got))
	}
	statuses := map[integrity.Status]bool{}
	for _, a := range got {
		statuses[a.Status] = true
		if a.NoticeID == "" {
			t.Fatalf("%s: no related identifier carried", a.ProviderTerm)
		}
	}
	if !statuses[integrity.StatusNewVersion] || !statuses[integrity.StatusWithdrawal] {
		t.Fatalf("statuses = %v", statuses)
	}
	// IsSupplementTo is a relation, not an integrity notice. It belongs to
	// the relation graph and must not appear here.
	for _, a := range got {
		if a.ProviderTerm == "IsSupplementTo" {
			t.Fatal("a non-integrity relation leaked into the integrity assertions")
		}
	}
}

// arxivV3Fixture is a submission arXiv currently serves at v3. arXiv itself
// asserts that v1 and v2 of the same base identifier are superseded.
const arxivV3Fixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/2101.00001v3</id>
    <updated>2021-03-01T00:00:00Z</updated>
    <published>2021-01-01T00:00:00Z</published>
    <title>A result revised twice</title>
    <summary>abstract</summary>
  </entry>
</feed>`

func TestArXivIntegrity_VersionSupersession(t *testing.T) {
	recs := ArXivNormalizer{}.Normalize([]byte(arxivV3Fixture))
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	got := (arxivIdentity{}).IntegrityAssertions(recs[0], integAt)
	if len(got) != 1 {
		t.Fatalf("want 1 assertion, got %d", len(got))
	}
	a := got[0]
	if a.Status != integrity.StatusNewVersion || !a.Recognized {
		t.Fatalf("status = %+v", a)
	}
	if a.NoticeID != "2101.00001v3" {
		t.Fatalf("notice = %q", a.NoticeID)
	}
	if a.Work.Key() != "arxiv:2101.00001" {
		t.Fatalf("affected work = %q", a.Work.Key())
	}
	if a.Annotations["current_version"] != "3" {
		t.Fatalf("annotations = %v", a.Annotations)
	}
}

// arxivV1Fixture is a submission at its first version: nothing to supersede.
const arxivV1Fixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/2101.00002v1</id>
    <updated>2021-01-01T00:00:00Z</updated>
    <published>2021-01-01T00:00:00Z</published>
    <title>A first submission</title>
    <summary>abstract</summary>
  </entry>
</feed>`

func TestArXivIntegrity_FirstVersionAssertsNothing(t *testing.T) {
	recs := ArXivNormalizer{}.Normalize([]byte(arxivV1Fixture))
	if got := (arxivIdentity{}).IntegrityAssertions(recs[0], integAt); got != nil {
		t.Fatalf("v1 should assert nothing superseded, got %v", got)
	}
}

func TestIntegrityAssertions_NeverPanicOnUnknownBody(t *testing.T) {
	junk := NormalizedRecord{ID: "x", Raw: []byte(`{"nope":true}`)}
	empty := NormalizedRecord{ID: "x"}
	for _, rec := range []NormalizedRecord{junk, empty} {
		if got := (crossrefIdentity{}).IntegrityAssertions(rec, integAt); got != nil {
			t.Fatalf("crossref returned %v", got)
		}
		if got := (epmcIdentity{}).IntegrityAssertions(rec, integAt); got != nil {
			t.Fatalf("europepmc returned %v", got)
		}
		if got := (dataciteIdentity{}).IntegrityAssertions(rec, integAt); got != nil {
			t.Fatalf("datacite returned %v", got)
		}
		if got := (arxivIdentity{}).IntegrityAssertions(rec, integAt); got != nil {
			t.Fatalf("arxiv returned %v", got)
		}
	}
}

func TestIntegrityCapabilityReported(t *testing.T) {
	for _, a := range []*Adapter{CrossrefSearchAdapter, CrossrefFetchAdapter,
		EuropePMCSearchAdapter, EuropePMCFetchAdapter, DataCiteSearchAdapter, DataCiteFetchAdapter,
		ArXivSearchAdapter, ArXivFetchAdapter} {
		if !a.Supports(CapIntegrity) {
			t.Fatalf("%s does not report the integrity capability", a.ID)
		}
	}
	if PubMedSearchAdapter.Supports(CapIntegrity) {
		t.Fatal("pubmed-search claims an integrity capability it does not implement")
	}
}
