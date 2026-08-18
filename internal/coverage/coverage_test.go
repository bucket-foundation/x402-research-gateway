package coverage

import (
	"encoding/json"
	"testing"

	"github.com/gianyrox/x402-research-gateway/internal/registry"
)

const registryPath = "../../config/providers.yaml"

func load(t *testing.T) *registry.Registry {
	t.Helper()
	r, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestBuild_EveryFieldReportsEveryDimension(t *testing.T) {
	rep := Build(load(t))
	if len(rep.Fields) < 5 {
		t.Fatalf("only %d fields reported", len(rep.Fields))
	}
	for _, fc := range rep.Fields {
		if len(fc.Dimensions) != len(Dimensions) {
			t.Fatalf("%s reports %d dimensions, want %d", fc.Field, len(fc.Dimensions), len(Dimensions))
		}
		seen := map[Dimension]bool{}
		for _, dc := range fc.Dimensions {
			seen[dc.Dimension] = true
			if dc.State == "" {
				t.Fatalf("%s/%s has no state", fc.Field, dc.Dimension)
			}
		}
		for _, d := range Dimensions {
			if !seen[d] {
				t.Fatalf("%s omits the %s row", fc.Field, d)
			}
		}
	}
}

// A gap is a row saying not_researched, never a missing row.
func TestBuild_NotResearchedIsStatedNotOmitted(t *testing.T) {
	rep := Build(load(t))
	found := false
	for _, fc := range rep.Fields {
		for _, dc := range fc.Dimensions {
			if dc.State != StateNotResearched {
				continue
			}
			found = true
			if len(dc.Providers) != 0 {
				t.Fatalf("%s/%s is not_researched yet lists providers", fc.Field, dc.Dimension)
			}
		}
	}
	if !found {
		t.Fatal("no dimension is at not_researched, which would mean every field is covered on every axis")
	}
}

func TestBuild_DistinguishesBlockedFromUnimplementedFromPartial(t *testing.T) {
	rep := Build(load(t))
	states := map[State]bool{}
	for _, fc := range rep.Fields {
		for _, dc := range fc.Dimensions {
			states[dc.State] = true
			for _, p := range dc.Providers {
				states[p.State] = true
			}
		}
	}
	// The shipped registry holds providers at researched, implemented,
	// production, excluded, and sunset, so these four states are derivable
	// from it today. StateRegistered has its own unit test below, because
	// no provider currently sits in that lifecycle stage and asserting it
	// here would test the data rather than the derivation.
	for _, want := range []State{
		StateNotResearched, StateSourceKnown,
		StateLicenseBlocked, StateCoverageIncomplete, StateAdapterImplemented,
	} {
		if !states[want] {
			t.Fatalf("no row or provider reaches state %q, so the report cannot distinguish it", want)
		}
	}
}

func TestBuild_LicenseBlockedCarriesItsReason(t *testing.T) {
	rep := Build(load(t))
	for _, fc := range rep.Fields {
		for _, dc := range fc.Dimensions {
			for _, p := range dc.Providers {
				if p.State == StateLicenseBlocked && p.Reason == "" {
					t.Fatalf("%s is license_blocked with no reason", p.ProviderID)
				}
				if p.State == StateCoverageIncomplete && p.Reason == "" {
					t.Fatalf("%s is coverage_incomplete with no reason", p.ProviderID)
				}
			}
		}
	}
}

func TestBuild_HistoricalDepthReported(t *testing.T) {
	rep := Build(load(t))
	depthKnown := 0
	for _, fc := range rep.Fields {
		if !fc.HistoricalDepthKnown {
			continue
		}
		depthKnown++
		if fc.HistoricalFromAnySource == 0 {
			t.Fatalf("%s reports depth known with no earliest year", fc.Field)
		}
		if fc.HistoricalFrom != 0 && fc.HistoricalFrom < fc.HistoricalFromAnySource {
			t.Fatalf("%s: implemented reach %d predates every known source's %d",
				fc.Field, fc.HistoricalFrom, fc.HistoricalFromAnySource)
		}
	}
	if depthKnown == 0 {
		t.Fatal("no field reports a historical depth")
	}
}

func TestBuild_LanguageCoverageReported(t *testing.T) {
	rep := Build(load(t))
	langKnown := 0
	for _, fc := range rep.Fields {
		if fc.LanguagesKnown {
			langKnown++
			if len(fc.Languages) == 0 {
				t.Fatalf("%s reports languages known with none listed", fc.Field)
			}
		}
	}
	if langKnown == 0 {
		t.Fatal("no field reports language coverage")
	}
}

func TestBuild_SummaryCountsEveryState(t *testing.T) {
	rep := Build(load(t))
	for _, dim := range Dimensions {
		counts, ok := rep.Summary[string(dim)]
		if !ok {
			t.Fatalf("summary omits %s", dim)
		}
		for _, s := range States {
			if _, ok := counts[string(s)]; !ok {
				t.Fatalf("summary for %s omits the %s count", dim, s)
			}
		}
	}
	if rep.Notice == "" {
		t.Fatal("the report carries no notice")
	}
}

func TestBuild_Deterministic(t *testing.T) {
	r := load(t)
	one, _ := json.Marshal(Build(r))
	two, _ := json.Marshal(Build(r))
	if string(one) != string(two) {
		t.Fatal("two builds over one registry differ")
	}
}

func TestStateFor_RegisteredLifecycleStates(t *testing.T) {
	for _, status := range []registry.Status{
		registry.StatusRegistered, registry.StatusVerified, registry.StatusAdapterPlanned,
	} {
		p := registry.Provider{ProviderID: "example", Status: status}
		if got := stateFor(&p, DimLiteratureMetadata).State; got != StateRegistered {
			t.Fatalf("%s derived %q, want registered", status, got)
		}
	}
	for _, status := range []registry.Status{
		registry.StatusDiscovered, registry.StatusResearched,
	} {
		p := registry.Provider{ProviderID: "example", Status: status}
		if got := stateFor(&p, DimLiteratureMetadata).State; got != StateSourceKnown {
			t.Fatalf("%s derived %q, want source_known", status, got)
		}
	}
}

func TestStateFor_ExcludedProviderIsLicenseBlocked(t *testing.T) {
	p := registry.Provider{ProviderID: "sci-hub", Status: registry.StatusExcluded}
	note := stateFor(&p, DimFullText)
	if note.State != StateLicenseBlocked || note.Reason == "" {
		t.Fatalf("note = %+v", note)
	}
}

func TestStateFor_MetadataAdapterIsIncompleteOnFullText(t *testing.T) {
	p := registry.Provider{
		ProviderID: "crossref", Status: registry.StatusProduction,
		FulltextAccess: false,
	}
	if got := stateFor(&p, DimFullText).State; got != StateCoverageIncomplete {
		t.Fatalf("state = %q; a metadata adapter does not cover full text", got)
	}
	if got := stateFor(&p, DimLiteratureMetadata).State; got != StateAdapterImplemented {
		t.Fatalf("state = %q", got)
	}
}
