package identity

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// SourceRecord is one provider's view of one record, handed to the
// resolver. Nothing here is merged or rewritten: the resolver reads it,
// emits a Node that carries it verbatim, and adds relations.
type SourceRecord struct {
	// Provider is the route/adapter id that produced this record, e.g.
	// "openalex-works". It becomes the citation's provider attribution.
	Provider string
	// ProviderRecordID is the provider-local identifier, unprefixed. It
	// survives resolution untouched.
	ProviderRecordID string
	// CanonicalURL is the record's stable public address at the provider.
	CanonicalURL string
	// Identifiers are the cross-provider identifiers this record carries,
	// including its own. Duplicates are fine.
	Identifiers []Identifier
	// Title, Authors, Year feed similarity inference only. Absent values
	// disable fuzzy matching for this record rather than weakening it.
	Title   string
	Authors []string
	Year    int
	// Raw is the provider's original bytes for this record, preserved so a
	// consumer never has to re-fetch to see what the upstream said.
	Raw json.RawMessage
	// AssertedRelations are relations the provider itself published,
	// carried through with EvidenceProviderAsserted untouched.
	AssertedRelations []Relation
}

// NodeID is the stable address of a record inside a resolution graph:
// `provider:provider_record_id`. Provider-local, so a record stays
// addressable no matter what the resolver concludes about it. A preprint
// node and its published node have different NodeIDs and both survive.
func NodeID(provider, recordID string) string { return provider + ":" + recordID }

