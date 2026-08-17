package registry

import (
	"os"
	"strings"
	"testing"
)

const proseDir = "../../docs/research-index"

// The generated document must round-trip: regenerating it produces exactly
// what is committed. Otherwise the tables drift from the registry, which is
// the failure this whole issue exists to fix.
func TestGeneratedIndexIsUpToDate(t *testing.T) {
	r := load(t)
	got, err := r.RenderIndex(proseDir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../RESEARCH-INDEX.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Error("RESEARCH-INDEX.md is out of date; run `make research-index`")
	}
}

// Human judgment has no registry field and must survive generation verbatim.
func TestHandWrittenAnalysisIsPreserved(t *testing.T) {
	r := load(t)
	doc, err := r.RenderIndex(proseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// Section 2 legal posture
		"Posture disclaimer (read first)",
		"Summary recommendation (Section 2)",
		// Section 3 priority rationale
		"Section 3 — Integration priority recommendation",
		"Deferred on purpose:",
		// Section 4 parser-reuse analysis
		"Section 4 — Implementation patterns observed",
		"parser reuse map",
		// prologue conventions
		"Conventions used below:",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("generated document lost hand-written content: %q", want)
		}
	}
}

func TestGeneratedIndexIsMarkedGenerated(t *testing.T) {
	r := load(t)
	doc, err := r.RenderIndex(proseDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc, "<!-- GENERATED FILE") {
		t.Error("the generated document must warn against hand-editing")
	}
}

// Every registered source has to appear in the document, or the generated
// index silently under-reports what the gateway knows about.
func TestEveryProviderAppearsInTheDocument(t *testing.T) {
	r := load(t)
	doc, err := r.RenderIndex(proseDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range r.Providers {
		p := &r.Providers[i]
		if p.Section == "2" {
			// Grey literature is covered by the preserved Section 2 prose.
			continue
		}
		if !strings.Contains(doc, mdEscape(p.Name)) {
			t.Errorf("provider %q (%s) is missing from the generated document", p.Name, p.ProviderID)
		}
	}
}

// A pipe in a cell would silently break the table it sits in.
func TestTableCellsAreEscaped(t *testing.T) {
	if got := mdEscape("a|b"); got != `a\|b` {
		t.Errorf("mdEscape(%q) = %q", "a|b", got)
	}
	if got := mdEscape("line\nbreak"); strings.Contains(got, "\n") {
		t.Errorf("newlines must not survive into a table cell: %q", got)
	}
}

// The lifecycle summary is what makes the backlog visible.
func TestCoverageReportsLifecycle(t *testing.T) {
	r := load(t)
	out := r.RenderCoverage()
	for _, want := range []string{"researched", "production", "excluded", "Provider type"} {
		if !strings.Contains(out, want) {
			t.Errorf("coverage report missing %q", want)
		}
	}
}
