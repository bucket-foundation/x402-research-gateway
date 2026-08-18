package identity

import "time"

// RelationType is the typed edge between two nodes in the identity graph.
// The vocabulary is closed in this revision; adding a type is an additive
// change that existing consumers degrade on by treating the unknown type as
// "a relation I do not know how to act on," the same rule feed402 SPEC §2.3
// applies to unknown capabilities.
type RelationType string

const (
	// RelSameWork asserts two nodes describe the same work. Only ever set
	// from an exact identifier match or a provider's own assertion.
	RelSameWork RelationType = "same_work"
	// RelVersionOf points from a version to the work it versions.
	RelVersionOf RelationType = "version_of"
	// RelPreprintOf points from a preprint to its published article.
	RelPreprintOf RelationType = "preprint_of"
	// RelPublishedAs points from a work to its published manifestation.
	RelPublishedAs   RelationType = "published_as"
	RelCorrects      RelationType = "corrects"
	RelRetracts      RelationType = "retracts"
	RelWithdraws     RelationType = "withdraws"
	RelSupplementTo  RelationType = "supplement_to"
	RelTranslationOf RelationType = "translation_of"
	// RelPossibleSameWork is a similarity match the gateway computed. It is
	// evidence. It never becomes RelSameWork; a consumer decides whether to
	// act on it. Enforced by Graph.Add.
	RelPossibleSameWork RelationType = "possible_same_work"

	// Organizational relations (x402-research-gateway#30). An institution's
	// identity is temporal: it merges, splits, and is renamed, and the
	// superseded record stays retrievable rather than being overwritten.
	//
	// RelParentOf points from a parent organization to a child.
	RelParentOf RelationType = "parent_of"
	// RelChildOf points from a child organization to its parent, the
	// inverse of RelParentOf.
	RelChildOf RelationType = "child_of"
	// RelSuccessorOf points from a successor organization to the
	// organization it succeeded.
	RelSuccessorOf RelationType = "successor_of"
	// RelPredecessorOf points from a predecessor organization to the
	// organization that succeeded it, the inverse of RelSuccessorOf.
	RelPredecessorOf RelationType = "predecessor_of"
	// RelRelatedOrg is ROR's own "related" relationship type, for
	// organizational ties that are neither hierarchical nor successional.
	RelRelatedOrg RelationType = "related_org"
)

// EvidenceKind separates a fact a provider published from a fact the
// gateway computed. Keeping these apart at all times is the point of the
// evidence model: an agent must be able to filter to provider-asserted
// relations alone without reading a method string.
type EvidenceKind string

const (
	// EvidenceProviderAsserted means an upstream published this relation,
	// e.g. Crossref carrying a `relation` block, OpenAlex carrying a PMID
	// on a work, arXiv carrying a published DOI.
	EvidenceProviderAsserted EvidenceKind = "provider_asserted"
	// EvidenceGatewayInferred means this gateway derived the relation.
	// Method says how.
	EvidenceGatewayInferred EvidenceKind = "gateway_inferred"
)

// Inference method names, used as Evidence.Method on gateway-inferred
// relations. Open strings; these are the ones this revision emits.
const (
	MethodSharedIdentifier = "shared_exact_identifier"
	MethodArXivVersion     = "arxiv_version_suffix"
	MethodTitleAuthor      = "title_author_similarity"
)

// Evidence records why a relation exists. Exactly one of Provider (for
// provider-asserted) or Method (for gateway-inferred) carries the load;
// Valid enforces that.
type Evidence struct {
	Kind EvidenceKind `json:"kind"`
	// Provider is the upstream that asserted the relation. Required when
	// Kind is EvidenceProviderAsserted, empty otherwise.
	Provider string `json:"provider,omitempty"`
	// Method names the inference. Required when Kind is
	// EvidenceGatewayInferred.
	Method string `json:"method,omitempty"`
	// Detail qualifies Method, e.g. which identifier scheme matched.
	Detail string `json:"detail,omitempty"`
	// Score is the similarity in [0,1] for a computed match. Omitted for
	// exact matches and provider assertions, where a confidence number
	// would be an invention.
	Score float64 `json:"score,omitempty"`
	// RetrievedAt is when the gateway obtained the underlying fact.
	RetrievedAt string `json:"retrieved_at"`
}

// Valid reports whether this evidence is internally consistent.
func (e Evidence) Valid() bool {
	switch e.Kind {
	case EvidenceProviderAsserted:
		return e.Provider != "" && e.Method == ""
	case EvidenceGatewayInferred:
		return e.Method != "" && e.Provider == ""
	default:
		return false
	}
}

// ProviderAsserted builds evidence for a relation an upstream published.
func ProviderAsserted(provider string, at time.Time) Evidence {
	return Evidence{
		Kind:        EvidenceProviderAsserted,
		Provider:    provider,
		RetrievedAt: at.UTC().Format(time.RFC3339),
	}
}

// GatewayInferred builds evidence for a relation this gateway computed.
func GatewayInferred(method, detail string, score float64, at time.Time) Evidence {
	return Evidence{
		Kind:        EvidenceGatewayInferred,
		Method:      method,
		Detail:      detail,
		Score:       score,
		RetrievedAt: at.UTC().Format(time.RFC3339),
	}
}

// Relation is one typed, evidenced edge. From and To are NodeIDs.
type Relation struct {
	From     string       `json:"from"`
	To       string       `json:"to"`
	Type     RelationType `json:"type"`
	Evidence Evidence     `json:"evidence"`
}
