// Package federate merges results from several research providers without
// destroying what any one provider said (x402-research-gateway#4).
//
// Three invariants hold everywhere here.
//
// Provenance survives the merge. Every result keeps the provider that
// produced it, that provider's rank, that provider's score, and that
// provider's raw record. A merged result is never a new anonymous record,
// and an agent can reconstruct each provider's own list exactly by
// filtering on provider and sorting by provider_rank.
//
// Fused order is labeled. When the gateway computes a merged ranking it
// says so, names the method, and leaves every provider's own rank readable
// beside it. An agent that distrusts the fusion ignores it and loses
// nothing.
//
// A provider that failed is reported. Two providers succeeding and one
// timing out returns results plus an explicit statement of which provider
// failed and why. A missing provider would read as a negative result, and
// it is not one.
package federate

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Result is one provider's hit, carried through the merge intact.
type Result struct {
	// Provider is the route id that produced this hit.
	Provider string `json:"provider"`
	// SourceID is the prefixed identifier the provider's own citation
	// logic produced, unchanged.
	SourceID     string `json:"source_id"`
	CanonicalURL string `json:"canonical_url,omitempty"`
	// ProviderRank is this hit's 1-based position in that provider's own
	// result list. It is never rewritten by the merge.
	ProviderRank int `json:"provider_rank"`
	// ProviderScore is the relevance score the provider reported. Omitted
	// when the provider reported none, because a default score would be an
	// invention.
	ProviderScore *float64 `json:"provider_score,omitempty"`
	// FusedRank is the gateway's merged position, 1-based. Present only
	// when a fusion ran.
	FusedRank int `json:"fused_rank,omitempty"`
	// Identifiers are the cross-provider identifiers this record carries,
	// used to surface duplicate candidates.
	Identifiers []identity.Identifier `json:"identifiers,omitempty"`
	// Raw is the provider's original bytes for this record.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// Outcome is what happened when one provider was asked.
type Outcome string

const (
	// OutcomeOK means the provider answered. ResultCount may be zero, which
	// means that provider found nothing for this query.
	OutcomeOK Outcome = "ok"
	// OutcomeUnsupportedCapability means the provider was not asked because
	// it does not declare the requested capability.
	OutcomeUnsupportedCapability Outcome = "unsupported_capability"
	// OutcomeCostCapExceeded means the provider was excluded because adding
	// it would have pushed the fan-out past the caller's cost cap.
	OutcomeCostCapExceeded Outcome = "cost_cap_exceeded"
	OutcomeUpstreamError   Outcome = "upstream_error"
	OutcomeUpstreamStatus  Outcome = "upstream_status"
	OutcomeTimeout         Outcome = "timeout"
)

// ProviderReport is the per-provider account of the fan-out. Every provider
// considered appears, answered or not.
type ProviderReport struct {
	Provider  string  `json:"provider"`
	Consulted bool    `json:"consulted"`
	Outcome   Outcome `json:"outcome"`
	// ResultCount is how many results this provider contributed. Zero with
	// Outcome ok means the provider found nothing, which differs from every
	// other zero in this struct.
	ResultCount    int     `json:"result_count"`
	UpstreamStatus int     `json:"upstream_status,omitempty"`
	LatencyMs      int64   `json:"latency_ms,omitempty"`
	PriceUSD       float64 `json:"price_usd"`
	// Charged reports whether this provider's price counts toward the
	// call's cost. A provider that was never called is not charged.
	Charged bool `json:"charged"`
}

// Fusion labels the gateway's merged ordering. Its presence is what makes
// fused_rank readable; without it every result carries only its provider's
// own rank.
type Fusion struct {
	// Method names the ranking mechanics. `reciprocal_rank_fusion` is the
	// only method this revision implements.
	Method string `json:"method"`
	// K is the reciprocal-rank-fusion constant.
	K int `json:"k"`
	// Note states the limit of what the fused order means.
	Note string `json:"note"`
}

// FusionNote is carried on every fused response. Fusion here is ranking
// mechanics over positions in provider lists. It makes no claim about
// cross-disciplinary relevance and encodes no model of meaning.
const FusionNote = "Fused order is computed from each provider's own rank position " +
	"by reciprocal rank fusion. It is a merge of orderings and carries no claim about " +
	"cross-disciplinary relevance. Sort by provider_rank within a provider to recover " +
	"that provider's list exactly."

// DuplicateCandidate is a set of results from different providers that may
// describe one work. It is surfaced with its evidence and never collapsed:
// two records stay two records, and the caller decides.
type DuplicateCandidate struct {
	// Results are indices into Response.Results, sorted.
	Results  []int             `json:"results"`
	Relation string            `json:"relation"`
	Evidence identity.Evidence `json:"evidence"`
}

// CostLine is one provider's contribution to the price of a fan-out.
type CostLine struct {
	Provider string  `json:"provider"`
	PriceUSD float64 `json:"price_usd"`
	Included bool    `json:"included"`
	// ExcludedBecause names why a provider is out of the fan-out, empty
	// when it is in.
	ExcludedBecause Outcome `json:"excluded_because,omitempty"`
}

// CostEstimate is what a fan-out costs, computable before payment.
type CostEstimate struct {
	Capability string     `json:"capability"`
	Lines      []CostLine `json:"providers"`
	TotalUSD   float64    `json:"total_usd"`
	// CapUSD is the caller's cap, zero when none was set.
	CapUSD float64 `json:"cap_usd,omitempty"`
	// WithinCap is false when providers had to be dropped to fit.
	WithinCap bool `json:"within_cap"`
}

// Response is the whole federated answer.
type Response struct {
	Query      string           `json:"query"`
	Capability string           `json:"capability"`
	Results    []Result         `json:"results"`
	Fusion     *Fusion          `json:"fusion,omitempty"`
	Providers  []ProviderReport `json:"providers"`
	// DuplicateCandidates are surfaced, never applied.
	DuplicateCandidates []DuplicateCandidate `json:"duplicate_candidates,omitempty"`
	Cost                CostEstimate         `json:"cost"`
}

// defaultRRFK is the standard reciprocal-rank-fusion constant. 60 is the
// value from the original Cormack, Clarke, and Buettcher formulation and is
// what makes the fused score insensitive to a single provider's top hit.
const defaultRRFK = 60

// Merge assembles the response: it stamps fused ranks, computes duplicate
// candidates, and orders everything deterministically.
//
// Results arrive grouped by provider and keep their provider ranks. Merge
// adds an ordering; it removes nothing.
func Merge(query, capability string, results []Result, reports []ProviderReport, cost CostEstimate, at time.Time) Response {
	fused := fuse(results, defaultRRFK)

	sort.SliceStable(reports, func(i, j int) bool { return reports[i].Provider < reports[j].Provider })
	if reports == nil {
		reports = []ProviderReport{}
	}
	if fused == nil {
		fused = []Result{}
	}
	resp := Response{
		Query:      query,
		Capability: capability,
		Results:    fused,
		Providers:  reports,
		Cost:       cost,
	}
	if len(fused) > 0 {
		resp.Fusion = &Fusion{Method: "reciprocal_rank_fusion", K: defaultRRFK, Note: FusionNote}
	}
	resp.DuplicateCandidates = Duplicates(fused, at)
	return resp
}

// fuse orders results by reciprocal rank fusion over each provider's own
// rank, stamping FusedRank. Ties break on provider then provider rank, so
// the output is byte-stable across runs.
func fuse(results []Result, k int) []Result {
	if len(results) == 0 {
		return nil
	}
	out := make([]Result, len(results))
	copy(out, results)

	score := func(r Result) float64 {
		if r.ProviderRank < 1 {
			return 0
		}
		return 1 / float64(k+r.ProviderRank)
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := score(out[i]), score(out[j])
		if si != sj {
			return si > sj
		}
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].SourceID < out[j].SourceID
	})
	for i := range out {
		out[i].FusedRank = i + 1
	}
	return out
}

