package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const registryPath = "../../config/providers.yaml"

func load(t *testing.T) *Registry {
	t.Helper()
	r, err := Load(registryPath)
	if err != nil {
		t.Fatalf("registry does not load: %v", err)
	}
	return r
}

func TestRegistryLoadsAndValidates(t *testing.T) {
	r := load(t)
	if r.Len() < 100 {
		t.Fatalf("expected the full index to be registered, got %d providers", r.Len())
	}
	if r.LastReviewed == "" {
		t.Error("last_reviewed must be recorded")
	}
}

// Every source in the original RESEARCH-INDEX.md must have an entry. The
// index is the historical record; losing a source silently is the failure
// this guards.
func TestEveryIndexedSourceIsRegistered(t *testing.T) {
	r := load(t)
	for _, want := range []string{
		// one from each section, including the licence- and legal-deferred
		"arxiv", "zbmath-open-rest", "mathscinet", // 1.1
		"inspire-hep", "osti-gov", // 1.2
		"pubchem-pug-rest", "reaxys", // 1.3
		"dblp", "software-heritage", "google-scholar", // 1.4
		"pubmed", "clinicaltrials-gov-v2", // 1.5
		"simbad", "ivoa-vo-registry", // 1.6
		"cognitive-atlas", "dandi-archive", // 1.7
		"nasa-cmr", "gbif", "planet-labs", // 1.8
		"crossref", "opencitations", "ror", "orcid-public-api", // 1.9
		"sci-hub", "libgen", "anna-s-archive", "z-library", // 2
	} {
		if _, ok := r.Get(want); !ok {
			t.Errorf("source %q from RESEARCH-INDEX.md has no registry entry", want)
		}
	}
}

// The provider_type vocabulary has to cover the non-paper classes, otherwise
// the registry silently flattens ontologies and datasets into "literature".
func TestProviderTypeVocabularyCoversNonPaperClasses(t *testing.T) {
	for _, want := range []ProviderType{
		TypeOntology, TypeControlledVocabulary, TypeThesaurus,
		TypeClassification, TypeNomenclature, TypeHistoricalVocabulary,
		TypeStandard, TypeDatasetRepository, TypeSoftwareRepository,
		TypePatentDatabase, TypeAuthorRegistry, TypeOrganizationRegistry,
		TypeChemicalDatabase, TypeBiologicalDatabase, TypeMaterialsDatabase,
		TypeMathematicalObjectDatabase, TypeAstronomicalDatabase,
		TypeEarthScienceDatabase, TypeExperimentalRegistry,
		TypeDocumentStandard, TypeMathematicalSemanticsStandard,
		TypeIntegrityRegistry, TypeRetractionUpdateSource,
	} {
		if !want.Valid() {
			t.Errorf("provider_type %q is missing from the vocabulary", want)
		}
	}
}

// A source can be known long before an adapter exists. If everything had to be
// production, the registry would be a deployment manifest rather than a
// backlog.
func TestLifecycleStagesTheBacklog(t *testing.T) {
	r := load(t)
	counts := r.StatusCounts()
	if counts[StatusResearched] == 0 {
		t.Error("expected researched providers with no adapter")
	}
	if counts[StatusProduction] == 0 {
		t.Error("expected providers actually serving traffic")
	}
	if counts[StatusExcluded] == 0 {
		t.Error("expected excluded providers")
	}
}

// Excluded means excluded: no endpoints, no redistribution, note retained.
func TestExcludedProvidersAreNotOperable(t *testing.T) {
	r := load(t)
	for _, p := range r.ByStatus(StatusExcluded) {
		if len(p.Endpoints) != 0 {
			t.Errorf("%s: excluded provider registers %d endpoints", p.ProviderID, len(p.Endpoints))
		}
		if p.Status.Operational() {
			t.Errorf("%s: excluded provider reports itself operational", p.ProviderID)
		}
		if p.Rights.RedistributionAllowed() {
			t.Errorf("%s: excluded provider must not permit redistribution", p.ProviderID)
		}
		if strings.TrimSpace(p.Notes) == "" {
			t.Errorf("%s: excluded provider lost its research note", p.ProviderID)
		}
	}

	// The shadow libraries specifically must never be operable.
	for _, id := range []string{"sci-hub", "libgen", "anna-s-archive", "z-library"} {
		p, ok := r.Get(id)
		if !ok {
			t.Fatalf("%s missing from registry", id)
		}
		if p.Status != StatusExcluded {
			t.Errorf("%s: expected excluded, got %s", id, p.Status)
		}
		if p.BaseURL != "" {
			t.Errorf("%s: excluded source must register no base_url", id)
		}
	}
}

// Rights are data. UNKNOWN is never ALLOWED.
func TestUnknownRightsAreNotPermission(t *testing.T) {
	var r Rights
	if r.RedistributionAllowed() {
		t.Error("an empty rights block must not read as permission")
	}
	if (Rights{Redistribution: "unknown"}).RedistributionAllowed() {
		t.Error("unknown must not read as permission")
	}
	if (Rights{Redistribution: "denied"}).RedistributionAllowed() {
		t.Error("denied must not read as permission")
	}
	if !(Rights{Redistribution: "allowed"}).RedistributionAllowed() {
		t.Error("allowed should read as permission")
	}
}

