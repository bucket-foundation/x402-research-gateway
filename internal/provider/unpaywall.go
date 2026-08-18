package provider

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Unpaywall adapter (x402-research-gateway#28).
//
// Verified against unpaywall.org/products/api on 2026-08-17:
//
//	GET https://api.unpaywall.org/v2/{doi}?email=…
//
// The email parameter is a caller-identifying courtesy header Unpaywall's
// terms require, never a credential. It is a route-level query parameter set by
// the operator (config/routes.yaml), never something this adapter reads
// from or writes into a record: Unpaywall's response body does not echo it
// back, and nothing in this file touches it, so it cannot leak into a
// fingerprint, a cursor, or a citation.
//
// No API key. Published rate guidance is 100,000 requests per day.
// Coverage is DOI-only: there is no search endpoint, only a per-DOI
// lookup, so this adapter implements Fetcher and not Searcher.
//
// Unpaywall reports locations and the licence each one states. It does
// not grant rights, and a bronze open-access location is readable
// without being redistributable: `oa_status` is Unpaywall's own coverage
// classification across gold/hybrid/bronze/green; it is never treated as a licence, so
// RecordRights never reads it. Only a location's own `license` field
// becomes a redistribution grant, and a location with no `license` value
// reports unknown even when it is the best OA location.

type unpaywallLocation struct {
	URL                   string `json:"url"`
	URLForPDF             string `json:"url_for_pdf"`
	URLForLanding         string `json:"url_for_landing_page"`
	HostType              string `json:"host_type"`
	Version               string `json:"version"`
	License               string `json:"license"`
	IsBest                bool   `json:"is_best"`
	RepositoryInstitution string `json:"repository_institution"`
}

type unpaywallBody struct {
	DOI            string              `json:"doi"`
	DOIURL         string              `json:"doi_url"`
	Title          string              `json:"title"`
	Genre          string              `json:"genre"`
	IsOA           bool                `json:"is_oa"`
	OAStatus       string              `json:"oa_status"`
	PublishedDate  string              `json:"published_date"`
	Year           int                 `json:"year"`
	JournalName    string              `json:"journal_name"`
	Publisher      string              `json:"publisher"`
	BestOALocation *unpaywallLocation  `json:"best_oa_location"`
	OALocations    []unpaywallLocation `json:"oa_locations"`
	ZAuthors       []struct {
		Given  string `json:"given"`
		Family string `json:"family"`
	} `json:"z_authors"`
}

// UnpaywallNormalizer parses the single-DOI lookup response. There is no
// list shape: one request answers one DOI.
type UnpaywallNormalizer struct{}

func (UnpaywallNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b unpaywallBody
	if err := json.Unmarshal(body, &b); err != nil || b.DOI == "" {
		return nil
	}
	return []NormalizedRecord{{
		ID:           b.DOI,
		CanonicalURL: firstNonEmpty(b.DOIURL, "https://doi.org/"+b.DOI),
		Raw:          json.RawMessage(body),
	}}
}

type unpaywallFetchByDOI struct{}

func (unpaywallFetchByDOI) IdentifierSchemes() []string { return []string{"doi"} }

type unpaywallIdentity struct{}

func (unpaywallIdentity) parse(rec NormalizedRecord) (unpaywallBody, bool) {
	var b unpaywallBody
	if len(rec.Raw) == 0 {
		return b, false
	}
	if err := json.Unmarshal(rec.Raw, &b); err != nil {
		return b, false
	}
	return b, true
}

func (u unpaywallIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	b, ok := u.parse(rec)
	if !ok {
		return nil
	}
	return appendID(nil, identity.SchemeDOI, b.DOI)
}

// AssertedRelations returns nil: Unpaywall asserts no relations between
// works, only open-access locations for the one DOI it was asked about.
func (u unpaywallIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

func (u unpaywallIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	b, ok := u.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: b.Title, Year: b.Year}
	for _, a := range b.ZAuthors {
		name := strings.TrimSpace(strings.TrimSpace(a.Given) + " " + strings.TrimSpace(a.Family))
		if name != "" {
			d.Authors = append(d.Authors, name)
		}
	}
	return d
}

// Availability distinguishes retrievable, restricted, and absent rather
// than collapsing a DOI with no open-access copy into an empty asset
// list that could read as "not checked." retrievable: at least one OA
// location exists. restricted: Unpaywall marked the work open access but
// published no location for it, an inconsistency the caller should see
// rather than one this adapter papers over. absent: Unpaywall found no
// open-access copy.
type Availability string

const (
	AvailabilityRetrievable Availability = "retrievable"
	AvailabilityRestricted  Availability = "restricted"
	AvailabilityAbsent      Availability = "absent"
)

func (u unpaywallIdentity) Availability(rec NormalizedRecord) Availability {
	b, ok := u.parse(rec)
	if !ok {
		return AvailabilityAbsent
	}
	if !b.IsOA {
		return AvailabilityAbsent
	}
	if len(b.OALocations) == 0 {
		return AvailabilityRestricted
	}
	return AvailabilityRetrievable
}

// RecordRights reports unknown at the record level, always. oa_status is
// Unpaywall's own coverage classification (gold/hybrid/bronze/green), not
// a licence a consumer can act on, so it never becomes a redistribution
// grant. Per-location rights are what matters, and they live on Assets.
func (u unpaywallIdentity) RecordRights(rec NormalizedRecord) Rights {
	if _, ok := u.parse(rec); !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "unpaywall (unparseable record)"}
	}
	return Rights{
		Redistribution: RedistributionUnknown,
		Source:         "unpaywall:oa_status is a coverage classification rather than a licence; see per-asset rights",
	}
}

