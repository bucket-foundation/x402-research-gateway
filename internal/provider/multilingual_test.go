package provider

import (
	"net/http"
	"testing"
)

// Fixture tests for x402-research-gateway#21: a CJK-titled work, a
// Cyrillic-titled work, and a multilingual vocabulary concept, covering the
// issue's own acceptance criterion ("Fixture tests covering a CJK-titled
// work, a Cyrillic-titled work, and a multilingual vocabulary concept").

// openAlexCJKFixture is a work whose title OpenAlex reports as Japanese
// (language: "ja"). The title string is not translated by this adapter;
// OpenAlex's own title field is already in this language.
const openAlexCJKFixture = `{"id":"https://openalex.org/W1000000001","title":"量子もつれの新しい観測方法","display_name":"量子もつれの新しい観測方法","language":"ja","publication_year":2023}`

// openAlexCyrillicFixture is a work whose title OpenAlex reports as Russian
// (language: "ru").
const openAlexCyrillicFixture = `{"id":"https://openalex.org/W1000000002","title":"Новый метод наблюдения квантовой запутанности","display_name":"Новый метод наблюдения квантовой запутанности","language":"ru","publication_year":2023}`

func TestMultilingual_OpenAlex_CJKTitle(t *testing.T) {
	recs := OpenAlexWorksNormalizer{}.Normalize([]byte(`{"results":[` + openAlexCJKFixture + `]}`))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	d := openAlexIdentity{}.Descriptor(recs[0])
	if d.Title != "量子もつれの新しい観測方法" {
		t.Errorf("title should be preserved verbatim in its original script, got %q", d.Title)
	}
	m := openAlexIdentity{}.Multilingual(recs[0])
	if m.Language != "ja" {
		t.Errorf("language = %q, want ja", m.Language)
	}
	// No English form is invented for a work OpenAlex never translated.
	if len(m.Forms) != 0 {
		t.Errorf("no translated forms exist in this response shape, got %+v", m.Forms)
	}
}

func TestMultilingual_OpenAlex_CyrillicTitle(t *testing.T) {
	recs := OpenAlexWorksNormalizer{}.Normalize([]byte(`{"results":[` + openAlexCyrillicFixture + `]}`))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	d := openAlexIdentity{}.Descriptor(recs[0])
	if d.Title != "Новый метод наблюдения квантовой запутанности" {
		t.Errorf("title should be preserved verbatim in its original script, got %q", d.Title)
	}
	m := openAlexIdentity{}.Multilingual(recs[0])
	if m.Language != "ru" {
		t.Errorf("language = %q, want ru", m.Language)
	}
}

func TestMultilingual_OpenAlex_NoLanguageReportsZeroValue(t *testing.T) {
	recs := OpenAlexWorksNormalizer{}.Normalize([]byte(`{"results":[{"id":"https://openalex.org/W9","title":"No language field"}]}`))
	m := openAlexIdentity{}.Multilingual(recs[0])
	if m.Language != "" {
		t.Errorf("absent language field should report empty, not a default, got %q", m.Language)
	}
}

// TestMultilingual_MeSH_ConceptLabelLanguage is the multilingual vocabulary
// concept fixture case x402-research-gateway#21 asks for: MeSH tags its
// label field with an explicit @language on every response this adapter
// reads, and GetConcept must carry that tag through PrefLabelLanguage and
// Labels rather than discarding it the way a prior revision of
// meshLangValue did (it read only @value).
func TestMultilingual_MeSH_ConceptLabelLanguage(t *testing.T) {
	p := newTestMeSHProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(meshDescriptorFixture))
	})
	c, ok := p.GetConcept("D017209", "")
	if !ok {
		t.Fatal("GetConcept failed")
	}
	if c.PrefLabelLanguage != "en" {
		t.Errorf("PrefLabelLanguage = %q, want en (the tag the source published, not discarded)", c.PrefLabelLanguage)
	}
	if len(c.Labels) != 1 {
		t.Fatalf("got %d labels, want 1: %+v", len(c.Labels), c.Labels)
	}
	if c.Labels[0].Language != "en" || c.Labels[0].Kind != FormOriginal || c.Labels[0].Provider != "mesh" {
		t.Errorf("label = %+v", c.Labels[0])
	}
	if c.Labels[0].Value != "Apoptosis" {
		t.Errorf("label value = %q", c.Labels[0].Value)
	}
}

