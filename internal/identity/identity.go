// Package identity models scholarly identifiers and the relations between
// them as a graph.
//
// The design premise, from x402-research-gateway#5: a DOI does not equal a
// paper. A DOI identifies a registered record. One work can carry several
// DOIs (one per version, one for the preprint, one for the published
// article), and a preprint and its published version are related without
// being the same object. So this package never collapses records into a
// single canonical entity. It emits nodes that stay independently
// addressable plus typed, evidenced relations between them.
//
// Two rules hold everywhere in this package:
//
//   - Raw survives. Every Identifier keeps the exact string the provider
//     sent alongside the normalized value, and every node keeps the raw
//     provider bytes. Normalization is additive.
//   - Exact and fuzzy stay distinguishable. A relation asserted by a
//     provider and a relation the gateway inferred from title similarity
//     carry different evidence, and PossibleSameWork never becomes
//     SameWork.
package identity

import (
	"regexp"
	"sort"
	"strings"
)

// Scheme names an identifier namespace. The set is extensible at runtime
// through RegisterScheme; the per-provider `identifier_schemes` field in
// internal/registry drives which schemes a given provider contributes.
type Scheme string

const (
	SchemeDOI             Scheme = "doi"
	SchemePMID            Scheme = "pmid"
	SchemePMCID           Scheme = "pmcid"
	SchemeArXiv           Scheme = "arxiv"
	SchemeOpenAlex        Scheme = "openalex"
	SchemeSemanticScholar Scheme = "semantic_scholar"
	SchemeDBLP            Scheme = "dblp"
	SchemeZbMATH          Scheme = "zbmath"
	SchemeORCID           Scheme = "orcid"
	SchemeROR             Scheme = "ror"
	// SchemeDOAJ and SchemeOpenAIRE (x402-research-gateway#13, Wave 2) are
	// opaque provider-local ids, registered so appendID does not silently
	// drop them the way an unregistered scheme would.
	SchemeDOAJ     Scheme = "doaj"
	SchemeOpenAIRE Scheme = "openaire"
	// SchemePubChemCID, SchemeUniProt, SchemeGBIF, SchemeOSF, and
	// SchemeFigshare (x402-research-gateway#16) are opaque provider-local
	// ids, registered on the same "trim only" precedent as SchemeDOAJ and
	// SchemeOpenAIRE above.
	SchemePubChemCID    Scheme = "pubchem-cid"
	SchemeUniProt       Scheme = "uniprot"
	SchemeGBIF          Scheme = "gbif"
	SchemeOSF           Scheme = "osf"
	SchemeFigshare      Scheme = "figshare"
	SchemeNASAExoplanet Scheme = "nasa-exoplanet"
	// SchemeUSPTOApplication and SchemeUSPTOPatent (x402-research-gateway#18)
	// are opaque provider-local ids on the same "trim only" precedent as
	// SchemeDOAJ/SchemeOpenAIRE above. They are kept distinct from each
	// other because an application number and a granted patent number name
	// different things about the same prosecution history: an application
	// without SchemeUSPTOPatent has not (yet, or ever) issued.
	SchemeUSPTOApplication Scheme = "uspto-application"
	SchemeUSPTOPatent      Scheme = "uspto-patent"
)

// Identifier is one identifier under one scheme. Value is the normalized
// form used for matching. Raw is the exact string the provider supplied,
// retained so a consumer can reproduce the upstream record and so
// normalization can never destroy an input.
//
// Version is set for schemes that carry an explicit version in the
// identifier (arXiv today). It is separate from Value: Value holds the
// version-stripped base so that two versions of one arXiv paper match as a
// version relation rather than as unrelated identifiers.
type Identifier struct {
	Scheme  Scheme `json:"scheme"`
	Value   string `json:"value"`
	Raw     string `json:"raw"`
	Version string `json:"version,omitempty"`
}

// Key is the exact-match key for this identifier: scheme plus normalized
// value, version excluded. Two identifiers with the same Key name the same
// registered record modulo version.
func (id Identifier) Key() string { return string(id.Scheme) + ":" + id.Value }

// String renders the identifier in the compact `scheme:value` form used in
// feed402 source ids, with the version appended when present.
func (id Identifier) String() string {
	if id.Version != "" {
		return id.Key() + "v" + id.Version
	}
	return id.Key()
}

// Normalizer turns a raw identifier string into a normalized value plus an
// optional version. It reports false when the input is not a well-formed
// identifier for its scheme, which is never an error: an unparseable
// identifier is dropped from matching and kept in Raw.
type Normalizer func(raw string) (value, version string, ok bool)

var schemes = map[Scheme]Normalizer{}

