package provider

import "encoding/json"

// ClinicalTrialsSearchNormalizer parses the ClinicalTrials.gov v2 API:
//
//	{"studies": [{"protocolSection": {"identificationModule":
//	    {"nctId": "NCT01234567"}}}, ...]}
type ClinicalTrialsSearchNormalizer struct{}

func (ClinicalTrialsSearchNormalizer) Normalize(body []byte) []NormalizedRecord {
	var parsed struct {
		Studies []json.RawMessage `json:"studies"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(parsed.Studies))
	for _, raw := range parsed.Studies {
		var s struct {
			ProtocolSection struct {
				IdentificationModule struct {
					NCTID string `json:"nctId"`
				} `json:"identificationModule"`
			} `json:"protocolSection"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		id := s.ProtocolSection.IdentificationModule.NCTID
		// Raw is retained so relation extraction
		// (x402-research-gateway#7) can read referencesModule without a
		// second upstream call. Record ID and canonical URL are unchanged.
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://clinicaltrials.gov/study/" + id,
			Raw:          raw,
		})
	}
	return recs
}

// clinicalTrialsTokenPagination implements Searcher: ClinicalTrials.gov v2
// pages via an opaque `pageToken`.
type clinicalTrialsTokenPagination struct{}

func (clinicalTrialsTokenPagination) PaginationModel() string { return "token" }

// ClinicalTrialsSearchAdapter backs route ID "clinicaltrials-search".
var ClinicalTrialsSearchAdapter = &Adapter{
	ID:                     "clinicaltrials-search",
	Description:            "ClinicalTrials.gov v2 API study search.",
	Searcher:               clinicalTrialsTokenPagination{},
	Normalizer:             ClinicalTrialsSearchNormalizer{},
	CitationProvider:       GenericCitationProvider{},
	ObjectRelationProvider: clinicalTrialsRelations{},
	Paginator:              clinicalTrialsPaginator{},
}
