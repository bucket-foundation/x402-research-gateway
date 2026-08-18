package provider

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// dblpPassthroughCharsetReader accepts a declared charset as already-valid
// UTF-8 rather than transcoding it. DBLP declares US-ASCII, which is a
// strict subset of UTF-8, so passing the bytes through unchanged is
// correct; a declared charset outside that guarantee would need a real
// transcoder, which DBLP's records never require.
func dblpPassthroughCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	return input, nil
}

// DBLP adapters (x402-research-gateway#31).
//
// Verified against dblp.org on 2026-08-17, live queries against
// dblp.org/search/publ/api, dblp.org/search/author/api, and
// dblp.org/rec/{key}.xml:
//
//	GET https://dblp.org/search/publ/api?q=…&format=json&h=&f=
//	GET https://dblp.org/search/author/api?q=…&format=json&h=&f=
//	GET https://dblp.org/rec/{key}.xml
//
// No auth. No published numeric rate limit; DBLP's own dataset-download
// FAQ steers heavy use toward the bulk XML dump rather than the search
// API, so the route carries a generous timeout and the operator is
// expected to cache. Metadata licence is CC0 1.0 per DBLP's FAQ, for both
// the API responses and the bulk dump.
//
// DBLP's author search is a first-class operation, unusual among these
// providers, and its own disambiguation signal: a name that resolves to
// several distinct people returns one hit per DBLP person id (pid), never
// one merged hit. That separation is DBLP's own curated judgment about
// who is who, more trustworthy than anything this adapter could infer,
// and it is preserved by emitting one record per pid rather than
// collapsing same-named hits.
//
// Single-record fetch has no JSON representation; /rec/{key}.json 404s.
// The XML representation at /rec/{key}.xml is what this adapter parses
// for dblp-fetch.

// ---------- Publication search (JSON) ----------

type dblpPublSearchBody struct {
	Result struct {
		Hits struct {
			Total string           `json:"@total"`
			Hit   []dblpPublHitRaw `json:"hit"`
		} `json:"hits"`
	} `json:"result"`
}

type dblpPublHitRaw struct {
	Info struct {
		Authors struct {
			Author dblpAuthorList `json:"author"`
		} `json:"authors"`
		Title  string `json:"title"`
		Venue  string `json:"venue"`
		Pages  string `json:"pages"`
		Year   string `json:"year"`
		Type   string `json:"type"`
		Access string `json:"access"`
		Key    string `json:"key"`
		DOI    string `json:"doi"`
		EE     string `json:"ee"`
		URL    string `json:"url"`
		Volume string `json:"volume"`
	} `json:"info"`
}

// dblpAuthorRef is one author DBLP's search API names on a publication.
type dblpAuthorRef struct {
	PID  string `json:"@pid"`
	Text string `json:"text"`
}

// dblpAuthorList unmarshals DBLP's "authors":{"author": …} field, which
// is a bare object when the publication has exactly one author and an
// array otherwise. encoding/json cannot express "object or array of
// objects" through struct tags alone, so this type implements
// UnmarshalJSON to normalize both shapes to a slice: a hit with one
// author must not fail the whole response's Unmarshal call and silently
// drop every other hit in it along with it.
type dblpAuthorList []dblpAuthorRef

func (l *dblpAuthorList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*l = nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []dblpAuthorRef
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*l = arr
		return nil
	}
	var one dblpAuthorRef
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	*l = []dblpAuthorRef{one}
	return nil
}

// DBLPPublNormalizer parses dblp.org/search/publ/api's JSON response.
type DBLPPublNormalizer struct{}

func (DBLPPublNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b dblpPublSearchBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(b.Result.Hits.Hit))
	for _, hit := range b.Result.Hits.Hit {
		if hit.Info.Key == "" {
			continue
		}
		raw, err := marshalRecord(hit.Info)
		if err != nil {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           hit.Info.Key,
			CanonicalURL: firstNonEmpty(hit.Info.URL, "https://dblp.org/rec/"+hit.Info.Key),
			Raw:          raw,
		})
	}
	return recs
}

type dblpPagePagination struct{}

func (dblpPagePagination) PaginationModel() string { return "page" }

type dblpFetchByKey struct{}

func (dblpFetchByKey) IdentifierSchemes() []string { return []string{"dblp"} }

type dblpPublIdentity struct{}