// Known migrations are recorded and the predecessor is never deleted.
func TestHistoricalMigrationsRetainPredecessors(t *testing.T) {
	r := load(t)
	for _, tc := range []struct{ pred, succ string }{
		{"microsoft-academic-graph", "openalex"},
		{"pacs", "physh"},
		{"patentsview-legacy", "uspto-odp"},
	} {
		pred, ok := r.Get(tc.pred)
		if !ok {
			t.Errorf("predecessor %q was deleted; it must be retained", tc.pred)
			continue
		}
		if pred.HistoricalSuccessor != tc.succ {
			t.Errorf("%s: historical_successor = %q, want %q", tc.pred, pred.HistoricalSuccessor, tc.succ)
		}
		succ, ok := r.Get(tc.succ)
		if !ok {
			t.Errorf("successor %q missing", tc.succ)
			continue
		}
		if succ.HistoricalPredecessor != tc.pred {
			t.Errorf("%s: historical_predecessor = %q, want %q", tc.succ, succ.HistoricalPredecessor, tc.pred)
		}
	}
}

// Historical vocabularies stay retrievable; a successor never overwrites one.
func TestHistoricalVocabularyIsRetained(t *testing.T) {
	r := load(t)
	pacs, ok := r.Get("pacs")
	if !ok {
		t.Fatal("PACS must stay in the registry: pre-2010 literature is classified with it")
	}
	if pacs.Type != TypeHistoricalVocabulary {
		t.Errorf("pacs provider_type = %q, want historical_vocabulary", pacs.Type)
	}
}

// The seven configured routes must reconcile against registry entries.
func TestRoutesReconcileWithRegistry(t *testing.T) {
	r := load(t)
	ids, err := RouteIDsFromConfig("../../config/routes.yaml")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("no routes found in config/routes.yaml")
	}
	if errs := r.ReconcileRoutes(ids); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("reconcile: %v", e)
		}
	}
}

func TestReconcileCatchesDrift(t *testing.T) {
	r := load(t)

	// A route the registry does not claim must be reported.
	errs := r.ReconcileRoutes([]string{"pubmed-search", "pubmed-fetch",
		"semantic-scholar-search", "openalex-works", "clinicaltrials-search",
		"kruse-search", "pubchem-compound", "ghost-route"})
	if !hasErrContaining(errs, "ghost-route") {
		t.Error("an unclaimed route should be reported")
	}

	// A registry entry claiming a route that does not exist must be reported.
	errs = r.ReconcileRoutes([]string{"pubmed-search"})
	if len(errs) == 0 {
		t.Error("missing routes should be reported")
	}
}

func TestDuplicateProviderIDRejected(t *testing.T) {
	_, err := Parse([]byte(`
version: "1"
providers:
  - provider_id: dup
    name: One
    provider_type: scholarly_metadata
    status: researched
  - provider_id: dup
    name: Two
    provider_type: scholarly_metadata
    status: researched
`))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected a duplicate-id error, got %v", err)
	}
}

func TestDanglingHistoricalLinkRejected(t *testing.T) {
	_, err := Parse([]byte(`
version: "1"
providers:
  - provider_id: old
    name: Old
    provider_type: scholarly_metadata
    status: sunset
    historical_successor: nonexistent
`))
	if err == nil || !strings.Contains(err.Error(), "historical_successor") {
		t.Fatalf("expected a dangling-successor error, got %v", err)
	}
}

func TestUnknownProviderTypeRejected(t *testing.T) {
	_, err := Parse([]byte(`
version: "1"
providers:
  - provider_id: x
    name: X
    provider_type: quantum_opportunity_engine
    status: researched
`))
	if err == nil || !strings.Contains(err.Error(), "unknown provider_type") {
		t.Fatalf("expected an unknown-type error, got %v", err)
	}
}

func TestExcludedProviderWithEndpointsRejected(t *testing.T) {
	_, err := Parse([]byte(`
version: "1"
providers:
  - provider_id: shadow
    name: Shadow
    provider_type: full_text_repository
    status: excluded
    notes: researched and refused
    endpoints:
      - id: dl
        url: https://example.invalid/dl
`))
	if err == nil || !strings.Contains(err.Error(), "no endpoints") {
		t.Fatalf("expected excluded-with-endpoints to be rejected, got %v", err)
	}
}

// The registry must not leak credentials.
func TestRegistryCarriesNoSecrets(t *testing.T) {
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, pattern := range []string{
		"api_key:", "apikey:", "secret", "password", "Bearer ey",
		"PRIVATE KEY", "AKIA",
	} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(pattern)) {
			t.Errorf("registry appears to contain a credential (%q); keys belong in env, not the registry", pattern)
		}
	}
}

func TestRegistryPathExists(t *testing.T) {
	if _, err := os.Stat(filepath.Clean(registryPath)); err != nil {
		t.Fatalf("registry file missing: %v", err)
	}
}

func hasErrContaining(errs []error, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), sub) {
			return true
		}
	}
	return false
}
