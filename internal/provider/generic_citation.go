package provider

import "github.com/gianyrox/x402-research-gateway/internal/config"

// maxHitsPerEnvelope caps how many hits are enumerated in a feed402
// envelope to keep responses bounded regardless of upstream page size.
// Matches the pre-#2 constant from the handler package's hit_parsers.go.
const maxHitsPerEnvelope = 10

// GenericCitationProvider builds Hits from a route's source_prefix plus
// each NormalizedRecord's ID and CanonicalURL. It covers every adapter in
// this package: a Normalizer that has already resolved the correct public
// URL for a record (the common case) needs no per-provider citation code at
// all, which is the architectural win over the one-off URL-building that
// used to live inside each hit_parsers.go function.
type GenericCitationProvider struct{}

func (GenericCitationProvider) Citations(route *config.RouteConfig, records []NormalizedRecord) []Hit {
	if len(records) == 0 {
		return nil
	}
	prefix := route.Citation.SourcePrefix
	if prefix == "" {
		prefix = route.ID
	}
	// Rank is the record's original position (1-based), not a compacted
	// count of hits actually emitted — a record with a missing ID is
	// skipped without shifting the rank of the ones after it, matching the
	// pre-#2 per-provider parsers this replaces.
	hits := make([]Hit, 0, len(records))
	for i, rec := range records {
		if rec.ID == "" || i >= maxHitsPerEnvelope {
			continue
		}
		hits = append(hits, Hit{
			SourceID:     prefix + ":" + rec.ID,
			CanonicalURL: rec.CanonicalURL,
			Rank:         i + 1,
		})
	}
	return hits
}
