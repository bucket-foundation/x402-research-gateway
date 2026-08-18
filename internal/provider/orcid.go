package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// ORCID adapter (x402-research-gateway#29).
//
// Verified against info.orcid.org on 2026-08-18: the public API's read
// operations require an OAuth 2.0 access token minted via the client-
// credentials grant (RFC 6749 §4.4):
//
//	POST https://orcid.org/oauth/token
//	  grant_type=client_credentials&scope=/read-public
//
// ORCID reports the resulting token valid for 631138518 seconds (20
// years); internal/auth.ClientCredentialsSource caches it for that
// reported lifetime rather than re-minting per request. This adapter is
// the first credentialed provider in the gateway; internal/auth is the
// generic seam patents (#18) and licensed vocabularies (#14) reuse.
//
// Public-API base and routes, all under the /read-public scope:
//
//	GET https://pub.orcid.org/v3.0/{orcid-id}/record          full record
//	GET https://pub.orcid.org/v3.0/{orcid-id}/person          identity block only
//	GET https://pub.orcid.org/v3.0/{orcid-id}/works           works summary list
//	GET https://pub.orcid.org/v3.0/expanded-search/?q={query} SOLR-syntax search
//
// This adapter registers fetch-by-ORCID-iD (record) and search
// (expanded-search); works are read from the record's own
// activities-summary rather than a second call, since /record already
// carries a work-summary per group.
//
// Data returned is the "Everyone"-visibility subset of the record, which
// ORCID's Terms of Use dedicate to the public domain under CC0-1.0 — the
// same grant ROR makes for its own records (ror.go).
//
// Every external identifier ORCID publishes on a person (Scopus Author
// ID, ResearcherID, LinkedIn, GitHub, and any type ORCID adds later) is
// preserved verbatim as a raw external identifier, the same pattern
// rorIdentity.ExternalIdentifiers uses for ROR's external-ids block: the
// identity graph has no node type for most of them, so they are evidence
// on the record rather than folded into identity.Identifier.

type orcidExternalID struct {
	Type  string `json:"external-id-type"`
	Value string `json:"external-id-value"`
	URL   *struct {
		Value string `json:"value"`
	} `json:"external-id-url"`
	Relationship string `json:"external-id-relationship"`
}

type orcidName struct {
	GivenNames *struct {
		Value string `json:"value"`
	} `json:"given-names"`
	FamilyName *struct {
		Value string `json:"value"`
	} `json:"family-name"`
	CreditName *struct {
		Value string `json:"value"`
	} `json:"credit-name"`
}

type orcidWorkSummary struct {
	Title *struct {
		Title *struct {
			Value string `json:"value"`
		} `json:"title"`
	} `json:"title"`
	Type            string `json:"type"`
	PublicationDate *struct {
		Year *struct {
			Value string `json:"value"`
		} `json:"year"`
	} `json:"publication-date"`
	ExternalIDs *struct {
		ExternalID []orcidExternalID `json:"external-id"`
	} `json:"external-ids"`
	PutCode int `json:"put-code"`
}

// orcidRecord covers the fields this adapter reads out of GET
// /v3.0/{id}/record. Every field ORCID publishes that this struct does
// not name is still present in NormalizedRecord.Raw; this struct is a
// read surface, not a schema.
type orcidRecord struct {
	OrcidIdentifier *struct {
		Path string `json:"path"`
		URI  string `json:"uri"`
	} `json:"orcid-identifier"`
	Person *struct {
		Name                *orcidName `json:"name"`
		ExternalIdentifiers *struct {
			ExternalIdentifier []orcidExternalID `json:"external-identifier"`
		} `json:"external-identifiers"`
	} `json:"person"`
	ActivitiesSummary *struct {
		Employments *struct {
			AffiliationGroup []struct {
				Summaries []struct {
					EmploymentSummary *struct {
						Organization *struct {
							Name    string `json:"name"`
							Address *struct {
								City    string `json:"city"`
								Country string `json:"country"`
							} `json:"address"`
							DisambiguatedOrganization *struct {
								ID     string `json:"disambiguated-organization-identifier"`
								Source string `json:"disambiguation-source"`
							} `json:"disambiguated-organization"`
						} `json:"organization"`
					} `json:"employment-summary"`
				} `json:"summaries"`
			} `json:"affiliation-group"`
		} `json:"employments"`
		Works *struct {
			Group []struct {
				WorkSummary []orcidWorkSummary `json:"work-summary"`
				ExternalIDs *struct {
					ExternalID []orcidExternalID `json:"external-id"`
				} `json:"external-ids"`
			} `json:"group"`
		} `json:"works"`
	} `json:"activities-summary"`
}

// orcidSearchResult is one row of GET /v3.0/expanded-search/?q=…. It
// carries only identity fields, never the full record: a search hit is a
// pointer to fetch, not a substitute for it.
type orcidSearchResult struct {
	OrcidID         string   `json:"orcid-id"`
	GivenNames      string   `json:"given-names"`
	FamilyNames     string   `json:"family-names"`
	CreditName      string   `json:"credit-name"`
	OtherName       string   `json:"other-name"`
	InstitutionName []string `json:"institution-name"`
}

