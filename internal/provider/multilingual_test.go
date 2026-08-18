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
