// Package coverage answers "what does this gateway not cover"
// (x402-research-gateway#20).
//
// Seven routes and a prose index left nobody able to tell whether a field is
// uncovered because nobody looked, because the sources are licence-blocked,
// or because an adapter is half-built. Those are three different answers and
// the difference is the whole point: "we have not implemented this" reading
// as "this source does not exist" is how a coverage gap becomes a false
// negative in someone's research.
//
// The report is derived from the registry rather than maintained beside it,
// so it cannot drift from the data it describes. Nothing here is a statement
// about the underlying science: a field at not_researched is a fact about
// this gateway.
package coverage

import (
	"sort"
	"strings"

	"github.com/gianyrox/x402-research-gateway/internal/registry"
)

// State is what the gateway can honestly say about one field and one
// dimension. Every state is a distinct answer, and none of them may be
// rendered as an empty result or a silent absence.
type State string

const (
	// StateNotResearched is an admission that nobody has looked.
	StateNotResearched State = "not_researched"
	// StateSourceKnown means a source is known to exist and nothing more
	// has been established about it.
	StateSourceKnown State = "source_known"
	// StateRegistered means a source has a complete, reviewed registry
	// entry and no adapter.
	StateRegistered State = "registered"
	// StateAdapterImplemented means an adapter exists and serves this
	// dimension.
	StateAdapterImplemented State = "adapter_implemented"
	// StateCoverageIncomplete means an adapter exists and does not cover
	// everything the source offers.
	StateCoverageIncomplete State = "coverage_incomplete"
	// StateLicenseBlocked means a source exists and cannot be served.
	StateLicenseBlocked State = "license_blocked"
)

// States is the closed set, in ascending order of what the gateway can do.
var States = []State{
	StateNotResearched, StateSourceKnown, StateRegistered,
	StateLicenseBlocked, StateCoverageIncomplete, StateAdapterImplemented,
}

// rank orders states so a field's best available answer wins the summary
// while every contributing provider stays listed. license_blocked ranks
// below a working adapter and above a bare registry entry: it is a real,
// established fact about a source.
var rank = map[State]int{
	StateNotResearched: 0, StateSourceKnown: 1, StateRegistered: 2,
	StateLicenseBlocked: 3, StateCoverageIncomplete: 4, StateAdapterImplemented: 5,
}

// Dimension is one axis of coverage.
type Dimension string

const (
	DimLiteratureMetadata Dimension = "literature_metadata"
	DimCitationGraph      Dimension = "citation_graph"
	DimFullText           Dimension = "full_text"
	DimOntology           Dimension = "ontology"
	DimDataset            Dimension = "dataset"
	DimSoftware           Dimension = "software"
	DimPatent             Dimension = "patent"
	DimHistoricalDepth    Dimension = "historical_depth"
	DimLanguage           Dimension = "language"
)

// Dimensions is the closed set this revision reports.
var Dimensions = []Dimension{
	DimLiteratureMetadata, DimCitationGraph, DimFullText, DimOntology,
	DimDataset, DimSoftware, DimPatent, DimHistoricalDepth, DimLanguage,
}

// Notice is emitted in every report.
const Notice = "This report describes this gateway, never the underlying science. A field at " +
	"not_researched means nobody here has looked, which is not evidence that no source exists. " +
	"license_blocked means a source exists and cannot be served. coverage_incomplete means an adapter " +
	"exists and does not reach everything the source offers. Every field and dimension appears in the " +
	"report, including the ones at not_researched, so a gap is never a missing row."

// ProviderNote is one provider's contribution to one dimension.
type ProviderNote struct {
	ProviderID string `json:"provider_id"`
	Name       string `json:"name"`
	State      State  `json:"state"`
	// Status is the provider's registry lifecycle state, so a consumer can
	// see what the coverage state was derived from.
	Status string `json:"status"`
	// Reason names why this state and not a better one, for the states that
	// need it.
	Reason string `json:"reason,omitempty"`
}

// DimensionCoverage is one field's answer on one dimension.
type DimensionCoverage struct {
	Dimension Dimension `json:"dimension"`
	State     State     `json:"state"`
	// Providers are the contributing providers, best state first. Empty
	// when the state is not_researched, which is the honest rendering of
	// "nobody has looked."
	Providers []ProviderNote `json:"providers,omitempty"`
}

