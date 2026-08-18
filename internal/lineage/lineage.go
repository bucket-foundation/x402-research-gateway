// Package lineage is the gateway's in-process representation of feed402
// SPEC §3.7 derivation and lineage provenance.
//
// The gateway derives objects in two places: the insight tier assembles and
// summarizes context, and the relation layer (x402-research-gateway#7)
// carries provider assertions that one research object was produced from
// another. Both are lineage. feed402 already defines a wire shape for it,
// so this package mirrors that shape exactly instead of inventing a second
// derivation vocabulary the spec would then have to be reconciled with.
//
// Field names, optionality, and the two-form `sources` rule (an integer
// index into the envelope's citation array, or a string naming an earlier
// step's derived_object) come from feed402 SPEC §3.7 verbatim.
package lineage

import (
	"encoding/json"
	"fmt"
	"time"
)

// Transformation names what a step did. feed402 §3.7 leaves the vocabulary
// open under §2.3, so these are the strings this revision emits and a
// caller may use any other.
const (
	TransformContextAssembly = "context_assembly"
	TransformSummarization   = "summarization"
	TransformDedup           = "dedup"
	TransformRerank          = "rerank"
	TransformMerge           = "merge"
	// TransformProviderAssertedDerivation records that the derivation is a
	// fact an upstream published rather than an operation this gateway
	// performed. The gateway's step is the reporting of it.
	TransformProviderAssertedDerivation = "provider_asserted_derivation"
)

// Source is one entry of a step's `sources` array. Exactly one of Index and
// DerivedObject carries the value; feed402 §3.7 permits an integer index
// into the citation array or a string naming an earlier step's output, and
// the JSON encoding here emits whichever is set.
type Source struct {
	Index         int
	DerivedObject string
}

// CitationSource references citation[i] of the envelope this lineage rides
// on.
func CitationSource(i int) Source { return Source{Index: i} }

// ObjectSource references an earlier step's derived_object, or any opaque
// object identity another merchant published.
func ObjectSource(id string) Source { return Source{Index: -1, DerivedObject: id} }

// MarshalJSON emits the bare integer or the bare string feed402 requires.
func (s Source) MarshalJSON() ([]byte, error) {
	if s.DerivedObject != "" {
		return json.Marshal(s.DerivedObject)
	}
	if s.Index < 0 {
		return nil, fmt.Errorf("lineage: source has neither a citation index nor a derived_object")
	}
	return json.Marshal(s.Index)
}

// UnmarshalJSON accepts both forms.
func (s *Source) UnmarshalJSON(b []byte) error {
	var i int
	if err := json.Unmarshal(b, &i); err == nil {
		s.Index, s.DerivedObject = i, ""
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return fmt.Errorf("lineage: source is neither an integer nor a string")
	}
	s.Index, s.DerivedObject = -1, str
	return nil
}

// Entry is one lineage step, feed402 SPEC §3.7. DerivedObject, Sources, and
// Transformation are the only required fields.
type Entry struct {
	Step            int      `json:"step"`
	DerivedObject   string   `json:"derived_object"`
	Sources         []Source `json:"sources"`
	Transformation  string   `json:"transformation"`
	Software        string   `json:"software,omitempty"`
	SoftwareVersion string   `json:"software_version,omitempty"`
	GitCommit       string   `json:"git_commit,omitempty"`
	Timestamp       string   `json:"timestamp,omitempty"`
	Notes           string   `json:"notes,omitempty"`
}

// Valid reports whether the entry carries the three required fields and a
// usable sources array.
func (e Entry) Valid() bool {
	if e.DerivedObject == "" || e.Transformation == "" || len(e.Sources) == 0 {
		return false
	}
	for _, s := range e.Sources {
		if s.DerivedObject == "" && s.Index < 0 {
			return false
		}
	}
	return true
}

// Number renumbers a lineage array so Step matches array position, which
// feed402 §3.7 requires of the emitted envelope. Entries keep their order.
func Number(entries []Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	for i, e := range entries {
		e.Step = i
		out = append(out, e)
	}
	return out
}

// Stamp sets Timestamp on an entry from a time, in the ISO-8601 form
// feed402 uses everywhere else in the envelope.
func Stamp(e Entry, at time.Time) Entry {
	e.Timestamp = at.UTC().Format(time.RFC3339)
	return e
}