// crossrefTranslatedWorkFixture is a Crossref work published in Russian
// whose record also carries the work's original-language title, distinct
// from `title` (x402-research-gateway#21). Real Crossref works rarely
// populate `original-title`; when they do it names the original-language
// form separately from the (possibly translated) primary title.
const crossrefTranslatedWorkFixture = `{"message":{"DOI":"10.1234/cyr1","URL":"https://doi.org/10.1234/cyr1","title":["Новый метод наблюдения квантовой запутанности"],"original-title":["Новый метод наблюдения квантовой запутанности"],"language":"ru"}}`

func TestMultilingual_Crossref_CyrillicWorkLanguageAndOriginalTitle(t *testing.T) {
	recs := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefTranslatedWorkFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	m := crossrefIdentity{}.Multilingual(recs[0])
	if m.Language != "ru" {
		t.Errorf("language = %q, want ru", m.Language)
	}
	if len(m.Forms) != 1 {
		t.Fatalf("got %d forms, want 1: %+v", len(m.Forms), m.Forms)
	}
	f := m.Forms[0]
	if f.Kind != FormOriginal || f.Provider != "crossref" {
		t.Errorf("form = %+v", f)
	}
	if f.Value != "Новый метод наблюдения квантовой запутанности" {
		t.Errorf("original-title value = %q", f.Value)
	}
	// Crossref does not tag original-title with its own language, so this
	// gateway does not borrow the item-level `language` field for it.
	if f.Language != "" {
		t.Errorf("original-title form should carry no borrowed language, got %q", f.Language)
	}
}

func TestMultilingual_Crossref_NoLanguageReportsZeroValue(t *testing.T) {
	recs := CrossrefWorksNormalizer{}.Normalize([]byte(`{"message":{"DOI":"10.1234/x","title":["No language field"]}}`))
	m := crossrefIdentity{}.Multilingual(recs[0])
	if m.Language != "" || len(m.Forms) != 0 {
		t.Errorf("absent language/original-title should report zero value, got %+v", m)
	}
}

// dataciteCJKWorkFixture is a DataCite record whose primary title is
// Japanese and which also carries a depositor-supplied English translation
// tagged titleType=TranslatedTitle (x402-research-gateway#21) — the CJK
// fixture case the issue's acceptance criteria ask for, on the DataCite
// adapter this session wires.
const dataciteCJKWorkFixture = `{"data":{"id":"10.5281/zenodo.1000001","attributes":{"doi":"10.5281/zenodo.1000001","url":"https://zenodo.org/record/1000001","language":"ja","titles":[{"title":"量子もつれの新しい観測方法"},{"title":"A New Method for Observing Quantum Entanglement","titleType":"TranslatedTitle","lang":"en"}]}}}`

func TestMultilingual_DataCite_CJKTitleWithTranslatedForm(t *testing.T) {
	recs := DataCiteNormalizer{}.Normalize([]byte(dataciteCJKWorkFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	d := dataciteIdentity{}.Descriptor(recs[0])
	if d.Title != "量子もつれの新しい観測方法" {
		t.Errorf("primary title should be preserved verbatim in its original script, got %q", d.Title)
	}
	m := dataciteIdentity{}.Multilingual(recs[0])
	if m.Language != "ja" {
		t.Errorf("language = %q, want ja", m.Language)
	}
	if len(m.Forms) != 1 {
		t.Fatalf("got %d forms, want 1: %+v", len(m.Forms), m.Forms)
	}
	f := m.Forms[0]
	if f.Kind != FormTranslated || f.Provider != "datacite" || f.Language != "en" {
		t.Errorf("translated form = %+v", f)
	}
	if f.Value != "A New Method for Observing Quantum Entanglement" {
		t.Errorf("translated title value = %q", f.Value)
	}
}

func TestMultilingual_DataCite_NoLanguageReportsZeroValue(t *testing.T) {
	recs := DataCiteNormalizer{}.Normalize([]byte(`{"data":{"id":"10.5281/zenodo.2","attributes":{"doi":"10.5281/zenodo.2","titles":[{"title":"No language field"}]}}}`))
	m := dataciteIdentity{}.Multilingual(recs[0])
	if m.Language != "" || len(m.Forms) != 0 {
		t.Errorf("absent language/translated-title should report zero value, got %+v", m)
	}
}