// Node is one record in the graph.
type Node struct {
	ID           string          `json:"id"`
	Provider     string          `json:"provider"`
	RecordID     string          `json:"record_id"`
	CanonicalURL string          `json:"canonical_url,omitempty"`
	Identifiers  []Identifier    `json:"identifiers,omitempty"`
	Title        string          `json:"title,omitempty"`
	Year         int             `json:"year,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

// Graph is the resolution result: every input record as its own node, plus
// typed relations. There is no canonical merged entity, by design. Callers
// that want one make that judgment themselves from the relations.
type Graph struct {
	Nodes     []Node     `json:"nodes"`
	Relations []Relation `json:"relations"`
	// Providers lists the contributing providers in sorted order, so a
	// caller can build one feed402 citation per provider without walking
	// every node.
	Providers []string `json:"providers"`
}

// Add appends a relation after validating it. A relation with invalid
// evidence is dropped. An attempt to record RelSameWork on gateway-inferred
// similarity evidence is rewritten to RelPossibleSameWork rather than
// accepted: the no-auto-promotion rule is enforced here so no caller can
// bypass it.
func (g *Graph) Add(rel Relation) {
	if rel.From == "" || rel.To == "" || rel.From == rel.To {
		return
	}
	if !rel.Evidence.Valid() {
		return
	}
	if rel.Type == RelSameWork &&
		rel.Evidence.Kind == EvidenceGatewayInferred &&
		rel.Evidence.Method == MethodTitleAuthor {
		rel.Type = RelPossibleSameWork
	}
	for _, existing := range g.Relations {
		if existing == rel {
			return
		}
	}
	g.Relations = append(g.Relations, rel)
}

// Resolver turns a set of provider records into a graph.
type Resolver struct {
	// Now supplies the retrieval timestamp. Injectable so tests are
	// deterministic. Nil means time.Now.
	Now func() time.Time
	// SimilarityThreshold is the minimum score at which a title/author
	// match is recorded as RelPossibleSameWork. Zero means the default.
	SimilarityThreshold float64
}

const defaultSimilarityThreshold = 0.82

func (r *Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Resolver) threshold() float64 {
	if r.SimilarityThreshold > 0 {
		return r.SimilarityThreshold
	}
	return defaultSimilarityThreshold
}

// Resolve builds the graph. Output ordering is deterministic: nodes follow
// input order, relations follow the order they were derived over sorted
// node pairs, so two runs over the same input produce byte-identical JSON.
func (r *Resolver) Resolve(records []SourceRecord) Graph {
	at := r.now()
	g := Graph{}

	providerSet := map[string]bool{}
	byKey := map[string][]int{} // identifier key -> node indices
	for _, rec := range records {
		node := Node{
			ID:           NodeID(rec.Provider, rec.ProviderRecordID),
			Provider:     rec.Provider,
			RecordID:     rec.ProviderRecordID,
			CanonicalURL: rec.CanonicalURL,
			Identifiers:  dedupeIdentifiers(rec.Identifiers),
			Title:        rec.Title,
			Year:         rec.Year,
			Raw:          rec.Raw,
		}
		idx := len(g.Nodes)
		g.Nodes = append(g.Nodes, node)
		providerSet[rec.Provider] = true
		for _, id := range node.Identifiers {
			if id.Value == "" {
				continue
			}
			byKey[id.Key()] = append(byKey[id.Key()], idx)
		}
		// Provider assertions pass through verbatim. The gateway does not
		// second-guess them and does not deduplicate a disagreement away:
		// two providers asserting contradictory relations both appear.
		for _, rel := range rec.AssertedRelations {
			g.Add(rel)
		}
	}

	// Exact identifier co-occurrence. Two nodes carrying the same
	// scheme:value are the same registered record, which is a fact, so
	// same_work is recorded with the identifier scheme as the detail.
	// Different versions of the same base identifier get version_of
	// instead, so an arXiv v1 is never erased by its v3.
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		idxs := byKey[k]
		for i := 0; i < len(idxs); i++ {
			for j := i + 1; j < len(idxs); j++ {
				a, b := idxs[i], idxs[j]
				va := versionFor(g.Nodes[a], k)
				vb := versionFor(g.Nodes[b], k)
				if va != vb && (va != "" || vb != "") {
					from, to := a, b
					if versionLess(vb, va) {
						from, to = b, a
					}
					g.Add(Relation{
						From:     g.Nodes[from].ID,
						To:       g.Nodes[to].ID,
						Type:     RelVersionOf,
						Evidence: GatewayInferred(MethodArXivVersion, k, 0, at),
					})
					continue
				}
				g.Add(Relation{
					From:     g.Nodes[a].ID,
					To:       g.Nodes[b].ID,
					Type:     RelSameWork,
					Evidence: GatewayInferred(MethodSharedIdentifier, schemeOf(k), 0, at),
				})
			}
		}
	}

	// Similarity. Only for node pairs with no exact link already, and the
	// result is always possible_same_work.
	linked := map[[2]string]bool{}
	for _, rel := range g.Relations {
		linked[[2]string{rel.From, rel.To}] = true
		linked[[2]string{rel.To, rel.From}] = true
	}
	for i := 0; i < len(g.Nodes); i++ {
		for j := i + 1; j < len(g.Nodes); j++ {
			if linked[[2]string{g.Nodes[i].ID, g.Nodes[j].ID}] {
				continue
			}
			score := Similarity(records[i], records[j])
			if score < r.threshold() {
				continue
			}
			g.Add(Relation{
				From:     g.Nodes[i].ID,
				To:       g.Nodes[j].ID,
				Type:     RelPossibleSameWork,
				Evidence: GatewayInferred(MethodTitleAuthor, "", round2(score), at),
			})
		}
	}

	for p := range providerSet {
		g.Providers = append(g.Providers, p)
	}
	sort.Strings(g.Providers)
	return g
}

func schemeOf(key string) string {
	if i := strings.Index(key, ":"); i > 0 {
		return key[:i]
	}
	return key
}

func versionFor(n Node, key string) string {
	for _, id := range n.Identifiers {
		if id.Key() == key {
			return id.Version
		}
	}
	return ""
}

// versionLess orders version strings so version_of points from the lower
// version to the higher one. An absent version sorts lowest.
func versionLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func dedupeIdentifiers(ids []Identifier) []Identifier {
	seen := map[string]bool{}
	out := make([]Identifier, 0, len(ids))
	for _, id := range ids {
		k := id.String() + "|" + id.Raw
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Similarity scores two records in [0,1] from title tokens and author
// surnames. It is deliberately conservative: a record missing a title
// scores 0, because guessing on thin metadata is what produces the wrong
// merges this package exists to avoid. A year disagreement of more than one
// caps the score below any usable threshold.
func Similarity(a, b SourceRecord) float64 {
	ta, tb := titleTokens(a.Title), titleTokens(b.Title)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	titleScore := jaccard(ta, tb)
	if a.Year > 0 && b.Year > 0 && absInt(a.Year-b.Year) > 1 {
		return titleScore * 0.5
	}
	sa, sb := surnames(a.Authors), surnames(b.Authors)
	if len(sa) == 0 || len(sb) == 0 {
		// No author evidence on either side. Title alone is weaker
		// evidence, so it is discounted rather than trusted outright.
		return titleScore * 0.9
	}
	authorScore := jaccard(sa, sb)
	return 0.75*titleScore + 0.25*authorScore
}

var titleSplit = strings.NewReplacer(
	"-", " ", ":", " ", ";", " ", ",", " ", ".", " ", "(", " ", ")", " ",
	"[", " ", "]", " ", "/", " ", "\"", " ", "'", " ",
)

var titleStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "on": true, "in": true,
	"for": true, "and": true, "to": true, "with": true, "by": true,
}

func titleTokens(title string) map[string]bool {
	out := map[string]bool{}
	for _, tok := range strings.Fields(titleSplit.Replace(strings.ToLower(title))) {
		if len(tok) < 2 || titleStopwords[tok] {
			continue
		}
		out[tok] = true
	}
	return out
}

// surnames takes the last whitespace-separated component of each author
// string. It handles "Jane Q. Doe" and "Doe, Jane" crudely, which is why
// the output is similarity evidence and never identity.
func surnames(authors []string) map[string]bool {
	out := map[string]bool{}
	for _, a := range authors {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if i := strings.Index(a, ","); i > 0 {
			out[strings.TrimSpace(a[:i])] = true
			continue
		}
		fields := strings.Fields(a)
		if len(fields) > 0 {
			out[fields[len(fields)-1]] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
