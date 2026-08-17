package config

import (
	"os"
	"testing"
)

// The shipped route files must parse, and the resolve block added in
// x402-research-gateway#5 must survive round-tripping through YAML with its
// declared values rather than silently falling back to defaults.
func TestLoadFromFile_ResolveBlock(t *testing.T) {
	t.Setenv("RECIPIENT_ADDRESS", "0x0000000000000000000000000000000000000001")
	for _, path := range []string{"../../config/routes.yaml", "../../config/routes.hetzner.yaml"} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("route file %s not present", path)
		}
		c, err := LoadFromFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		r := c.Feed402.Resolve
		if !r.Enabled {
			t.Fatalf("%s: resolve block not enabled", path)
		}
		if r.Path != "/research/resolve" || r.MaxConcurrency != 4 || r.TimeoutSeconds != 10 {
			t.Errorf("%s: resolve config = %+v", path, r)
		}
		if r.SimilarityThreshold != 0.82 {
			t.Errorf("%s: similarity threshold = %v", path, r.SimilarityThreshold)
		}
		// The insight block, and every pre-existing route, are untouched.
		if !c.Feed402.Insight.Enabled || c.Feed402.Insight.MaxContextChars != 4000 {
			t.Errorf("%s: insight config regressed: %+v", path, c.Feed402.Insight)
		}
		if len(c.Routes) == 0 {
			t.Errorf("%s: no routes loaded", path)
		}
		// The citation-graph block and its seven provider routes
		// (x402-research-gateway#6).
		cit := c.Feed402.Citations
		if !cit.Enabled || cit.Path != "/research/citations" || cit.MaxConcurrency != 4 || cit.TimeoutSeconds != 10 {
			t.Errorf("%s: citations config = %+v", path, cit)
		}
		byID := map[string]bool{}
		for i := range c.Routes {
			byID[c.Routes[i].ID] = true
		}
		for _, id := range []string{
			"openalex-references", "openalex-cited-by",
			"semantic-scholar-references", "semantic-scholar-cited-by",
			"opencitations-references", "opencitations-cited-by",
			"crossref-references",
		} {
			if !byID[id] {
				t.Errorf("%s: citation route %q missing", path, id)
			}
		}
		// Every route that existed before this change is still declared.
		for _, id := range []string{
			"pubmed-search", "pubmed-fetch", "semantic-scholar-search",
			"openalex-works", "clinicaltrials-search", "pubchem-compound",
		} {
			if !byID[id] {
				t.Errorf("%s: pre-existing route %q was dropped", path, id)
			}
		}
	}
}

// Defaults fill in for an enabled resolve block that declares nothing else.
func TestLoadFromFile_ResolveDefaults(t *testing.T) {
	t.Setenv("RECIPIENT_ADDRESS", "0x0000000000000000000000000000000000000001")
	dir := t.TempDir()
	path := dir + "/routes.yaml"
	body := `port: 8092
network: base-sepolia
facilitatorUrl: https://facilitator.x402.rs
defaultPrice: "0.001"
feed402:
  enabled: true
  name: test
  spec: feed402/0.3
  resolve:
    enabled: true
routes:
  - id: r1
    path: /r1
    method: GET
    price: "0.001"
    upstream:
      baseUrl: https://example.invalid
      path: /x
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Feed402.Resolve
	if r.Path != "/research/resolve" || r.Price != "0.005" || r.MaxConcurrency != 4 || r.TimeoutSeconds != 10 {
		t.Errorf("defaults not applied: %+v", r)
	}
	if r.Description == "" {
		t.Error("description default missing")
	}
	// A zero threshold is left at zero here and interpreted by the resolver
	// as "use the package default," so an operator never has to restate it.
	if r.SimilarityThreshold != 0 {
		t.Errorf("threshold should stay unset, got %v", r.SimilarityThreshold)
	}
}

// A gateway with resolve disabled loads and runs exactly as before.
func TestLoadFromFile_ResolveDisabledByOmission(t *testing.T) {
	t.Setenv("RECIPIENT_ADDRESS", "0x0000000000000000000000000000000000000001")
	dir := t.TempDir()
	path := dir + "/routes.yaml"
	body := `port: 8092
network: base-sepolia
facilitatorUrl: https://facilitator.x402.rs
defaultPrice: "0.001"
feed402:
  enabled: true
  name: test
  spec: feed402/0.3
routes:
  - id: r1
    path: /r1
    method: GET
    price: "0.001"
    upstream:
      baseUrl: https://example.invalid
      path: /x
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Feed402.Resolve.Enabled || c.Feed402.Resolve.Path != "" {
		t.Errorf("omitted resolve block should stay off, got %+v", c.Feed402.Resolve)
	}
}