func (dblpPublIdentity) parse(rec NormalizedRecord) (dblpPublHitRaw, bool) {
	var info struct {
		Authors struct {
			Author dblpAuthorList `json:"author"`
		} `json:"authors"`
		Title  string `json:"title"`
		Venue  string `json:"venue"`
		Pages  string `json:"pages"`
		Year   string `json:"year"`
		Type   string `json:"type"`
		Access string `json:"access"`
		Key    string `json:"key"`
		DOI    string `json:"doi"`
		EE     string `json:"ee"`
		URL    string `json:"url"`
		Volume string `json:"volume"`
	}
	if len(rec.Raw) == 0 {
		return dblpPublHitRaw{}, false
	}
	if err := json.Unmarshal(rec.Raw, &info); err != nil {
		return dblpPublHitRaw{}, false
	}
	var out dblpPublHitRaw
	out.Info = info
	return out, true
}

func (di dblpPublIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	h, ok := di.parse(rec)
	if !ok {
		return nil
	}
	var out []identity.Identifier
	out = appendID(out, identity.SchemeDBLP, h.Info.Key)
	out = appendID(out, identity.SchemeDOI, h.Info.DOI)
	return out
}

// AssertedRelations returns nil: DBLP's publ search response carries no
// cross-record relation this gateway's vocabulary has a term for. Venue
// and author associations are descriptive fields; the identity model has
// no relation type for them.
func (di dblpPublIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

func (di dblpPublIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	h, ok := di.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: h.Info.Title, Year: atoiSafe(h.Info.Year)}
	for _, a := range h.Info.Authors.Author {
		if a.Text != "" {
			d.Authors = append(d.Authors, a.Text)
		}
	}
	return d
}

// VenueInfo names the publication venue with conference and journal
// distinguished, since the distinction carries weight in CS. type is
// DBLP's own publication-type string (e.g. "Conference and Workshop
// Papers", "Journal Articles"), the actual source of the distinction
// rather than a guess inferred from the venue name.
type VenueInfo struct {
	Venue string `json:"venue,omitempty"`
	Type  string `json:"type,omitempty"`
	Year  int    `json:"year,omitempty"`
}

// Venue reports the venue and publication type DBLP published for this
// record.
func (di dblpPublIdentity) Venue(rec NormalizedRecord) VenueInfo {
	h, ok := di.parse(rec)
	if !ok {
		return VenueInfo{}
	}
	return VenueInfo{Venue: h.Info.Venue, Type: h.Info.Type, Year: atoiSafe(h.Info.Year)}
}

// RecordRights reports the CC0 metadata licence DBLP publishes for every
// bibliographic record, per DBLP's dataset FAQ. access ("open"/"closed")
// describes the referenced full text, a separate fact from the DBLP
// metadata record itself; it never changes this answer, and is preserved
// on Assets instead.
func (dblpPublIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "dblp (unparseable record)"}
	}
	return Rights{
		License:        "CC0-1.0",
		LicenseURL:     "https://creativecommons.org/publicdomain/zero/1.0/",
		Redistribution: RedistributionAllowed,
		Source:         "dblp:metadata-license",
		FreeToRead:     true,
	}
}

// Assets reports the external link (ee) DBLP carries for a publication.
// DBLP's own access flag ("open"/"closed") describes that external
// target rather than the DBLP bibliographic record, so it is read here
// instead of being folded into RecordRights.
func (di dblpPublIdentity) Assets(rec NormalizedRecord) []Asset {
	h, ok := di.parse(rec)
	if !ok || h.Info.EE == "" {
		return nil
	}
	rights := Rights{Redistribution: RedistributionUnknown, Source: "dblp:access-flag (describes the linked target rather than stating a licence)"}
	if strings.EqualFold(h.Info.Access, "open") {
		rights.FreeToRead = true
	}
	return []Asset{{
		AssetID:        "dblp:" + h.Info.Key + "#ee",
		Representation: "unspecified; role=external-link",
		CanonicalURL:   h.Info.EE,
		Rights:         rights,
	}}
}

type dblpSync struct{}

// SyncCapability reports bulk true: DBLP publishes its full dataset as
// CC0 XML, rebuilt on every site rebuild, at dblp.org/xml/. Incremental
// is false: there is no delta feed, only the full rebuild.
func (dblpSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: true, Incremental: false}
}

// DBLPPublSearchAdapter backs route ID "dblp-publ-search".
var DBLPPublSearchAdapter = &Adapter{
	ID:                 "dblp-publ-search",
	Description:        "DBLP publication search over the CS bibliography.",
	Searcher:           dblpPagePagination{},
	Normalizer:         DBLPPublNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   dblpPublIdentity{},
	DescriptorProvider: dblpPublIdentity{},
	AssetProvider:      dblpPublIdentity{},
	SyncProvider:       dblpSync{},
}

// ---------- Author search (JSON) ----------

type dblpAuthorSearchBody struct {
	Result struct {
		Hits struct {
			Hit []dblpAuthorHitRaw `json:"hit"`
		} `json:"hits"`
	} `json:"result"`
}

