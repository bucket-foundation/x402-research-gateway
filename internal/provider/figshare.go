package provider

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Figshare adapter (x402-research-gateway#16).
//
// Verified live against api.figshare.com on 2026-08-18 (no key required
// for public search/read; figshare's own docs recommend no more than one
// request per second and warn of throttling on abuse, no numeric ceiling
// published):
//
//	GET https://api.figshare.com/v2/articles?search_for={q}&page={n}&page_size={n}
//	  -> a bare JSON array of article summaries, no envelope:
//	     [{"id":32826848,"doi":"10.6084/m9.figshare.32826848.v7","title":...,...}]
//	GET https://api.figshare.com/v2/articles/{id}
//	  -> the single article record, a superset shape carrying "license":
//	     {"value":6,"name":"GPL 3.0+","url":"https://www.gnu.org/licenses/gpl-3.0.html"}
//	     the search summary omits entirely
//
// Figshare is a general-purpose research output repository (datasets,
// software, posters, preprints, figures); each article states its own
// licence from a controlled vocabulary, read per record rather than
// declared once for the whole provider, on the same precedent GBIF's
// adapter established: two articles routinely carry two different
// licences, and one search summary carries none at all.
type figshareArticle struct {
	ID      int64  `json:"id"`
	DOI     string `json:"doi"`
	Title   string `json:"title"`
	Year    int    `json:"published_date_year"`
	URL     string `json:"url_public_html"`
	License *struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"license"`
}

// FigshareNormalizer handles the search-list shape (a bare array) and the
// single-article fetch shape (the article itself, no envelope) — both raw
// JSON, only their top-level shape differs.
type FigshareNormalizer struct{}

func (FigshareNormalizer) Normalize(body []byte) []NormalizedRecord {
	var list []json.RawMessage
	items := []json.RawMessage{}
	if err := json.Unmarshal(body, &list); err == nil && len(list) > 0 {
		items = list
	} else {
		var probe figshareArticle
		if err := json.Unmarshal(body, &probe); err != nil || probe.ID == 0 {
			return nil
		}
		items = []json.RawMessage{body}
	}

	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var a figshareArticle
		if err := json.Unmarshal(raw, &a); err != nil || a.ID == 0 {
			continue
		}
		id := strconv.FormatInt(a.ID, 10)
		url := "https://doi.org/" + a.DOI
		if a.DOI == "" {
			url = a.URL
		}
		recs = append(recs, NormalizedRecord{ID: id, CanonicalURL: url, Raw: raw})
	}
	return recs
}

// figsharePagePagination implements Searcher: figshare's articles endpoint
// pages via page/page_size, a page-shaped scheme.
type figsharePagePagination struct{}

func (figsharePagePagination) PaginationModel() string { return "page" }

// figshareFetchByID implements Fetcher: a figshare article id.
type figshareFetchByID struct{}

func (figshareFetchByID) IdentifierSchemes() []string { return []string{"figshare", "doi"} }

type figshareIdentity struct{}

func (figshareIdentity) parse(rec NormalizedRecord) (figshareArticle, bool) {
	var a figshareArticle
	if len(rec.Raw) == 0 {
		return a, false
	}
	if err := json.Unmarshal(rec.Raw, &a); err != nil {
		return a, false
	}
	return a, true
}

func (fi figshareIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	a, ok := fi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: a.Title, Year: a.Year}
}

// RecordRights reads the licence figshare carries on this specific article.
// The search-summary shape omits the field entirely, which reports
// unknown rather than a guess; the single-article shape carries it, and a
// recognized CC0 or CC-BY name reports allowed.
func (fi figshareIdentity) RecordRights(rec NormalizedRecord) Rights {
	a, ok := fi.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "figshare (unparseable record)"}
	}
	if a.License == nil || a.License.Name == "" {
		return Rights{Redistribution: RedistributionUnknown, Source: "figshare:license (absent on this record shape)"}
	}
	rights := Rights{
		License:        a.License.Name,
		LicenseURL:     a.License.URL,
		Redistribution: RedistributionUnknown,
		Source:         "figshare:license (per-article)",
		FreeToRead:     true,
	}
	name := strings.ToUpper(a.License.Name)
	// A CC BY-NC name also contains "CC BY", so the noncommercial variant is
	// excluded explicitly rather than falling through to allowed on a
	// substring match, matching GBIF's adapter treating NC clauses as not
	// unconditionally allowed.
	if strings.Contains(name, "CC0") ||
		(strings.Contains(name, "CC BY") && !strings.Contains(name, "NC") && !strings.Contains(name, "ND")) {
		rights.Redistribution = RedistributionAllowed
	}
	return rights
}

// FigshareSearchAdapter backs route ID "figshare-search".
var FigshareSearchAdapter = &Adapter{
	ID:                   "figshare-search",
	Description:          "Figshare research output search: datasets, software, posters, and preprints.",
	Searcher:             figsharePagePagination{},
	Normalizer:           FigshareNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   figshareIdentity{},
	RecordRightsProvider: figshareIdentity{},
}

// FigshareFetchAdapter backs route ID "figshare-fetch".
var FigshareFetchAdapter = &Adapter{
	ID:                   "figshare-fetch",
	Description:          "Figshare single article record by figshare article id.",
	Fetcher:              figshareFetchByID{},
	Normalizer:           FigshareNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   figshareIdentity{},
	RecordRightsProvider: figshareIdentity{},
}