type orcidSearchBody struct {
	ExpandedResult []orcidSearchResult `json:"expanded-result"`
	NumFound       int                 `json:"num-found"`
}

// ORCIDRecordNormalizer handles GET /v3.0/{id}/record: a single record,
// no list envelope.
type ORCIDRecordNormalizer struct{}

func (ORCIDRecordNormalizer) Normalize(body []byte) []NormalizedRecord {
	var r orcidRecord
	if err := json.Unmarshal(body, &r); err != nil || r.OrcidIdentifier == nil || r.OrcidIdentifier.Path == "" {
		return nil
	}
	id := r.OrcidIdentifier.Path
	url := firstNonEmpty(r.OrcidIdentifier.URI, "https://orcid.org/"+id)
	return []NormalizedRecord{{ID: id, CanonicalURL: url, Raw: json.RawMessage(body)}}
}

// ORCIDSearchNormalizer handles GET /v3.0/expanded-search/?q=…: a list of
// identity rows under expanded-result.
type ORCIDSearchNormalizer struct{}

func (ORCIDSearchNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b orcidSearchBody
	if err := json.Unmarshal(body, &b); err != nil || len(b.ExpandedResult) == 0 {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(b.ExpandedResult))
	for _, row := range b.ExpandedResult {
		if row.OrcidID == "" {
			continue
		}
		raw, err := json.Marshal(row)
		if err != nil {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           row.OrcidID,
			CanonicalURL: "https://orcid.org/" + row.OrcidID,
			Raw:          raw,
		})
	}
	return recs
}

type orcidSearchPagination struct{}

// PaginationModel reports "offset": expanded-search takes start/rows
// parameters over its result set, per info.orcid.org's search
// documentation.
func (orcidSearchPagination) PaginationModel() string { return "offset" }

type orcidFetchByID struct{}

func (orcidFetchByID) IdentifierSchemes() []string { return []string{"orcid"} }

type orcidIdentity struct{}

