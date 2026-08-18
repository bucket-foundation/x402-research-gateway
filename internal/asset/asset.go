// Package asset answers one question: what representations of this work are
// legally discoverable, and where (x402-research-gateway#8).
//
// It discovers locations. It never fetches, mirrors, caches, or re-serves
// content, under any configuration. The gateway holds no distribution right
// it was not granted per representation, and a location a provider
// published is a locator rather than a grant.
//
// Four rules hold everywhere here.
//
// Unknown is never allowed. A representation whose rights the gateway could
// not establish carries Redistribution "unknown", which permits nothing.
// Permits() returns true only for an explicit allowance, and a test asserts
// no construction path can produce a permissive unknown.
//
// Metadata rights and content rights are separate. The common case is a CC0
// metadata record pointing at an all-rights-reserved PDF, so a Set carries
// both statements and never lets one stand in for the other.
//
// Absence is an answer. "No open-access copy found" is a result, reported as
// availability "absent" with the providers that were asked, rather than as
// an empty list a consumer could read as "not checked."
//
// Every asset says who published it and when.
package asset

import (
	"sort"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Redistribution is what a rights statement permits. Mirrors
// provider.Redistribution, restated here so this package's wire model does
// not depend on the adapter layer.
type Redistribution string

const (
	// RedistributionUnknown grants nothing. It is the zero value, so a
	// rights block nobody filled in permits nothing by construction.
	RedistributionUnknown    Redistribution = "unknown"
	RedistributionAllowed    Redistribution = "allowed"
	RedistributionProhibited Redistribution = "prohibited"
)

// Rights is one rights statement, per record or per representation.
type Rights struct {
	License        string         `json:"license,omitempty"`
	LicenseURL     string         `json:"license_url,omitempty"`
	Redistribution Redistribution `json:"redistribution"`
	// Source names the upstream field the statement was read out of, so a
	// consumer can audit the claim.
	Source string `json:"source,omitempty"`
	// FreeToRead records that the provider says the content is readable
	// without payment. It says nothing about redistribution.
	FreeToRead bool `json:"free_to_read"`
	// TermsReadOn is the date a human last read this provider's terms
	// (YYYY-MM-DD), carried from the registry. Empty means nobody has.
	TermsReadOn string `json:"terms_read_on,omitempty"`
	// TermsURL is the authoritative terms document for the provider.
	TermsURL string `json:"terms_url,omitempty"`
}

// Permits reports whether this statement allows redistribution. Unknown,
// absent, and prohibited all return false.
func (r Rights) Permits() bool { return r.Redistribution == RedistributionAllowed }

// Normalize fills the zero value with the explicit unknown, so a rights
// block that reaches a consumer always states its posture rather than
// leaving the field empty.
func (r Rights) Normalize() Rights {
	if r.Redistribution == "" {
		r.Redistribution = RedistributionUnknown
	}
	if r.Redistribution != RedistributionAllowed &&
		r.Redistribution != RedistributionProhibited &&
		r.Redistribution != RedistributionUnknown {
		// A value from nowhere is not a grant. It is recorded in Source and
		// the posture falls back to unknown.
		r.Source = strings.TrimSpace(r.Source + " (unrecognized redistribution value " +
			string(r.Redistribution) + "; treated as unknown)")
		r.Redistribution = RedistributionUnknown
	}
	return r
}

// Availability distinguishes three answers that are all useful and all
// different.
type Availability string

const (
	// AvailabilityRetrievable means at least one representation is
	// reachable at a published location.
	AvailabilityRetrievable Availability = "retrievable"
	// AvailabilityRestricted means the work is known to exist in a
	// representation the consulted providers could not hand over: a
	// provider marked it open access and published no location, or the
	// only representations are behind publisher access control.
	AvailabilityRestricted Availability = "restricted"
	// AvailabilityAbsent means no consulted provider published any
	// discoverable representation. It is a real answer.
	AvailabilityAbsent Availability = "absent"
	// AvailabilityUnknown means no provider answered at all, so nothing is
	// known either way. Distinct from absent, which is a negative result
	// from providers that did answer.
	AvailabilityUnknown Availability = "unknown"
)

// Asset is one discoverable representation of one work.
type Asset struct {
	// Provider is the route/adapter id that published this location.
	Provider string `json:"provider"`
	AssetID  string `json:"asset_id"`
	// Representation is the media type plus whatever role, version, and
	// host qualifiers the provider supplied, in the provider's own terms.
	Representation string `json:"representation"`
	// CanonicalURL is the location the provider published. The gateway does
	// not dereference it.
	CanonicalURL string `json:"canonical_url"`
	// Rights are the terms on this representation. They are never inherited
	// from the provider's metadata licence.
	Rights Rights `json:"rights"`
	// Availability is this representation's own posture.
	Availability Availability `json:"availability"`
	RetrievedAt  string       `json:"retrieved_at"`
}

// Outcome is what happened when one provider was consulted.
type Outcome string

const (
	// OutcomeOK means the provider answered. AssetCount may be zero, which
	// means that provider published no representation for this work.
	OutcomeOK Outcome = "ok"
	// OutcomeUnsupportedIdentifier means the provider cannot express a
	// lookup for the caller's identifier scheme.
	OutcomeUnsupportedIdentifier Outcome = "unsupported_identifier"
	// OutcomeNotConfigured means the provider's route is not configured on
	// this deployment, e.g. an upstream whose access key the operator has
	// not supplied.
	OutcomeNotConfigured  Outcome = "not_configured"
	OutcomeUpstreamError  Outcome = "upstream_error"
	OutcomeUpstreamStatus Outcome = "upstream_status"
	OutcomeTimeout        Outcome = "timeout"
)

// ProviderReport is the per-provider account every response carries.
type ProviderReport struct {
	Provider  string  `json:"provider"`
	Consulted bool    `json:"consulted"`
	Outcome   Outcome `json:"outcome"`
	// AssetCount is how many representations this provider published. Zero
	// with Outcome ok is a negative answer from that provider.
	AssetCount     int `json:"asset_count"`
	UpstreamStatus int `json:"upstream_status,omitempty"`
	// MetadataRights is the provider's licence over the records it serves.
	// It is stated apart from every asset's content rights, because the two
	// routinely differ and one never implies the other.
	MetadataRights Rights `json:"metadata_rights"`
}

// DiscoveryNotice is emitted verbatim in every response.
const DiscoveryNotice = "This is location discovery. The gateway does not fetch, mirror, cache, or " +
	"re-serve content, and a published location is not a grant. Rights are stated per representation " +
	"and unknown never means permitted. metadata_rights covers the provider's records and says nothing " +
	"about the content at a location. availability absent means the consulted providers published no " +
	"discoverable representation, which is a result rather than a failure to look."

// Set is one asset-discovery query's full answer.
type Set struct {
	// Query is the identifier the discovery started from.
	Query identity.Identifier `json:"query"`
	// Availability is the work-level answer, derived from the per-asset
	// postures and the provider reports.
	Availability Availability `json:"availability"`
	Assets       []Asset      `json:"assets"`
	// ProvidersConsulted covers every provider considered, answered or not.
	ProvidersConsulted []ProviderReport `json:"providers_consulted"`
	// OpenAccessCopyFound restates the negative answer as its own field, so
	// a consumer never has to infer it from an empty array.
	OpenAccessCopyFound bool   `json:"open_access_copy_found"`
	DiscoveryNotice     string `json:"discovery_notice"`
}

// Build assembles a Set. Assets are sorted deterministically and never
// merged across providers: two providers publishing the same location are
// two assertions, and collapsing them would hide which provider said what.
func Build(query identity.Identifier, assets []Asset, reports []ProviderReport, at time.Time) Set {
	stamped := make([]Asset, 0, len(assets))
	for _, a := range assets {
		a.Rights = a.Rights.Normalize()
		if a.Availability == "" {
			a.Availability = AvailabilityRetrievable
		}
		if a.RetrievedAt == "" {
			a.RetrievedAt = at.UTC().Format(time.RFC3339)
		}
		stamped = append(stamped, a)
	}
	sort.SliceStable(stamped, func(i, j int) bool {
		if stamped[i].Provider != stamped[j].Provider {
			return stamped[i].Provider < stamped[j].Provider
		}
		return stamped[i].AssetID < stamped[j].AssetID
	})
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].Provider < reports[j].Provider })
	for i := range reports {
		reports[i].MetadataRights = reports[i].MetadataRights.Normalize()
	}
	if reports == nil {
		reports = []ProviderReport{}
	}

	return Set{
		Query:               query,
		Availability:        availabilityOf(stamped, reports),
		Assets:              stamped,
		ProvidersConsulted:  reports,
		OpenAccessCopyFound: openAccessFound(stamped),
		DiscoveryNotice:     DiscoveryNotice,
	}
}

// availabilityOf derives the work-level answer. Retrievable wins over
// restricted, restricted over absent, and a set where no provider answered
// at all is unknown rather than absent.
func availabilityOf(assets []Asset, reports []ProviderReport) Availability {
	for _, a := range assets {
		if a.Availability == AvailabilityRetrievable {
			return AvailabilityRetrievable
		}
	}
	for _, a := range assets {
		if a.Availability == AvailabilityRestricted {
			return AvailabilityRestricted
		}
	}
	answered := false
	for _, r := range reports {
		if r.Outcome == OutcomeOK {
			answered = true
			break
		}
	}
	if !answered {
		return AvailabilityUnknown
	}
	return AvailabilityAbsent
}

// openAccessFound reports whether any retrievable representation says it is
// free to read. A retrievable location whose provider said nothing about
// free-to-read does not count, because that is not what the provider said.
func openAccessFound(assets []Asset) bool {
	for _, a := range assets {
		if a.Availability == AvailabilityRetrievable && a.Rights.FreeToRead {
			return true
		}
	}
	return false
}
