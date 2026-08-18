// Package provider defines small, independently implementable interfaces
// that separate a research upstream's semantics (how to search it, how to
// fetch a record, how to cite a hit, what capabilities it exposes) from HTTP
// routing, x402 payment, and feed402 enveloping, which stay in
// internal/handler and remain provider-agnostic.
//
// The generic, config-driven proxy path (internal/handler/proxy.go,
// config.UpstreamConfig) is unchanged and stays the cheapest way to add a
// simple REST upstream: a route with no entry in a Registry gets no adapter
// behavior and is served exactly as before x402-research-gateway#2. An
// adapter is what a provider graduates to when it needs normalized
// per-record citations, a non-JSON body a route parser can't express, or
// capability reporting an agent can filter on before paying.
//
// Adding a new adapter is a single new file in this package implementing
// whichever interfaces the provider actually supports, plus one entry in
// registry.go's DefaultRegistry(). No other file needs to change.
package provider

import (
	"encoding/json"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
	"github.com/gianyrox/x402-research-gateway/internal/relation"
)

// Capability names an operation category, mirroring the feed402 SPEC §1.1
// vocabulary (feed402/types.ts CAPABILITIES) so a future capability-
// discovery manifest can report these strings verbatim. Deliberately
// generic: a capability describes what an operation does, never which
// upstream backs it.
type Capability string

const (
	CapSearch             Capability = "search"
	CapFetch              Capability = "fetch"
	CapReferences         Capability = "references"
	CapCitedBy            Capability = "cited_by"
	CapAuthors            Capability = "authors"
	CapInstitutions       Capability = "institutions"
	CapDatasets           Capability = "datasets"
	CapSoftware           Capability = "software"
	CapPatents            Capability = "patents"
	CapVocabulary         Capability = "vocabulary"
	CapFullText           Capability = "full_text"
	CapStructuredFullText Capability = "structured_full_text"
	CapAssets             Capability = "assets"
	CapBulk               Capability = "bulk"
	CapIncrementalSync    Capability = "incremental_sync"
	CapSemanticSearch     Capability = "semantic_search"
	CapFilters            Capability = "filters"
	CapPagination         Capability = "pagination"
	// CapIdentityResolution is an extension capability under feed402 SPEC
	// §1.1's open-vocabulary rule: the spec's closed list has no name for
	// resolving one work's identifiers across providers. An agent that does
	// not know this string degrades it to "an operation I cannot drive,"
	// which the spec requires and which costs it nothing.
	CapIdentityResolution Capability = "identity_resolution"
	// CapRelations is a second extension capability under the same rule:
	// the spec's list names datasets, software, and patents as operation
	// categories but has no name for "the links a provider publishes
	// between research objects" (x402-research-gateway#7).
	CapRelations Capability = "relations"
)

// NormalizedRecord is one upstream result, decoupled from any particular
// wire shape. ID is provider-local and unprefixed; a CitationProvider adds
// the source_prefix. CanonicalURL is optional: when a Normalizer can
// construct the record's stable public address itself (most providers), it
// sets this and GenericCitationProvider uses it verbatim rather than
// templating from route config, which keeps output byte-identical to the
// pre-adapter hardcoded URL logic it replaces. Raw preserves the original
// per-record bytes for an adapter capability that needs more than ID and
// URL (e.g. a future AssetProvider).
type NormalizedRecord struct {
	ID           string
	CanonicalURL string
	Raw          json.RawMessage
}

// Hit is the per-record re-verification handle emitted on search-tier
// feed402 envelopes: {source_id, canonical_url, rank}. Field names match
// the wire shape via the handler package's feed402Hit, which this type
// converts to at the handler/provider boundary.
type Hit struct {
	SourceID     string
	CanonicalURL string
	Rank         int
}