// FieldCoverage is everything the gateway can say about one field.
type FieldCoverage struct {
	Field      string              `json:"field"`
	Dimensions []DimensionCoverage `json:"dimensions"`
	// HistoricalFrom is the earliest year any implemented provider in this
	// field reaches, and HistoricalFromAnySource the earliest any known
	// source reaches. The two differ exactly where an adapter is missing,
	// which is the gap worth seeing.
	HistoricalFrom          int `json:"historical_from,omitempty"`
	HistoricalFromAnySource int `json:"historical_from_any_source,omitempty"`
	// HistoricalDepthKnown reports whether any provider in this field
	// publishes a start year at all. False means the depth is unresearched
	// rather than shallow.
	HistoricalDepthKnown bool `json:"historical_depth_known"`
	// Languages are the languages the implemented providers in this field
	// declare, sorted. Empty means no provider declares any, which is a
	// gap in the registry rather than a claim that the field is
	// English-only.
	Languages []string `json:"languages,omitempty"`
	// LanguagesKnown reports whether any provider declares languages.
	LanguagesKnown bool `json:"languages_known"`
}

// Report is the whole coverage answer.
type Report struct {
	Fields []FieldCoverage `json:"fields"`
	// Summary counts fields by their best state per dimension, so a reader
	// sees the shape before reading the rows.
	Summary map[string]map[string]int `json:"summary"`
	States  []State                   `json:"states"`
	Notice  string                    `json:"notice"`
}

// Build derives the report from the registry.
func Build(r *registry.Registry) Report {
	fields := map[string]bool{}
	for i := range r.Providers {
		for _, f := range r.Providers[i].Fields {
			fields[f] = true
		}
	}
	names := make([]string, 0, len(fields))
	for f := range fields {
		names = append(names, f)
	}
	sort.Strings(names)

	summary := map[string]map[string]int{}
	out := make([]FieldCoverage, 0, len(names))
	for _, field := range names {
		fc := FieldCoverage{Field: field}
		for _, dim := range Dimensions {
			dc := DimensionCoverage{Dimension: dim, State: StateNotResearched}
			for i := range r.Providers {
				p := &r.Providers[i]
				if !hasField(p, field) || !serves(p, dim) {
					continue
				}
				note := stateFor(p, dim)
				dc.Providers = append(dc.Providers, note)
				if rank[note.State] > rank[dc.State] {
					dc.State = note.State
				}
			}
			sort.SliceStable(dc.Providers, func(a, b int) bool {
				if rank[dc.Providers[a].State] != rank[dc.Providers[b].State] {
					return rank[dc.Providers[a].State] > rank[dc.Providers[b].State]
				}
				return dc.Providers[a].ProviderID < dc.Providers[b].ProviderID
			})
			fc.Dimensions = append(fc.Dimensions, dc)

			if summary[string(dim)] == nil {
				summary[string(dim)] = map[string]int{}
			}
			summary[string(dim)][string(dc.State)]++
		}
		fillHistoryAndLanguages(&fc, r, field)
		out = append(out, fc)
	}

	// Every state appears in the summary, zero included, so a reader never
	// has to tell an absent key from a zero count.
	for _, dim := range Dimensions {
		if summary[string(dim)] == nil {
			summary[string(dim)] = map[string]int{}
		}
		for _, s := range States {
			if _, ok := summary[string(dim)][string(s)]; !ok {
				summary[string(dim)][string(s)] = 0
			}
		}
	}

	return Report{Fields: out, Summary: summary, States: States, Notice: Notice}
}

func hasField(p *registry.Provider, field string) bool {
	for _, f := range p.Fields {
		if f == field {
			return true
		}
	}
	return false
}

// serves reports whether this provider is a source for a dimension at all.
// A provider that is not a source for a dimension contributes nothing to it
// rather than contributing a negative.
func serves(p *registry.Provider, dim Dimension) bool {
	switch dim {
	case DimLiteratureMetadata:
		return p.Type == registry.TypeScholarlyMetadata ||
			p.Type == registry.TypePreprintRepository ||
			p.Type == registry.TypeFullTextRepository ||
			p.Type == registry.TypeCitationGraph
	case DimCitationGraph:
		return p.Type == registry.TypeCitationGraph || hasCapability(p, "references") || hasCapability(p, "cited_by")
	case DimFullText:
		return p.Type == registry.TypeFullTextRepository || p.FulltextAccess || hasCapability(p, "full_text")
	case DimOntology:
		return p.Type == registry.TypeOntology || p.Type == registry.TypeControlledVocabulary ||
			p.Type == registry.TypeThesaurus || p.Type == registry.TypeClassification ||
			p.Type == registry.TypeNomenclature || p.Type == registry.TypeHistoricalVocabulary
	case DimDataset:
		return p.Type == registry.TypeDatasetRepository || hasCapability(p, "datasets")
	case DimSoftware:
		return p.Type == registry.TypeSoftwareRepository || hasCapability(p, "software")
	case DimPatent:
		return p.Type == registry.TypePatentDatabase || hasCapability(p, "patents")
	case DimHistoricalDepth, DimLanguage:
		// Every source contributes to what is known about depth and
		// language, so these dimensions read across the whole field.
		return true
	}
	return false
}

