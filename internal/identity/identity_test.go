package identity

import "testing"

func TestNormalizePreservesRaw(t *testing.T) {
	cases := []struct {
		scheme  Scheme
		raw     string
		value   string
		version string
	}{
		{SchemeDOI, "https://doi.org/10.1038/S41586-020-2649-2", "10.1038/s41586-020-2649-2", ""},
		{SchemeDOI, "doi:10.1234/ABC.def", "10.1234/abc.def", ""},
		{SchemePMID, "https://pubmed.ncbi.nlm.nih.gov/38831607/", "38831607", ""},
		{SchemePMID, "PMID:00123", "123", ""},
		{SchemePMCID, "https://www.ncbi.nlm.nih.gov/pmc/articles/pmc7654321/", "PMC7654321", ""},
		{SchemeArXiv, "arXiv:2101.00001v3", "2101.00001", "3"},
		{SchemeArXiv, "https://arxiv.org/abs/2101.00001", "2101.00001", ""},
		{SchemeArXiv, "arXiv:math.GT/0309136v2", "math.gt/0309136", "2"},
		{SchemeOpenAlex, "https://openalex.org/W2741809807", "W2741809807", ""},
		{SchemeORCID, "https://orcid.org/0000-0002-1825-0097", "0000-0002-1825-0097", ""},
		{SchemeROR, "https://ror.org/03vek6s52", "03vek6s52", ""},
		{SchemeDBLP, "https://dblp.org/rec/journals/cacm/Codd70.html", "journals/cacm/Codd70", ""},
	}
	for _, c := range cases {
		got, ok := New(c.scheme, c.raw)
		if !ok {
			t.Fatalf("New(%s, %q) rejected", c.scheme, c.raw)
		}
		if got.Value != c.value || got.Version != c.version {
			t.Errorf("New(%s, %q) = %q/v%q, want %q/v%q", c.scheme, c.raw, got.Value, got.Version, c.value, c.version)
		}
		if got.Raw != c.raw {
			t.Errorf("raw destroyed: got %q want %q", got.Raw, c.raw)
		}
	}
}

func TestNewRejectionKeepsRaw(t *testing.T) {
	id, ok := New(SchemeDOI, "not-a-doi")
	if ok {
		t.Fatal("expected rejection")
	}
	if id.Raw != "not-a-doi" || id.Value != "" {
		t.Errorf("rejected identifier should keep raw and carry no value, got %+v", id)
	}
	if _, ok := New(Scheme("made-up"), "x"); ok {
		t.Error("unregistered scheme should not resolve")
	}
}

func TestParseSniffsScheme(t *testing.T) {
	cases := map[string]Scheme{
		"10.1038/s41586-020-2649-2":                 SchemeDOI,
		"https://doi.org/10.1234/xyz":               SchemeDOI,
		"arXiv:2101.00001v2":                        SchemeArXiv,
		"https://pubmed.ncbi.nlm.nih.gov/38831607/": SchemePMID,
		"PMC7654321":                                SchemePMCID,
		"https://openalex.org/W123":                 SchemeOpenAlex,
		"https://orcid.org/0000-0002-1825-0097":     SchemeORCID,
		"38831607":                                  SchemePMID,
	}
	for raw, want := range cases {
		got, ok := Parse(raw)
		if !ok {
			t.Fatalf("Parse(%q) failed", raw)
		}
		if got.Scheme != want {
			t.Errorf("Parse(%q) scheme = %s, want %s", raw, got.Scheme, want)
		}
	}
	if _, ok := Parse("   "); ok {
		t.Error("blank input should not parse")
	}
	if got, ok := Parse("total nonsense here"); ok {
		t.Errorf("nonsense parsed as %+v", got)
	}
}

func TestVersionSplitKeepsBaseMatchable(t *testing.T) {
	v1, _ := New(SchemeArXiv, "arXiv:2101.00001v1")
	v3, _ := New(SchemeArXiv, "arXiv:2101.00001v3")
	if v1.Key() != v3.Key() {
		t.Fatalf("versions should share an exact key: %q vs %q", v1.Key(), v3.Key())
	}
	if v1.String() == v3.String() {
		t.Fatal("versions should remain distinguishable in String()")
	}
}

func TestSchemesAreSortedAndExtensible(t *testing.T) {
	before := len(Schemes())
	RegisterScheme(Scheme("zzz-test"), func(raw string) (string, string, bool) { return raw, "", true })
	defer delete(schemes, Scheme("zzz-test"))
	after := Schemes()
	if len(after) != before+1 {
		t.Fatalf("RegisterScheme did not extend the registry: %d -> %d", before, len(after))
	}
	for i := 1; i < len(after); i++ {
		if after[i-1] > after[i] {
			t.Fatal("Schemes() must be sorted")
		}
	}
}

func TestEvidenceValidity(t *testing.T) {
	if (Evidence{Kind: EvidenceProviderAsserted}).Valid() {
		t.Error("provider-asserted evidence with no provider must be invalid")
	}
	if (Evidence{Kind: EvidenceGatewayInferred}).Valid() {
		t.Error("gateway-inferred evidence with no method must be invalid")
	}
	mixed := Evidence{Kind: EvidenceProviderAsserted, Provider: "crossref", Method: "guess"}
	if mixed.Valid() {
		t.Error("evidence must not carry both a provider and a method")
	}
}