// Searcher declares that a provider supports query-in, result-set-out
// search against its upstream. As of x402-research-gateway#2 the HTTP call
// itself is still made by the shared declarative proxy
// (internal/handler/proxy.go) so behavior, pricing, and headers stay
// byte-identical across the migration; presence of a Searcher lets
// capability discovery report `search` as implemented rather than
// unsupported, and PaginationModel names the provider's own pagination
// scheme. A future revision MAY move the HTTP call itself behind this
// interface for a provider whose request shape declarative config cannot
// express (a GraphQL body, an OAuth-gated call, TAP/ADQL).
type Searcher interface {
	// PaginationModel mirrors feed402 SPEC §1.2's PaginationModel vocabulary:
	// "none", "offset", "page", "cursor", or "token".
	PaginationModel() string
}

// Fetcher declares single-identifier-in, single-record-out support. Same
// staging note as Searcher: proxy.go still makes the call in this revision.
type Fetcher interface {
	// IdentifierSchemes lists the identifier namespaces this fetch accepts,
	// mirroring feed402 SPEC §1.2 OperationSpec.identifier_schemes.
	IdentifierSchemes() []string
}

// Normalizer turns a raw upstream response body into normalized records.
// Implementations must never panic; a body shape they don't recognize is a
// valid "no records," represented as a nil slice, not an error.
type Normalizer interface {
	Normalize(body []byte) []NormalizedRecord
}

// CitationProvider builds the per-record Hit handles a search-tier envelope
// carries alongside its primary citation, replacing the hardcoded per-route
// logic that lived in the handler package's hit_parsers.go before #2.
// GenericCitationProvider (generic_citation.go) covers the common case and
// is what every adapter in this package uses; an adapter overrides it only
// when a provider's identifiers need handling GenericCitationProvider
// cannot express.
type CitationProvider interface {
	Citations(route *config.RouteConfig, records []NormalizedRecord) []Hit
}

// Redistribution is what a rights statement permits. The three values are
// deliberately not a boolean: UNKNOWN is not ALLOWED, and a consumer must
// never be able to read an absent statement as permission.
type Redistribution string

const (
	// RedistributionUnknown means no rights statement was found. It grants
	// nothing. It is the zero value, so a Rights struct nobody filled in
	// permits nothing by construction.
	RedistributionUnknown    Redistribution = "unknown"
	RedistributionAllowed    Redistribution = "allowed"
	RedistributionProhibited Redistribution = "prohibited"
)

// Rights is one rights statement, per record or per representation.
//
// Providers publish rights per record, and a provider-level licence string
// is wrong for most of them: arXiv submissions carry per-submission
// licences from CC0 through the arXiv non-exclusive licence, Europe PMC's
// open-access subset mixes several, and a CC0 DataCite record routinely
// describes an object under some other licence or none. So rights are read
// per record and never inherited from a provider default.
//
// Free to read and open to redistribute are different facts, which is why
// Redistribution is separate from License.
type Rights struct {
	// License is the provider's own licence string or identifier, verbatim.
	License string
	// LicenseURL is the licence document the provider pointed at.
	LicenseURL string
	// Redistribution is what the statement permits. Zero value is unknown.
	Redistribution Redistribution
	// Source names where the statement came from, e.g. the record field it
	// was read out of, so a consumer can audit the claim.
	Source string
	// FreeToRead records that the provider says the content is readable
	// without payment. It says nothing about redistribution.
	FreeToRead bool
}

// Permits reports whether this statement allows redistribution. An unknown
// or absent statement returns false.
func (r Rights) Permits() bool { return r.Redistribution == RedistributionAllowed }

// Asset is the provider-package's echo of feed402 SPEC §3.5's asset model.
type Asset struct {
	AssetID        string
	Representation string
	CanonicalURL   string
	// Rights are the terms on this representation, which differ from the
	// record's metadata rights and, in many cases, from another
	// representation of the same record. An abstract, a PDF, and a TeX
	// source are three representations with three sets of terms.
	Rights Rights
}

