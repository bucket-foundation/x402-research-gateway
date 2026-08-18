package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A two-entry Atom response, as export.arxiv.org/api/query returns it.
const arxivFeedFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
  <opensearch:totalResults>2</opensearch:totalResults>
  <opensearch:startIndex>0</opensearch:startIndex>
  <opensearch:itemsPerPage>2</opensearch:itemsPerPage>
  <entry>
    <id>http://arxiv.org/abs/2101.00001v3</id>
    <updated>2021-02-01T00:00:00Z</updated>
    <published>2021-01-01T00:00:00Z</published>
    <title>  A paper about   spacing  </title>
    <summary>An abstract.</summary>
    <author><name>Jane Doe</name></author>
    <author><name>John Roe</name></author>
    <link href="http://arxiv.org/abs/2101.00001v3" rel="alternate" type="text/html"/>
    <link title="pdf" href="http://arxiv.org/pdf/2101.00001v3" rel="related" type="application/pdf"/>
    <arxiv:doi>10.1234/published-version</arxiv:doi>
    <arxiv:journal_ref>J. Example 1, 2021</arxiv:journal_ref>
    <arxiv:license>http://creativecommons.org/licenses/by/4.0/</arxiv:license>
    <arxiv:primary_category term="math.AG" scheme="http://arxiv.org/schemas/atom"/>
    <category term="math.AG" scheme="http://arxiv.org/schemas/atom"/>
    <category term="math.AT" scheme="http://arxiv.org/schemas/atom"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2101.00002</id>
    <updated>2021-03-01T00:00:00Z</updated>
    <published>2021-03-01T00:00:00Z</published>
    <title>No licence, no DOI</title>
    <summary>No rights statement published.</summary>
    <author><name>Only Author</name></author>
  </entry>
