package provider

import (
	"encoding/json"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// bioRxiv / medRxiv adapter (x402-research-gateway#13, Wave 2).
//
// Verified live against api.biorxiv.org on 2026-08-18, one shared API
// serving both preprint servers via a `server` path segment:
//
//	GET https://api.biorxiv.org/details/{server}/{interval}/{cursor}
//	  {server} is "biorxiv" or "medrxiv"; {interval} is either two
//	  YYYY-MM-DD dates or "N" for the last N days.
//	  -> {"messages":[...],"collection":[{doi, title, authors, date,
//	     version, license, category, jatsxml, published, server, ...}]}
//	GET https://api.biorxiv.org/details/{server}/{doi}
//	  -> the same collection shape, keyed to one preprint's version history
//	     (one entry per version)
//
// No auth, no published rate limit beyond politeness. There is no
// keyword-search endpoint: the API is a date-interval listing (paged via an
// integer cursor, 100 records per page) plus DOI lookup, so this adapter
// implements Searcher over the interval listing rather than a text query,
// and Fetcher for DOI lookup.
//
// Each preprint's `published` field carries the DOI of its published
// journal version once one exists ("NA" otherwise), which is exactly the
// preprint-to-published-work relation x402-research-gateway#13's acceptance
// criteria requires ("preprint providers carry the version and
// published-work relations needed by identity resolution"). It is exposed
// through IdentityProvider.AssertedRelations and never reduces the preprint
// record to its published counterpart: the two stay distinct addressable
// records.
//
// License is per-record (bioRxiv/medRxiv authors choose their own preprint
// license at submission, e.g. "cc_by", "cc_no"), read verbatim rather than
// assumed from server-wide policy.
type biorxivRecord struct {
	Title     string `json:"title"`
	Authors   string `json:"authors"`
	DOI       string `json:"doi"`
	Date      string `json:"date"`
	Version   string `json:"version"`
	License   string `json:"license"`
	Category  string `json:"category"`
	JatsXML   string `json:"jatsxml"`
	Published string `json:"published"`
	Server    string `json:"server"`
}

type biorxivBody struct {
	Collection []biorxivRecord `json:"collection"`
}

// BioRxivNormalizer parses api.biorxiv.org's shared response shape for both
// the interval listing and the per-DOI version history. A preprint with
// several versions yields one record per version, since a version is its
// own addressable snapshot (x402-research-gateway#13's version-relation
// requirement) rather than a fact folded into a single merged record.
type BioRxivNormalizer struct{}

func (BioRxivNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b biorxivBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(b.Collection))
	for _, r := range b.Collection {
		if r.DOI == "" {
			continue
		}
		raw, err := marshalRecord(r)
		if err != nil {
			continue
		}
		id := r.DOI
		if r.Version != "" {
			id = r.DOI + "v" + r.Version
		}
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://doi.org/" + r.DOI,
			Raw:          raw,
		})
	}
	return recs
}

// biorxivCursorPagination implements Searcher: the interval-listing
// endpoint pages via an integer cursor, 100 records per page per bioRxiv's
// own API documentation.
type biorxivCursorPagination struct{}

func (biorxivCursorPagination) PaginationModel() string { return "cursor" }

// biorxivFetchByDOI implements Fetcher: a preprint DOI, returning its full
// version history.
type biorxivFetchByDOI struct{}

func (biorxivFetchByDOI) IdentifierSchemes() []string { return []string{"doi"} }

type biorxivIdentity struct{}

func (biorxivIdentity) parse(rec NormalizedRecord) (biorxivRecord, bool) {
	var r biorxivRecord
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (bi biorxivIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	r, ok := bi.parse(rec)
	if !ok {
		return nil
	}
	return appendID(nil, identity.SchemeDOI, r.DOI)
}

// AssertedRelations reports the preprint's own published-version link when
// the provider states one ("published" != "NA" and non-empty). The
// published DOI is a provider-asserted fact about a different addressable
// record, never a merge of the two.
func (bi biorxivIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	r, ok := bi.parse(rec)
	if !ok || r.Published == "" || r.Published == "NA" {
		return nil
	}
	target, parsed := identity.New(identity.SchemeDOI, r.Published)
	if !parsed {
		return nil
	}
	return []identity.Relation{{
		From:     nodeID,
		To:       target.Key(),
		Type:     identity.RelPreprintOf,
		Evidence: identity.ProviderAsserted("biorxiv", at),
	}}
}

func (bi biorxivIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := bi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: r.Title, Year: yearFromDate(r.Date)}
}