// RecordRightsProvider reports the rights on a record's content, read from
// that record rather than from a provider-level default. Optional: an
// adapter that does not implement it reports no statement, which grants
// nothing.
type RecordRightsProvider interface {
	RecordRights(record NormalizedRecord) Rights
}

// AssetProvider reports representation discovery (feed402 SPEC §3.5). The
// defined seam x402-research-gateway#8 (rights-aware asset discovery)
// fills; arXiv, DataCite, Europe PMC, ROR, and Unpaywall implement it.
type AssetProvider interface {
	Assets(record NormalizedRecord) []Asset
}

// ObjectRelationProvider reports the links a provider publishes between
// research objects: a work and its dataset, its software, the trial it
// reports, the correction issued against it (x402-research-gateway#7).
//
// The provider's own relation term is what an implementation must carry;
// internal/relation adds any normalized term. An implementation that drops
// a relation because the gateway has no word for it is wrong, and
// relation.NormalizePredicate exists so it never has to.
//
// Implementations must never panic and must return nil for a body shape
// they do not recognize.
type ObjectRelationProvider interface {
	ObjectRelations(record NormalizedRecord, at time.Time) []relation.Relation
}

// VocabularyProvider reports concept/term lookup for ontology-shaped
// providers. No adapter implements it yet; seam for x402-research-gateway#14.
type VocabularyProvider interface {
	LookupTerm(id string) (NormalizedRecord, bool)
}

// IdentityProvider extracts cross-provider identifiers and provider-asserted
// relations from a record, feeding internal/identity's resolver
// (x402-research-gateway#5).
//
// The split between the two methods is the evidence boundary the identity
// model rests on. Identifiers reports what the provider says this record
// *is*; AssertedRelations reports relations the provider itself published.
// Neither method computes similarity: inference belongs to the resolver, so
// a provider can never smuggle a guess in as a fact.
//
// Implementations must never panic and must return nil for a body shape
// they do not recognize.
type IdentityProvider interface {
	// Identifiers returns every identifier the record carries, including
	// the provider's own. Raw strings are preserved inside each
	// identity.Identifier.
	Identifiers(record NormalizedRecord) []identity.Identifier
	// AssertedRelations returns relations the provider published, built
	// with identity.ProviderAsserted evidence. `at` is the retrieval time
	// stamped onto each relation. nodeID is the resolver's address for this
	// record, so relations can be anchored without the adapter knowing the
	// NodeID construction rule.
	AssertedRelations(nodeID string, record NormalizedRecord, at time.Time) []identity.Relation
}

// Descriptor is the thin bibliographic surface the identity resolver uses
// for similarity inference. Kept separate from NormalizedRecord because it
// is only ever evidence: a provider that cannot supply it disables fuzzy
// matching for its records rather than producing a weaker guess.
type Descriptor struct {
	Title   string
	Authors []string
	Year    int
}

// DescriptorProvider supplies the similarity surface above. Optional, and
// independent of IdentityProvider: a provider can contribute exact
// identifiers without ever contributing similarity evidence.
type DescriptorProvider interface {
	Descriptor(record NormalizedRecord) Descriptor
}

// CitationGraphProvider serves one direction of the citation graph for one
// upstream (x402-research-gateway#6).
//
// One adapter serves one direction. A provider that offers both references
// and cited-by gets two adapters, because the two are different upstream
// calls with different response shapes, and because a provider that offers
// only one must be able to say so rather than silently returning nothing
// for the other.
type CitationGraphProvider interface {
	// Direction is the traversal this adapter serves.
	Direction() citation.Direction
	// EdgeQuery builds the query parameters the gateway sets on the
	// synthetic upstream request for this identifier. ok=false means this
	// provider cannot express a query for that identifier scheme, which is
	// reported as unsupported_identifier and never as an empty result.
	EdgeQuery(id identity.Identifier) (params map[string]string, ok bool)
	// Edges parses an upstream body into normalized edges. `query` is the
	// identifier the traversal started from, needed because most providers
	// return only the far end of each edge. Must never panic; an
	// unrecognized body is nil edges.
	Edges(query identity.Identifier, body []byte, at time.Time) []citation.Edge
	// EdgePagination reports how this provider pages its edge list and, for
	// a given body, whether more edges exist beyond it. The cursor is
	// opaque and provider-defined.
	EdgePagination(body []byte) (model string, truncated bool, nextCursor string)
}