func (orcidIdentity) parseRecord(rec NormalizedRecord) (orcidRecord, bool) {
	var r orcidRecord
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

// Identifiers returns the ORCID iD itself plus every external identifier
// ORCID's identity model has a scheme for. ORCID's own external-id-type
// vocabulary (Scopus Author ID, ResearcherID, LinkedIn, GitHub…) has no
// counterpart in identity.Scheme beyond DOI-shaped work identifiers, which
// belong to the person's works rather than the person; those are
// preserved by ExternalIdentifiers below rather than folded in here.
func (oi orcidIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	r, ok := oi.parseRecord(rec)
	if ok && r.OrcidIdentifier != nil && r.OrcidIdentifier.Path != "" {
		return appendID(nil, identity.SchemeORCID, r.OrcidIdentifier.Path)
	}
	// Either the record didn't parse as the full /record shape, or it
	// parsed but carried no orcid-identifier block (a JSON object with no
	// overlapping field names unmarshals without error, leaving every
	// pointer field nil) — either way, fall back to the search-result
	// shape, which carries only an orcid-id but is still worth an
	// identifier.
	var s orcidSearchResult
	if err := json.Unmarshal(rec.Raw, &s); err == nil && s.OrcidID != "" {
		return appendID(nil, identity.SchemeORCID, s.OrcidID)
	}
	return nil
}

// AssertedRelations returns nil: this record's own affiliations are
// organizational facts about the person, not a relation between two
// resolvable graph nodes this gateway can address without a ROR lookup
// the record does not itself provide (an ORCID employment names an
// organization by string and, sometimes, a disambiguated-organization
// identifier from a source other than ROR). Surfacing a guessed ROR edge
// here would be exactly the inference internal/identity.Resolver exists
// to own instead.
func (oi orcidIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

// ExternalPersonIdentifier is one identifier ORCID carries for this
// person under a scheme the identity graph has no node type for (Scopus
// Author ID, ResearcherID, LinkedIn, GitHub, and any type ORCID adds
// later). Mirrors rorIdentity.ExternalOrgIdentifier: preserved verbatim
// rather than dropped, since the identity graph's scheme registry is for
// resolvable cross-provider node identity, not a complete list of what a
// provider publishes.
type ExternalPersonIdentifier struct {
	Type         string `json:"type"`
	Value        string `json:"value"`
	URL          string `json:"url,omitempty"`
	Relationship string `json:"relationship,omitempty"`
}

// ExternalIdentifiers reports every external identifier ORCID published
// on this person, mapped or not.
func (oi orcidIdentity) ExternalIdentifiers(rec NormalizedRecord) []ExternalPersonIdentifier {
	r, ok := oi.parseRecord(rec)
	if !ok || r.Person == nil || r.Person.ExternalIdentifiers == nil {
		return nil
	}
	out := make([]ExternalPersonIdentifier, 0, len(r.Person.ExternalIdentifiers.ExternalIdentifier))
	for _, e := range r.Person.ExternalIdentifiers.ExternalIdentifier {
		ep := ExternalPersonIdentifier{Type: e.Type, Value: e.Value, Relationship: e.Relationship}
		if e.URL != nil {
			ep.URL = e.URL.Value
		}
		out = append(out, ep)
	}
	return out
}

// WorkRef is one entry from the person's works list, kept thin: a title,
// a type, a year, and the external identifiers (DOI foremost) a caller
// cross-references back to the DOI/arXiv-resolved works the gateway's
// other adapters already carry (x402-research-gateway#5).
type WorkRef struct {
	Title       string            `json:"title,omitempty"`
	Type        string            `json:"type,omitempty"`
	Year        string            `json:"year,omitempty"`
	DOI         string            `json:"doi,omitempty"`
	ExternalIDs []orcidExternalID `json:"external_ids,omitempty"`
}

// Works reports the work-summary entries the record's own
// activities-summary carries. Returns nil for a search-result record,
// which has no activities-summary to read.
func (oi orcidIdentity) Works(rec NormalizedRecord) []WorkRef {
	r, ok := oi.parseRecord(rec)
	if !ok || r.ActivitiesSummary == nil || r.ActivitiesSummary.Works == nil {
		return nil
	}
	var out []WorkRef
	for _, group := range r.ActivitiesSummary.Works.Group {
		for _, ws := range group.WorkSummary {
			wr := WorkRef{Type: ws.Type}
			if ws.Title != nil && ws.Title.Title != nil {
				wr.Title = ws.Title.Title.Value
			}
			if ws.PublicationDate != nil && ws.PublicationDate.Year != nil {
				wr.Year = ws.PublicationDate.Year.Value
			}
			if ws.ExternalIDs != nil {
				wr.ExternalIDs = ws.ExternalIDs.ExternalID
				for _, eid := range ws.ExternalIDs.ExternalID {
					if strings.EqualFold(eid.Type, "doi") && wr.DOI == "" {
						wr.DOI = eid.Value
					}
				}
			}
			out = append(out, wr)
		}
	}
	return out
}

func (oi orcidIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := oi.parseRecord(rec)
	if ok && r.Person != nil && r.Person.Name != nil {
		return Descriptor{Authors: []string{orcidDisplayName(r.Person.Name)}}
	}
	var s orcidSearchResult
	if err := json.Unmarshal(rec.Raw, &s); err == nil {
		name := firstNonEmpty(s.CreditName, strings.TrimSpace(s.GivenNames+" "+s.FamilyNames))
		if name != "" {
			return Descriptor{Authors: []string{name}}
		}
	}
	return Descriptor{}
}

func orcidDisplayName(n *orcidName) string {
	if n.CreditName != nil && n.CreditName.Value != "" {
		return n.CreditName.Value
	}
	var given, family string
	if n.GivenNames != nil {
		given = n.GivenNames.Value
	}
	if n.FamilyName != nil {
		family = n.FamilyName.Value
	}
	return strings.TrimSpace(given + " " + family)
}

// RecordRights reports CC0-1.0 for every record: ORCID's public API
// serves only "Everyone"-visibility data, which ORCID's Terms of Use
// dedicate to the public domain under CC0-1.0 (info.orcid.org/terms-of-use,
// verified 2026-08-18) — the same grant ror.go reports for organization
// records.
func (oi orcidIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "orcid (unparseable record)"}
	}
	return Rights{
		License:        "CC0-1.0",
		LicenseURL:     "https://creativecommons.org/publicdomain/zero/1.0/",
		Redistribution: RedistributionAllowed,
		Source:         "orcid:terms-of-use (Everyone-visibility data)",
		FreeToRead:     true,
	}
}

type orcidSync struct{}

// SyncCapability reports neither: ORCID's periodic public-data-file
// export (info.orcid.org, an annual CC0 snapshot) is a distinct product
// this adapter's live routes do not call, and there is no incremental
// change feed under the public API. Recorded in the registry
// (config/providers.yaml) as a note rather than claimed here.
func (orcidSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: false, Incremental: false}
}

// ORCIDFetchAdapter backs route ID "orcid-fetch": GET /v3.0/{id}/record.
var ORCIDFetchAdapter = &Adapter{
	ID:                   "orcid-fetch",
	Description:          "ORCID researcher record by ORCID iD: identity, external identifiers, and works.",
	Fetcher:              orcidFetchByID{},
	Normalizer:           ORCIDRecordNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     orcidIdentity{},
	DescriptorProvider:   orcidIdentity{},
	RecordRightsProvider: orcidIdentity{},
	SyncProvider:         orcidSync{},
}

// ORCIDSearchAdapter backs route ID "orcid-search": GET
// /v3.0/expanded-search/?q=….
var ORCIDSearchAdapter = &Adapter{
	ID:                   "orcid-search",
	Description:          "ORCID researcher search (SOLR syntax) over public identity records.",
	Searcher:             orcidSearchPagination{},
	Normalizer:           ORCIDSearchNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     orcidIdentity{},
	DescriptorProvider:   orcidIdentity{},
	RecordRightsProvider: orcidIdentity{},
	SyncProvider:         orcidSync{},
}
