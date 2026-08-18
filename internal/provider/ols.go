package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// OLS adapter (x402-research-gateway#14, #15).
//
// The EBI Ontology Lookup Service (OLS4) is a single REST surface over the
// Gene Ontology and every OBO Foundry ontology it indexes, which is why one
// adapter here covers both source families the issue names rather than one
// adapter per ontology. Verified live against www.ebi.ac.uk on 2026-08-18
// (polite single GETs, no key required):
//
//	GET /ols4/api/ontologies/{onto}
//	  -> {"version":"2026-07-26", "config":{"versionIri":...,"title":...}}
//	GET /ols4/api/search?q={q}&ontology={onto}
//	  -> {"response":{"docs":[{"iri":...,"label":...,"synonym":[...]}]}}
//	GET /ols4/api/ontologies/{onto}/terms/{doubleURLEncoded(iri)}
//	  -> {"iri":..., "label":..., "synonyms":[...], "description":[...],
//	      "is_obsolete":bool, "term_replaced_by":iri|null, "consider":[iri,...]}
//	GET .../terms/{iri}/parents | /children
//	  -> {"_embedded":{"terms":[...]}}
//
// is_obsolete + term_replaced_by/consider is GO's (and OBO's generally)
// deprecation model: a term stays permanently resolvable with a
// deprecation flag and, where the source recorded one, a direct
// replacement (term_replaced_by, exact) or a set of replacement
// candidates (consider, non-exact) — which is exactly the "successor
// relations do not imply equivalence" and "partial mappings expressible"
// shape #15 requires, verified against a live obsolete term
// (GO:0000005) rather than assumed from documentation.
type OLSProvider struct {
	Client    *http.Client
	BaseURL   string // default https://www.ebi.ac.uk/ols4/api
	UserAgent string
	// Ontology is the OLS ontology id this instance serves: "go" for Gene
	// Ontology, or any other OBO Foundry id OLS indexes (chebi, hp, mondo,
	// ...). One OLSProvider serves one ontology, matching the registry
	// convention of one provider entry per source.
	Ontology string
}

// NewOLSProvider returns a provider with polite defaults for one ontology.
func NewOLSProvider(ontology string) *OLSProvider {
	return &OLSProvider{
		Client:    &http.Client{Timeout: 15 * time.Second},
		BaseURL:   "https://www.ebi.ac.uk/ols4/api",
		UserAgent: "x402-research-gateway/vocabulary (+https://github.com/bucket-foundation/x402-research-gateway)",
		Ontology:  ontology,
	}
}

type olsSearchDoc struct {
	IRI      string   `json:"iri"`
	Label    string   `json:"label"`
	Synonyms []string `json:"synonym"`
	Ontology string   `json:"ontology_name"`
}

type olsSearchResponse struct {
	Response struct {
		Docs []olsSearchDoc `json:"docs"`
	} `json:"response"`
}

type olsTerm struct {
	IRI            string   `json:"iri"`
	Label          string   `json:"label"`
	Description    []string `json:"description"`
	Synonyms       []string `json:"synonyms"`
	IsObsolete     bool     `json:"is_obsolete"`
	TermReplacedBy *string  `json:"term_replaced_by"`
	Consider       []string `json:"consider"`
}

type olsEmbeddedTerms struct {
	Embedded struct {
		Terms []olsTerm `json:"terms"`
	} `json:"_embedded"`
}