// RegisterScheme adds or replaces the normalizer for a scheme. Calling it
// for a scheme that already exists replaces the normalizer, which lets a
// provider package tighten a rule without this file knowing about it.
func RegisterScheme(s Scheme, n Normalizer) { schemes[s] = n }

// Schemes lists every registered scheme in sorted order.
func Schemes() []Scheme {
	out := make([]Scheme, 0, len(schemes))
	for s := range schemes {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// New builds an Identifier under a named scheme. An unregistered scheme, or
// a value the scheme's normalizer rejects, yields ok=false; the caller
// keeps the raw string either way.
func New(s Scheme, raw string) (Identifier, bool) {
	n, known := schemes[s]
	if !known {
		return Identifier{Scheme: s, Raw: raw}, false
	}
	value, version, ok := n(raw)
	if !ok {
		return Identifier{Scheme: s, Raw: raw}, false
	}
	return Identifier{Scheme: s, Value: value, Raw: raw, Version: version}, true
}

var (
	doiRe    = regexp.MustCompile(`10\.\d{4,9}/\S+`)
	pmidRe   = regexp.MustCompile(`^\d{1,9}$`)
	pmcidRe  = regexp.MustCompile(`(?i)PMC\d+`)
	arxivNew = regexp.MustCompile(`(?i)(\d{4}\.\d{4,5})(?:v(\d+))?`)
	arxivOld = regexp.MustCompile(`(?i)([a-z-]+(?:\.[A-Za-z]{2})?/\d{7})(?:v(\d+))?`)
	openAlex = regexp.MustCompile(`(?i)\b(W\d+)\b`)
	s2Re     = regexp.MustCompile(`(?i)\b([0-9a-f]{40})\b`)
	orcidRe  = regexp.MustCompile(`(\d{4}-\d{4}-\d{4}-\d{3}[\dXx])`)
	rorRe    = regexp.MustCompile(`(?i)\b(0[0-9a-hj-km-np-tv-z]{8})\b`)
	// zbl identifiers are four digits, a dot, and five digits (e.g.
	// "0794.68104", "0197.43702"), confirmed against live zbMATH Open
	// records during x402-research-gateway#32's verification on
	// 2026-08-17. A prior seven-digit prefix never matched a real zbl id.
	zbmathRe = regexp.MustCompile(`(\d{4}\.\d{5})`)
)

func init() {
	// DOI: case-insensitive per the DOI handbook, so the normalized form is
	// lowercase. A resolver prefix (doi.org, dx.doi.org, a `doi:` scheme)
	// is stripped; the raw string keeps it.
	RegisterScheme(SchemeDOI, func(raw string) (string, string, bool) {
		m := doiRe.FindString(raw)
		if m == "" {
			return "", "", false
		}
		return strings.ToLower(strings.TrimRight(m, ".,;)")), "", true
	})
	RegisterScheme(SchemePMID, func(raw string) (string, string, bool) {
		v := strings.TrimSpace(raw)
		v = strings.TrimPrefix(strings.TrimPrefix(v, "pmid:"), "PMID:")
		v = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(v, "https://pubmed.ncbi.nlm.nih.gov/"), "/"))
		if !pmidRe.MatchString(v) {
			return "", "", false
		}
		return strings.TrimLeft(v, "0"), "", true
	})
	RegisterScheme(SchemePMCID, func(raw string) (string, string, bool) {
		m := pmcidRe.FindString(raw)
		if m == "" {
			return "", "", false
		}
		return strings.ToUpper(m), "", true
	})
	// arXiv: the version suffix is split off Value into Version so that
	// 2101.00001v1 and 2101.00001v3 share an exact key and relate as
	// versions rather than reading as two unrelated papers.
	RegisterScheme(SchemeArXiv, func(raw string) (string, string, bool) {
		if m := arxivNew.FindStringSubmatch(raw); m != nil {
			return m[1], m[2], true
		}
		if m := arxivOld.FindStringSubmatch(raw); m != nil {
			return strings.ToLower(m[1]), m[2], true
		}
		return "", "", false
	})
	RegisterScheme(SchemeOpenAlex, func(raw string) (string, string, bool) {
		m := openAlex.FindStringSubmatch(raw)
		if m == nil {
			return "", "", false
		}
		return strings.ToUpper(m[1]), "", true
	})
	RegisterScheme(SchemeSemanticScholar, func(raw string) (string, string, bool) {
		m := s2Re.FindStringSubmatch(raw)
		if m == nil {
			return "", "", false
		}
		return strings.ToLower(m[1]), "", true
	})
	// DBLP keys are path-shaped ("journals/cacm/Codd70") and case-bearing,
	// so the only normalization is stripping the resolver prefix and any
	// .html suffix.
	RegisterScheme(SchemeDBLP, func(raw string) (string, string, bool) {
		v := strings.TrimSpace(raw)
		for _, p := range []string{"https://dblp.org/rec/", "http://dblp.org/rec/", "dblp:"} {
			v = strings.TrimPrefix(v, p)
		}
		v = strings.TrimSuffix(v, ".html")
		if v == "" || strings.Contains(v, " ") {
			return "", "", false
		}
		return v, "", true
	})
	RegisterScheme(SchemeZbMATH, func(raw string) (string, string, bool) {
		m := zbmathRe.FindStringSubmatch(raw)
		if m == nil {
			return "", "", false
		}
		return m[1], "", true
	})
	RegisterScheme(SchemeORCID, func(raw string) (string, string, bool) {
		m := orcidRe.FindStringSubmatch(raw)
		if m == nil {
			return "", "", false
		}
		return strings.ToUpper(m[1]), "", true
	})
	RegisterScheme(SchemeROR, func(raw string) (string, string, bool) {
		m := rorRe.FindStringSubmatch(raw)
		if m == nil {
			return "", "", false
		}
		return strings.ToLower(m[1]), "", true
	})
	// DOAJ article ids are opaque hex strings the provider mints itself; no
	// normalization beyond trimming applies.
	RegisterScheme(SchemeDOAJ, func(raw string) (string, string, bool) {
		v := strings.TrimSpace(raw)
		if v == "" {
			return "", "", false
		}
		return v, "", true
	})
	// OpenAIRE result ids are opaque dedup-cluster keys
	// (e.g. "doi_dedup___::...", "openaire____::..."); no normalization
	// beyond trimming applies, matching the DBLP precedent above for a
	// provider-minted, path-shaped identifier.
	RegisterScheme(SchemeOpenAIRE, func(raw string) (string, string, bool) {
		v := strings.TrimSpace(raw)
		if v == "" {
			return "", "", false
		}
		return v, "", true
	})
	// PubChem CIDs, UniProt accessions, GBIF keys, OSF node ids, figshare
	// article ids, and NASA Exoplanet Archive planet names
	// (x402-research-gateway#16) are each opaque provider-local ids; no
	// normalization beyond trimming applies, matching the DOAJ/OpenAIRE
	// precedent above.
	for _, s := range []Scheme{SchemePubChemCID, SchemeUniProt, SchemeGBIF, SchemeOSF, SchemeFigshare, SchemeNASAExoplanet, SchemeUSPTOApplication, SchemeUSPTOPatent} {
		s := s
		RegisterScheme(s, func(raw string) (string, string, bool) {
			v := strings.TrimSpace(raw)
			if v == "" {
				return "", "", false
			}
			return v, "", true
		})
	}
}