</feed>`

func TestArXivNormalizer_ParsesAtomAndKeepsVersion(t *testing.T) {
	recs := ArXivNormalizer{}.Normalize([]byte(arxivFeedFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "2101.00001v3" {
		t.Errorf("record id = %q, want version preserved", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://arxiv.org/abs/2101.00001v3" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}

	var got arxivEntryRecord
	if err := json.Unmarshal(recs[0].Raw, &got); err != nil {
		t.Fatalf("raw is not valid JSON: %v", err)
	}
	if got.Title != "A paper about spacing" {
		t.Errorf("title not whitespace-collapsed: %q", got.Title)
	}
	if len(got.Authors) != 2 || got.Authors[0] != "Jane Doe" {
		t.Errorf("authors = %v", got.Authors)
	}
	if got.Version != "3" {
		t.Errorf("version = %q, want 3", got.Version)
	}
}

func TestArXivNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not xml`), []byte(`<feed></feed>`)} {
		if recs := (ArXivNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestArXivIdentity_IdentifiersAndDescriptor(t *testing.T) {
	recs := ArXivNormalizer{}.Normalize([]byte(arxivFeedFixture))
	rec := recs[0]

	ids := arxivIdentity{}.Identifiers(rec)
	var gotArxiv, gotDOI bool
	for _, id := range ids {
		if id.Scheme == identity.SchemeArXiv && id.Value == "2101.00001" && id.Version == "3" {
			gotArxiv = true
		}
		if id.Scheme == identity.SchemeDOI && id.Value == "10.1234/published-version" {
			gotDOI = true
		}
	}
	if !gotArxiv {
		t.Errorf("missing arxiv identifier: %+v", ids)
	}
	if !gotDOI {
		t.Errorf("missing doi identifier: %+v", ids)
	}

	d := arxivIdentity{}.Descriptor(rec)
	if d.Title != "A paper about spacing" || d.Year != 2021 {
		t.Errorf("descriptor = %+v", d)
	}
	if len(d.Authors) != 2 {
		t.Errorf("authors = %v", d.Authors)
	}

	// A submission with no DOI carries no arxiv identifier collision and no
	// DOI identifier.
	noDOI := recs[1]
	ids2 := arxivIdentity{}.Identifiers(noDOI)
	for _, id := range ids2 {
		if id.Scheme == identity.SchemeDOI {
			t.Errorf("a submission with no arxiv:doi must not carry a DOI identifier, got %+v", id)
		}
	}
}

// arxiv:doi becomes a provider-asserted preprint_of relation, never
// same_work: a preprint and its published article are related, not
// identical.
func TestArXivIdentity_AssertedRelations(t *testing.T) {
	recs := ArXivNormalizer{}.Normalize([]byte(arxivFeedFixture))
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	rels := arxivIdentity{}.AssertedRelations("arxiv-fetch:2101.00001v3", recs[0], at)
	if len(rels) != 1 {
		t.Fatalf("got %d relations, want 1", len(rels))
	}
	r := rels[0]
	if r.Type != identity.RelPreprintOf {
		t.Errorf("relation type = %q, want preprint_of", r.Type)
	}
	if r.To != "doi:10.1234/published-version" {
		t.Errorf("relation target = %q", r.To)
	}
	if r.Evidence.Kind != identity.EvidenceProviderAsserted || r.Evidence.Provider != "arxiv" {
		t.Errorf("evidence = %+v", r.Evidence)
	}

	// A submission with no DOI asserts no relation.
	if rels := (arxivIdentity{}).AssertedRelations("n", recs[1], at); len(rels) != 0 {
		t.Errorf("no doi must assert no relation, got %+v", rels)
	}
}

// Per-submission rights: CC-BY grants redistribution, an absent licence
// reports unknown, and unknown is never treated as permission.
func TestArXivIdentity_RecordRights(t *testing.T) {
	recs := ArXivNormalizer{}.Normalize([]byte(arxivFeedFixture))

	ccBy := arxivIdentity{}.RecordRights(recs[0])
	if ccBy.Redistribution != RedistributionAllowed {
		t.Errorf("CC-BY submission must permit redistribution, got %q", ccBy.Redistribution)
	}
	if !ccBy.FreeToRead {
		t.Error("a submission with a declared licence is free to read")
	}

	noLicense := arxivIdentity{}.RecordRights(recs[1])
	if noLicense.Redistribution != RedistributionUnknown {
		t.Errorf("no declared licence must report unknown, got %q", noLicense.Redistribution)
	}
	if noLicense.Permits() {
		t.Error("unknown must never permit redistribution")
	}
}

func TestArXivIdentity_Assets(t *testing.T) {
	recs := ArXivNormalizer{}.Normalize([]byte(arxivFeedFixture))
	assets := arxivIdentity{}.Assets(recs[0])

	var haveAbs, havePDF, haveSource bool
	for _, a := range assets {
		switch {
		case strings.Contains(a.Representation, "role=abstract"):
			haveAbs = true
			if a.CanonicalURL != "http://arxiv.org/abs/2101.00001v3" {
				t.Errorf("abs url = %q", a.CanonicalURL)
			}
		case a.Representation == "application/pdf":
			havePDF = true
			if a.CanonicalURL != "http://arxiv.org/pdf/2101.00001v3" {
				t.Errorf("pdf url = %q", a.CanonicalURL)
			}
		case strings.Contains(a.Representation, "role=source"):
			haveSource = true
		}
		// Every representation of a per-submission-licensed record carries
		// that record's own rights instead of a provider default.
		if a.Rights.Redistribution != RedistributionAllowed {
			t.Errorf("asset %q rights = %+v, want allowed (CC-BY submission)", a.AssetID, a.Rights)
		}
	}
	if !haveAbs || !havePDF || !haveSource {
		t.Errorf("missing an expected representation: abs=%v pdf=%v source=%v", haveAbs, havePDF, haveSource)
	}
}

func TestArXivAdapters_CapabilitiesAndSync(t *testing.T) {
	if ArXivSearchAdapter.Searcher.PaginationModel() != "offset" {
		t.Errorf("pagination model = %q, want offset", ArXivSearchAdapter.Searcher.PaginationModel())
	}
	if schemes := ArXivFetchAdapter.Fetcher.IdentifierSchemes(); len(schemes) != 1 || schemes[0] != "arxiv" {
		t.Errorf("fetch identifier schemes = %v", schemes)
	}
	sc := ArXivSearchAdapter.SyncProvider.SyncCapability()
	if sc.Bulk {
		t.Error("full-text bulk sets are requester-pays; bulk must be reported false")
	}
	if !sc.Incremental {
		t.Error("OAI-PMH incremental harvest is open, so incremental must be true")
	}
	if !ArXivSearchAdapter.Supports(CapSearch) || !ArXivSearchAdapter.Supports(CapAssets) {
		t.Error("arxiv-search should support search and assets")
	}
	if !ArXivFetchAdapter.Supports(CapFetch) {
		t.Error("arxiv-fetch should support fetch")
	}
}

func TestArXivIdentity_MalformedRecord(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (arxivIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if rels := (arxivIdentity{}).AssertedRelations("n", bad, time.Now()); len(rels) != 0 {
		t.Errorf("invented relations: %+v", rels)
	}
	if assets := (arxivIdentity{}).Assets(bad); len(assets) != 0 {
		t.Errorf("invented assets: %+v", assets)
	}
	empty := NormalizedRecord{}
	if d := (arxivIdentity{}).Descriptor(empty); d.Title != "" {
		t.Errorf("descriptor on empty record: %+v", d)
	}
	r := (arxivIdentity{}).RecordRights(bad)
	if r.Redistribution != RedistributionUnknown {
		t.Errorf("unparseable record must report unknown rights, got %q", r.Redistribution)
	}
}
