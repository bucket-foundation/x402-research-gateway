package federate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

var at = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func mustDOI(t *testing.T, raw string) identity.Identifier {
	t.Helper()
	id, ok := identity.New(identity.SchemeDOI, raw)
	if !ok {
		t.Fatalf("fixture DOI %q rejected", raw)
	}
	return id
}

func score(f float64) *float64 { return &f }

// The merge adds an ordering. Every provider's own list stays exactly
// recoverable by filtering on provider and sorting by provider_rank.
func TestMerge_ProviderListsRecoverableExactly(t *testing.T) {
	results := []Result{
		{Provider: "pubmed-search", SourceID: "pubmed:1", ProviderRank: 1, Raw: json.RawMessage(`{"a":1}`)},
		{Provider: "pubmed-search", SourceID: "pubmed:2", ProviderRank: 2},
		{Provider: "pubmed-search", SourceID: "pubmed:3", ProviderRank: 3},
		{Provider: "openalex-works", SourceID: "openalex:W1", ProviderRank: 1, ProviderScore: score(0.91)},
		{Provider: "openalex-works", SourceID: "openalex:W2", ProviderRank: 2, ProviderScore: score(0.55)},
	}
	resp := Merge("photosynthesis", "search", results, nil, CostEstimate{}, at)

	if len(resp.Results) != 5 {
		t.Fatalf("the merge must not drop results, got %d", len(resp.Results))
	}
	for _, want := range []struct {
		provider string
		order    []string
	}{
		{"pubmed-search", []string{"pubmed:1", "pubmed:2", "pubmed:3"}},
		{"openalex-works", []string{"openalex:W1", "openalex:W2"}},
	} {
		var got []string
		lastRank := 0
		for _, r := range resp.Results {
			if r.Provider != want.provider {
				continue
			}
			if r.ProviderRank <= lastRank {
				t.Errorf("%s: provider ranks out of order in the merged list", want.provider)
			}
			lastRank = r.ProviderRank
			got = append(got, r.SourceID)
		}
		if strings.Join(got, ",") != strings.Join(want.order, ",") {
			t.Errorf("%s list = %v, want %v", want.provider, got, want.order)
		}
	}
	// Scores and raw records survive.
	for _, r := range resp.Results {
		if r.SourceID == "openalex:W1" && (r.ProviderScore == nil || *r.ProviderScore != 0.91) {
			t.Errorf("provider score lost: %+v", r)
		}
		if r.SourceID == "pubmed:1" && string(r.Raw) != `{"a":1}` {
			t.Errorf("raw record lost: %+v", r)
		}
		// A provider reporting no score carries none rather than a zero.
		if r.SourceID == "pubmed:2" && r.ProviderScore != nil {
			t.Errorf("a missing score must stay missing, got %v", *r.ProviderScore)
		}
	}
}

// Fused order is labeled and separable from provider order.
func TestMerge_FusionIsLabeledAndSeparable(t *testing.T) {
	results := []Result{
		{Provider: "b", SourceID: "b:1", ProviderRank: 1},
		{Provider: "a", SourceID: "a:1", ProviderRank: 1},
		{Provider: "a", SourceID: "a:2", ProviderRank: 2},
	}
	resp := Merge("q", "search", results, nil, CostEstimate{}, at)

	if resp.Fusion == nil {
		t.Fatal("a fused response must label its fusion")
	}
	if resp.Fusion.Method != "reciprocal_rank_fusion" || resp.Fusion.K != defaultRRFK {
		t.Errorf("fusion = %+v", resp.Fusion)
	}
	if resp.Fusion.Note != FusionNote {
		t.Error("the fusion must state the limit of what its order means")
	}
	// Both rank-1 hits outrank the rank-2 hit, and every result carries
	// both its fused position and its provider's own.
	for i, r := range resp.Results {
		if r.FusedRank != i+1 {
			t.Errorf("fused_rank must be the merged position, got %d at index %d", r.FusedRank, i)
		}
		if r.ProviderRank == 0 {
			t.Errorf("provider rank erased on %s", r.SourceID)
		}
	}
	if resp.Results[2].SourceID != "a:2" {
		t.Errorf("the rank-2 hit should fuse last, got %s", resp.Results[2].SourceID)
	}

	// An empty result set carries no fusion label, because nothing was
	// fused.
	if empty := Merge("q", "search", nil, nil, CostEstimate{}, at); empty.Fusion != nil {
		t.Error("an empty result set must not claim a fusion")
	}
}

