package provider

import (
	"encoding/json"
	"regexp"
	"strconv"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/relation"
)

// OEIS adapter (x402-research-gateway#17).
//
// Verified live against oeis.org on 2026-08-18:
//
//	GET https://oeis.org/search?q={query}&fmt=json
//	  -> a bare JSON array of sequence objects, no envelope
//	GET https://oeis.org/A{number}?fmt=json (equivalently q=id:A{number})
//	  -> the same shape, one element
//
// No API key. oeis.org sits behind Cloudflare bot-mitigation that 403s a
// request with no User-Agent header; a route operator must set one (this
// adapter does not send HTTP itself, per this package's staging note in
// provider.go, so the route config carries the header). No numeric rate
// limit is published; OEIS's own tools (e.g. the SuperSeeker system)
// recommend a polite delay between automated queries.
//
// An OEIS sequence is a source object, not a paper: it has terms, an
// offset, formulae, cross-references, and authorship, and this adapter
// never reshapes it into a citation-shaped record. Raw preserves every
// field OEIS published (comment, formula, xref, program, link, ...)
// unflattened.
//
// Licence: CC BY-SA 4.0, per the OEIS End-User License Agreement
// (oeis.org/wiki/The_OEIS_End-User_License_Agreement, read 2026-08-18),
// which requires attribution and share-alike redistribution — narrower
// than a plain CC-BY/CC0 source, and recorded as such rather than
// simplified to "open."
type oeisRecord struct {
	Number  int      `json:"number"`
	Data    string   `json:"data"`
	Name    string   `json:"name"`
	Offset  string   `json:"offset"`
	Author  string   `json:"author"`
	Keyword string   `json:"keyword"`
	Xref    []string `json:"xref"`
}

// oeisANumberRe matches an OEIS A-number, e.g. "A000045", inside a free-text
// xref comment line. OEIS's xref field is prose ("Cf. A001622 (phi),
// A039834 (signed Fibonacci numbers), ..."), not a structured list, so an
// A-number is extracted by pattern rather than parsed as a delimited field.
var oeisANumberRe = regexp.MustCompile(`\bA(\d{6})\b`)

// OEISNormalizer parses oeis.org/search's bare JSON array (or the
// single-element array a fetch-by-id query returns; there is no separate
// single-record shape).
type OEISNormalizer struct{}

func (OEISNormalizer) Normalize(body []byte) []NormalizedRecord {
	var items []oeisRecord
	if err := json.Unmarshal(body, &items); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(items))
	for _, r := range items {
		if r.Number == 0 {
			continue
		}
		raw, err := marshalRecord(r)
		if err != nil {
			continue
		}
		id := oeisID(r.Number)
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://oeis.org/" + id,
			Raw:          raw,
		})
	}
	return recs
}

func oeisID(number int) string {
	s := "000000" + strconv.Itoa(number)
	return "A" + s[len(s)-6:]
}

// oeisPagePagination implements Searcher: OEIS's search endpoint pages via
// a start offset, which the feed402 vocabulary reports as "offset" — but
// OEIS's own parameter is a raw index into the result stream rather than a
// page number, so PaginationModel reports "offset" here, distinct from the
// "page" providers above that take a page number.
type oeisPagePagination struct{}

func (oeisPagePagination) PaginationModel() string { return "offset" }

// oeisFetchByID implements Fetcher: an OEIS A-number.
type oeisFetchByID struct{}

func (oeisFetchByID) IdentifierSchemes() []string { return []string{"oeis"} }

type oeisIdentity struct{}

func (oeisIdentity) parse(rec NormalizedRecord) (oeisRecord, bool) {
	var r oeisRecord
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (oi oeisIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := oi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: r.Name}
	if r.Author != "" {
		d.Authors = []string{r.Author}
	}
	return d
}

// RecordRights reports OEIS's site-wide CC BY-SA 4.0 licence, which every
// sequence carries identically (OEIS publishes one licence for the whole
// database, unlike a per-record aggregator).
func (oeisIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "oeis (unparseable record)"}
	}
	return Rights{
		License:        "CC-BY-SA-4.0",
		LicenseURL:     "https://creativecommons.org/licenses/by-sa/4.0/",
		Redistribution: RedistributionAllowed,
		Source:         "oeis:eula (oeis.org/wiki/The_OEIS_End-User_License_Agreement)",
		FreeToRead:     true,
	}
}

// ObjectRelations reports every A-number this sequence's xref field
// mentions, as an untyped "xref" relation between two OEIS sequence
// objects. OEIS's xref is prose commentary, not a typed relation vocabulary
// (unlike DataCite's relationType), so the predicate is the literal term
// "xref" and Predicate.Recognized is false for every edge: this gateway has
// no finer-grained term for what OEIS itself does not distinguish
// (a "see also" is not a "generalizes" is not a "same sequence as", and
// OEIS's own xref field does not separate them either).
func (oi oeisIdentity) ObjectRelations(rec NormalizedRecord, at time.Time) []relation.Relation {
	r, ok := oi.parse(rec)
	if !ok || r.Number == 0 {
		return nil
	}
	subjectID := oeisID(r.Number)
	subject := relation.NewObject("oeis-sequence", subjectID)
	subject.CanonicalURL = "https://oeis.org/" + subjectID

	seen := map[string]bool{subjectID: true}
	var out []relation.Relation
	for _, line := range r.Xref {
		for _, m := range oeisANumberRe.FindAllString(line, -1) {
			if seen[m] {
				continue
			}
			seen[m] = true
			object := relation.NewObject("oeis-sequence", m)
			object.CanonicalURL = "https://oeis.org/" + m
			out = append(out, relation.New("oeis", "oeis:xref", subject, "xref", object, at))
		}
	}
	return out
}

// OEISSearchAdapter backs route ID "oeis-search".
var OEISSearchAdapter = &Adapter{
	ID:                     "oeis-search",
	Description:            "OEIS (Online Encyclopedia of Integer Sequences) search.",
	Searcher:               oeisPagePagination{},
	Normalizer:             OEISNormalizer{},
	CitationProvider:       GenericCitationProvider{},
	DescriptorProvider:     oeisIdentity{},
	RecordRightsProvider:   oeisIdentity{},
	ObjectRelationProvider: oeisIdentity{},
}

// OEISFetchAdapter backs route ID "oeis-fetch".
var OEISFetchAdapter = &Adapter{
	ID:                     "oeis-fetch",
	Description:            "OEIS single sequence by A-number.",
	Fetcher:                oeisFetchByID{},
	Normalizer:             OEISNormalizer{},
	CitationProvider:       GenericCitationProvider{},
	DescriptorProvider:     oeisIdentity{},
	RecordRightsProvider:   oeisIdentity{},
	ObjectRelationProvider: oeisIdentity{},
}
