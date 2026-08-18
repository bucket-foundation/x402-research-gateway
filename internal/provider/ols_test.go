package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const olsOntologyFixture = `{"ontologyId":"go","version":"2026-07-26"}`

const olsSearchFixture = `{"response":{"docs":[
  {"iri":"http://purl.obolibrary.org/obo/GO_0006915","label":"apoptotic process","ontology_name":"go","synonym":["apoptosis"]}
]}}`

// olsObsoleteTermFixture is trimmed from the live GO:0000005 record verified
// 2026-08-18 (see ols.go's doc comment): is_obsolete true, term_replaced_by
// null, consider naming a non-exact replacement candidate. This is the #15
// deprecated-term-with-replacement-pointer acceptance case.
const olsObsoleteTermFixture = `{
  "iri":"http://purl.obolibrary.org/obo/GO_0000005",
  "label":"obsolete ribosomal chaperone activity",
  "description":["OBSOLETE."],
  "synonyms":["chaperone activity"],
  "is_obsolete": true,
  "term_replaced_by": null,
  "consider": ["http://purl.obolibrary.org/obo/GO_0042254"]
}`

const olsCurrentTermFixture = `{
  "iri":"http://purl.obolibrary.org/obo/GO_0006915",
  "label":"apoptotic process",
  "description":["A programmed cell death process."],
  "synonyms":["apoptosis","programmed cell death by apoptosis"],
  "is_obsolete": false
}`

const olsParentsFixture = `{"_embedded":{"terms":[
  {"iri":"http://purl.obolibrary.org/obo/GO_0012501","label":"programmed cell death","synonyms":[],"is_obsolete":false}
]}}`

func newTestOLSProvider(t *testing.T, handler http.HandlerFunc) *OLSProvider {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := NewOLSProvider("go")
	p.BaseURL = srv.URL
	return p
}

func TestOLSProvider_CurrentRelease(t *testing.T) {
	p := newTestOLSProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(olsOntologyFixture))
	})
	rel, ok := p.CurrentRelease()
	if !ok || rel.Release != "2026-07-26" {
		t.Fatalf("got %+v ok=%v", rel, ok)
	}
}

func TestOLSProvider_SearchTerms(t *testing.T) {
	p := newTestOLSProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ontologies/go") && !strings.Contains(r.URL.Path, "/terms") {
			w.Write([]byte(olsOntologyFixture))
			return
		}
		w.Write([]byte(olsSearchFixture))
	})
	concepts, ok := p.SearchTerms("apoptosis", "")
	if !ok || len(concepts) != 1 {
		t.Fatalf("got %v ok=%v", concepts, ok)
	}
	if concepts[0].PrefLabel != "apoptotic process" {
		t.Errorf("label = %q", concepts[0].PrefLabel)
	}
	if concepts[0].SourceRelease != "2026-07-26" {
		t.Errorf("expected the concept stamped with the ontology release, got %q", concepts[0].SourceRelease)
	}
}

// TestOLSProvider_GetConcept_ObsoleteWithReplacementCandidates is the #15
// acceptance case: a deprecated term carrying a date-equivalent signal
// (is_obsolete) and a non-exact replacement pointer (consider), which must
// not be collapsed into an equivalence claim the source never made.
func TestOLSProvider_GetConcept_ObsoleteWithReplacementCandidates(t *testing.T) {
	p := newTestOLSProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/terms") {
			w.Write([]byte(olsObsoleteTermFixture))
			return
		}
		w.Write([]byte(olsOntologyFixture))
	})
	c, ok := p.GetConcept("http://purl.obolibrary.org/obo/GO_0000005", "")
	if !ok {
		t.Fatal("expected GetConcept to succeed")
	}
	if !c.Deprecated {
		t.Error("expected an obsolete term reported Deprecated")
	}
	if len(c.SupersededBy) != 0 {
		t.Errorf("term_replaced_by was null; SupersededBy must stay empty, got %v", c.SupersededBy)
	}
	if len(c.Successor) != 1 || c.Successor[0] != "http://purl.obolibrary.org/obo/GO_0042254" {
		t.Errorf("expected the 'consider' candidate surfaced as a non-exact Successor, got %v", c.Successor)
	}
}

func TestOLSProvider_Broader(t *testing.T) {
	p := newTestOLSProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/parents"):
			w.Write([]byte(olsParentsFixture))
		case strings.Contains(r.URL.Path, "/terms"):
			w.Write([]byte(olsCurrentTermFixture))
		default:
			w.Write([]byte(olsOntologyFixture))
		}
	})
	broader, ok := p.Broader("http://purl.obolibrary.org/obo/GO_0006915", "")
	if !ok || len(broader) != 1 {
		t.Fatalf("got %v ok=%v", broader, ok)
	}
	if broader[0].PrefLabel != "programmed cell death" {
		t.Errorf("broader label = %q", broader[0].PrefLabel)
	}
}

func TestOLSProvider_Broader_NoParentsIsAValidAnswer(t *testing.T) {
	p := newTestOLSProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/parents") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(olsOntologyFixture))
	})
	broader, ok := p.Broader("http://purl.obolibrary.org/obo/GO_ROOT", "")
	if !ok {
		t.Error("a root term with no parents is a supported, empty answer, not a failure")
	}
	if len(broader) != 0 {
		t.Errorf("expected no parents, got %v", broader)
	}
}

func TestOLSProvider_SearchTerms_RejectsWrongRelease(t *testing.T) {
	p := newTestOLSProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(olsOntologyFixture))
	})
	if _, ok := p.SearchTerms("apoptosis", "2020-01-01"); ok {
		t.Error("asking for a release OLS is not currently serving must report unsupported, not silently substitute the current one")
	}
}