func (p *OLSProvider) get(ctx context.Context, path string) ([]byte, error) {
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
		return nil, fmt.Errorf("ols: %s returned %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// olsTermPath double-URL-encodes the term IRI, which is what OLS4's REST
// API requires of a path segment that is itself a URL.
func olsTermPath(iri string) string {
	return url.QueryEscape(url.QueryEscape(iri))
}

func olsTermToConcept(t olsTerm, release string) Concept {
	c := Concept{
		ID:            t.IRI,
		PrefLabel:     t.Label,
		AltLabels:     t.Synonyms,
		SourceRelease: release,
		Deprecated:    t.IsObsolete,
	}
	if len(t.Description) > 0 {
		c.Definition = t.Description[0]
	}
	if t.TermReplacedBy != nil && *t.TermReplacedBy != "" {
		// A direct replacement the source names exactly: still not
		// asserted as equivalence, just the strongest pointer OBO's model
		// offers.
		c.SupersededBy = []string{*t.TermReplacedBy}
	}
	if len(t.Consider) > 0 {
		// "consider" is deliberately weaker than term_replaced_by: OBO's
		// own convention for "candidates, no exact successor." Kept in
		// Successor rather than merged into SupersededBy so a caller can
		// tell an exact replacement from a set of candidates.
		c.Successor = t.Consider
	}
	return c
}

// SearchTerms implements TermSearcher. release, if non-empty, is only
// honored when it matches the ontology's current version (OLS serves one
// live version per ontology id; historical OWL releases are not queryable
// through this REST surface, only through OBO's dated PURLs, which the
// registry records under Sync for direct download).
func (p *OLSProvider) SearchTerms(query, release string) ([]Concept, bool) {
	rel, ok := p.CurrentRelease()
	if release != "" && (!ok || release != rel.Release) {
		return nil, false
	}
	body, err := p.get(context.Background(), "/search?q="+url.QueryEscape(query)+"&ontology="+p.Ontology)
	if err != nil {
		return nil, false
	}
	var parsed olsSearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, false
	}
	out := make([]Concept, 0, len(parsed.Response.Docs))
	for _, d := range parsed.Response.Docs {
		out = append(out, Concept{
			ID: d.IRI, PrefLabel: d.Label, AltLabels: d.Synonyms, SourceRelease: rel.Release,
		})
	}
	return out, true
}

// GetConcept fetches one term by IRI.
func (p *OLSProvider) GetConcept(id, release string) (Concept, bool) {
	rel, relOK := p.CurrentRelease()
	if release != "" && (!relOK || release != rel.Release) {
		return Concept{}, false
	}
	body, err := p.get(context.Background(), "/ontologies/"+p.Ontology+"/terms/"+olsTermPath(id))
	if err != nil {
		return Concept{}, false
	}
	var t olsTerm
	if err := json.Unmarshal(body, &t); err != nil {
		return Concept{}, false
	}
	c := olsTermToConcept(t, rel.Release)
	c.Native = json.RawMessage(body)
	c.NativeFormat = "obo-owl-json"
	return c, true
}

// Broader/Narrower walk OWL subsumption (rdfs:subClassOf), which OLS
// exposes as parents/children — a real hierarchy relation, not a
// SKOS-broader relation coerced from something weaker: preserving that
// distinction is the point of #14's "do not force OWL" rule, and it cuts
// the other way here too, since OLS's parents/children ARE subsumption and
// this adapter reports them as exactly that rather than downgrading them
// to a generic "broader."
func (p *OLSProvider) Broader(id, release string) ([]Concept, bool) {
	return p.walkHierarchy(id, release, "/parents")
}

func (p *OLSProvider) Narrower(id, release string) ([]Concept, bool) {
	return p.walkHierarchy(id, release, "/children")
}

func (p *OLSProvider) walkHierarchy(id, release, edge string) ([]Concept, bool) {
	rel, relOK := p.CurrentRelease()
	if release != "" && (!relOK || release != rel.Release) {
		return nil, false
	}
	body, err := p.get(context.Background(), "/ontologies/"+p.Ontology+"/terms/"+olsTermPath(id)+edge)
	if err != nil {
		// A 404 here legitimately means "no parents" (root term) or "no
		// children" (leaf term), which OLS reports as a missing
		// _embedded rather than an empty list; either way the operation
		// is supported, it found nothing.
		return nil, true
	}
	var parsed olsEmbeddedTerms
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, true
	}
	out := make([]Concept, 0, len(parsed.Embedded.Terms))
	for _, t := range parsed.Embedded.Terms {
		out = append(out, olsTermToConcept(t, rel.Release))
	}
	return out, true
}

// Synonyms reads the same term document GetConcept does; kept as a
// separate method per the SynonymProvider interface so a caller wanting
// only synonyms doesn't have to reason about the rest of the Concept.
func (p *OLSProvider) Synonyms(id, release string) ([]string, bool) {
	c, ok := p.GetConcept(id, release)
	if !ok {
		return nil, false
	}
	return c.AltLabels, true
}

// DeprecatedTerms is not implemented as a bulk listing: OLS has no cheap
// "all obsolete terms in this ontology" endpoint short of paging the whole
// term set, which would be the expensive, non-"cheap by construction"
// traffic #22 exists to avoid generating. A caller checking one term's
// deprecation status uses GetConcept, which reports Deprecated/SupersededBy
// per term at no extra cost.
func (p *OLSProvider) DeprecatedTerms(release string) ([]Concept, bool) { return nil, false }

// CurrentRelease reports the ontology's live OLS version.
func (p *OLSProvider) CurrentRelease() (VocabularyRelease, bool) {
	body, err := p.get(context.Background(), "/ontologies/"+p.Ontology)
	if err != nil {
		return VocabularyRelease{}, false
	}
	var parsed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Version == "" {
		return VocabularyRelease{}, false
	}
	return VocabularyRelease{Release: parsed.Version}, true
}
