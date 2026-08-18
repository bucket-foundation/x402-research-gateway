package provider

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/relation"
)

// USPTO Open Data Portal adapter (x402-research-gateway#18).
//
// Verified live against api.uspto.gov on 2026-08-18, with an ODP API key
// provisioned to the founder's USPTO account that day:
//
//	GET https://api.uspto.gov/api/v1/patent/applications/search?q={query}&limit=&offset=
//	  -> {"count": N, "patentFileWrapperDataBag": [{...}, ...]}
//	GET https://api.uspto.gov/api/v1/patent/applications/{applicationNumber}
//	  -> the same envelope, one entry
//
// Auth is a static header, `X-API-KEY: <key>`, sent from the operator's
// environment via config's `${USPTO_ODP_API_KEY}` expansion (config/routes.yaml
// upstream.headers), the same static-secret-via-Headers pattern CORE uses
// (internal/auth's own doc comment names this as preferred whenever a key
// needs no minting or caching, which a static ODP key does not). No
// internal/auth TokenSource is registered for this provider.
//
// USPTO ODP replaced the retired PatentsView API on 2026-03-20
// (config/providers.yaml's "patentsview-legacy" entry records that
// migration as historical_successor/-predecessor). This adapter is
// registered against the Open Data Portal only.
//
// Rate limits, as provisioned to this key: 1,200,000 Patent File Wrapper
// Document requests/week, 5,000,000 metadata-retrieval requests/week.
//
// Scope: this is a US-jurisdiction-only source (the USPTO's own prosecution
// history database). It carries no EPO, WIPO PATENTSCOPE, or non-US patent
// office data, and every record this adapter emits is implicitly
// jurisdiction "US" — never asserted as international coverage.
//
// Operations this adapter implements: search, fetch (by application
// number), and family (patent continuity/family grouping, via
// ObjectRelationProvider over parentContinuityBag/childContinuityBag,
// documented below). assignee, inventor, and classification are not
// separate upstream endpoints on this API: they are fields already present
// on every search and fetch record (applicantBag, inventorBag,
// cpcClassificationBag/uspcSymbolText), preserved on NormalizedRecord.Raw
// and surfaced through DescriptorProvider/RecordRights rather than as
// distinct routes, since ODP publishes no assignee/inventor/classification
// lookup independent of the application record itself.
//
// Operations this adapter does NOT implement, verified absent from every
// application record inspected during this session (pending, published,
// and granted): forward citations (patents citing this one) and backward
// citations (patents this one cites). USPTO ODP is a prosecution-history
// (file wrapper) API, not a citation-graph API; PatentsView's retired API
// carried citation tables, but the Open Data Portal migration did not
// preserve them under any field this adapter found. No CitationGraphProvider
// is registered for uspto-odp in either direction, the same "provider
// serves one direction, or none, and says so" rule Crossref's
// references-only registration already established — an agent requesting
// uspto-odp citations gets an explicit unsupported answer, never an empty
// result read as "no citations."
//
// Classification: cpcClassificationBag carries CPC symbols in the
// USPTO/EPO shared CPC scheme (no explicit version field on the record;
// the scheme name "CPC" is asserted by this adapter, the version is
// whatever the applied classification's CPC edition was at grant/publication
// and is not separately stated by ODP). uspcSymbolText/class/subclass
// carries the legacy USPC scheme where the record has one. Both are
// preserved verbatim on Raw; this adapter does not attempt to translate
// between the two schemes.
//
// Rights: patent applications and grants prosecuted before the USPTO are US
// federal government records. 17 U.S.C. §105 places US government works
// outside copyright, and USPTO's own site states patent and trademark data
// is available for public use; this adapter reports Redistribution allowed
// on that public-domain basis, consistent with config/providers.yaml's
// existing "uspto-odp" registry entry (license: "US-gov public domain").
// This does not extend to non-US patent office data if a jurisdiction-aware
// caller later mixes in EPO/WIPO/other-office records: those providers are
// not implemented by this adapter and are recorded registry-only with their
// own rights unresolved (see final report / registry entries for
// patentsview-legacy's successors and the other providers issue #18 names).

// usptoApplicant and usptoInventor carry the party fields the search and
// fetch endpoints publish inline on every record; ODP has no separate
// assignee or inventor lookup.
type usptoApplicant struct {
	ApplicantNameText string `json:"applicantNameText"`
}

type usptoInventor struct {
	FirstName        string `json:"firstName"`
	LastName         string `json:"lastName"`
	InventorNameText string `json:"inventorNameText"`
}

