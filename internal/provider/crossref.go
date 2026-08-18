package provider

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Crossref search and fetch adapters (x402-research-gateway#23).
//
// Verified against api.crossref.org on 2026-08-17:
//
//	GET /works?query=…&rows=&cursor=*  -> {"message":{"items":[…],
//	                                      "total-results":N,"next-cursor":"…"}}
//	GET /works/{doi}                   -> {"message":{…}}
//
// No API key. A `mailto` in the query or User-Agent joins the polite pool.
// Rate limits changed on 2025-12-01: public pool 5 req/s single-record and
// 1 req/s list; polite pool 10 req/s single-record and 3 req/s list, three
// concurrent. Metadata Plus is the paid tier with monthly dumps and higher
// limits.
//
// Pagination: `rows` plus `offset` up to 10,000 results, and `cursor=*`
// with a `next-cursor` for deep paging. Cursors expire after five minutes,
// so the pagination model reported here is "cursor".
//
// Rights: Crossref publishes its *metadata* under CC0. That says nothing
// about the rights on the content a `link` entry points at, which belong to
// the publisher and are separate data. The registry records the two apart
// and the asset discovery below never treats a link as permission.

// CrossrefWorksNormalizer parses the works list, retaining each item's raw
// bytes so identity, descriptor, and asset extraction need no second call.
type CrossrefWorksNormalizer struct{}

func (CrossrefWorksNormalizer) Normalize(body []byte) []NormalizedRecord {
	var parsed struct {
		Message struct {
			Items []json.RawMessage `json:"items"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	// A single-record fetch has no `items`; the message is the work. The
	// same Normalizer serves both shapes so a fetch route gets citations
	// and identity for free.
	if len(parsed.Message.Items) == 0 {
		var single struct {
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(body, &single); err != nil || len(single.Message) == 0 {
			return nil
		}
		if rec, ok := crossrefRecord(single.Message); ok {
			return []NormalizedRecord{rec}
		}
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(parsed.Message.Items))
	for _, raw := range parsed.Message.Items {
		if rec, ok := crossrefRecord(raw); ok {
			recs = append(recs, rec)
		}
	}
	return recs
}

func crossrefRecord(raw json.RawMessage) (NormalizedRecord, bool) {
	var w struct {
		DOI string `json:"DOI"`
		URL string `json:"URL"`
	}
	if err := json.Unmarshal(raw, &w); err != nil || w.DOI == "" {
		return NormalizedRecord{}, false
	}
	url := w.URL
	if url == "" {
		url = "https://doi.org/" + w.DOI
	}
	return NormalizedRecord{ID: w.DOI, CanonicalURL: url, Raw: raw}, true
}

type crossrefCursorPagination struct{}

func (crossrefCursorPagination) PaginationModel() string { return "cursor" }

type crossrefFetchByDOI struct{}

func (crossrefFetchByDOI) IdentifierSchemes() []string { return []string{"doi"} }

// crossrefWork is the subset of a work record the adapters read.
type crossrefWork struct {
	DOI    string   `json:"DOI"`
	URL    string   `json:"URL"`
	Title  []string `json:"title"`
	Issued struct {
		DateParts [][]int `json:"date-parts"`
	} `json:"issued"`
	Author []struct {
		Given  string `json:"given"`
		Family string `json:"family"`
		ORCID  string `json:"ORCID"`
	} `json:"author"`
	Link []struct {
		URL                 string `json:"URL"`
		ContentType         string `json:"content-type"`
		ContentVersion      string `json:"content-version"`
		IntendedApplication string `json:"intended-application"`
	} `json:"link"`
	License []struct {
		URL            string `json:"URL"`
		ContentVersion string `json:"content-version"`
		DelayInDays    int    `json:"delay-in-days"`
	} `json:"license"`
	UpdateTo []struct {
		DOI   string `json:"DOI"`
		Type  string `json:"type"`
		Label string `json:"label"`
	} `json:"update-to"`
	UpdatedBy []struct {
		DOI   string `json:"DOI"`
		Type  string `json:"type"`
		Label string `json:"label"`
	} `json:"updated-by"`
}

type crossrefIdentity struct{}

func (crossrefIdentity) parse(rec NormalizedRecord) (crossrefWork, bool) {
	var w crossrefWork
	if len(rec.Raw) == 0 {
		return w, false
	}
	if err := json.Unmarshal(rec.Raw, &w); err != nil {
		return w, false
	}
	return w, true
}

func (c crossrefIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	w, ok := c.parse(rec)
	if !ok {
		return nil
	}
	return appendID(nil, identity.SchemeDOI, w.DOI)
}

// crossrefUpdateType maps Crossref's update-type vocabulary onto the
// identity relation types. An update type this mapping does not know is
// dropped from the typed relations rather than coerced into the nearest
// one, because calling a correction a retraction is worse than saying
// nothing.
func crossrefUpdateType(t string) (identity.RelationType, bool) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "retraction":
		return identity.RelRetracts, true
	case "correction", "corrigendum", "erratum", "addendum":
		return identity.RelCorrects, true
	case "withdrawal", "removal":
		return identity.RelWithdraws, true
	default:
		return "", false
	}
}

// AssertedRelations surfaces Crossmark update relations for the integrity
// graph. `update-to` names works this record updates, so this record is the
// relation's source; `updated-by` names works that update this record, so
// this record is the target. Both are Crossref's own assertions and carry
// provider-asserted evidence.
func (c crossrefIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	w, ok := c.parse(rec)
	if !ok {
		return nil
	}
	ev := identity.ProviderAsserted("crossref", at)
	var out []identity.Relation
	for _, u := range w.UpdateTo {
		rel, known := crossrefUpdateType(u.Type)
		if !known || u.DOI == "" {
			continue
		}
		out = append(out, identity.Relation{
			From: nodeID, To: "doi:" + strings.ToLower(u.DOI), Type: rel, Evidence: ev,
		})
	}
	for _, u := range w.UpdatedBy {
		rel, known := crossrefUpdateType(u.Type)
		if !known || u.DOI == "" {
			continue
		}
		out = append(out, identity.Relation{
			From: "doi:" + strings.ToLower(u.DOI), To: nodeID, Type: rel, Evidence: ev,
		})
	}
	return out
}

func (c crossrefIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	w, ok := c.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{}
	if len(w.Title) > 0 {
		d.Title = w.Title[0]
	}
	if len(w.Issued.DateParts) > 0 && len(w.Issued.DateParts[0]) > 0 {
		d.Year = w.Issued.DateParts[0][0]
	}
	for _, a := range w.Author {
		name := strings.TrimSpace(a.Given + " " + a.Family)
		if name != "" {
			d.Authors = append(d.Authors, name)
		}
	}
	return d
}

// RecordRights reads the licence the publisher deposited on the work. It is
// not Crossref's CC0, which covers the metadata record and says nothing
// about the content. A work with no deposited licence reports unknown even
// when a `link` entry exists, because a link is a locator.
//
// Crossref deposits a licence per content version (vor, am, tdm) with an
// embargo in `delay-in-days`. The statement reported here is the first
// deposited licence, with its content version and any embargo recorded in
// Source, so a consumer can see which version the licence covers rather
// than reading it as covering all of them.
func (c crossrefIdentity) RecordRights(rec NormalizedRecord) Rights {
	w, ok := c.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "crossref (unparseable record)"}
	}
	if len(w.License) == 0 {
		return Rights{
			Redistribution: RedistributionUnknown,
			Source:         "crossref:license (absent); the CC0 metadata licence does not cover the content",
		}
	}
	first := w.License[0]
	source := "crossref:license"
	if first.ContentVersion != "" {
		source += "; content-version=" + first.ContentVersion
	}
	if first.DelayInDays > 0 {
		source += "; delay-in-days=" + strconv.Itoa(first.DelayInDays)
	}
	rights := Rights{
		LicenseURL:     first.URL,
		Redistribution: RedistributionUnknown,
		Source:         source,
	}
	l := strings.ToLower(first.URL)
	switch {
	case strings.Contains(l, "creativecommons.org/publicdomain/zero"):
		rights.License, rights.Redistribution, rights.FreeToRead = "CC0", RedistributionAllowed, true
	case strings.Contains(l, "creativecommons.org/licenses/by"):
		rights.License, rights.Redistribution, rights.FreeToRead = "CC-BY family", RedistributionAllowed, true
	}
	return rights
}

// Assets reports the representations Crossref's `link` metadata names.
//
// A link is a locator. It is not permission, and this adapter does not
// fetch it, proxy it, or infer a right from its presence. Crossref's CC0
// applies to the metadata record; whatever sits at the URL is the
// publisher's and carries its own terms, which the `license` array
// describes when the publisher deposited one.
func (c crossrefIdentity) Assets(rec NormalizedRecord) []Asset {
	w, ok := c.parse(rec)
	if !ok {
		return nil
	}
	var out []Asset
	for _, l := range w.Link {
		if l.URL == "" {
			continue
		}
		representation := l.ContentType
		if representation == "" {
			representation = "unspecified"
		}
		if l.IntendedApplication != "" {
			representation += "; intended-application=" + l.IntendedApplication
		}
		if l.ContentVersion != "" {
			representation += "; content-version=" + l.ContentVersion
		}
		out = append(out, Asset{
			AssetID:        "crossref:" + w.DOI + "#" + l.URL,
			Representation: representation,
			CanonicalURL:   l.URL,
			// The deposited licence, which is unknown on most records. A
			// link is never read as permission.
			Rights: c.RecordRights(rec),
		})
	}
	return out
}

// crossrefSync reports what bulk and incremental access Crossref offers.
// The REST API supports incremental harvest through `from-index-date` and
// cursor paging, which is open. Whole-corpus monthly dumps are the paid
// Metadata Plus tier, so Bulk is reported false here: the gateway has no
// Metadata Plus subscription, and reporting a capability it cannot exercise
// would be a lie an agent could act on.
type crossrefSync struct{}

func (crossrefSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: false, Incremental: true}
}

// CrossrefSearchAdapter backs route ID "crossref-search".
var CrossrefSearchAdapter = &Adapter{
	ID:                     "crossref-search",
	Description:            "Crossref /works search over CC0 DOI metadata.",
	Searcher:               crossrefCursorPagination{},
	Normalizer:             CrossrefWorksNormalizer{},
	CitationProvider:       GenericCitationProvider{},
	IdentityProvider:       crossrefIdentity{},
	DescriptorProvider:     crossrefIdentity{},
	AssetProvider:          crossrefIdentity{},
	RecordRightsProvider:   crossrefIdentity{},
	ObjectRelationProvider: crossrefIdentity{},
	SyncProvider:           crossrefSync{},
}

// CrossrefFetchAdapter backs route ID "crossref-fetch": one DOI in, one
// work record out. It shares the search adapter's Normalizer, which handles
// the single-record message shape.
var CrossrefFetchAdapter = &Adapter{
	ID:                     "crossref-fetch",
	Description:            "Crossref single work record by DOI.",
	Fetcher:                crossrefFetchByDOI{},
	Normalizer:             CrossrefWorksNormalizer{},
	CitationProvider:       GenericCitationProvider{},
	IdentityProvider:       crossrefIdentity{},
	DescriptorProvider:     crossrefIdentity{},
	AssetProvider:          crossrefIdentity{},
	RecordRightsProvider:   crossrefIdentity{},
	ObjectRelationProvider: crossrefIdentity{},
	SyncProvider:           crossrefSync{},
}
