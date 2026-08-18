package provider

import "encoding/json"

// Zenodo adapter (x402-research-gateway#13, Wave 2).
//
// Verified live against zenodo.org on 2026-08-18 (a single polite GET, no
// key required for search or record fetch):
//
//	GET https://zenodo.org/api/records?q={query}&size={n}
//	  -> {"hits":{"hits":[{"id":..., "doi":"10.5281/zenodo.{id}", "metadata":{...}}]}}
//	GET https://zenodo.org/api/records/{id}
//	  -> the record object directly, same shape as one hits[] entry
//
// Zenodo is CERN's general-purpose dataset/software/publication repository:
// records carry a resource_type (dataset, software, publication, ...) and
// per-record license and access_right, both preserved on Raw rather than
// flattened, since the gateway serves what Zenodo publishes as-is
// (x402-research-gateway#17's non-goal applies here too: no reclassifying
// a record into a shape Zenodo didn't assert).
type zenodoSearchBody struct {
	Hits struct {
		Hits []json.RawMessage `json:"hits"`
	} `json:"hits"`
}

type zenodoRecord struct {
	ID  json.Number `json:"id"`
	DOI string      `json:"doi"`
}

// ZenodoNormalizer handles both the search-list shape (hits.hits) and the
// single-record shape (the record itself, no envelope).
type ZenodoNormalizer struct{}

func (ZenodoNormalizer) Normalize(body []byte) []NormalizedRecord {
	var search zenodoSearchBody
	var items []json.RawMessage
	if err := json.Unmarshal(body, &search); err == nil && len(search.Hits.Hits) > 0 {
		items = search.Hits.Hits
	} else {
		items = []json.RawMessage{body}
	}

	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var rec zenodoRecord
		if err := json.Unmarshal(raw, &rec); err != nil || rec.ID == "" {
			continue
		}
		id := rec.ID.String()
		url := "https://zenodo.org/records/" + id
		if rec.DOI != "" {
			url = "https://doi.org/" + rec.DOI
		}
		recs = append(recs, NormalizedRecord{ID: id, CanonicalURL: url, Raw: raw})
	}
	return recs
}

// zenodoOffsetPagination implements Searcher: Zenodo's records endpoint
// pages via page/size, an offset-shaped scheme.
type zenodoOffsetPagination struct{}

func (zenodoOffsetPagination) PaginationModel() string { return "page" }

// zenodoFetchByID implements Fetcher: a single Zenodo record id.
type zenodoFetchByID struct{}

func (zenodoFetchByID) IdentifierSchemes() []string { return []string{"zenodo-id", "doi"} }