func TestMerge_Deterministic(t *testing.T) {
	results := []Result{
		{Provider: "c", SourceID: "c:1", ProviderRank: 1},
		{Provider: "a", SourceID: "a:1", ProviderRank: 1},
		{Provider: "b", SourceID: "b:1", ProviderRank: 1},
		{Provider: "a", SourceID: "a:2", ProviderRank: 2},
	}
	reports := []ProviderReport{{Provider: "c"}, {Provider: "a"}, {Provider: "b"}}
	first, _ := json.Marshal(Merge("q", "search", results, reports, CostEstimate{}, at))
	for i := 0; i < 20; i++ {
		next, _ := json.Marshal(Merge("q", "search", results, reports, CostEstimate{}, at))
		if string(first) != string(next) {
			t.Fatal("merge output is not deterministic")
		}
	}
}

// Duplicates are surfaced with evidence, never applied.
func TestDuplicates_SurfacedNotMerged(t *testing.T) {
	shared := mustDOI(t, "10.7717/peerj.4375")
	results := []Result{
		{Provider: "openalex-works", SourceID: "openalex:W1", ProviderRank: 1, Identifiers: []identity.Identifier{shared}},
		{Provider: "semantic-scholar-search", SourceID: "s2:abc", ProviderRank: 1, Identifiers: []identity.Identifier{shared}},
		{Provider: "pubmed-search", SourceID: "pubmed:9", ProviderRank: 1, Identifiers: []identity.Identifier{mustDOI(t, "10.1234/other")}},
	}
	resp := Merge("q", "search", results, nil, CostEstimate{}, at)

	if len(resp.Results) != 3 {
		t.Fatalf("duplicates must not be collapsed, got %d results", len(resp.Results))
	}
	if len(resp.DuplicateCandidates) != 1 {
		t.Fatalf("expected one duplicate candidate, got %+v", resp.DuplicateCandidates)
	}
	c := resp.DuplicateCandidates[0]
	if len(c.Results) != 2 {
		t.Errorf("the candidate should name both results, got %v", c.Results)
	}
	if c.Evidence.Kind != identity.EvidenceGatewayInferred ||
		c.Evidence.Method != identity.MethodSharedIdentifier ||
		c.Evidence.Detail != string(identity.SchemeDOI) {
		t.Errorf("duplicate evidence = %+v", c.Evidence)
	}
	if c.Evidence.RetrievedAt == "" {
		t.Error("duplicate candidate must carry a timestamp")
	}
}

// One provider returning the same identifier twice is not a cross-provider
// duplicate.
func TestDuplicates_SingleProviderNeverPaired(t *testing.T) {
	shared := mustDOI(t, "10.1234/x")
	results := []Result{
		{Provider: "a", SourceID: "a:1", ProviderRank: 1, Identifiers: []identity.Identifier{shared}},
		{Provider: "a", SourceID: "a:2", ProviderRank: 2, Identifiers: []identity.Identifier{shared}},
	}
	if got := Duplicates(results, at); len(got) != 0 {
		t.Errorf("results from one provider must not be paired, got %+v", got)
	}
}

// Cost is computable before payment, and a cap drops providers explicitly.
func TestEstimate_CapDropsProvidersExplicitly(t *testing.T) {
	prices := map[string]float64{
		"pubmed-search":           0.001,
		"openalex-works":          0.001,
		"semantic-scholar-search": 0.002,
		"clinicaltrials-search":   0.001,
	}
	full := Estimate("search", prices, 0)
	if full.TotalUSD != 0.005 {
		t.Errorf("uncapped total = %v, want 0.005", full.TotalUSD)
	}
	if !full.WithinCap || len(full.Included()) != 4 {
		t.Errorf("an uncapped estimate must include everything, got %+v", full)
	}

	capped := Estimate("search", prices, 0.003)
	if capped.TotalUSD > 0.003+1e-9 {
		t.Errorf("capped total %v exceeds the cap", capped.TotalUSD)
	}
	if capped.WithinCap {
		t.Error("an estimate that dropped providers must not report within_cap")
	}
	// The cap keeps the cheapest providers, so it buys the most coverage
	// it can afford.
	included := capped.Included()
	if len(included) != 3 {
		t.Fatalf("cap of 0.003 over three 0.001 providers should keep three, got %v", included)
	}
	for _, p := range included {
		if p == "semantic-scholar-search" {
			t.Error("the most expensive provider should be the one dropped")
		}
	}
	// Every provider appears in the lines, dropped ones with a reason.
	if len(capped.Lines) != 4 {
		t.Fatalf("every candidate must be listed, got %+v", capped.Lines)
	}
	for _, l := range capped.Lines {
		if l.Included == (l.ExcludedBecause != "") {
			t.Errorf("line %+v must carry a reason exactly when excluded", l)
		}
		if !l.Included && l.ExcludedBecause != OutcomeCostCapExceeded {
			t.Errorf("wrong exclusion reason: %+v", l)
		}
	}
	// Lines are sorted so the estimate serializes identically across runs.
	for i := 1; i < len(capped.Lines); i++ {
		if capped.Lines[i-1].Provider > capped.Lines[i].Provider {
			t.Fatal("cost lines must be sorted by provider")
		}
	}
}

