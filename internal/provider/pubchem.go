package provider

// pubChemFetchByName implements Fetcher: pubchem-compound looks up one
// compound record by name (path-templated into the PubChem PUG REST call),
// not a search over many.
type pubChemFetchByName struct{}

func (pubChemFetchByName) IdentifierSchemes() []string { return []string{"name"} }

// PubChemCompoundAdapter backs route ID "pubchem-compound". No Normalizer
// or CitationProvider: a raw-tier single-record fetch, matching pre-#2
// behavior where this route never had a hit parser.
var PubChemCompoundAdapter = &Adapter{
	ID:          "pubchem-compound",
	Description: "PubChem PUG REST — a single compound record by name.",
	Fetcher:     pubChemFetchByName{},
}