type dblpAuthorHitRaw struct {
	Info struct {
		Author string `json:"author"`
		URL    string `json:"url"`
		Notes  struct {
			Note []struct {
				Type string `json:"@type"`
				Text string `json:"text"`
			} `json:"note"`
		} `json:"notes"`
	} `json:"info"`
}

// DBLPAuthorNormalizer parses dblp.org/search/author/api's JSON response.
// One hit per DBLP person id: two people who share a name are two
// records, DBLP's own disambiguation, never merged by this adapter.
type DBLPAuthorNormalizer struct{}

func (DBLPAuthorNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b dblpAuthorSearchBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(b.Result.Hits.Hit))
	for _, hit := range b.Result.Hits.Hit {
		pid := dblpPIDFromURL(hit.Info.URL)
		if pid == "" {
			continue
		}
		raw, err := marshalRecord(hit.Info)
		if err != nil {
			continue
		}
		recs = append(recs, NormalizedRecord{ID: pid, CanonicalURL: hit.Info.URL, Raw: raw})
	}
	return recs
}

// dblpPIDFromURL extracts the DBLP person id from a person-page URL of
// the form https://dblp.org/pid/{pid}.
func dblpPIDFromURL(u string) string {
	const marker = "/pid/"
	i := strings.Index(u, marker)
	if i < 0 {
		return ""
	}
	return strings.Trim(u[i+len(marker):], "/")
}

type dblpAuthorIdentity struct{}

func (dblpAuthorIdentity) parse(rec NormalizedRecord) (dblpAuthorHitRaw, bool) {
	var info struct {
		Author string `json:"author"`
		URL    string `json:"url"`
		Notes  struct {
			Note []struct {
				Type string `json:"@type"`
				Text string `json:"text"`
			} `json:"note"`
		} `json:"notes"`
	}
	if len(rec.Raw) == 0 {
		return dblpAuthorHitRaw{}, false
	}
	if err := json.Unmarshal(rec.Raw, &info); err != nil {
		return dblpAuthorHitRaw{}, false
	}
	return dblpAuthorHitRaw{Info: info}, true
}

func (ai dblpAuthorIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	if _, ok := ai.parse(rec); !ok {
		return nil
	}
	return appendID(nil, identity.SchemeDBLP, "pid/"+rec.ID)
}

func (ai dblpAuthorIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

// AffiliationNote is one note DBLP's person record carries: an
// affiliation, an award, or another curated annotation. Preserved
// verbatim under its own DBLP-published type rather than reduced to a
// single "affiliation" string, since a Turing Award note and a current
// affiliation are different facts.
type AffiliationNote struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Notes reports every note DBLP published on this person record.
func (ai dblpAuthorIdentity) Notes(rec NormalizedRecord) []AffiliationNote {
	h, ok := ai.parse(rec)
	if !ok {
		return nil
	}
	out := make([]AffiliationNote, 0, len(h.Info.Notes.Note))
	for _, n := range h.Info.Notes.Note {
		out = append(out, AffiliationNote{Type: n.Type, Text: n.Text})
	}
	return out
}

func (ai dblpAuthorIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	h, ok := ai.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: h.Info.Author}
}

func (dblpAuthorIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "dblp (unparseable record)"}
	}
	return Rights{
		License: "CC0-1.0", LicenseURL: "https://creativecommons.org/publicdomain/zero/1.0/",
		Redistribution: RedistributionAllowed, Source: "dblp:metadata-license", FreeToRead: true,
	}
}

// DBLPAuthorSearchAdapter backs route ID "dblp-author-search". Author
// search is a first-class DBLP operation, unusual among these providers.
var DBLPAuthorSearchAdapter = &Adapter{
	ID:                 "dblp-author-search",
	Description:        "DBLP author search, one record per disambiguated DBLP person id.",
	Searcher:           dblpPagePagination{},
	Normalizer:         DBLPAuthorNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   dblpAuthorIdentity{},
	DescriptorProvider: dblpAuthorIdentity{},
	SyncProvider:       dblpSync{},
}

// ---------- Fetch by key (XML) ----------

// dblpRecordXML models the shared fields across DBLP's publication
// element types (inproceedings, article, proceedings, …). DBLP's DTD
// defines the type by the XML element name; that name is kept in Type.
type dblpRecordXML struct {
	XMLName   xml.Name `xml:""`
	Key       string   `xml:"key,attr"`
	Authors   []string `xml:"author"`
	Title     string   `xml:"title"`
	Booktitle string   `xml:"booktitle"`
	Journal   string   `xml:"journal"`
	Pages     string   `xml:"pages"`
	Year      string   `xml:"year"`
	Volume    string   `xml:"volume"`
	EE        []string `xml:"ee"`
	URL       string   `xml:"url"`
	Crossref  string   `xml:"crossref"`
}