// Duplicates surfaces results from different providers sharing an exact
// identifier. The relation is identity.RelSameWork for an exact match, and
// nothing weaker ever appears here: similarity-based candidates would need
// the descriptor metadata federated search does not carry, and inventing
// them would be the silent merge this whole package refuses.
//
// Results from one provider are never paired with each other.
func Duplicates(results []Result, at time.Time) []DuplicateCandidate {
	byKey := map[string][]int{}
	var keys []string
	for i, r := range results {
		for _, id := range r.Identifiers {
			if id.Value == "" {
				continue
			}
			if _, seen := byKey[id.Key()]; !seen {
				keys = append(keys, id.Key())
			}
			byKey[id.Key()] = append(byKey[id.Key()], i)
		}
	}
	sort.Strings(keys)

	var out []DuplicateCandidate
	emitted := map[string]bool{}
	for _, key := range keys {
		idxs := byKey[key]
		if len(idxs) < 2 {
			continue
		}
		providers := map[string]bool{}
		for _, i := range idxs {
			providers[results[i].Provider] = true
		}
		if len(providers) < 2 {
			continue
		}
		sorted := append([]int(nil), idxs...)
		sort.Ints(sorted)
		fingerprint := fingerprintOf(sorted)
		if emitted[fingerprint] {
			continue
		}
		emitted[fingerprint] = true
		out = append(out, DuplicateCandidate{
			Results:  sorted,
			Relation: string(identity.RelSameWork),
			Evidence: identity.GatewayInferred(
				identity.MethodSharedIdentifier, schemeOf(key), 0, at),
		})
	}
	return out
}

