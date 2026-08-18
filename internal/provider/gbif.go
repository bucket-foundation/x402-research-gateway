package provider

import (
	"encoding/json"
	"strconv"
	"strings"
)

// GBIF adapter (x402-research-gateway#16).
//
// Verified live against api.gbif.org on 2026-08-18 (no key required):
//
//	GET https://api.gbif.org/v1/occurrence/search?q={query}&limit={n}&offset={n}
//	  -> {"offset":0,"limit":1,"endOfRecords":false,"count":29392,
//	       "results":[{"key":5938145577,"license":"http://creativecommons.org/licenses/by-nc/4.0/legalcode",
//	         "scientificName":"Puma concolor (Linnaeus, 1771)", ...}]}
//	GET https://api.gbif.org/v1/occurrence/{key}
//	  -> the occurrence object directly, same shape as one results[] item
//
// GBIF aggregates biodiversity occurrence records from publishing
// institutions worldwide; each occurrence carries its own `license` field
// set by the publishing dataset, which is why rights here are read per
// record rather than declared once for the whole provider, on the same
// precedent CORE's adapter established: two occurrences from two different
// museums routinely carry two different licences (CC0, CC-BY, or
// CC-BY-NC), and an occurrence GBIF has not classified reports none.
type gbifOccurrence struct {
	Key            int64  `json:"key"`
	ScientificName string `json:"scientificName"`
	License        string `json:"license"`
	// EventDate lets a search result carry a year for similarity matching
	// without dragging the rest of the Darwin Core record into Descriptor.
	Year int `json:"year"`
}

type gbifSearchBody struct {
	Results []json.RawMessage `json:"results"`
}

// GBIFNormalizer handles both the search-list shape (results: [...]) and
// the single-occurrence fetch shape (the occurrence itself, no envelope).
type GBIFNormalizer struct{}

func (GBIFNormalizer) Normalize(body []byte) []NormalizedRecord {
	var search gbifSearchBody
	items := []json.RawMessage{}
	if err := json.Unmarshal(body, &search); err == nil && len(search.Results) > 0 {
		items = search.Results
	} else {
		var probe gbifOccurrence
		if err := json.Unmarshal(body, &probe); err != nil || probe.Key == 0 {
			return nil
		}
		items = []json.RawMessage{body}
	}

	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var o gbifOccurrence
		if err := json.Unmarshal(raw, &o); err != nil || o.Key == 0 {
			continue
		}
		id := strconv.FormatInt(o.Key, 10)
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://www.gbif.org/occurrence/" + id,
			Raw:          raw,
		})
	}
	return recs
}

// gbifOffsetPagination implements Searcher: GBIF's occurrence search pages
// via offset/limit.
type gbifOffsetPagination struct{}

func (gbifOffsetPagination) PaginationModel() string { return "offset" }

// gbifFetchByKey implements Fetcher: a GBIF occurrence key.
type gbifFetchByKey struct{}

func (gbifFetchByKey) IdentifierSchemes() []string { return []string{"gbif", "gbif-occurrence"} }

type gbifIdentity struct{}

func (gbifIdentity) parse(rec NormalizedRecord) (gbifOccurrence, bool) {
	var o gbifOccurrence
	if len(rec.Raw) == 0 {
		return o, false
	}
	if err := json.Unmarshal(rec.Raw, &o); err != nil {
		return o, false
	}
	return o, true
}

func (gi gbifIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	o, ok := gi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: o.ScientificName, Year: o.Year}
}

// RecordRights reads the licence GBIF carries on this specific occurrence,
// set by the publishing dataset rather than by GBIF itself. An absent
// licence reports unknown; a recognized CC0 or CC-BY URL reports allowed;
// CC-BY-NC is recorded with its licence string but not marked allowed,
// since a noncommercial clause is a redistribution restriction this
// gateway's binary Redistribution model treats conservatively as not
// unconditionally allowed.
func (gi gbifIdentity) RecordRights(rec NormalizedRecord) Rights {
	o, ok := gi.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "gbif (unparseable record)"}
	}
	if strings.TrimSpace(o.License) == "" {
		return Rights{Redistribution: RedistributionUnknown, Source: "gbif:license (absent on this occurrence)"}
	}
	l := strings.ToLower(o.License)
	rights := Rights{
		License:        o.License,
		LicenseURL:     o.License,
		Redistribution: RedistributionUnknown,
		Source:         "gbif:license (per-occurrence, set by the publishing dataset)",
		FreeToRead:     true,
	}
	if strings.Contains(l, "publicdomain/zero") || strings.Contains(l, "/licenses/by/") {
		rights.Redistribution = RedistributionAllowed
	}
	return rights
}

// GBIFOccurrenceSearchAdapter backs route ID "gbif-occurrence-search".
var GBIFOccurrenceSearchAdapter = &Adapter{
	ID:                   "gbif-occurrence-search",
	Description:          "GBIF biodiversity occurrence search.",
	Searcher:             gbifOffsetPagination{},
	Normalizer:           GBIFNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   gbifIdentity{},
	RecordRightsProvider: gbifIdentity{},
}

// GBIFOccurrenceFetchAdapter backs route ID "gbif-occurrence-fetch".
var GBIFOccurrenceFetchAdapter = &Adapter{
	ID:                   "gbif-occurrence-fetch",
	Description:          "GBIF single occurrence record by GBIF key.",
	Fetcher:              gbifFetchByKey{},
	Normalizer:           GBIFNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   gbifIdentity{},
	RecordRightsProvider: gbifIdentity{},
}
