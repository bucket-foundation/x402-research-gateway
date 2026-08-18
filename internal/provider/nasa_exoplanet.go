package provider

import (
	"encoding/json"
	"net/url"
)

// NASA Exoplanet Archive adapter (x402-research-gateway#16).
//
// Verified live against exoplanetarchive.ipac.caltech.edu on 2026-08-18 (no
// key required):
//
//	GET https://exoplanetarchive.ipac.caltech.edu/TAP/sync?query={ADQL}&format=json
//	  -> a bare JSON array, no envelope:
//	     [{"pl_name":"Kepler-370 b","hostname":"Kepler-370","disc_year":2014}, ...]
//
// This is a Table Access Protocol (IVOA TAP) service, not a keyword search.
// The caller supplies a full ADQL query string (e.g.
// "select top 20 pl_name,hostname,disc_year from ps where hostname like
// '%Kepler%'"), and the archive is a dataset locator over its own tables
// rather than a search-box-shaped index, per this issue's "provider type
// matches source reality" rule. TAP/sync returns the whole result set in
// one call (bounded by the query's own TOP clause) rather than paging, so
// PaginationModel reports "none": a caller controls page size through ADQL,
// not through gateway pagination parameters.
//
// The archive holds one row per query the caller wrote; there is no
// provider-side single-object endpoint independent of the table system
// itself, so this adapter registers one Searcher-shaped route rather than a
// separate Fetcher, matching this issue's non-uniformity instruction.
//
// Rights: NASA Exoplanet Archive is IPAC/Caltech-operated under a NASA
// contract; its own site states data "may be freely used" but does not
// publish a single blanket public-domain or CC designation covering every
// table (parameter values are sourced from published literature in many
// rows, each of which may carry its own reuse terms). Redistribution is
// recorded unknown per this gateway's own prior researched entry, unchanged
// by this pass, since no site-wide statement was found to supersede it.
type nasaExoplanetRow struct {
	PlName   string `json:"pl_name"`
	HostName string `json:"hostname"`
	DiscYear int    `json:"disc_year"`
}

// NASAExoplanetNormalizer parses the bare JSON array TAP/sync returns. A
// row missing pl_name (a malformed or off-schema query) is dropped, never
// invented an id.
type NASAExoplanetNormalizer struct{}

func (NASAExoplanetNormalizer) Normalize(body []byte) []NormalizedRecord {
	var rows []json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(rows))
	for _, raw := range rows {
		var r nasaExoplanetRow
		if err := json.Unmarshal(raw, &r); err != nil || r.PlName == "" {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           r.PlName,
			CanonicalURL: "https://exoplanetarchive.ipac.caltech.edu/overview/" + url.PathEscape(r.PlName),
			Raw:          raw,
		})
	}
	return recs
}

// nasaExoplanetNoPagination implements Searcher: TAP/sync returns its whole
// result set (bounded by the caller's own ADQL TOP clause) in one call.
type nasaExoplanetNoPagination struct{}

func (nasaExoplanetNoPagination) PaginationModel() string { return "none" }

type nasaExoplanetIdentity struct{}

func (nasaExoplanetIdentity) parse(rec NormalizedRecord) (nasaExoplanetRow, bool) {
	var r nasaExoplanetRow
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (ei nasaExoplanetIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := ei.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: r.PlName, Year: r.DiscYear}
}

// RecordRights reports the unknown redistribution status carried over from
// this provider's prior researched registry entry; see the package doc
// comment above.
func (nasaExoplanetIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "nasa-exoplanet-archive (unparseable record)"}
	}
	return Rights{
		Redistribution: RedistributionUnknown,
		Source:         "nasa-exoplanet-archive:site (no single blanket redistribution statement found covering every table)",
	}
}

// NASAExoplanetArchiveAdapter backs route ID "nasa-exoplanet-archive-tap".
var NASAExoplanetArchiveAdapter = &Adapter{
	ID:                   "nasa-exoplanet-archive-tap",
	Description:          "NASA Exoplanet Archive TAP/sync — an ADQL query over confirmed-planet and related tables.",
	Searcher:             nasaExoplanetNoPagination{},
	Normalizer:           NASAExoplanetNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   nasaExoplanetIdentity{},
	RecordRightsProvider: nasaExoplanetIdentity{},
}
