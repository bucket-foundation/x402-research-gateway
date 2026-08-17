package provider

import "testing"

// Both kruse-search and pubchem-compound had no registered hit_parsers.go
// entry before x402-research-gateway#2. These tests pin that their
// migration to adapters does not silently add a hits array where none
// existed — declaring capability without changing wire output.

func TestKruseSearchAdapter_NoNormalizerNoCitationProvider(t *testing.T) {
	if KruseSearchAdapter.Normalizer != nil || KruseSearchAdapter.CitationProvider != nil {
		t.Error("kruse-search must not gain a hits array as a side effect of adapter migration")
	}
	if !KruseSearchAdapter.Supports(CapSearch) {
		t.Error("kruse-search should still report search support")
	}
	if KruseSearchAdapter.Searcher.PaginationModel() != "none" {
		t.Errorf("pagination model: got %q want none", KruseSearchAdapter.Searcher.PaginationModel())
	}
}

func TestPubChemCompoundAdapter_FetchOnly(t *testing.T) {
	if PubChemCompoundAdapter.Normalizer != nil || PubChemCompoundAdapter.CitationProvider != nil {
		t.Error("pubchem-compound is a single-record fetch; no hits array")
	}
	if !PubChemCompoundAdapter.Supports(CapFetch) {
		t.Error("pubchem-compound should report fetch support")
	}
	if got := PubChemCompoundAdapter.Fetcher.IdentifierSchemes(); len(got) != 1 || got[0] != "name" {
		t.Errorf("identifier_schemes: got %v want [name]", got)
	}
}