// usptoContinuity is one entry of parentContinuityBag or childContinuityBag,
// verified live 2026-08-18. claimParentageTypeCode is USPTO's own
// continuity-relationship vocabulary (e.g. "NST" national-stage entry,
// "CON" continuation, "DIV" division, "CIP" continuation-in-part); this
// adapter carries it verbatim as the relation's provider term rather than
// inventing a normalized one, per internal/relation's "the provider's own
// term is the record" rule.
type usptoContinuity struct {
	ClaimParentageTypeCode                string `json:"claimParentageTypeCode"`
	ClaimParentageTypeCodeDescriptionText string `json:"claimParentageTypeCodeDescriptionText"`
	ParentApplicationNumberText           string `json:"parentApplicationNumberText"`
	ParentApplicationFilingDate           string `json:"parentApplicationFilingDate"`
	ChildApplicationNumberText            string `json:"childApplicationNumberText"`
}

type usptoMetaData struct {
	ApplicationStatusCode            int              `json:"applicationStatusCode"`
	ApplicationStatusDescriptionText string           `json:"applicationStatusDescriptionText"`
	ApplicationTypeCode              string           `json:"applicationTypeCode"`
	FilingDate                       string           `json:"filingDate"`
	InventionTitle                   string           `json:"inventionTitle"`
	FirstInventorName                string           `json:"firstInventorName"`
	FirstApplicantName               string           `json:"firstApplicantName"`
	InventorBag                      []usptoInventor  `json:"inventorBag"`
	ApplicantBag                     []usptoApplicant `json:"applicantBag"`
	PatentNumber                     string           `json:"patentNumber"`
	GrantDate                        string           `json:"grantDate"`
	CPCClassificationBag             []string         `json:"cpcClassificationBag"`
	USPCSymbolText                   string           `json:"uspcSymbolText"`
	PublicationCategoryBag           []string         `json:"publicationCategoryBag"`
}

type usptoRecord struct {
	ApplicationNumberText string            `json:"applicationNumberText"`
	ApplicationMetaData   usptoMetaData     `json:"applicationMetaData"`
	ParentContinuityBag   []usptoContinuity `json:"parentContinuityBag"`
	ChildContinuityBag    []usptoContinuity `json:"childContinuityBag"`
}

type usptoSearchResponse struct {
	Count                    int               `json:"count"`
	PatentFileWrapperDataBag []json.RawMessage `json:"patentFileWrapperDataBag"`
}

// USPTONormalizer handles both the multi-result search envelope and the
// single-result fetch envelope: ODP wraps a fetch-by-application-number
// response in the same patentFileWrapperDataBag array, one entry long.
type USPTONormalizer struct{}

func (USPTONormalizer) Normalize(body []byte) []NormalizedRecord {
	var env usptoSearchResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(env.PatentFileWrapperDataBag))
	for _, raw := range env.PatentFileWrapperDataBag {
		var r usptoRecord
		if err := json.Unmarshal(raw, &r); err != nil || r.ApplicationNumberText == "" {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID: r.ApplicationNumberText,
			// USPTO Patent Center is the USPTO's own public per-application
			// record, reachable for every application regardless of grant
			// status, unlike Google Patents which only resolves a granted
			// or published number.
			CanonicalURL: "https://patentcenter.uspto.gov/applications/" + r.ApplicationNumberText,
			Raw:          raw,
		})
	}
	return recs
}

type usptoOffsetPagination struct{}

func (usptoOffsetPagination) PaginationModel() string { return "offset" }

type usptoFetchByApplication struct{}

func (usptoFetchByApplication) IdentifierSchemes() []string {
	return []string{string(identity.SchemeUSPTOApplication)}
}

type usptoIdentity struct{}

func (usptoIdentity) parse(rec NormalizedRecord) (usptoRecord, bool) {
	var r usptoRecord
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

// Identifiers reports the application number always, and the patent number
// when one has issued. An application that never issues carries only the
// former, which is the accurate statement: it is not a patent yet, and may
// never be one.
func (u usptoIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	r, ok := u.parse(rec)
	if !ok {
		return nil
	}
	var ids []identity.Identifier
	ids = appendID(ids, identity.SchemeUSPTOApplication, r.ApplicationNumberText)
	ids = appendID(ids, identity.SchemeUSPTOPatent, r.ApplicationMetaData.PatentNumber)
	return ids
}

// AssertedRelations returns nil: cross-provider identity equivalence is not
// what parentContinuityBag/childContinuityBag assert. They assert
// continuity between two USPTO applications, which is a family relation
// (ObjectRelationProvider, below), not an identity claim about the same
// record under two providers' ids.
func (usptoIdentity) AssertedRelations(string, NormalizedRecord, time.Time) []identity.Relation {
	return nil
}

func usptoYear(dateText string) int {
	if len(dateText) < 4 {
		return 0
	}
	y, err := strconv.Atoi(dateText[:4])
	if err != nil {
		return 0
	}
	return y
}

func (u usptoIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := u.parse(rec)
	if !ok {
		return Descriptor{}
	}
	amd := r.ApplicationMetaData
	d := Descriptor{Title: amd.InventionTitle, Year: usptoYear(amd.FilingDate)}
	for _, inv := range amd.InventorBag {
		name := strings.TrimSpace(inv.InventorNameText)
		if name != "" {
			d.Authors = append(d.Authors, name)
		}
	}
	return d
}

// RecordRights reports the public-domain status of US government
// prosecution records; see the package doc comment above for the
// 17 U.S.C. §105 basis. This is a provider-level fact stated per record
// (never a provider-default inherited silently), consistent with this
// package's "rights are read per record" rule — every uspto-odp record
// carries the identical basis because every one of them is the same kind of
// US federal government record.
func (usptoIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "uspto-odp (unparseable record)"}
	}
	return Rights{
		License:        "US-gov public domain",
		Redistribution: RedistributionAllowed,
		Source:         "uspto-odp:us-government-work (17 U.S.C. §105; USPTO ODP patent prosecution data)",
		FreeToRead:     true,
	}
}