// Parse sniffs the scheme from a raw string and normalizes under it. Order
// matters: PMCID is tried before DOI because a PMC URL contains no DOI, and
// arXiv before the numeric PMID rule because an arXiv id is also digits and
// a dot. A string no scheme claims returns ok=false.
func Parse(raw string) (Identifier, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Identifier{}, false
	}
	lower := strings.ToLower(s)
	order := []Scheme{}
	switch {
	case strings.Contains(lower, "pmc"):
		order = []Scheme{SchemePMCID}
	case strings.Contains(lower, "arxiv"):
		order = []Scheme{SchemeArXiv}
	case strings.Contains(lower, "orcid"):
		order = []Scheme{SchemeORCID}
	case strings.Contains(lower, "ror.org"):
		order = []Scheme{SchemeROR}
	case strings.Contains(lower, "dblp"):
		order = []Scheme{SchemeDBLP}
	case strings.Contains(lower, "zbmath"):
		order = []Scheme{SchemeZbMATH}
	case strings.Contains(lower, "openalex"):
		order = []Scheme{SchemeOpenAlex}
	case strings.Contains(lower, "semanticscholar"):
		order = []Scheme{SchemeSemanticScholar}
	case strings.Contains(lower, "pubmed") || strings.HasPrefix(lower, "pmid"):
		order = []Scheme{SchemePMID}
	}
	// An explicit `scheme:value` prefix wins over every heuristic below.
	if i := strings.Index(s, ":"); i > 0 {
		if _, known := schemes[Scheme(strings.ToLower(s[:i]))]; known {
			order = append([]Scheme{Scheme(strings.ToLower(s[:i]))}, order...)
		}
	}
	order = append(order, SchemeDOI, SchemeArXiv, SchemeOpenAlex, SchemeSemanticScholar, SchemeORCID, SchemePMID)
	for _, sc := range order {
		if id, ok := New(sc, s); ok {
			return id, true
		}
	}
	return Identifier{Raw: raw}, false
}
