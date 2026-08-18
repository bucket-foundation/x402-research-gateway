package provider

import "encoding/json"

// UniProt adapter (x402-research-gateway#16).
//
// Verified live against rest.uniprot.org on 2026-08-18 (no key required):
//
//	GET https://rest.uniprot.org/uniprotkb/search?query={query}&format=json&size={n}
//	  -> {"results":[{...entry...}], "next": "<opaque cursor url>" (via Link header)}
//	GET https://rest.uniprot.org/uniprotkb/{accession}.json
//	  -> the entry object directly, same per-entry shape as one results[] item
//
// UniProt is the curated protein sequence and annotation database (UniProtKB,
// Swiss-Prot reviewed plus TrEMBL unreviewed). This adapter preserves the
// entry verbatim on Raw and reads only primaryAccession, uniProtkbId,
// proteinDescription.recommendedName.fullName.value, and organism for the
// bibliographic surface a search result needs; annotation content (features,
// cross-references, sequence) is not reshaped, since the protein record is
// the payload and this gateway discovers and cites it rather than
// reclassifying it into a citation-shaped record.
//
// Search pagination: UniProt's `search` endpoint returns up to `size`
// results and signals a further page via a `Link: <url>; rel="next"` HTTP
// header carrying an opaque cursor, not a body field. This Normalizer reads
// only the body; PaginationModel reports "cursor" so a caller reads the
// scheme correctly, and NextPosition support (Paginator) is left to a
// future harvest revision, matching the pre-#10 baseline other cursor-paged
// adapters in this package start from.
//
// Rights: UniProt states its data is released under Creative Commons
// Attribution 4.0 International (CC BY 4.0) on its License page
// (uniprot.org/help/license). That page renders client-side and could not
// be refetched as plain text in this verification pass; the CC BY 4.0
// designation is retained from this registry's prior researched entry
// rather than freshly re-read, and is flagged unverified-this-pass in
// config/providers.yaml accordingly.
type uniprotEntry struct {
	PrimaryAccession   string `json:"primaryAccession"`
	UniProtkbID        string `json:"uniProtkbId"`
	ProteinDescription struct {
		RecommendedName struct {
			FullName struct {
				Value string `json:"value"`
			} `json:"fullName"`
		} `json:"recommendedName"`
	} `json:"proteinDescription"`
	Organism struct {
		ScientificName string `json:"scientificName"`
	} `json:"organism"`
}

type uniprotSearchBody struct {
	Results []json.RawMessage `json:"results"`
}

// UniProtNormalizer handles both the search-list shape (results: [...]) and
// the single-entry fetch shape (the entry itself, no envelope).
type UniProtNormalizer struct{}

func (UniProtNormalizer) Normalize(body []byte) []NormalizedRecord {
	var search uniprotSearchBody
	items := []json.RawMessage{}
	if err := json.Unmarshal(body, &search); err == nil && len(search.Results) > 0 {
		items = search.Results
	} else {
		var probe uniprotEntry
		if err := json.Unmarshal(body, &probe); err != nil || probe.PrimaryAccession == "" {
			return nil
		}
		items = []json.RawMessage{body}
	}

	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var e uniprotEntry
		if err := json.Unmarshal(raw, &e); err != nil || e.PrimaryAccession == "" {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           e.PrimaryAccession,
			CanonicalURL: "https://www.uniprot.org/uniprotkb/" + e.PrimaryAccession + "/entry",
			Raw:          raw,
		})
	}
	return recs
}

// uniprotCursorPagination implements Searcher: UniProt pages search results
// through an opaque cursor carried in a response Link header.
type uniprotCursorPagination struct{}

func (uniprotCursorPagination) PaginationModel() string { return "cursor" }

// uniprotFetchByAccession implements Fetcher: a UniProtKB accession
// (e.g. "P01308").
type uniprotFetchByAccession struct{}

func (uniprotFetchByAccession) IdentifierSchemes() []string {
	return []string{"uniprot", "uniprot-accession"}
}

type uniprotIdentity struct{}

func (uniprotIdentity) parse(rec NormalizedRecord) (uniprotEntry, bool) {
	var e uniprotEntry
	if len(rec.Raw) == 0 {
		return e, false
	}
	if err := json.Unmarshal(rec.Raw, &e); err != nil {
		return e, false
	}
	return e, true
}

func (ui uniprotIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	e, ok := ui.parse(rec)
	if !ok {
		return Descriptor{}
	}
	title := e.ProteinDescription.RecommendedName.FullName.Value
	if title == "" {
		title = e.UniProtkbID
	}
	return Descriptor{Title: title}
}

// RecordRights reports UniProt's site-wide CC BY 4.0 designation. See the
// package doc comment above for this pass's verification caveat: retained
// from a prior researched entry, not independently re-read this session.
func (uniprotIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "uniprot (unparseable record)"}
	}
	return Rights{
		License:        "CC-BY-4.0",
		LicenseURL:     "https://www.uniprot.org/help/license",
		Redistribution: RedistributionAllowed,
		Source:         "uniprot:site license page (CC BY 4.0); not independently re-read live this pass, see doc comment",
		FreeToRead:     true,
	}
}

// UniProtSearchAdapter backs route ID "uniprot-search".
var UniProtSearchAdapter = &Adapter{
	ID:                   "uniprot-search",
	Description:          "UniProtKB protein sequence and annotation search.",
	Searcher:             uniprotCursorPagination{},
	Normalizer:           UniProtNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   uniprotIdentity{},
	RecordRightsProvider: uniprotIdentity{},
}

// UniProtFetchAdapter backs route ID "uniprot-fetch".
var UniProtFetchAdapter = &Adapter{
	ID:                   "uniprot-fetch",
	Description:          "UniProtKB single protein entry by accession.",
	Fetcher:              uniprotFetchByAccession{},
	Normalizer:           UniProtNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   uniprotIdentity{},
	RecordRightsProvider: uniprotIdentity{},
}