func hasCapability(p *registry.Provider, c string) bool {
	for _, got := range p.Capabilities {
		if got == c {
			return true
		}
	}
	return false
}

// stateFor derives one provider's state on one dimension from its registry
// lifecycle, its rights, and what it declares about itself.
func stateFor(p *registry.Provider, dim Dimension) ProviderNote {
	note := ProviderNote{
		ProviderID: p.ProviderID, Name: p.Name, Status: string(p.Status),
	}
	switch {
	case p.Status == registry.StatusExcluded:
		note.State = StateLicenseBlocked
		note.Reason = "excluded on legal posture; the source exists and this gateway does not operate it"
		return note
	case p.Status == registry.StatusSunset:
		note.State = StateSourceKnown
		note.Reason = "the upstream is gone; the entry is retained as history"
		return note
	case p.Status == registry.StatusDiscovered:
		note.State = StateSourceKnown
		return note
	case p.Status == registry.StatusResearched:
		note.State = StateSourceKnown
		note.Reason = "researched, no complete registry entry yet"
		return note
	case p.Status == registry.StatusRegistered || p.Status == registry.StatusVerified ||
		p.Status == registry.StatusAdapterPlanned:
		note.State = StateRegistered
		return note
	}

	// An adapter exists. Whether it reaches this dimension is a separate
	// question, and a licence that blocks serving outranks both.
	if p.Rights.Redistribution == "denied" || p.Rights.Redistribution == "prohibited" {
		note.State = StateLicenseBlocked
		note.Reason = "rights on this source forbid serving its records on"
		return note
	}
	switch dim {
	case DimFullText:
		if !p.FulltextAccess {
			note.State = StateCoverageIncomplete
			note.Reason = "the adapter serves metadata; full text is not reachable through this route"
			return note
		}
		if !p.StructuredFulltext {
			note.State = StateCoverageIncomplete
			note.Reason = "full text is reachable as a location; structured full text is not served"
			return note
		}
	case DimHistoricalDepth:
		if p.HistoricalFrom == 0 {
			note.State = StateCoverageIncomplete
			note.Reason = "the source's earliest year is not recorded, so its depth is unknown"
			return note
		}
	case DimLanguage:
		if len(p.Languages) == 0 {
			note.State = StateCoverageIncomplete
			note.Reason = "the source's language coverage is not recorded"
			return note
		}
	}
	note.State = StateAdapterImplemented
	return note
}

// fillHistoryAndLanguages aggregates depth and language across a field,
// keeping the implemented reach apart from the reach of every known source.
func fillHistoryAndLanguages(fc *FieldCoverage, r *registry.Registry, field string) {
	langs := map[string]bool{}
	for i := range r.Providers {
		p := &r.Providers[i]
		if !hasField(p, field) {
			continue
		}
		if p.HistoricalFrom > 0 {
			fc.HistoricalDepthKnown = true
			if fc.HistoricalFromAnySource == 0 || p.HistoricalFrom < fc.HistoricalFromAnySource {
				fc.HistoricalFromAnySource = p.HistoricalFrom
			}
			if p.Status.Operational() {
				if fc.HistoricalFrom == 0 || p.HistoricalFrom < fc.HistoricalFrom {
					fc.HistoricalFrom = p.HistoricalFrom
				}
			}
		}
		if len(p.Languages) > 0 && p.Status.Operational() {
			fc.LanguagesKnown = true
			for _, l := range p.Languages {
				langs[strings.ToLower(l)] = true
			}
		}
	}
	for l := range langs {
		fc.Languages = append(fc.Languages, l)
	}
	sort.Strings(fc.Languages)
}
