package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MeSH adapter (x402-research-gateway#14, #15).
//
// Verified live against id.nlm.nih.gov on 2026-08-18 (polite single GETs,
// no key required):
//
//	GET https://id.nlm.nih.gov/mesh/lookup/term?label={q}&match=exact&limit={n}
//	  -> [{"resource":"http://id.nlm.nih.gov/mesh/D017209","label":"Apoptosis"}]
//	GET https://id.nlm.nih.gov/mesh/{descriptorID}.json
//	  -> full descriptor: broaderDescriptor, previousIndexing, dateIntroduced,
//	     active (bool), label
//
// MeSH is the vocabulary #15 was filed around for its annual re-issue and
// its previousIndexing field, which is a first-class historical alias: the
// live D017209 record's previousIndexing reads "Cell Survival (1972-1992)"
// for what is now labeled "Apoptosis," so a query written against the 1980s
// heading has somewhere to resolve. active=false plus a broaderDescriptor
// pointer is how MeSH expresses a heading folded into a broader one.
//
// This adapter reads only the current descriptor endpoint id.nlm.nih.gov
// serves; it does not hold a full annual-release snapshot, so
// GetConcept's release parameter is currently only honored when empty
// (current). Registry.Sync records that MeSH publishes full RDF release
// downloads separately (mesh.nlm.nih.gov) for anyone needing a specific
// past year's edition offline.
type MeSHProvider struct {
	Client    *http.Client
	BaseURL   string // default https://id.nlm.nih.gov/mesh
	UserAgent string
}

// NewMeSHProvider returns a provider with polite defaults.
func NewMeSHProvider() *MeSHProvider {
	return &MeSHProvider{
		Client:    &http.Client{Timeout: 15 * time.Second},
		BaseURL:   "https://id.nlm.nih.gov/mesh",
		UserAgent: "x402-research-gateway/vocabulary (+https://github.com/bucket-foundation/x402-research-gateway)",
	}
}

type meshLookupHit struct {
	Resource string `json:"resource"`
	Label    string `json:"label"`
}

type meshDescriptor struct {
	ID                string `json:"identifier"`
	Label             meshLangValue
	Active            bool
	BroaderDescriptor string
	DateIntroduced    string
	PreviousIndexing  meshLangValue
}

// meshLangValue absorbs MeSH's {"@language":"en","@value":"..."} shape,
// which several fields use for a plain string. Language is kept alongside
// Value (x402-research-gateway#21): every id.nlm.nih.gov response this
// adapter reads carries an explicit @language tag on these fields, and a
// prior revision read only @value, discarding the language MeSH itself
// stated on every single response.
type meshLangValue struct {
	Value    string
	Language string
}

func (v *meshLangValue) UnmarshalJSON(b []byte) error {
	var wrapped struct {
		Value    string `json:"@value"`
		Language string `json:"@language"`
	}
	if err := json.Unmarshal(b, &wrapped); err == nil && wrapped.Value != "" {
		v.Value = wrapped.Value
		v.Language = wrapped.Language
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v.Value = s
	}
	return nil
}

// meshRaw captures the fields meshDescriptor needs out of the flat JSON-LD
// document id.nlm.nih.gov serves, without requiring a full JSON-LD parser.
type meshRaw struct {
	ID                string        `json:"identifier"`
	Label             meshLangValue `json:"label"`
	Active            bool          `json:"http://id.nlm.nih.gov/mesh/vocab#active"`
	BroaderDescriptor string        `json:"broaderDescriptor"`
	DateIntroduced    string        `json:"dateIntroduced"`
	PreviousIndexing  meshLangValue `json:"previousIndexing"`
}

func (p *MeSHProvider) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mesh: %s returned %d", path, resp.StatusCode)
	}
	// A descriptor document is a few KB; cap at 4MiB against a misbehaving
	// server rather than trusting Content-Length.
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// SearchTerms implements TermSearcher via the lookup/term endpoint. release
// is currently only honored when empty: MeSH's live lookup endpoint always
// answers from the current release.
func (p *MeSHProvider) SearchTerms(query, release string) ([]Concept, bool) {
	if release != "" {
		return nil, false
	}
	body, err := p.get(context.Background(), "/lookup/term?label="+url.QueryEscape(query)+"&match=contains&limit=20")
	if err != nil {
		return nil, false
	}
	var hits []meshLookupHit
	if err := json.Unmarshal(body, &hits); err != nil {
		return nil, false
	}
	out := make([]Concept, 0, len(hits))
	for _, h := range hits {
		id := strings.TrimPrefix(h.Resource, "http://id.nlm.nih.gov/mesh/")
		out = append(out, Concept{ID: id, PrefLabel: h.Label, SourceRelease: p.currentReleaseLabel()})
	}
	return out, true
}

