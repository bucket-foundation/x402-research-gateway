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

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
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

// Asset is the provider-package's minimal echo of feed402 SPEC §3.5's asset
// model — just enough for AssetProvider to have a concrete return type
// ahead of x402-research-gateway#8, which will extend this.
type Asset struct {
	AssetID        string
	Representation string
	CanonicalURL   string
}

// AssetProvider reports representation discovery (feed402 SPEC §3.5). No
// adapter in this package implements it yet; the interface exists as the
// defined seam x402-research-gateway#8 (rights-aware asset discovery) fills.
type AssetProvider interface {
	Assets(record NormalizedRecord) []Asset
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

	Searcher           Searcher
	Fetcher            Fetcher
	Normalizer         Normalizer
	CitationProvider   CitationProvider
	AssetProvider      AssetProvider
	VocabularyProvider VocabularyProvider
	SyncProvider       SyncProvider
	IdentityProvider   IdentityProvider
	DescriptorProvider DescriptorProvider
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
	if a.VocabularyProvider != nil {
		caps = append(caps, CapVocabulary)
	}
	if a.IdentityProvider != nil {
		caps = append(caps, CapIdentityResolution)
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
