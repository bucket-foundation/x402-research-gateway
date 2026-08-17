package provider

import "encoding/json"

// ClinicalTrialsSearchNormalizer parses the ClinicalTrials.gov v2 API:
//
//	{"studies": [{"protocolSection": {"identificationModule":
//	    {"nctId": "NCT01234567"}}}, ...]}
type ClinicalTrialsSearchNormalizer struct{}

func (ClinicalTrialsSearchNormalizer) Normalize(body []byte) []NormalizedRecord {
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
	recs := make([]NormalizedRecord, 0, len(parsed.Studies))
	for _, s := range parsed.Studies {
		id := s.ProtocolSection.IdentificationModule.NCTID
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://clinicaltrials.gov/study/" + id,
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
	ID:               "clinicaltrials-search",
	Description:      "ClinicalTrials.gov v2 API study search.",
	Searcher:         clinicalTrialsTokenPagination{},
	Normalizer:       ClinicalTrialsSearchNormalizer{},
	CitationProvider: GenericCitationProvider{},
}
