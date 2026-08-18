package provider

import (
	"encoding/json"
)

// LMFDB adapter (x402-research-gateway#17).
//
// Verified live against lmfdb.org on 2026-08-18:
//
//	GET https://www.lmfdb.org/api/{table}/?_format=json&_offset=&_limit=
//	  -> {"table":..., "data":[{...}, ...]}
//	GET https://www.lmfdb.org/api/{table}/?_format=json&{field}={value}
//	  -> the same shape, filtered rather than a distinct single-record path;
//	     LMFDB's API has no per-object endpoint, only a filtered table query
//
// No auth, no published rate limit; LMFDB's own client library
// (lmfdb-lite) issues single sequential requests, and this adapter follows
// the same politeness convention. LMFDB spans many independent database
// tables (elliptic curves, modular forms, L-functions, number fields, ...);
// this adapter is table-agnostic by design, since normalizing every field
// LMFDB's dozens of tables publish would require one struct per table and
// contradicts the "native structure is the payload" principle #17 states:
// Raw carries each row's fields verbatim regardless of which table they
// came from, and this adapter parses only the shared envelope
// (`table`, `data[]`) LMFDB documents as common to every table's API.
//
// Licence: the licence page (lmfdb.org/license) sits behind a JavaScript
// "prove you're human" interstitial that this verification pass could not
// get past without a browser, so a specific SPDX identifier is left
// unverified rather than assumed; LMFDB is widely known to publish under a
// permissive/public-domain-style academic norm, but this adapter records
// that as unread rather than acting on hearsay.
type lmfdbBody struct {
	Table string            `json:"table"`
	Data  []json.RawMessage `json:"data"`
}

// lmfdbRow is the minimal shared shape this adapter reads across every
// LMFDB table: an internal row id and, where present, the table's own
// human-facing label field. LMFDB tables name their label field
// differently (lmfdb_label, label, ...); this adapter reads the row id as
// the addressable identifier and leaves every other field on Raw
// untouched, rather than guessing at a label convention that does not hold
// across tables.
type lmfdbRow struct {
	ID json.Number `json:"id"`
}

// LMFDBNormalizer parses the shared {table, data[]} envelope every LMFDB
// API table response carries. Each row keeps its own fields, whatever they
// are for that table, on Raw.
type LMFDBNormalizer struct{}

func (LMFDBNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b lmfdbBody
	if err := json.Unmarshal(body, &b); err != nil || len(b.Data) == 0 {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(b.Data))
	for _, raw := range b.Data {
		var row lmfdbRow
		if err := json.Unmarshal(raw, &row); err != nil || row.ID == "" {
			continue
		}
		id := b.Table + ":" + row.ID.String()
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://www.lmfdb.org/api/" + b.Table + "/?_format=json&id=" + row.ID.String(),
			Raw:          raw,
		})
	}
	return recs
}

// lmfdbOffsetPagination implements Searcher: LMFDB's table API pages via
// _offset/_limit.
type lmfdbOffsetPagination struct{}

func (lmfdbOffsetPagination) PaginationModel() string { return "offset" }

// lmfdbFetchByField implements Fetcher: LMFDB has no dedicated single-
// object endpoint, only the same table query filtered to one row by its
// own field (e.g. lmfdb_label). The identifier scheme is per-table by
// nature; "lmfdb-row" names the general case.
type lmfdbFetchByField struct{}

func (lmfdbFetchByField) IdentifierSchemes() []string { return []string{"lmfdb-row"} }

// LMFDBSearchAdapter backs route ID "lmfdb-search". Route config supplies
// the {table} path segment; the adapter itself is table-agnostic.
var LMFDBSearchAdapter = &Adapter{
	ID:               "lmfdb-search",
	Description:      "LMFDB (L-functions and Modular Forms Database) table query, table-agnostic.",
	Searcher:         lmfdbOffsetPagination{},
	Normalizer:       LMFDBNormalizer{},
	CitationProvider: GenericCitationProvider{},
}

// LMFDBFetchAdapter backs route ID "lmfdb-fetch": a table query filtered to
// one row.
var LMFDBFetchAdapter = &Adapter{
	ID:               "lmfdb-fetch",
	Description:      "LMFDB single row fetch via a table query filtered to one identifying field.",
	Fetcher:          lmfdbFetchByField{},
	Normalizer:       LMFDBNormalizer{},
	CitationProvider: GenericCitationProvider{},
}
