package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// A publ search response, trimmed from a live 2026-08-17 verification
// query against dblp.org/search/publ/api?q=Perceptrons+Minsky. The second
// hit's authors.author is a bare object, DBLP's shape for a publication
// with exactly one author, the case dblpAuthorList exists to handle.
const dblpPublSearchFixture = `{"result":{"hits":{"@total":"2","hit":[
 {"info":{"authors":{"author":[
    {"@pid":"m/MarvinMinsky","text":"Marvin Minsky"},
    {"@pid":"p/SeymourPapert","text":"Seymour Papert"}
  ]},"title":"Perceptrons: An Introduction to Computational Geometry.",
  "venue":"MIT Press","year":"1969","type":"Books and Theses",
  "key":"books/daglib/0066902","url":"https://dblp.org/rec/books/daglib/0066902"}},
 {"info":{"authors":{"author":{"@pid":"92/731","text":"H. D. Block"}},
  "title":"A Review of \"Perceptrons\" by Marvin Minsky and Seymour Papert",
  "venue":"Inf. Control.","volume":"17","pages":"501-522","year":"1970",
  "type":"Journal Articles","access":"open",
  "key":"journals/iandc/Block70","doi":"10.1016/S0019-9958(70)90409-2",
  "ee":"https://doi.org/10.1016/S0019-9958(70)90409-2",
  "url":"https://dblp.org/rec/journals/iandc/Block70"}}
]}}}`