func TestEstimate_EmptyProviderSet(t *testing.T) {
	est := Estimate("cited_by", nil, 0.01)
	if est.TotalUSD != 0 || !est.WithinCap {
		t.Errorf("an empty fan-out costs nothing and fits any cap, got %+v", est)
	}
	out, _ := json.Marshal(est)
	if !strings.Contains(string(out), `"providers":[]`) {
		t.Errorf("provider lines must serialize as an empty array, got %s", out)
	}
}

// truncateFixture builds a merged response with a duplicate candidate
// spanning results 2 and 4 (0-based), so a limit of 3 must drop it while a
// limit of 5 must keep it.
func truncateFixture(t *testing.T) Response {
	t.Helper()
	doi := mustDOI(t, "10.7717/peerj.4375")
	results := []Result{
		{Provider: "openalex-works", SourceID: "openalex:W1", ProviderRank: 1},
		{Provider: "openalex-works", SourceID: "openalex:W2", ProviderRank: 2},
		{Provider: "openalex-works", SourceID: "openalex:W3", ProviderRank: 3, Identifiers: []identity.Identifier{doi}},
		{Provider: "s2-search", SourceID: "s2:P1", ProviderRank: 1},
		{Provider: "s2-search", SourceID: "s2:P2", ProviderRank: 2, Identifiers: []identity.Identifier{doi}},
	}
	return Merge("photosynthesis", "search", results, nil, CostEstimate{}, at)
}

func TestTruncate_KeepsOnlyTheFirstN(t *testing.T) {
	resp := truncateFixture(t)
	if len(resp.Results) != 5 {
		t.Fatalf("fixture setup: got %d results, want 5", len(resp.Results))
	}
	got := resp.Truncate(3)
	if len(got.Results) != 3 {
		t.Fatalf("Truncate(3) kept %d results, want 3", len(got.Results))
	}
	for i, r := range got.Results {
		if r.FusedRank != i+1 {
			t.Errorf("result %d: FusedRank = %d, want the untouched pre-truncation rank %d", i, r.FusedRank, i+1)
		}
	}
}

func TestTruncate_DropsDuplicateCandidatesOutsideRange(t *testing.T) {
	resp := truncateFixture(t)
	if len(resp.DuplicateCandidates) != 1 {
		t.Fatalf("fixture setup: got %d duplicate candidates, want 1", len(resp.DuplicateCandidates))
	}
	dupIdx := resp.DuplicateCandidates[0].Results
	maxDupIdx := 0
	for _, i := range dupIdx {
		if i > maxDupIdx {
			maxDupIdx = i
		}
	}

	// A limit that excludes the higher-indexed member of the pair must drop
	// the candidate rather than leave it pointing past the kept results.
	truncated := resp.Truncate(maxDupIdx) // keeps indices [0, maxDupIdx), so the pair no longer fits
	for _, dc := range truncated.DuplicateCandidates {
		for _, i := range dc.Results {
			if i >= len(truncated.Results) {
				t.Fatalf("duplicate candidate %+v references index %d outside %d kept results",
					dc, i, len(truncated.Results))
			}
		}
	}
	if len(truncated.DuplicateCandidates) != 0 {
		t.Errorf("expected the duplicate candidate to be dropped, got %+v", truncated.DuplicateCandidates)
	}

	// A limit that keeps every member of the pair must keep the candidate.
	full := resp.Truncate(len(resp.Results))
	if len(full.DuplicateCandidates) != 1 {
		t.Errorf("Truncate(len(results)) must be a no-op on duplicate candidates, got %+v", full.DuplicateCandidates)
	}
}

func TestTruncate_ZeroOrOversizedIsNoOp(t *testing.T) {
	resp := truncateFixture(t)
	if got := resp.Truncate(0); len(got.Results) != len(resp.Results) {
		t.Errorf("Truncate(0) must be unlimited behavior, got %d results", len(got.Results))
	}
	if got := resp.Truncate(100); len(got.Results) != len(resp.Results) {
		t.Errorf("Truncate(n) for n beyond the result count must be a no-op, got %d results", len(got.Results))
	}
}