type dblpFetchRecordXML struct {
	XMLName xml.Name        `xml:"dblp"`
	Records []dblpRecordXML `xml:",any"`
}

// dblpRecordJSON is the JSON projection stored in NormalizedRecord.Raw,
// since XML cannot be stored in a json.RawMessage.
type dblpRecordJSON struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"`
	Authors  []string `json:"authors,omitempty"`
	Title    string   `json:"title,omitempty"`
	Venue    string   `json:"venue,omitempty"`
	Pages    string   `json:"pages,omitempty"`
	Year     string   `json:"year,omitempty"`
	Volume   string   `json:"volume,omitempty"`
	EE       []string `json:"ee,omitempty"`
	Crossref string   `json:"crossref,omitempty"`
}

// DBLPRecordNormalizer parses the /rec/{key}.xml single-record fetch
// response. DBLP has no JSON representation for a single record.
type DBLPRecordNormalizer struct{}

func (DBLPRecordNormalizer) Normalize(body []byte) []NormalizedRecord {
	var wrap dblpFetchRecordXML
	// DBLP declares US-ASCII, which encoding/xml's decoder refuses without
	// an explicit CharsetReader even though US-ASCII is a strict subset of
	// UTF-8. dblpPassthroughCharsetReader treats any declared charset as
	// already-decoded bytes instead of transcoding them, safe here because
	// US-ASCII needs no transcoding to be read as UTF-8.
	dec := xml.NewDecoder(bytesReader(body))
	dec.CharsetReader = dblpPassthroughCharsetReader
	if err := dec.Decode(&wrap); err != nil || len(wrap.Records) == 0 {
		return nil
	}
	r := wrap.Records[0]
	if r.Key == "" {
		return nil
	}
	j := dblpRecordJSON{
		Key: r.Key, Type: r.XMLName.Local, Authors: r.Authors, Title: r.Title,
		Venue: firstNonEmpty(r.Booktitle, r.Journal), Pages: r.Pages, Year: r.Year,
		Volume: r.Volume, EE: r.EE, Crossref: r.Crossref,
	}
	raw, err := marshalRecord(j)
	if err != nil {
		return nil
	}
	return []NormalizedRecord{{
		ID:           r.Key,
		CanonicalURL: "https://dblp.org/rec/" + r.Key,
		Raw:          raw,
	}}
}

type dblpRecordIdentity struct{}

func (dblpRecordIdentity) parse(rec NormalizedRecord) (dblpRecordJSON, bool) {
	var j dblpRecordJSON
	if len(rec.Raw) == 0 {
		return j, false
	}
	if err := json.Unmarshal(rec.Raw, &j); err != nil {
		return j, false
	}
	return j, true
}

func (ri dblpRecordIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	j, ok := ri.parse(rec)
	if !ok {
		return nil
	}
	return appendID(nil, identity.SchemeDBLP, j.Key)
}

func (ri dblpRecordIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

func (ri dblpRecordIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	j, ok := ri.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: j.Title, Authors: j.Authors, Year: atoiSafe(j.Year)}
}

func (dblpRecordIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "dblp (unparseable record)"}
	}
	return Rights{
		License: "CC0-1.0", LicenseURL: "https://creativecommons.org/publicdomain/zero/1.0/",
		Redistribution: RedistributionAllowed, Source: "dblp:metadata-license", FreeToRead: true,
	}
}

func (ri dblpRecordIdentity) Assets(rec NormalizedRecord) []Asset {
	j, ok := ri.parse(rec)
	if !ok {
		return nil
	}
	out := make([]Asset, 0, len(j.EE))
	for i, ee := range j.EE {
		if ee == "" {
			continue
		}
		out = append(out, Asset{
			AssetID:        "dblp:" + j.Key + "#ee-" + strconv.Itoa(i),
			Representation: "unspecified; role=external-link",
			CanonicalURL:   ee,
			Rights:         Rights{Redistribution: RedistributionUnknown, Source: "dblp:ee (external target, no rights statement)"},
		})
	}
	return out
}

// DBLPFetchAdapter backs route ID "dblp-fetch". DBLP has no JSON
// single-record representation, so this adapter parses XML, the reason
// it needs the adapter layer rather than the declarative proxy.
var DBLPFetchAdapter = &Adapter{
	ID:                 "dblp-fetch",
	Description:        "DBLP single bibliographic record by DBLP key.",
	Fetcher:            dblpFetchByKey{},
	Normalizer:         DBLPRecordNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   dblpRecordIdentity{},
	DescriptorProvider: dblpRecordIdentity{},
	AssetProvider:      dblpRecordIdentity{},
	SyncProvider:       dblpSync{},
}