func TestDBLPPublNormalizer_HandlesSingleAndMultiAuthor(t *testing.T) {
	recs := DBLPPublNormalizer{}.Normalize([]byte(dblpPublSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (a single-author hit must not drop the whole response)", len(recs))
	}
	if recs[0].ID != "books/daglib/0066902" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[1].ID != "journals/iandc/Block70" {
		t.Errorf("record id = %q", recs[1].ID)
	}
}

func TestDBLPPublNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (DBLPPublNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestDBLPPublIdentity_MultiAuthorDescriptor(t *testing.T) {
	recs := DBLPPublNormalizer{}.Normalize([]byte(dblpPublSearchFixture))
	d := dblpPublIdentity{}.Descriptor(recs[0])
	if d.Title != "Perceptrons: An Introduction to Computational Geometry." || d.Year != 1969 {
		t.Errorf("descriptor = %+v", d)
	}
	if len(d.Authors) != 2 || d.Authors[0] != "Marvin Minsky" || d.Authors[1] != "Seymour Papert" {
		t.Errorf("authors = %v", d.Authors)
	}
}

// The single-author hit exercises dblpAuthorList's object-shape branch.
func TestDBLPPublIdentity_SingleAuthorDescriptor(t *testing.T) {
	recs := DBLPPublNormalizer{}.Normalize([]byte(dblpPublSearchFixture))
	d := dblpPublIdentity{}.Descriptor(recs[1])
	if len(d.Authors) != 1 || d.Authors[0] != "H. D. Block" {
		t.Errorf("single-author descriptor authors = %v, want [\"H. D. Block\"]", d.Authors)
	}
}

func TestDBLPPublIdentity_Identifiers(t *testing.T) {
	recs := DBLPPublNormalizer{}.Normalize([]byte(dblpPublSearchFixture))
	ids := dblpPublIdentity{}.Identifiers(recs[1])
	byScheme := map[identity.Scheme]string{}
	for _, id := range ids {
		byScheme[id.Scheme] = id.Value
	}
	if byScheme[identity.SchemeDBLP] != "journals/iandc/Block70" {
		t.Errorf("dblp key = %q", byScheme[identity.SchemeDBLP])
	}
	if byScheme[identity.SchemeDOI] != "10.1016/s0019-9958(70)90409-2" {
		t.Errorf("doi = %q", byScheme[identity.SchemeDOI])
	}

	// A record with no DOI carries only the DBLP key.
	noDOI := dblpPublIdentity{}.Identifiers(recs[0])
	for _, id := range noDOI {
		if id.Scheme == identity.SchemeDOI {
			t.Errorf("books/daglib/0066902 has no doi field, must not carry a DOI identifier: %+v", id)
		}
	}
}

func TestDBLPPublIdentity_Venue(t *testing.T) {
	recs := DBLPPublNormalizer{}.Normalize([]byte(dblpPublSearchFixture))
	v := dblpPublIdentity{}.Venue(recs[1])
	if v.Venue != "Inf. Control." || v.Type != "Journal Articles" || v.Year != 1970 {
		t.Errorf("venue = %+v", v)
	}
}

func TestDBLPPublIdentity_RecordRights(t *testing.T) {
	recs := DBLPPublNormalizer{}.Normalize([]byte(dblpPublSearchFixture))
	r := dblpPublIdentity{}.RecordRights(recs[0])
	if r.Redistribution != RedistributionAllowed {
		t.Errorf("DBLP metadata is CC0, must permit redistribution, got %q", r.Redistribution)
	}
}

// access describes the linked external target, never the DBLP metadata
// record, so it surfaces on Assets and never changes RecordRights.
func TestDBLPPublIdentity_Assets(t *testing.T) {
	recs := DBLPPublNormalizer{}.Normalize([]byte(dblpPublSearchFixture))

	open := dblpPublIdentity{}.Assets(recs[1])
	if len(open) != 1 || open[0].CanonicalURL != "https://doi.org/10.1016/S0019-9958(70)90409-2" {
		t.Fatalf("assets = %+v", open)
	}
	if !open[0].Rights.FreeToRead {
		t.Error("access=open must report free to read")
	}

	// A hit with no ee field yields no assets.
	if got := (dblpPublIdentity{}).Assets(recs[0]); len(got) != 0 {
		t.Errorf("a hit with no ee must yield no assets, got %+v", got)
	}
}

func TestDBLPPublAdapter_CapabilitiesAndSync(t *testing.T) {
	if DBLPPublSearchAdapter.Searcher.PaginationModel() != "page" {
		t.Errorf("pagination model = %q, want page", DBLPPublSearchAdapter.Searcher.PaginationModel())
	}
	sc := DBLPPublSearchAdapter.SyncProvider.SyncCapability()
	if !sc.Bulk {
		t.Error("DBLP publishes a CC0 full-dataset XML dump; bulk must be true")
	}
	if sc.Incremental {
		t.Error("DBLP has no delta feed, only the full rebuild; incremental must be false")
	}
	if !DBLPPublSearchAdapter.Supports(CapSearch) || !DBLPPublSearchAdapter.Supports(CapAssets) {
		t.Error("dblp-publ-search should support search and assets")
	}
}

// ---------- Author search ----------

// An author search response, trimmed from a live 2026-08-17 verification
// query for "geoffrey hinton": two distinct DBLP person ids for two
// people sharing a first-and-last name, DBLP's own disambiguation.
const dblpAuthorSearchFixture = `{"result":{"hits":{"hit":[
 {"info":{"author":"Geoffrey Hinton","url":"https://dblp.org/pid/428/8371"}},
 {"info":{"author":"Geoffrey E. Hinton","url":"https://dblp.org/pid/10/3248",
   "notes":{"note":[
     {"@type":"affiliation","text":"Google DeepMind, London, UK"},
     {"@type":"award","text":"Turing Award"}
   ]}}}
]}}}`

func TestDBLPAuthorNormalizer_OneRecordPerPID(t *testing.T) {
	recs := DBLPAuthorNormalizer{}.Normalize([]byte(dblpAuthorSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (two distinct pids, never merged by name)", len(recs))
	}
	if recs[0].ID != "428/8371" || recs[1].ID != "10/3248" {
		t.Errorf("record ids = %q, %q", recs[0].ID, recs[1].ID)
	}
}

func TestDBLPAuthorNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`)} {
		if recs := (DBLPAuthorNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestDBLPAuthorIdentity_NotesPreserveEachType(t *testing.T) {
	recs := DBLPAuthorNormalizer{}.Normalize([]byte(dblpAuthorSearchFixture))
	notes := dblpAuthorIdentity{}.Notes(recs[1])
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want 2", len(notes))
	}
	var haveAffiliation, haveAward bool
	for _, n := range notes {
		if n.Type == "affiliation" && n.Text == "Google DeepMind, London, UK" {
			haveAffiliation = true
		}
		if n.Type == "award" && n.Text == "Turing Award" {
			haveAward = true
		}
	}
	if !haveAffiliation || !haveAward {
		t.Errorf("missing a note type: affiliation=%v award=%v (%+v)", haveAffiliation, haveAward, notes)
	}

	// The homonym record with no notes yields an empty list rather than invented ones.
	if got := (dblpAuthorIdentity{}).Notes(recs[0]); len(got) != 0 {
		t.Errorf("a record with no notes must yield none, got %+v", got)
	}
}

func TestDBLPAuthorIdentity_Identifiers(t *testing.T) {
	recs := DBLPAuthorNormalizer{}.Normalize([]byte(dblpAuthorSearchFixture))
	ids := dblpAuthorIdentity{}.Identifiers(recs[1])
	if len(ids) != 1 || ids[0].Value != "pid/10/3248" {
		t.Errorf("identifiers = %+v", ids)
	}
}

// ---------- Fetch by key (XML) ----------

// A single-record XML fetch, trimmed from a live 2026-08-17 verification
// query against dblp.org/rec/conf/dac/ZhangYY21.xml.
const dblpRecordXMLFixture = `<?xml version="1.0" encoding="US-ASCII"?>
<dblp>
<inproceedings key="conf/dac/ZhangYY21" mdate="2023-05-03">
<author>Xiaopeng Zhang 0009</author>
<author>Haoyu Yang</author>
<author>Evangeline F. Y. Young</author>
<title>Attentional Transfer is All You Need: Technology-aware Layout Pattern Generation.</title>
<pages>169-174</pages>
<year>2021</year>
<booktitle>DAC</booktitle>
<ee>https://doi.org/10.1109/DAC18074.2021.9586227</ee>
<crossref>conf/dac/2021</crossref>
<url>db/conf/dac/dac2021.html#ZhangYY21</url>
</inproceedings></dblp>`

func TestDBLPRecordNormalizer_ParsesXML(t *testing.T) {
	recs := DBLPRecordNormalizer{}.Normalize([]byte(dblpRecordXMLFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "conf/dac/ZhangYY21" {
		t.Errorf("record id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://dblp.org/rec/conf/dac/ZhangYY21" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}

	var got dblpRecordJSON
	if err := json.Unmarshal(recs[0].Raw, &got); err != nil {
		t.Fatalf("raw is not valid JSON: %v", err)
	}
	if got.Type != "inproceedings" {
		t.Errorf("type = %q, want inproceedings (the XML element name)", got.Type)
	}
	if len(got.Authors) != 3 {
		t.Errorf("authors = %v", got.Authors)
	}
	if got.Venue != "DAC" {
		t.Errorf("venue = %q", got.Venue)
	}
}

func TestDBLPRecordNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not xml`), []byte(`<dblp></dblp>`)} {
		if recs := (DBLPRecordNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestDBLPRecordIdentity_IdentifiersDescriptorRights(t *testing.T) {
	rec := DBLPRecordNormalizer{}.Normalize([]byte(dblpRecordXMLFixture))[0]

	ids := dblpRecordIdentity{}.Identifiers(rec)
	if len(ids) != 1 || ids[0].Scheme != identity.SchemeDBLP || ids[0].Value != "conf/dac/ZhangYY21" {
		t.Errorf("identifiers = %+v", ids)
	}

	d := dblpRecordIdentity{}.Descriptor(rec)
	if d.Year != 2021 || len(d.Authors) != 3 {
		t.Errorf("descriptor = %+v", d)
	}

	r := dblpRecordIdentity{}.RecordRights(rec)
	if r.Redistribution != RedistributionAllowed {
		t.Errorf("rights = %+v, want allowed (CC0 metadata)", r)
	}

	assets := dblpRecordIdentity{}.Assets(rec)
	if len(assets) != 1 || assets[0].CanonicalURL != "https://doi.org/10.1109/DAC18074.2021.9586227" {
		t.Errorf("assets = %+v", assets)
	}
}

func TestDBLPFetchAdapter_Capabilities(t *testing.T) {
	if schemes := DBLPFetchAdapter.Fetcher.IdentifierSchemes(); len(schemes) != 1 || schemes[0] != "dblp" {
		t.Errorf("fetch identifier schemes = %v", schemes)
	}
	if !DBLPFetchAdapter.Supports(CapFetch) {
		t.Error("dblp-fetch should support fetch")
	}
}

func TestDBLP_MalformedRecordHandling(t *testing.T) {
	bad := NormalizedRecord{ID: "x", Raw: json.RawMessage(`not json`)}
	if ids := (dblpPublIdentity{}).Identifiers(bad); len(ids) != 0 {
		t.Errorf("invented identifiers: %+v", ids)
	}
	if rels := (dblpPublIdentity{}).AssertedRelations("n", bad, time.Now()); len(rels) != 0 {
		t.Errorf("invented relations: %+v", rels)
	}
	if assets := (dblpRecordIdentity{}).Assets(bad); len(assets) != 0 {
		t.Errorf("invented assets: %+v", assets)
	}
}