func fingerprintOf(idxs []int) string {
	b := make([]byte, 0, len(idxs)*3)
	for _, i := range idxs {
		b = append(b, byte(i), byte(i>>8), ',')
	}
	return string(b)
}

func schemeOf(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i]
		}
	}
	return key
}

// Estimate computes the cost of a fan-out and applies a cap. Providers are
// added in ascending price order so a cap keeps the most providers it can
// afford rather than whichever happened to sort first by name.
//
// A zero cap means no cap.
func Estimate(capability string, prices map[string]float64, capUSD float64) CostEstimate {
	type entry struct {
		provider string
		price    float64
	}
	entries := make([]entry, 0, len(prices))
	for p, price := range prices {
		entries = append(entries, entry{p, price})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].price != entries[j].price {
			return entries[i].price < entries[j].price
		}
		return entries[i].provider < entries[j].provider
	})

	est := CostEstimate{Capability: capability, CapUSD: capUSD, WithinCap: true}
	total := 0.0
	for _, e := range entries {
		line := CostLine{Provider: e.provider, PriceUSD: e.price, Included: true}
		if capUSD > 0 && total+e.price > capUSD+1e-9 {
			line.Included = false
			line.ExcludedBecause = OutcomeCostCapExceeded
			est.WithinCap = false
		} else {
			total += e.price
		}
		est.Lines = append(est.Lines, line)
	}
	sort.Slice(est.Lines, func(i, j int) bool { return est.Lines[i].Provider < est.Lines[j].Provider })
	est.TotalUSD = round6(total)
	if est.Lines == nil {
		est.Lines = []CostLine{}
	}
	return est
}

// Included lists the providers an estimate keeps, sorted.
func (c CostEstimate) Included() []string {
	var out []string
	for _, l := range c.Lines {
		if l.Included {
			out = append(out, l.Provider)
		}
	}
	sort.Strings(out)
	return out
}

func round6(f float64) float64 { return float64(int64(f*1e6+0.5)) / 1e6 }
