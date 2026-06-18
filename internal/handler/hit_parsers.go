// Package handler — per-upstream hit parsers.
//
// For search-tier routes (feed402 tier "query"), the upstream response is a
// *list of hits*, not a single record. feed402 §3 only defines one
// `citation` per envelope (the provider-level `source`), but agents that
// re-verify retrieval need per-hit provenance: which records were returned,
// in what order, and how to re-fetch each one.
//
// The envelope therefore carries an optional v0.2-additive `hits` array
// alongside the primary `citation`. Each hit is a minimal re-verification
// handle: {source_id, canonical_url, rank}. Agents can re-request any hit
// via the corresponding raw-tier route on the same gateway (or any other
// feed402 merchant that indexes the same `source_prefix`).
//
// Parsers are keyed by route ID and are free to return nil when the
// upstream shape is unknown or the body is non-JSON. Adding a new parser
// is a matter of implementing `hitParser` and registering it in
// `defaultHitParsers()`.
package handler

import (
	"encoding/json"

	"github.com/gianyrox/x402-research-gateway/internal/config"
)

// maxHitsPerEnvelope caps how many hits we enumerate in the envelope to
// keep responses bounded regardless of upstream page size.
const maxHitsPerEnvelope = 10

// feed402Hit is the per-hit re-verification handle emitted on search-tier
// envelopes. It is intentionally minimal — agents re-fetch the full record
// via the canonical URL or a sibling raw-tier route.
type feed402Hit struct {
	SourceID     string `json:"source_id"`
	CanonicalURL string `json:"canonical_url,omitempty"`
	Rank         int    `json:"rank"`
}

// hitParser extracts per-hit citation handles from an upstream response
// body. It must never panic; return nil for bodies it doesn't recognize.
type hitParser func(route *config.RouteConfig, body []byte) []feed402Hit

// defaultHitParsers returns the built-in parser registry keyed by route
// ID. Routes without an entry get no `hits` array, which is spec-valid.
func defaultHitParsers() map[string]hitParser {
	return map[string]hitParser{
		"pubmed-search":           pubmedESearchHits,
		"semantic-scholar-search": semanticScholarSearchHits,
		"openalex-works":          openAlexWorksHits,
		"clinicaltrials-search":   clinicalTrialsSearchHits,
	}
}

// pubmedESearchHits parses NCBI E-utils ESearch JSON:
//
//	{"esearchresult": {"idlist": ["38831607", "34588695", ...]}}
func pubmedESearchHits(route *config.RouteConfig, body []byte) []feed402Hit {
	var parsed struct {
		ESearchResult struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	ids := parsed.ESearchResult.IDList
	if len(ids) > maxHitsPerEnvelope {
		ids = ids[:maxHitsPerEnvelope]
	}
	prefix := route.Citation.SourcePrefix
	if prefix == "" {
		prefix = "pubmed"
	}
	hits := make([]feed402Hit, 0, len(ids))
	for i, id := range ids {
		hits = append(hits, feed402Hit{
			SourceID:     prefix + ":" + id,
			CanonicalURL: "https://pubmed.ncbi.nlm.nih.gov/" + id + "/",
			Rank:         i + 1,
		})
	}
	return hits
}

// semanticScholarSearchHits parses Semantic Scholar Graph API search:
//
//	{"data": [{"paperId": "abc123...", ...}, ...]}
func semanticScholarSearchHits(route *config.RouteConfig, body []byte) []feed402Hit {
	var parsed struct {
		Data []struct {
			PaperID string `json:"paperId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	prefix := route.Citation.SourcePrefix
	if prefix == "" {
		prefix = "s2"
	}
	hits := make([]feed402Hit, 0, len(parsed.Data))
	for i, p := range parsed.Data {
		if p.PaperID == "" || i >= maxHitsPerEnvelope {
			continue
		}
		hits = append(hits, feed402Hit{
			SourceID:     prefix + ":" + p.PaperID,
			CanonicalURL: "https://www.semanticscholar.org/paper/" + p.PaperID,
			Rank:         i + 1,
		})
	}
	return hits
}

// openAlexWorksHits parses OpenAlex /works search:
//
//	{"results": [{"id": "https://openalex.org/W1234", ...}, ...]}
func openAlexWorksHits(route *config.RouteConfig, body []byte) []feed402Hit {
	var parsed struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	prefix := route.Citation.SourcePrefix
	if prefix == "" {
		prefix = "openalex"
	}
	hits := make([]feed402Hit, 0, len(parsed.Results))
	for i, r := range parsed.Results {
		if r.ID == "" || i >= maxHitsPerEnvelope {
			continue
		}
		// OpenAlex IDs are full URLs; the short form is the trailing segment.
		shortID := r.ID
		for j := len(shortID) - 1; j >= 0; j-- {
			if shortID[j] == '/' {
				shortID = shortID[j+1:]
				break
			}
		}
		hits = append(hits, feed402Hit{
			SourceID:     prefix + ":" + shortID,
			CanonicalURL: r.ID,
			Rank:         i + 1,
		})
	}
	return hits
}

// clinicalTrialsSearchHits parses ClinicalTrials.gov v2 API:
//
//	{"studies": [{"protocolSection": {"identificationModule":
//	    {"nctId": "NCT01234567"}}}, ...]}
func clinicalTrialsSearchHits(route *config.RouteConfig, body []byte) []feed402Hit {
	var parsed struct {
		Studies []struct {
			ProtocolSection struct {
				IdentificationModule struct {
					NCTID string `json:"nctId"`
				} `json:"identificationModule"`
			} `json:"protocolSection"`
		} `json:"studies"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	prefix := route.Citation.SourcePrefix
	if prefix == "" {
		prefix = "nct"
	}
	hits := make([]feed402Hit, 0, len(parsed.Studies))
	for i, s := range parsed.Studies {
		id := s.ProtocolSection.IdentificationModule.NCTID
		if id == "" || i >= maxHitsPerEnvelope {
			continue
		}
		hits = append(hits, feed402Hit{
			SourceID:     prefix + ":" + id,
			CanonicalURL: "https://clinicaltrials.gov/study/" + id,
			Rank:         i + 1,
		})
	}
	return hits
}
