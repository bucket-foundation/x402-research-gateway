package provider

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// OpenAIRE adapter (x402-research-gateway#13, Wave 2).
//
// Verified live against api.openaire.eu on 2026-08-18:
//
//	GET https://api.openaire.eu/graph/v3/research-products?search={query}&page=&pageSize=
//	  -> {"header":{"numFound":N,"page":N,"pageSize":N},"results":[{...}]}
//	GET https://api.openaire.eu/graph/v3/research-products/{id}
//	  -> the research product object directly, same shape as one results[]
//	     entry, no envelope
//
// v3 is OpenAIRE's current Graph API generation, replacing the legacy
// XML-flavored api.openaire.eu/search/publications endpoint this gateway
// does not use. No auth required for the calls above; OpenAIRE's docs note
// that registered access raises rate-limit ceilings, which this adapter
// does not need since it makes single polite requests. Offset paging (this
// adapter's model) is capped at 10,000 results per query per OpenAIRE's own
// docs, with a cursor-based mode for exhaustive traversal that this
// revision does not implement.
//
// OpenAIRE's licence documentation states data are "openly available" but,
// as of this verification pass, does not publish an explicit CC0/CC-BY/ODC
// designation for the Graph's own metadata the way DOAJ or DBLP do. Rights
// are read per record from the instance-level `license` field, which most
// records leave unset; an unset field reports unknown rather than
// inheriting a blanket claim this adapter could not verify.
type openaireInstance struct {
	License     string   `json:"license,omitempty"`
	URLs        []string `json:"urls,omitempty"`
	Type        string   `json:"type,omitempty"`
	AccessRight struct {
		Label string `json:"label,omitempty"`
	} `json:"accessRight,omitempty"`
}

type openaireResult struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Title           string `json:"mainTitle"`
	PublicationDate string `json:"publicationDate"`
	Authors         []struct {
		FullName string `json:"fullName"`
	} `json:"authors"`
	Pids []struct {
		Scheme string `json:"scheme"`
		Value  string `json:"value"`
	} `json:"pids"`
	Instances []openaireInstance `json:"instances"`
}

type openaireSearchBody struct {
	Header struct {
		NumFound int `json:"numFound"`
	} `json:"header"`
	Results []openaireResult `json:"results"`
}

// OpenAIRENormalizer handles both the search-list shape (results: [...])
// and the single-record fetch shape (the record itself, no envelope).
type OpenAIRENormalizer struct{}

func (OpenAIRENormalizer) Normalize(body []byte) []NormalizedRecord {
	var search openaireSearchBody
	var items []openaireResult
	if err := json.Unmarshal(body, &search); err == nil && len(search.Results) > 0 {
		items = search.Results
	} else {
		var single openaireResult
		if err := json.Unmarshal(body, &single); err != nil || single.ID == "" {
			return nil
		}
		items = []openaireResult{single}
	}

	recs := make([]NormalizedRecord, 0, len(items))
	for _, r := range items {
		if r.ID == "" {
			continue
		}
		raw, err := marshalRecord(r)
		if err != nil {
			continue
		}
		url := "https://explore.openaire.eu/search/publication?pid=" + r.ID
		for _, p := range r.Pids {
			if strings.EqualFold(p.Scheme, "doi") && p.Value != "" {
				url = "https://doi.org/" + p.Value
				break
			}
		}
		recs = append(recs, NormalizedRecord{ID: r.ID, CanonicalURL: url, Raw: raw})
	}
	return recs
}

// openairePagePagination implements Searcher: OpenAIRE's research-products
// endpoint pages via page/pageSize (offset), with a separate cursor mode
// for exhaustive traversal this adapter does not implement.
type openairePagePagination struct{}

func (openairePagePagination) PaginationModel() string { return "page" }

// openaireFetchByID implements Fetcher: an OpenAIRE result id (its own
// dedup-cluster identifier scheme, e.g. "doi_dedup___::...").
type openaireFetchByID struct{}

func (openaireFetchByID) IdentifierSchemes() []string { return []string{"openaire-id", "doi"} }

type openaireIdentity struct{}