// GetConcept fetches one descriptor's full record.
func (p *MeSHProvider) GetConcept(id, release string) (Concept, bool) {
	if release != "" {
		return Concept{}, false
	}
	body, err := p.get(context.Background(), "/"+id+".json")
	if err != nil {
		return Concept{}, false
	}
	var raw meshRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return Concept{}, false
	}
	c := Concept{
		ID:                id,
		PrefLabel:         raw.Label.Value,
		PrefLabelLanguage: raw.Label.Language,
		SourceRelease:     p.currentReleaseLabel(),
		IntroducedIn:      raw.DateIntroduced,
		Deprecated:        !raw.Active,
		Native:            json.RawMessage(body),
		NativeFormat:      "mesh-jsonld",
	}
	if raw.Label.Language != "" {
		// PrefLabel restated as a Labels entry so a caller reading only
		// Labels (the multilingual surface) still finds this record's own
		// language-tagged label, not just callers reading PrefLabel
		// directly.
		c.Labels = append(c.Labels, LocalizedForm{
			Value: raw.Label.Value, Language: raw.Label.Language,
			Kind: FormOriginal, Provider: "mesh",
		})
	}
	if raw.PreviousIndexing.Value != "" {
		// previousIndexing carries the concept's prior label(s) with the
		// years it was indexed under them, e.g. "Cell Survival
		// (1972-1992)" — MeSH's own historical-alias record.
		c.HistoricalAliases = []string{raw.PreviousIndexing.Value}
	}
	// broaderDescriptor is hierarchy (skos:broader-shaped), not lineage:
	// it never populates Predecessor/Successor, which are reserved for a
	// vocabulary's own migration/replacement relations.
	return c, true
}

// Broader implements BroaderNarrowerProvider via the descriptor's
// broaderDescriptor field. MeSH's public API does not expose a reverse
// (narrower) index cheaply, so Narrower reports unsupported rather than
// fabricating a scan of the whole vocabulary.
func (p *MeSHProvider) Broader(id, release string) ([]Concept, bool) {
	c, ok := p.GetConcept(id, release)
	if !ok {
		return nil, false
	}
	var raw meshRaw
	if err := json.Unmarshal(c.Native, &raw); err != nil || raw.BroaderDescriptor == "" {
		return nil, true // valid answer: this descriptor has no recorded broader term.
	}
	broaderID := strings.TrimPrefix(raw.BroaderDescriptor, "http://id.nlm.nih.gov/mesh/")
	broader, ok := p.GetConcept(broaderID, release)
	if !ok {
		return nil, true
	}
	return []Concept{broader}, true
}

// Narrower is unsupported: MeSH's REST surface has no cheap reverse index.
func (p *MeSHProvider) Narrower(id, release string) ([]Concept, bool) { return nil, false }

// HistoricalTerms returns the concept itself annotated with its
// previousIndexing alias, which is where MeSH's historical terminology
// lives: it is not a separate lookup, it is a field on the current record.
func (p *MeSHProvider) HistoricalTerms(id string) ([]Concept, bool) {
	c, ok := p.GetConcept(id, "")
	if !ok || len(c.HistoricalAliases) == 0 {
		return nil, ok
	}
	return []Concept{c}, true
}

// CurrentRelease reports MeSH's release label. MeSH re-issues annually and
// does not expose a machine-readable "current year" endpoint on
// id.nlm.nih.gov, so this is the best available signal: the calendar year,
// which is how NLM's own documentation names each release ("2026 MeSH").
func (p *MeSHProvider) CurrentRelease() (VocabularyRelease, bool) {
	return VocabularyRelease{
		Release: p.currentReleaseLabel(),
		Notes:   "id.nlm.nih.gov serves the current annual MeSH release; NLM does not expose a machine-readable release identifier on this endpoint as of 2026-08-18.",
	}, true
}

func (p *MeSHProvider) currentReleaseLabel() string {
	return fmt.Sprintf("%d", time.Now().Year())
}