func unpaywallLocationRights(loc unpaywallLocation) Rights {
	if loc.License == "" {
		return Rights{
			Redistribution: RedistributionUnknown,
			Source:         "unpaywall:location.license (absent)",
			FreeToRead:     true,
		}
	}
	rights := Rights{
		License:        loc.License,
		Redistribution: RedistributionUnknown,
		Source:         "unpaywall:location.license",
		FreeToRead:     true,
	}
	l := strings.ToLower(loc.License)
	if strings.HasPrefix(l, "cc-") || strings.Contains(l, "cc0") || strings.Contains(l, "public-domain") {
		rights.Redistribution = RedistributionAllowed
	}
	return rights
}

// Assets reports every open-access location Unpaywall published as a
// distinct representation, each with its own URL, host type, version,
// and rights. A location is a locator Unpaywall found; this gateway makes
// no grant of its own, and rights come from that location's own license
// field.
func (u unpaywallIdentity) Assets(rec NormalizedRecord) []Asset {
	b, ok := u.parse(rec)
	if !ok {
		return nil
	}
	var out []Asset
	for i, loc := range b.OALocations {
		url := firstNonEmpty(loc.URLForPDF, loc.URL, loc.URLForLanding)
		if url == "" {
			continue
		}
		rep := "unspecified"
		if loc.URLForPDF != "" && url == loc.URLForPDF {
			rep = "application/pdf"
		} else {
			rep = "text/html"
		}
		rep += "; host=" + firstNonEmpty(loc.HostType, "unspecified")
		rep += "; version=" + firstNonEmpty(loc.Version, "unspecified")
		if loc.IsBest {
			rep += "; role=best-oa-location"
		}
		out = append(out, Asset{
			AssetID:        "unpaywall:" + b.DOI + "#location-" + strconv.Itoa(i),
			Representation: rep,
			CanonicalURL:   url,
			Rights:         unpaywallLocationRights(loc),
		})
	}
	return out
}

type unpaywallSync struct{}

// SyncCapability reports neither: there is no bulk export in the free API
// tier, and there is no incremental cursor for it either, only per-DOI
// lookup. Unpaywall's snapshot and data-feed products exist under
// separate commercial terms this gateway does not subscribe to.
func (unpaywallSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: false, Incremental: false}
}

// UnpaywallFetchAdapter backs route ID "unpaywall-fetch". Unpaywall has no
// search endpoint, so this is the only adapter for the provider.
var UnpaywallFetchAdapter = &Adapter{
	ID:                   "unpaywall-fetch",
	Description:          "Unpaywall open-access location lookup by DOI.",
	Fetcher:              unpaywallFetchByDOI{},
	Normalizer:           UnpaywallNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     unpaywallIdentity{},
	DescriptorProvider:   unpaywallIdentity{},
	AssetProvider:        unpaywallIdentity{},
	RecordRightsProvider: unpaywallIdentity{},
	AvailabilityReporter: unpaywallIdentity{},
	SyncProvider:         unpaywallSync{},
}
