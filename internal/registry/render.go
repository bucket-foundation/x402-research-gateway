package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SectionTitles names the RESEARCH-INDEX.md sections the registry generates.
// A provider's Section field selects one of these.
var SectionTitles = map[string]string{
	"1.1":  "Mathematics",
	"1.2":  "Physics",
	"1.3":  "Chemistry",
	"1.4":  "Information / Computation",
	"1.5":  "Biophysics / Life Sciences",
	"1.6":  "Cosmology / Astronomy / Space",
	"1.7":  "Mind / Neuroscience / Psychology",
	"1.8":  "Earth Sciences",
	"1.9":  "Cross-cutting discovery APIs",
	"1.10": "Vocabularies, ontologies, and semantic standards",
}

// mdEscape keeps a cell from breaking the surrounding table. Pipes are the
// only character that can, since the rest is rendered as-is.
func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func code(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return "`" + s + "`"
}

// RenderSection1 writes the generated per-domain tables.
func (r *Registry) RenderSection1() string {
	var b strings.Builder
	b.WriteString("## Section 1 — Canonical APIs (clean legal status)\n")

	keys := make([]string, 0, len(SectionTitles))
	for k := range SectionTitles {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		providers := r.InSection(key)
		if len(providers) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s — %s\n\n", key, SectionTitles[key])
		b.WriteString("| Name | Status | Type | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Redistribution | Tier fit | source_prefix | Canonical URL template | Last verified | Notes |\n")
		b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")

		for _, p := range providers {
			redist := p.Rights.Redistribution
			if redist == "" {
				redist = "unknown"
			}
			fmt.Fprintf(&b, "| **%s** | `%s` | `%s` | %s | %s | %s | %s | %s | %s | `%s` | %s | %s | %s | %s | %s |\n",
				mdEscape(p.Name),
				p.Status,
				p.Type,
				mdEscape(orDash(p.Coverage)),
				mdEscape(orDash(p.CorpusSize)),
				code(mdEscape(p.BaseURL)),
				mdEscape(orDash(p.Auth)),
				mdEscape(orDash(p.RateLimit)),
				mdEscape(orDash(p.License)),
				redist,
				mdEscape(orDash(p.TierFit)),
				code(mdEscape(p.SourcePrefix)),
				code(mdEscape(p.CanonicalURL)),
				mdEscape(orDash(p.LastVerified)),
				mdEscape(orDash(p.Notes)),
			)
		}
	}
	return b.String()
}

// RenderCoverage writes the lifecycle summary, which is what makes the
// backlog visible: how many sources are known, and how many are actually
// served.
func (r *Registry) RenderCoverage() string {
	var b strings.Builder
	b.WriteString("## Coverage\n\n")
	fmt.Fprintf(&b, "%d sources registered · reviewed %s\n\n", r.Len(), r.LastReviewed)
	b.WriteString("| Lifecycle status | Sources |\n|---|---|\n")

	counts := r.StatusCounts()
	for _, s := range Statuses {
		if counts[s] == 0 {
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %d |\n", s, counts[s])
	}

	b.WriteString("\n| Provider type | Sources |\n|---|---|\n")
	typeCounts := map[ProviderType]int{}
	for i := range r.Providers {
		typeCounts[r.Providers[i].Type]++
	}
	for _, t := range ProviderTypes {
		if typeCounts[t] == 0 {
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %d |\n", t, typeCounts[t])
	}
	return b.String()
}

// RenderIndex composes the whole document: hand-written prose partials around
// the generated tables. The prose partials are emitted verbatim; the generator
// never rewrites human judgment.
func (r *Registry) RenderIndex(proseDir string) (string, error) {
	read := func(name string) (string, error) {
		raw, err := os.ReadFile(filepath.Join(proseDir, name))
		if err != nil {
			return "", fmt.Errorf("read prose partial %s: %w", name, err)
		}
		return strings.TrimRight(string(raw), "\n"), nil
	}

	prologue, err := read("00-prologue.md")
	if err != nil {
		return "", err
	}
	s2, err := read("20-section-2-grey-literature.md")
	if err != nil {
		return "", err
	}
	s3, err := read("30-section-3-priority.md")
	if err != nil {
		return "", err
	}
	s4, err := read("40-section-4-parser-reuse.md")
	if err != nil {
		return "", err
	}
	hk, err := read("90-housekeeping.md")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(GeneratedBanner)
	b.WriteString("\n")
	b.WriteString(prologue)
	b.WriteString("\n\n")
	b.WriteString(r.RenderCoverage())
	b.WriteString("\n---\n\n")
	b.WriteString(r.RenderSection1())
	b.WriteString("\n---\n\n")
	b.WriteString(s2)
	b.WriteString("\n\n---\n\n")
	b.WriteString(s3)
	b.WriteString("\n\n---\n\n")
	b.WriteString(s4)
	b.WriteString("\n\n---\n\n")
	b.WriteString(hk)
	b.WriteString("\n")
	return b.String(), nil
}

// GeneratedBanner marks the document as generated so nobody hand-edits the
// tables and loses the edit on the next run.
const GeneratedBanner = `<!-- GENERATED FILE — DO NOT EDIT BY HAND.
     Tables are generated from config/providers.yaml by ` + "`make research-index`" + `.
     Prose sections live in docs/research-index/ and are preserved verbatim.
     Edit the registry or the prose partials, then regenerate. -->
`