// CoverageReporter states what a provider's collection covers, in one
// sentence a consumer can read beside an edge or result count. Optional: a
// provider that does not implement it reports no coverage statement rather
// than a generated one.
type CoverageReporter interface {
	Coverage() string
}

// SyncCapability is what a SyncProvider reports about bulk/incremental
// access.
type SyncCapability struct {
	Bulk        bool
	Incremental bool
}

// SyncProvider reports bulk and incremental access metadata. No adapter
// implements it yet; seam for x402-research-gateway#11.
type SyncProvider interface {
	SyncCapability() SyncCapability
}

// Adapter is one provider's set of implemented capabilities. Every field is
// optional; a nil field means the provider does not implement that
// capability. Capabilities() computes what's actually present rather than a
// caller guessing from a nil-pointer check, so "unsupported" is always an
// explicit, reportable answer — the design goal x402-research-gateway#2
// states directly: "silence and unsupported must be distinguishable."
type Adapter struct {
	// ID must match the config.RouteConfig.ID this adapter backs.
	ID          string
	Description string

	Searcher               Searcher
	Fetcher                Fetcher
	Normalizer             Normalizer
	CitationProvider       CitationProvider
	AssetProvider          AssetProvider
	ObjectRelationProvider ObjectRelationProvider
	VocabularyProvider     VocabularyProvider
	SyncProvider           SyncProvider
	IdentityProvider       IdentityProvider
	DescriptorProvider     DescriptorProvider

	CitationGraphProvider CitationGraphProvider
}

// Capabilities reports the capability vocabulary this adapter implements,
// computed by presence. Order is stable (declaration order below) so tests
// and manifest output do not flap across runs.
func (a *Adapter) Capabilities() []Capability {
	if a == nil {
		return nil
	}
	var caps []Capability
	if a.Searcher != nil {
		caps = append(caps, CapSearch, CapPagination)
	}
	if a.Fetcher != nil {
		caps = append(caps, CapFetch)
	}
	if a.AssetProvider != nil {
		caps = append(caps, CapAssets)
	}
	if a.ObjectRelationProvider != nil {
		caps = append(caps, CapRelations)
	}
	if a.VocabularyProvider != nil {
		caps = append(caps, CapVocabulary)
	}
	if a.IdentityProvider != nil {
		caps = append(caps, CapIdentityResolution)
	}
	if a.CitationGraphProvider != nil {
		switch a.CitationGraphProvider.Direction() {
		case citation.DirectionReferences:
			caps = append(caps, CapReferences)
		case citation.DirectionCitedBy:
			caps = append(caps, CapCitedBy)
		}
	}
	if a.SyncProvider != nil {
		sc := a.SyncProvider.SyncCapability()
		if sc.Bulk {
			caps = append(caps, CapBulk)
		}
		if sc.Incremental {
			caps = append(caps, CapIncrementalSync)
		}
	}
	return caps
}

// Supports reports whether this adapter implements a given capability. An
// agent — or the gateway's own manifest-building code — uses this instead
// of inferring support from a struct field, so a capability this revision
// has not yet implemented is a clean "not supported," never a nil-pointer
// panic waiting to happen.
func (a *Adapter) Supports(c Capability) bool {
	for _, got := range a.Capabilities() {
		if got == c {
			return true
		}
	}
	return false
}

// Registry maps a config.RouteConfig.ID to the adapter implementing it. A
// route with no entry falls back entirely to the declarative config-driven
// proxy path, which remains fully supported.
type Registry map[string]*Adapter
