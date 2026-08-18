package provider

import (
	"encoding/json"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// IdentityProvider and DescriptorProvider implementations for the adapters
// migrated in x402-research-gateway#2. Each reads only NormalizedRecord.Raw,
// so identity extraction adds no upstream calls and no coupling to the
// route config.
//
// Every extraction here is provider-asserted fact: an identifier the
// upstream published on the record. Nothing in this file computes a
// similarity, a confidence, or a guess. That belongs to
// internal/identity.Resolver, which is where the exact/fuzzy boundary is
// enforced.

// appendID normalizes a scheme/raw pair and appends it when the raw string
// is non-empty and well-formed. A malformed identifier is dropped from
// matching; the record's own Raw still carries it verbatim.
func appendID(out []identity.Identifier, scheme identity.Scheme, raw string) []identity.Identifier {
	if raw == "" {
		return out
	}
	if id, ok := identity.New(scheme, raw); ok {
		return append(out, id)
	}
	return out
}

// ---------- OpenAlex ----------

// openAlexIdentity reads the `ids` block OpenAlex publishes on every work:
//
//	"ids": {"openalex": "...", "doi": "...", "pmid": "...", "pmcid": "..."}
//
// OpenAlex asserts these as identifier equivalences on one work record, so
// they are reported as identifiers. Typed work relations (preprint,
// correction, retraction) come from providers that publish them, Crossref
// and the integrity sources in x402-research-gateway#19.
type openAlexIdentity struct{}

type openAlexWork struct {
	ID  string `json:"id"`
	DOI string `json:"doi"`
	IDs struct {
		OpenAlex string `json:"openalex"`
		DOI      string `json:"doi"`
		PMID     string `json:"pmid"`
		PMCID    string `json:"pmcid"`
		MAG      string `json:"mag"`
	} `json:"ids"`
	Title           string `json:"title"`
	DisplayName     string `json:"display_name"`
	PublicationYear int    `json:"publication_year"`
	// Language is OpenAlex's own ISO-639-1 tag for the work
	// (x402-research-gateway#21), read here and reported through
	// Multilingual; a prior revision parsed this struct without the field
	// at all, discarding it on every work OpenAlex returns. Title and
	// DisplayName above are already in whatever language this names —
	// OpenAlex does not translate them into English — so there is no
	// separate "original title" to carry alongside them.
	Language    string `json:"language"`
	Authorships []struct {
		Author struct {
			DisplayName string `json:"display_name"`
			ORCID       string `json:"orcid"`
		} `json:"author"`
	} `json:"authorships"`
}

func (openAlexIdentity) parse(rec NormalizedRecord) (openAlexWork, bool) {
	var w openAlexWork
	if len(rec.Raw) == 0 {
		return w, false
	}
	if err := json.Unmarshal(rec.Raw, &w); err != nil {
		return w, false
	}
	return w, true
}

func (p openAlexIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	w, ok := p.parse(rec)
	if !ok {
		return nil
	}
	var out []identity.Identifier
	out = appendID(out, identity.SchemeOpenAlex, firstNonEmpty(w.IDs.OpenAlex, w.ID))
	out = appendID(out, identity.SchemeDOI, firstNonEmpty(w.IDs.DOI, w.DOI))
	out = appendID(out, identity.SchemePMID, w.IDs.PMID)
	out = appendID(out, identity.SchemePMCID, w.IDs.PMCID)
	return out
}

func (openAlexIdentity) AssertedRelations(string, NormalizedRecord, time.Time) []identity.Relation {
	// OpenAlex publishes identifier equivalences rather than typed work
	// relations, so the identifiers above carry the whole assertion and
	// there is nothing to add here. Returning nil is the accurate answer;
	// synthesizing a relation from co-located identifiers would restate an
	// inference the resolver already makes, with the wrong evidence kind.
	return nil
}

func (p openAlexIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	w, ok := p.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: firstNonEmpty(w.Title, w.DisplayName), Year: w.PublicationYear}
	for _, a := range w.Authorships {
		if a.Author.DisplayName != "" {
			d.Authors = append(d.Authors, a.Author.DisplayName)
		}
	}
	return d
}

// Multilingual reports OpenAlex's own `language` field
// (x402-research-gateway#21), the record's work language as OpenAlex
// states it. There is no separate translated or transliterated title in
// this response shape: OpenAlex's title/display_name fields are already in
// this language, never machine-translated into English, so Forms is empty
// and Language alone carries the fact this adapter has to report.
func (p openAlexIdentity) Multilingual(rec NormalizedRecord) Multilingual {
	w, ok := p.parse(rec)
	if !ok || w.Language == "" {
		return Multilingual{}
	}
	return Multilingual{Language: w.Language}
}

// ---------- Semantic Scholar ----------

// semanticScholarIdentity reads the Graph API's `externalIds` block:
//
//	"externalIds": {"DOI": "...", "PubMed": "...", "ArXiv": "...", ...}
type semanticScholarIdentity struct{}

type s2Paper struct {
	PaperID     string `json:"paperId"`
	ExternalIDs struct {
		DOI           string `json:"DOI"`
		PubMed        string `json:"PubMed"`
		PubMedCentral string `json:"PubMedCentral"`
		ArXiv         string `json:"ArXiv"`
		DBLP          string `json:"DBLP"`
	} `json:"externalIds"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
}

func (semanticScholarIdentity) parse(rec NormalizedRecord) (s2Paper, bool) {
	var p s2Paper
	if len(rec.Raw) == 0 {
		return p, false
	}
	if err := json.Unmarshal(rec.Raw, &p); err != nil {
		return p, false
	}
	return p, true
}

func (s semanticScholarIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	p, ok := s.parse(rec)
	if !ok {
		return nil
	}
	var out []identity.Identifier
	out = appendID(out, identity.SchemeSemanticScholar, p.PaperID)
	out = appendID(out, identity.SchemeDOI, p.ExternalIDs.DOI)
	out = appendID(out, identity.SchemePMID, p.ExternalIDs.PubMed)
	out = appendID(out, identity.SchemePMCID, p.ExternalIDs.PubMedCentral)
	out = appendID(out, identity.SchemeArXiv, p.ExternalIDs.ArXiv)
	out = appendID(out, identity.SchemeDBLP, p.ExternalIDs.DBLP)
	return out
}

func (semanticScholarIdentity) AssertedRelations(string, NormalizedRecord, time.Time) []identity.Relation {
	return nil
}

func (s semanticScholarIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	p, ok := s.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: p.Title, Year: p.Year}
	for _, a := range p.Authors {
		if a.Name != "" {
			d.Authors = append(d.Authors, a.Name)
		}
	}
	return d
}

// ---------- PubMed ----------

// pubMedIdentity covers ESearch, whose response is a bare PMID list. The
// PMID is the only identifier PubMed asserts at this tier, so that is all
// this reports. EFetch carries DOIs and PMCIDs and will extend this when
// the fetch route grows a Normalizer.
type pubMedIdentity struct{}

func (pubMedIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	return appendID(nil, identity.SchemePMID, rec.ID)
}

func (pubMedIdentity) AssertedRelations(string, NormalizedRecord, time.Time) []identity.Relation {
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