func (openaireIdentity) parse(rec NormalizedRecord) (openaireResult, bool) {
	var r openaireResult
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (oi openaireIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	r, ok := oi.parse(rec)
	if !ok {
		return nil
	}
	out := appendID(nil, identity.SchemeOpenAIRE, r.ID)
	for _, p := range r.Pids {
		if strings.EqualFold(p.Scheme, "doi") && p.Value != "" {
			out = appendID(out, identity.SchemeDOI, p.Value)
		}
	}
	return out
}

// AssertedRelations returns nil: this response surface carries no
// cross-record relation the gateway's vocabulary has a term for. OpenAIRE's
// Graph does publish project and organization links elsewhere in its data
// model, out of scope for this adapter's search/fetch surface.
func (oi openaireIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

func (oi openaireIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := oi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: r.Title, Year: yearFromDate(r.PublicationDate)}
	for _, a := range r.Authors {
		if a.FullName != "" {
			d.Authors = append(d.Authors, a.FullName)
		}
	}
	return d
}

// RecordRights reports unknown by default: OpenAIRE's own licence
// documentation does not, as of this verification, state a blanket
// CC0/CC-BY designation for Graph metadata the way DOAJ or DBLP do. Where
// an instance carries its own `license` field, that value is read and
// preserved verbatim rather than interpreted.
func (oi openaireIdentity) RecordRights(rec NormalizedRecord) Rights {
	r, ok := oi.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "openaire (unparseable record)"}
	}
	for _, inst := range r.Instances {
		if inst.License != "" {
			return Rights{
				License:        inst.License,
				Redistribution: RedistributionUnknown,
				Source:         "openaire:instances[].license (per-record; redistribution not asserted by the provider even where a licence string is present)",
				FreeToRead:     strings.EqualFold(inst.AccessRight.Label, "OPEN"),
			}
		}
	}
	return Rights{
		Redistribution: RedistributionUnknown,
		Source:         "openaire (no instance carried a license field; OpenAIRE's own docs state no blanket metadata licence as of verification)",
	}
}

// Assets reports the instance-level URLs OpenAIRE carries for a result,
// each under that instance's own rights.
func (oi openaireIdentity) Assets(rec NormalizedRecord) []Asset {
	r, ok := oi.parse(rec)
	if !ok {
		return nil
	}
	var out []Asset
	for idx, inst := range r.Instances {
		rights := Rights{Redistribution: RedistributionUnknown, Source: "openaire:instances[].accessRight"}
		if inst.License != "" {
			rights.License = inst.License
			rights.Source = "openaire:instances[].license"
		}
		rights.FreeToRead = strings.EqualFold(inst.AccessRight.Label, "OPEN")
		for i, u := range inst.URLs {
			if u == "" {
				continue
			}
			out = append(out, Asset{
				AssetID:        "openaire:" + r.ID + "#instance-" + strconv.Itoa(idx) + "-" + strconv.Itoa(i),
				Representation: firstNonEmpty(inst.Type, "unspecified"),
				CanonicalURL:   u,
				Rights:         rights,
			})
		}
	}
	return out
}

// yearFromDate extracts the leading 4-digit year from an ISO date string
// (e.g. "2024-01-01"), returning 0 when the string is empty or too short to
// carry one.
func yearFromDate(s string) int {
	if len(s) < 4 {
		return 0
	}
	return atoiSafe(s[:4])
}

// OpenAIRESearchAdapter backs route ID "openaire-search".
var OpenAIRESearchAdapter = &Adapter{
	ID:                 "openaire-search",
	Description:        "OpenAIRE Graph research-product search.",
	Searcher:           openairePagePagination{},
	Normalizer:         OpenAIRENormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   openaireIdentity{},
	DescriptorProvider: openaireIdentity{},
	AssetProvider:      openaireIdentity{},
}

// OpenAIREFetchAdapter backs route ID "openaire-fetch".
var OpenAIREFetchAdapter = &Adapter{
	ID:                 "openaire-fetch",
	Description:        "OpenAIRE Graph single research product by OpenAIRE id.",
	Fetcher:            openaireFetchByID{},
	Normalizer:         OpenAIRENormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   openaireIdentity{},
	DescriptorProvider: openaireIdentity{},
	AssetProvider:      openaireIdentity{},
}
