package provider

// kruseNonePagination implements Searcher: the Kruse search service (an
// in-process semantic index, see kruse-corpus/) returns its full ranked
// result set in one call and defines no pagination scheme of its own.
type kruseNonePagination struct{}

func (kruseNonePagination) PaginationModel() string { return "none" }

// KruseSearchAdapter backs route ID "kruse-search". No Normalizer or
// CitationProvider: the pre-#2 hit_parsers.go registry never had an entry
// for this route (its response shape is service-internal, not a public API
// contract to freeze here), so this migration keeps that byte-identical —
// declaring Searcher makes `search` a reported capability without changing
// what the envelope emits.
var KruseSearchAdapter = &Adapter{
	ID:          "kruse-search",
	Description: "Semantic/keyword/hybrid search over the Jack Kruse quantum-biology corpus.",
	Searcher:    kruseNonePagination{},
}
