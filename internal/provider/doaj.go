package provider

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// DOAJ adapter (x402-research-gateway#13, Wave 2).
//
// Verified live against doaj.org on 2026-08-18:
//
//	GET https://doaj.org/api/search/articles/{query}?page=&pageSize=
//	  -> {"total":N,"page":N,"pageSize":N,"results":[{"id":..., "bibjson":{...}}]}
//	GET https://doaj.org/api/articles/{id}
//	  -> {"id":..., "bibjson":{...}}, the single-record shape, no "results" envelope
//
// No auth for search or single-article fetch; an API key exists but per
// DOAJ's own docs (doaj.org/api/docs) it is issued to publishers submitting
// data, not to readers, and this adapter never sends one. DOAJ states a rate
// limit of 2 requests/second with small bursts queued.
//
// DOAJ's own metadata is CC0, an explicit fact rather than an assumption:
// its FAQ (doaj.org/docs/faq/, read 2026-08-18) states "we choose to waive
// all rights under a CC0 waiver" for the bibliographic records it publishes,
// and that this applies to DOAJ's own processed metadata rather than to the
// copyright of the underlying articles. Article full-text access is a
// separate fact carried per record on bibjson.link and is never inferred
// from journal-level openness.
type doajBibjson struct {
	Title      string `json:"title"`
	Year       string `json:"year"`
	Month      string `json:"month"`
	Abstract   string `json:"abstract"`
	Identifier []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"identifier"`
	Author []struct {
		Name string `json:"name"`
	} `json:"author"`
	Link []struct {
		Type        string `json:"type"`
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	} `json:"link"`
	Journal struct {
		Title     string   `json:"title"`
		Publisher string   `json:"publisher"`
		Language  []string `json:"language"`
	} `json:"journal"`
}

type doajArticle struct {
	ID      string      `json:"id"`
	Bibjson doajBibjson `json:"bibjson"`
}

type doajSearchBody struct {
	Total   int           `json:"total"`
	Results []doajArticle `json:"results"`
}

// DOAJNormalizer handles both the search-list shape (results: [...]) and the
// single-article fetch shape (the article itself, no envelope).
type DOAJNormalizer struct{}

func (DOAJNormalizer) Normalize(body []byte) []NormalizedRecord {
	var search doajSearchBody
	var items []doajArticle
	if err := json.Unmarshal(body, &search); err == nil && len(search.Results) > 0 {
		items = search.Results
	} else {
		var single doajArticle
		if err := json.Unmarshal(body, &single); err != nil || single.ID == "" {
			return nil
		}
		items = []doajArticle{single}
	}

	recs := make([]NormalizedRecord, 0, len(items))
	for _, a := range items {
		if a.ID == "" {
			continue
		}
		raw, err := marshalRecord(a)
		if err != nil {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           a.ID,
			CanonicalURL: "https://doaj.org/article/" + a.ID,
			Raw:          raw,
		})
	}
	return recs
}

// doajPagePagination implements Searcher: DOAJ's search endpoint pages via
// page/pageSize, an offset-shaped scheme.
type doajPagePagination struct{}

func (doajPagePagination) PaginationModel() string { return "page" }

// doajFetchByID implements Fetcher: a DOAJ article id.
type doajFetchByID struct{}

func (doajFetchByID) IdentifierSchemes() []string { return []string{"doaj-id", "doi"} }

type doajIdentity struct{}

func (doajIdentity) parse(rec NormalizedRecord) (doajArticle, bool) {
	var a doajArticle
	if len(rec.Raw) == 0 {
		return a, false
	}
	if err := json.Unmarshal(rec.Raw, &a); err != nil {
		return a, false
	}
	return a, true
}

func (di doajIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	a, ok := di.parse(rec)
	if !ok {
		return nil
	}
	out := appendID(nil, identity.SchemeDOAJ, a.ID)
	for _, id := range a.Bibjson.Identifier {
		if strings.EqualFold(id.Type, "doi") && id.ID != "" {
			out = appendID(out, identity.SchemeDOI, id.ID)
		}
	}
	return out
}

// AssertedRelations returns nil: DOAJ's article record carries no
// cross-record relation this gateway's vocabulary has a term for. The
// journal a record belongs to is a descriptive field, not a typed relation.
func (di doajIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

func (di doajIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	a, ok := di.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: a.Bibjson.Title, Year: atoiSafe(a.Bibjson.Year)}
	for _, au := range a.Bibjson.Author {
		if au.Name != "" {
			d.Authors = append(d.Authors, au.Name)
		}
	}
	return d
}

// RecordRights reports DOAJ's own CC0 waiver over its bibliographic
// metadata, verified against doaj.org/docs/faq/ on 2026-08-18. It says
// nothing about the underlying article's own copyright or licence, which a
// caller reads separately off bibjson.link / journal license fields when
// present.
func (doajIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "doaj (unparseable record)"}
	}
	return Rights{
		License:        "CC0-1.0",
		LicenseURL:     "https://creativecommons.org/publicdomain/zero/1.0/",
		Redistribution: RedistributionAllowed,
		Source:         "doaj:faq (\"we choose to waive all rights under a CC0 waiver\", applies to DOAJ's own processed metadata)",
		FreeToRead:     true,
	}
}

// Assets reports the links DOAJ carries per article (fulltext, etc.), each
// under whatever rights DOAJ itself published for that link; DOAJ's search
// index is journals it has vetted as open access, but a per-link rights
// statement is never assumed from that fact alone.
func (di doajIdentity) Assets(rec NormalizedRecord) []Asset {
	a, ok := di.parse(rec)
	if !ok {
		return nil
	}
	out := make([]Asset, 0, len(a.Bibjson.Link))
	for i, l := range a.Bibjson.Link {
		if l.URL == "" {
			continue
		}
		out = append(out, Asset{
			AssetID:        "doaj:" + a.ID + "#link-" + strconv.Itoa(i),
			Representation: firstNonEmpty(l.ContentType, "unspecified") + "; role=" + firstNonEmpty(strings.ToLower(l.Type), "external-link"),
			CanonicalURL:   l.URL,
			Rights: Rights{
				FreeToRead:     true,
				Redistribution: RedistributionUnknown,
				Source:         "doaj:bibjson.link (journal is DOAJ-vetted open access; no per-link licence statement to read)",
			},
		})
	}
	return out
}

// DOAJSearchAdapter backs route ID "doaj-search".
var DOAJSearchAdapter = &Adapter{
	ID:                 "doaj-search",
	Description:        "DOAJ (Directory of Open Access Journals) article search.",
	Searcher:           doajPagePagination{},
	Normalizer:         DOAJNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   doajIdentity{},
	DescriptorProvider: doajIdentity{},
	AssetProvider:      doajIdentity{},
}

// DOAJFetchAdapter backs route ID "doaj-fetch".
var DOAJFetchAdapter = &Adapter{
	ID:                 "doaj-fetch",
	Description:        "DOAJ single article record by DOAJ article id.",
	Fetcher:            doajFetchByID{},
	Normalizer:         DOAJNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   doajIdentity{},
	DescriptorProvider: doajIdentity{},
	AssetProvider:      doajIdentity{},
}
