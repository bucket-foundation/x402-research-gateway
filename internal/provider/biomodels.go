package provider

import "encoding/json"

// BioModels adapter (x402-research-gateway#17).
//
// Verified live against biomodels.org on 2026-08-18 (the EBI-hosted
// www.ebi.ac.uk/biomodels host 301-redirects every path to
// www.biomodels.org, so this adapter calls the latter directly):
//
//	GET https://www.biomodels.org/search?query={query}&format=json
//	  -> {"models":[{"id","name","format","submitter","submissionDate",...}],
//	     "matches":N, ...}
//	GET https://www.biomodels.org/{id}?format=json
//	  -> the single-model metadata record, no envelope; a distinct shape
//	     from the search result's per-model summary, since BioModels'
//	     single-model endpoint returns the full curated description
//	     (description, publication, authors, ...) the search summary omits
//
// No auth, no published numeric rate limit. A BioModels entry is a
// computational systems-biology model (SBML, some MATLAB/Octave), not a
// paper: this adapter preserves the model metadata verbatim on Raw and
// never reshapes it into a citation-shaped record, per #17's "native
// structure is the payload" principle. Retrieving the model file itself
// (the SBML/MATLAB content) is a separate BioModels download endpoint this
// revision does not fetch; the gateway discovers and cites the model,
// consistent with #17's non-goal against computing over source objects.
//
// Licence: CC0, per BioModels' own site ("freely accessible" under
// creativecommons.org/publicdomain/zero/1.0/, read 2026-08-18), stated
// site-wide for the curated model collection.
type biomodelsSearchResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Format string `json:"format"`
}

type biomodelsSearchBody struct {
	Models []biomodelsSearchResult `json:"models"`
}

// biomodelsSingle is the single-model fetch shape: no top-level "id" or
// "models" envelope, its own accession under publicationId instead, and
// fields (description, publication, contributors, ...) the search summary
// omits entirely.
type biomodelsSingle struct {
	Name          string `json:"name"`
	PublicationID string `json:"publicationId"`
}

// BioModelsNormalizer handles both the search-list shape (models: [...])
// and the single-model fetch shape (publicationId carries the accession
// the search summary's id field carries).
type BioModelsNormalizer struct{}

func (BioModelsNormalizer) Normalize(body []byte) []NormalizedRecord {
	var search biomodelsSearchBody
	if err := json.Unmarshal(body, &search); err == nil && len(search.Models) > 0 {
		recs := make([]NormalizedRecord, 0, len(search.Models))
		for _, m := range search.Models {
			if m.ID == "" {
				continue
			}
			raw, err := marshalRecord(m)
			if err != nil {
				continue
			}
			recs = append(recs, NormalizedRecord{
				ID:           m.ID,
				CanonicalURL: "https://www.biomodels.org/" + m.ID,
				Raw:          raw,
			})
		}
		return recs
	}

	var single biomodelsSingle
	if err := json.Unmarshal(body, &single); err != nil || single.PublicationID == "" {
		return nil
	}
	raw, err := marshalRecord(single)
	if err != nil {
		return nil
	}
	return []NormalizedRecord{{
		ID:           single.PublicationID,
		CanonicalURL: "https://www.biomodels.org/" + single.PublicationID,
		Raw:          raw,
	}}
}

// biomodelsOffsetPagination implements Searcher: BioModels' search
// endpoint pages via an offset-shaped scheme per its facet/paging
// parameters.
type biomodelsOffsetPagination struct{}

func (biomodelsOffsetPagination) PaginationModel() string { return "offset" }

// biomodelsFetchByID implements Fetcher: a BioModels accession
// (e.g. "BIOMD0000000482").
type biomodelsFetchByID struct{}

func (biomodelsFetchByID) IdentifierSchemes() []string { return []string{"biomodels-id"} }

type biomodelsIdentity struct{}

func (biomodelsIdentity) parse(rec NormalizedRecord) (biomodelsSearchResult, bool) {
	var m biomodelsSearchResult
	if len(rec.Raw) == 0 {
		return m, false
	}
	if err := json.Unmarshal(rec.Raw, &m); err != nil {
		return m, false
	}
	return m, true
}

func (bi biomodelsIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	m, ok := bi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: m.Name}
}

// RecordRights reports BioModels' site-wide CC0 waiver, verified against
// biomodels.org on 2026-08-18.
func (biomodelsIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "biomodels (unparseable record)"}
	}
	return Rights{
		License:        "CC0-1.0",
		LicenseURL:     "https://creativecommons.org/publicdomain/zero/1.0/",
		Redistribution: RedistributionAllowed,
		Source:         "biomodels:site (\"freely accessible\" under the CC0 waiver)",
		FreeToRead:     true,
	}
}

// BioModelsSearchAdapter backs route ID "biomodels-search".
var BioModelsSearchAdapter = &Adapter{
	ID:                   "biomodels-search",
	Description:          "BioModels curated systems-biology model search.",
	Searcher:             biomodelsOffsetPagination{},
	Normalizer:           BioModelsNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   biomodelsIdentity{},
	RecordRightsProvider: biomodelsIdentity{},
}

// BioModelsFetchAdapter backs route ID "biomodels-fetch".
var BioModelsFetchAdapter = &Adapter{
	ID:                   "biomodels-fetch",
	Description:          "BioModels single model record by BioModels accession.",
	Fetcher:              biomodelsFetchByID{},
	Normalizer:           BioModelsNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   biomodelsIdentity{},
	RecordRightsProvider: biomodelsIdentity{},
}