func (usptoIdentity) Coverage() string {
	return "US patent applications and granted patents, prosecution history (file wrapper) data from the USPTO Open Data Portal. " +
		"US jurisdiction only; no EPO, WIPO, or other national office coverage."
}

// usptoContinuityObject builds a relation.Object for a continuity-bag entry
// naming another USPTO application, typed as a patent object.
func usptoContinuityObject(appNum string) relation.Object {
	obj := relation.NewObject("application", appNum).WithType(relation.TypePatent)
	if appNum != "" {
		obj.CanonicalURL = "https://patentcenter.uspto.gov/applications/" + appNum
	}
	return obj
}

// ObjectRelations reports patent family membership as the continuity graph
// USPTO itself asserts: parentContinuityBag names applications this one
// claims priority from or continues, childContinuityBag names applications
// that continue this one. USPTO ODP publishes no separate "family id"; the
// family is the transitive closure of this continuity graph, which is also
// how patent-family grouping works in practice (a family is priority/
// continuity linkage, not an assigned identifier). Direction matters the
// same way Crossref's update-to/updated-by split does: a parent-bag entry
// is asserted with this application as object (this application continues
// the parent), a child-bag entry with this application as subject reversed
// (the child continues this application), so a consumer walking the graph
// in either direction reads "who continues whom" correctly rather than an
// undirected family set.
func (u usptoIdentity) ObjectRelations(rec NormalizedRecord, at time.Time) []relation.Relation {
	r, ok := u.parse(rec)
	if !ok || r.ApplicationNumberText == "" {
		return nil
	}
	self := usptoContinuityObject(r.ApplicationNumberText)

	var out []relation.Relation
	for _, c := range r.ParentContinuityBag {
		if c.ParentApplicationNumberText == "" || c.ClaimParentageTypeCode == "" {
			continue
		}
		parent := usptoContinuityObject(c.ParentApplicationNumberText)
		rel := relation.New("uspto-odp", "uspto-odp:parentContinuityBag",
			self, strings.ToLower(c.ClaimParentageTypeCode), parent, at)
		ann := map[string]string{}
		if c.ClaimParentageTypeCodeDescriptionText != "" {
			ann["description"] = c.ClaimParentageTypeCodeDescriptionText
		}
		if c.ParentApplicationFilingDate != "" {
			ann["parent_filing_date"] = c.ParentApplicationFilingDate
		}
		if len(ann) > 0 {
			rel.Annotations = ann
		}
		out = append(out, rel)
	}
	for _, c := range r.ChildContinuityBag {
		if c.ChildApplicationNumberText == "" || c.ClaimParentageTypeCode == "" {
			continue
		}
		child := usptoContinuityObject(c.ChildApplicationNumberText)
		rel := relation.New("uspto-odp", "uspto-odp:childContinuityBag",
			child, strings.ToLower(c.ClaimParentageTypeCode), self, at)
		if c.ClaimParentageTypeCodeDescriptionText != "" {
			rel.Annotations = map[string]string{"description": c.ClaimParentageTypeCodeDescriptionText}
		}
		out = append(out, rel)
	}
	return out
}

// USPTOSearchAdapter backs route ID "uspto-search".
var USPTOSearchAdapter = &Adapter{
	ID:                     "uspto-search",
	Description:            "USPTO Open Data Portal patent application search (bibliographic and prosecution data, US jurisdiction only).",
	Searcher:               usptoOffsetPagination{},
	Normalizer:             USPTONormalizer{},
	CitationProvider:       GenericCitationProvider{},
	IdentityProvider:       usptoIdentity{},
	DescriptorProvider:     usptoIdentity{},
	RecordRightsProvider:   usptoIdentity{},
	ObjectRelationProvider: usptoIdentity{},
}

// USPTOFetchAdapter backs route ID "uspto-fetch".
var USPTOFetchAdapter = &Adapter{
	ID:                     "uspto-fetch",
	Description:            "USPTO Open Data Portal single application record, by application number.",
	Fetcher:                usptoFetchByApplication{},
	Normalizer:             USPTONormalizer{},
	CitationProvider:       GenericCitationProvider{},
	IdentityProvider:       usptoIdentity{},
	DescriptorProvider:     usptoIdentity{},
	RecordRightsProvider:   usptoIdentity{},
	ObjectRelationProvider: usptoIdentity{},
}
