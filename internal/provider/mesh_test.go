package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const meshLookupFixture = `[{"resource":"http://id.nlm.nih.gov/mesh/D017209","label":"Apoptosis"}]`

// meshDescriptorFixture is trimmed from the live D017209 record verified
// 2026-08-18 (see mesh.go's doc comment). previousIndexing is MeSH's own
// historical-alias field: "Apoptosis" was indexed as "Cell Survival" from
// 1972-1992.
const meshDescriptorFixture = `{
  "identifier": "D017209",
  "label": {"@language":"en","@value":"Apoptosis"},
  "http://id.nlm.nih.gov/mesh/vocab#active": true,
  "broaderDescriptor": "http://id.nlm.nih.gov/mesh/D000079404",
  "dateIntroduced": "1993-01-01",
  "previousIndexing": {"@language":"en","@value":"Cell Survival (1972-1992)"}
}`

const meshBroaderDescriptorFixture = `{
  "identifier": "D000079404",
  "label": {"@language":"en","@value":"Cell Death"},
  "http://id.nlm.nih.gov/mesh/vocab#active": true
}`

func newTestMeSHProvider(t *testing.T, handler http.HandlerFunc) *MeSHProvider {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := NewMeSHProvider()
	p.BaseURL = srv.URL
	return p
}

func TestMeSHProvider_SearchTerms(t *testing.T) {
	p := newTestMeSHProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(meshLookupFixture))
	})
	concepts, ok := p.SearchTerms("Apoptosis", "")
	if !ok {
		t.Fatal("expected SearchTerms to succeed")
	}
	if len(concepts) != 1 || concepts[0].ID != "D017209" || concepts[0].PrefLabel != "Apoptosis" {
		t.Errorf("got %+v", concepts)
	}
}

func TestMeSHProvider_SearchTerms_RejectsHistoricalRelease(t *testing.T) {
	p := newTestMeSHProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not contact the upstream for an unsupported release")
	})
	if _, ok := p.SearchTerms("Apoptosis", "2005"); ok {
		t.Error("a specific historical release should report unsupported, not fall back silently")
	}
}

// TestMeSHProvider_GetConcept_HistoricalAlias is the #15 acceptance case:
// a renamed term retrievable through its historical alias.
func TestMeSHProvider_GetConcept_HistoricalAlias(t *testing.T) {
	p := newTestMeSHProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(meshDescriptorFixture))
	})
	c, ok := p.GetConcept("D017209", "")
	if !ok {
		t.Fatal("expected GetConcept to succeed")
	}
	if c.PrefLabel != "Apoptosis" {
		t.Errorf("pref label = %q", c.PrefLabel)
	}
	if len(c.HistoricalAliases) != 1 || c.HistoricalAliases[0] != "Cell Survival (1972-1992)" {
		t.Errorf("expected the previousIndexing historical alias preserved, got %v", c.HistoricalAliases)
	}
	if c.Deprecated {
		t.Error("an active descriptor must not be reported deprecated")
	}
	if c.SourceRelease == "" {
		t.Error("expected every concept response to carry a source release")
	}
	if len(c.Native) == 0 {
		t.Error("expected the native MeSH JSON-LD body preserved")
	}
}

func TestMeSHProvider_HistoricalTerms(t *testing.T) {
	p := newTestMeSHProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(meshDescriptorFixture))
	})
	concepts, ok := p.HistoricalTerms("D017209")
	if !ok || len(concepts) != 1 {
		t.Fatalf("expected one historical concept, got %v ok=%v", concepts, ok)
	}
	if concepts[0].HistoricalAliases[0] != "Cell Survival (1972-1992)" {
		t.Errorf("got %v", concepts[0].HistoricalAliases)
	}
}

func TestMeSHProvider_Broader(t *testing.T) {
	calls := 0
	p := newTestMeSHProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/D017209.json" {
			w.Write([]byte(meshDescriptorFixture))
			return
		}
		w.Write([]byte(meshBroaderDescriptorFixture))
	})
	broader, ok := p.Broader("D017209", "")
	if !ok || len(broader) != 1 {
		t.Fatalf("expected one broader concept, got %v ok=%v", broader, ok)
	}
	if broader[0].PrefLabel != "Cell Death" {
		t.Errorf("broader label = %q", broader[0].PrefLabel)
	}
}

func TestMeSHProvider_Narrower_Unsupported(t *testing.T) {
	p := NewMeSHProvider()
	if _, ok := p.Narrower("D017209", ""); ok {
		t.Error("MeSH has no cheap reverse index; Narrower must report unsupported")
	}
}
