package provider

import "encoding/json"

// OSF (Open Science Framework) adapter (x402-research-gateway#16).
//
// Verified live against api.osf.io on 2026-08-18 (no key required for
// public read):
//
//	GET https://api.osf.io/v2/nodes/?filter[title]={q}&page[size]={n}
//	  -> {"data":[{"id":"bqhrn","type":"nodes","attributes":{"title":...,
//	       "description":...,"date_created":...,"public":true,...},
//	       "links":{"html":"https://osf.io/bqhrn/",...}}], "links":{...}}
//	GET https://api.osf.io/v2/nodes/{id}/
//	  -> {"data":{...one node, same attributes shape...}}
//
// OSF is a general-purpose research project/registration/preprint host
// (JSON:API throughout); this adapter registers the `nodes` collection,
// OSF's umbrella object for a project or component. A node's per-object
// content licence, when set, lives behind its own `relationships.license`
// link to a separate `/v2/licenses/{id}/` resource (e.g. MIT, CC0, CC-BY),
// not inline on the node — this pass does not dereference that relationship
// automatically, so RecordRights below reports unknown rather than
// resolving a licence this adapter has not fetched, consistent with this
// issue's "unknown != allowed" rule.
//
// Rights: OSF's own API Terms of Use govern reuse of the API itself; no
// blanket public-domain or CC designation for node metadata was found on
// developer.osf.io in this pass (its docs render client-side and could not
// be refetched as plain text), so metadata redistribution is recorded
// unknown, flagged unverified in config/providers.yaml.
type osfAttributes struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Public      bool   `json:"public"`
}

type osfResource struct {
	ID         string        `json:"id"`
	Attributes osfAttributes `json:"attributes"`
	Links      struct {
		HTML string `json:"html"`
	} `json:"links"`
}

type osfListBody struct {
	Data []osfResource `json:"data"`
}

type osfSingleBody struct {
	Data osfResource `json:"data"`
}

// OSFNormalizer handles both the JSON:API list shape (data: [...]) and the
// single-resource shape (data: {...}).
type OSFNormalizer struct{}

func (OSFNormalizer) Normalize(body []byte) []NormalizedRecord {
	var list osfListBody
	if err := json.Unmarshal(body, &list); err == nil && len(list.Data) > 0 {
		recs := make([]NormalizedRecord, 0, len(list.Data))
		for _, r := range list.Data {
			if r.ID == "" {
				continue
			}
			recs = append(recs, osfRecord(r))
		}
		return recs
	}

	var single osfSingleBody
	if err := json.Unmarshal(body, &single); err != nil || single.Data.ID == "" {
		return nil
	}
	return []NormalizedRecord{osfRecord(single.Data)}
}

func osfRecord(r osfResource) NormalizedRecord {
	raw, err := marshalRecord(r)
	if err != nil {
		raw = nil
	}
	url := r.Links.HTML
	if url == "" {
		url = "https://osf.io/" + r.ID + "/"
	}
	return NormalizedRecord{ID: r.ID, CanonicalURL: url, Raw: raw}
}

// osfPagePagination implements Searcher: OSF's JSON:API collections page
// via page/page[size], a page-shaped scheme.
type osfPagePagination struct{}

func (osfPagePagination) PaginationModel() string { return "page" }

// osfFetchByID implements Fetcher: a 5-character OSF node id (e.g. "bqhrn").
type osfFetchByID struct{}

func (osfFetchByID) IdentifierSchemes() []string { return []string{"osf", "osf-node"} }

type osfIdentity struct{}

func (osfIdentity) parse(rec NormalizedRecord) (osfResource, bool) {
	var r osfResource
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (oi osfIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := oi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: r.Attributes.Title}
}

// RecordRights reports unknown: OSF nodes carry a per-node licence through
// a separate relationship this adapter does not dereference. See the
// package doc comment above.
func (osfIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "osf (unparseable record)"}
	}
	return Rights{
		Redistribution: RedistributionUnknown,
		Source:         "osf:relationships.license (present per-node but not dereferenced by this adapter); absent inline licence reports unknown",
	}
}

// OSFNodeSearchAdapter backs route ID "osf-node-search".
var OSFNodeSearchAdapter = &Adapter{
	ID:                   "osf-node-search",
	Description:          "OSF (Open Science Framework) node search: projects, components, and registrations.",
	Searcher:             osfPagePagination{},
	Normalizer:           OSFNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   osfIdentity{},
	RecordRightsProvider: osfIdentity{},
}

// OSFNodeFetchAdapter backs route ID "osf-node-fetch".
var OSFNodeFetchAdapter = &Adapter{
	ID:                   "osf-node-fetch",
	Description:          "OSF single node record by OSF node id.",
	Fetcher:              osfFetchByID{},
	Normalizer:           OSFNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   osfIdentity{},
	RecordRightsProvider: osfIdentity{},
}
