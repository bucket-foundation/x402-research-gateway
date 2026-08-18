package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// ROR adapter (x402-research-gateway#30).
//
// Verified live against api.ror.org on 2026-08-17 (a single polite GET per
// endpoint shape, User-Agent identified, no burst):
//
//	GET https://api.ror.org/v2/organizations?query=…&page=
//	GET https://api.ror.org/v2/organizations/{ror-id-url}
//
// v2 is the current schema; v1 is deprecated. No auth required as of
// 2026-08-17; ROR's documentation states a client ID will be required by
// Q3 2026 (unregistered requests dropping from 2000 req/5min to 50
// req/5min per IP at that point), so the route is a candidate to revisit
// then. Metadata licence is CC0.
//
// Organization identity is temporal: institutions merge, split, and
// rename. A superseded ROR record is never overwritten, and its successor
// is a followable relation rather than a destructive update. status
// (active/inactive/withdrawn) is preserved on the record rather than
// filtered out, so a caller can tell a historical record from a live one.

type rorBody struct {
	Items           []json.RawMessage `json:"items"`
	NumberOfResults int               `json:"number_of_results"`
}

type rorName struct {
	Value string   `json:"value"`
	Lang  string   `json:"lang"`
	Types []string `json:"types"`
}

type rorRecord struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Names       []rorName `json:"names"`
	Types       []string  `json:"types"`
	Established int       `json:"established"`
	ExternalIDs []struct {
		Type      string   `json:"type"`
		All       []string `json:"all"`
		Preferred string   `json:"preferred"`
	} `json:"external_ids"`
	Relationships []struct {
		Label string `json:"label"`
		Type  string `json:"type"`
		ID    string `json:"id"`
	} `json:"relationships"`
	Links []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"links"`
	Admin struct {
		LastModified struct {
			Date          string `json:"date"`
			SchemaVersion string `json:"schema_version"`
		} `json:"last_modified"`
	} `json:"admin"`
}

// RORNormalizer handles both the search-list shape (`items`) and the
// single-record shape (the record itself, no envelope).
type RORNormalizer struct{}

func (RORNormalizer) Normalize(body []byte) []NormalizedRecord {
	var list rorBody
	var items []json.RawMessage
	if err := json.Unmarshal(body, &list); err == nil && len(list.Items) > 0 {
		items = list.Items
	} else {
		items = []json.RawMessage{body}
	}
	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var r rorRecord
		if err := json.Unmarshal(raw, &r); err != nil || r.ID == "" {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           r.ID,
			CanonicalURL: r.ID,
			Raw:          raw,
		})
	}
	return recs
}

type rorPagePagination struct{}

func (rorPagePagination) PaginationModel() string { return "page" }

type rorFetchByID struct{}

func (rorFetchByID) IdentifierSchemes() []string { return []string{"ror"} }

type rorIdentity struct{}

func (rorIdentity) parse(rec NormalizedRecord) (rorRecord, bool) {
	var r rorRecord
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (ri rorIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	r, ok := ri.parse(rec)
	if !ok {
		return nil
	}
	return appendID(nil, identity.SchemeROR, r.ID)
}

// ExternalOrgIdentifier is one identifier ROR carries for this
// organization under a scheme the identity graph has no node type for
// (GRID, ISNI, Wikidata, FundRef, and any type ROR adds later). Preserved
// verbatim rather than folded into identity.Identifier, whose scheme
// registry is for resolvable, cross-provider node identity.
type ExternalOrgIdentifier struct {
	Type      string   `json:"type"`
	All       []string `json:"all"`
	Preferred string   `json:"preferred,omitempty"`
}

// ExternalIdentifiers reports every external identifier ROR published on
// this organization, mapped or not.
func (ri rorIdentity) ExternalIdentifiers(rec NormalizedRecord) []ExternalOrgIdentifier {
	r, ok := ri.parse(rec)
	if !ok {
		return nil
	}
	out := make([]ExternalOrgIdentifier, 0, len(r.ExternalIDs))
	for _, e := range r.ExternalIDs {
		out = append(out, ExternalOrgIdentifier{Type: e.Type, All: e.All, Preferred: e.Preferred})
	}
	return out
}

// NameVariant is one name ROR carries for this organization: the display
// name, an acronym, an alias, or a label in a given language. Reported
// verbatim rather than reduced to one display string, because an
// institution's Japanese label and its English acronym are both facts the
// record published.
type NameVariant struct {
	Value string   `json:"value"`
	Lang  string   `json:"lang,omitempty"`
	Types []string `json:"types"`
}

// NameVariants reports every name ROR published for this organization.
func (ri rorIdentity) NameVariants(rec NormalizedRecord) []NameVariant {
	r, ok := ri.parse(rec)
	if !ok {
		return nil
	}
	out := make([]NameVariant, 0, len(r.Names))
	for _, n := range r.Names {
		out = append(out, NameVariant{Value: n.Value, Lang: n.Lang, Types: n.Types})
	}
	return out
}

// AssertedRelations surfaces ROR's own relationships block with
// provider-asserted evidence. The direction follows the relation's own
// meaning: this record is the parent when ROR labels the far side "child",
// and the successor when ROR labels the far side "predecessor".
func (ri rorIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	r, ok := ri.parse(rec)
	if !ok {
		return nil
	}
	ev := identity.ProviderAsserted("ror", at)
	var out []identity.Relation
	for _, rel := range r.Relationships {
		if rel.ID == "" {
			continue
		}
		// ROR's relationship.type names what the FAR side is to THIS
		// record: a far side labeled "parent" means this record is the
		// child, so the edge direction is the inverse of a naive read.
		var typ identity.RelationType
		switch strings.ToLower(strings.TrimSpace(rel.Type)) {
		case "parent":
			typ = identity.RelChildOf
		case "child":
			typ = identity.RelParentOf
		case "successor":
			typ = identity.RelPredecessorOf
		case "predecessor":
			typ = identity.RelSuccessorOf
		case "related":
			typ = identity.RelRelatedOrg
		default:
			continue
		}
		out = append(out, identity.Relation{From: nodeID, To: rel.ID, Type: typ, Evidence: ev})
	}
	return out
}

// RecordRights reports the CC0 metadata licence ROR publishes for every
// record. Unlike DataCite or arXiv, ROR has no per-record content to
// diverge from its metadata: the organizational record is the content.
func (rorIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "ror (unparseable record)"}
	}
	return Rights{
		License:        "CC0-1.0",
		LicenseURL:     "https://creativecommons.org/publicdomain/zero/1.0/",
		Redistribution: RedistributionAllowed,
		Source:         "ror:metadata-license",
		FreeToRead:     true,
	}
}

type rorSync struct{}

// SyncCapability reports incremental only. ROR publishes versioned data
// dumps (data.ror.org) on a periodic release cadence rather than a
// scheduled one, so it is recorded in the registry rather than claimed as
// a scheduled bulk capability this adapter can exercise.
func (rorSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: false, Incremental: true}
}

// RORSearchAdapter backs route ID "ror-search".
var RORSearchAdapter = &Adapter{
	ID:               "ror-search",
	Description:      "ROR search over research organizations, institutions, and funders.",
	Searcher:         rorPagePagination{},
	Normalizer:       RORNormalizer{},
	CitationProvider: GenericCitationProvider{},
	IdentityProvider: rorIdentity{},
	SyncProvider:     rorSync{},
}

// RORFetchAdapter backs route ID "ror-fetch".
var RORFetchAdapter = &Adapter{
	ID:               "ror-fetch",
	Description:      "ROR organization record by ROR ID.",
	Fetcher:          rorFetchByID{},
	Normalizer:       RORNormalizer{},
	CitationProvider: GenericCitationProvider{},
	IdentityProvider: rorIdentity{},
	SyncProvider:     rorSync{},
}