// RecordRights reports the preprint's own author-chosen licence string
// verbatim. An empty license field reports unknown rather than assuming
// the server-wide default, since bioRxiv/medRxiv let authors choose per
// submission.
func (biorxivIdentity) RecordRights(rec NormalizedRecord) Rights {
	r, ok := biorxivIdentity{}.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "biorxiv (unparseable record)"}
	}
	if r.License == "" {
		return Rights{Redistribution: RedistributionUnknown, Source: "biorxiv:license (absent on this record)"}
	}
	return Rights{
		License:        r.License,
		Redistribution: RedistributionUnknown,
		Source:         "biorxiv:license (author-chosen at submission; redistribution not asserted by the provider even where a licence string is present)",
		FreeToRead:     true,
	}
}

// Assets reports the JATS XML full-text location bioRxiv/medRxiv publish
// for each version, when present.
func (bi biorxivIdentity) Assets(rec NormalizedRecord) []Asset {
	r, ok := bi.parse(rec)
	if !ok || r.JatsXML == "" {
		return nil
	}
	return []Asset{{
		AssetID:        "biorxiv:" + rec.ID + "#jats",
		Representation: "jats-xml",
		CanonicalURL:   r.JatsXML,
		Rights:         bi.RecordRights(rec),
	}}
}

// BioRxivSearchAdapter backs route ID "biorxiv-search": bioRxiv's date-
// interval listing. Route config supplies the {server} segment as
// "biorxiv"; the shared normalizer and identity logic are server-agnostic.
var BioRxivSearchAdapter = &Adapter{
	ID:                 "biorxiv-search",
	Description:        "bioRxiv preprint listing by date interval (no keyword search endpoint).",
	Searcher:           biorxivCursorPagination{},
	Normalizer:         BioRxivNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   biorxivIdentity{},
	DescriptorProvider: biorxivIdentity{},
	AssetProvider:      biorxivIdentity{},
}

// BioRxivFetchAdapter backs route ID "biorxiv-fetch": full version history
// for one bioRxiv DOI.
var BioRxivFetchAdapter = &Adapter{
	ID:                 "biorxiv-fetch",
	Description:        "bioRxiv preprint version history by DOI.",
	Fetcher:            biorxivFetchByDOI{},
	Normalizer:         BioRxivNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   biorxivIdentity{},
	DescriptorProvider: biorxivIdentity{},
	AssetProvider:      biorxivIdentity{},
}

// MedRxivSearchAdapter backs route ID "medrxiv-search": medRxiv's date-
// interval listing over the same shared api.biorxiv.org API with
// {server}=medrxiv.
var MedRxivSearchAdapter = &Adapter{
	ID:                 "medrxiv-search",
	Description:        "medRxiv preprint listing by date interval (no keyword search endpoint).",
	Searcher:           biorxivCursorPagination{},
	Normalizer:         BioRxivNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   biorxivIdentity{},
	DescriptorProvider: biorxivIdentity{},
	AssetProvider:      biorxivIdentity{},
}

// MedRxivFetchAdapter backs route ID "medrxiv-fetch": full version history
// for one medRxiv DOI.
var MedRxivFetchAdapter = &Adapter{
	ID:                 "medrxiv-fetch",
	Description:        "medRxiv preprint version history by DOI.",
	Fetcher:            biorxivFetchByDOI{},
	Normalizer:         BioRxivNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   biorxivIdentity{},
	DescriptorProvider: biorxivIdentity{},
	AssetProvider:      biorxivIdentity{},
}
