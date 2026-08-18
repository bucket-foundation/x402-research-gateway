package provider

import (
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/relation"
)

var relAt = time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)

// dataciteRelationFixture is a DataCite JSON:API record shaped like the
// live api.datacite.org response, carrying a dataset that supplements an
// article, is derived from another dataset, has software as its source, and
// one relation type this gateway has no term for.
const dataciteRelationFixture = `{
  "data": {
    "id": "10.5061/dryad.abc123",
    "attributes": {
      "doi": "10.5061/dryad.abc123",
      "url": "https://datadryad.org/stash/dataset/doi:10.5061/dryad.abc123",
      "titles": [{"title": "Mitochondrial membrane potential measurements"}],
      "types": {"resourceTypeGeneral": "Dataset", "resourceType": "Tabular data"},
      "relatedIdentifiers": [
        {"relatedIdentifier": "10.1038/s41586-024-00001-1",
         "relatedIdentifierType": "DOI", "relationType": "IsSupplementTo",
         "resourceTypeGeneral": "JournalArticle"},
        {"relatedIdentifier": "10.5281/zenodo.7654321",
         "relatedIdentifierType": "DOI", "relationType": "IsDerivedFrom",
         "resourceTypeGeneral": "Software"},
        {"relatedIdentifier": "10.5061/dryad.older",
         "relatedIdentifierType": "DOI", "relationType": "IsObsoletedBy",
         "resourceTypeGeneral": "Dataset"}
      ]
    }
  }
}`

func dataciteRelations(t *testing.T) []relation.Relation {
	t.Helper()
	recs := DataCiteNormalizer{}.Normalize([]byte(dataciteRelationFixture))
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	return dataciteIdentity{}.ObjectRelations(recs[0], relAt)
}

func findRelation(rels []relation.Relation, providerTerm string) (relation.Relation, bool) {
	for _, r := range rels {
		if r.Predicate.ProviderTerm == providerTerm {
			return r, true
		}
	}
	return relation.Relation{}, false
}

func TestDataCiteObjectRelations_WorkToDatasetAndSoftware(t *testing.T) {
	rels := dataciteRelations(t)
	if len(rels) != 3 {
		t.Fatalf("want 3 relations, got %d", len(rels))
	}

	supp, ok := findRelation(rels, "IsSupplementTo")
	if !ok {
		t.Fatal("IsSupplementTo missing")
	}
	if supp.Subject.Type != relation.TypeDataset {
		t.Fatalf("subject type = %q", supp.Subject.Type)
	}
	if supp.Object.Type != relation.TypeWork {
		t.Fatalf("object type = %q", supp.Object.Type)
	}
	if supp.Predicate.Normalized != relation.TermSupplementTo || !supp.Predicate.Recognized {
		t.Fatalf("predicate = %+v", supp.Predicate)
	}
	if supp.Provider != "datacite" || supp.RetrievedAt != "2026-08-17T10:00:00Z" {
		t.Fatalf("attribution = %q %q", supp.Provider, supp.RetrievedAt)
	}
	if supp.SourceField != "datacite:relatedIdentifiers" {
		t.Fatalf("source_field = %q", supp.SourceField)
	}
	if supp.Annotations["relatedIdentifierType"] != "DOI" {
		t.Fatalf("annotations = %v", supp.Annotations)
	}

	soft, ok := findRelation(rels, "IsDerivedFrom")
	if !ok {
		t.Fatal("IsDerivedFrom missing")
	}
	if soft.Object.Type != relation.TypeSoftware {
		t.Fatalf("software object type = %q", soft.Object.Type)
	}
	if soft.Lineage == nil || !soft.Lineage.Valid() {
		t.Fatalf("derivation carries no feed402 lineage entry: %+v", soft.Lineage)
	}
}

func TestDataCiteObjectRelations_UnknownTermPreserved(t *testing.T) {
	rels := dataciteRelations(t)
	unknown, ok := findRelation(rels, "IsObsoletedBy")
	if !ok {
		t.Fatal("a relation type the gateway does not recognize was dropped")
	}
	if unknown.Predicate.Recognized || unknown.Predicate.Normalized != "" {
		t.Fatalf("unmapped term normalized anyway: %+v", unknown.Predicate)
	}
	if unknown.Object.Key() != "doi:10.5061/dryad.older" {
		t.Fatalf("object key = %q", unknown.Object.Key())
	}
}

// crossrefRelationFixture carries an article with a dataset relation, a
// preprint relation, and a Crossmark correction, using Crossref's own
// hyphenated vocabulary rather than DataCite's camel case.
const crossrefRelationFixture = `{
  "message": {
    "DOI": "10.1038/s41586-024-00001-1",
    "URL": "https://doi.org/10.1038/s41586-024-00001-1",
    "type": "journal-article",
    "title": ["Mitochondrial membrane potential in vivo"],
    "relation": {
      "is-supplement-to": [{"id": "10.5061/dryad.abc123", "id-type": "doi", "asserted-by": "subject"}],
      "has-preprint": [{"id": "10.1101/2024.01.01.000001", "id-type": "doi", "asserted-by": "object"}]
    },
    "updated-by": [
      {"DOI": "10.1038/s41586-024-09999-9", "type": "correction", "label": "Correction"}
    ]
  }
}`

func crossrefRelations(t *testing.T) []relation.Relation {
	t.Helper()
	recs := CrossrefWorksNormalizer{}.Normalize([]byte(crossrefRelationFixture))
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	return crossrefIdentity{}.ObjectRelations(recs[0], relAt)
}

func TestCrossrefObjectRelations_WorkToDatasetAndCorrection(t *testing.T) {
	rels := crossrefRelations(t)
	if len(rels) != 3 {
		t.Fatalf("want 3 relations, got %d", len(rels))
	}

	ds, ok := findRelation(rels, "is-supplement-to")
	if !ok {
		t.Fatal("is-supplement-to missing")
	}
	if ds.Object.Key() != "doi:10.5061/dryad.abc123" {
		t.Fatalf("dataset object key = %q", ds.Object.Key())
	}
	if ds.Annotations["asserted-by"] != "subject" {
		t.Fatalf("annotations = %v", ds.Annotations)
	}
	// The two providers spell this relation differently and both spellings
	// survive; only the normalized annotation agrees.
	dcSupp, _ := findRelation(dataciteRelations(t), "IsSupplementTo")
	if ds.Predicate.ProviderTerm == dcSupp.Predicate.ProviderTerm {
		t.Fatal("provider spellings were flattened into one term")
	}
	if ds.Predicate.Normalized != dcSupp.Predicate.Normalized {
		t.Fatalf("normalized terms disagree: %q vs %q",
			ds.Predicate.Normalized, dcSupp.Predicate.Normalized)
	}

	corr, ok := findRelation(rels, "correction")
	if !ok {
		t.Fatal("Crossmark correction missing")
	}
	// updated-by means the correcting work is the subject.
	if corr.Subject.Key() != "doi:10.1038/s41586-024-09999-9" {
		t.Fatalf("correction subject = %q", corr.Subject.Key())
	}
	if corr.Object.Key() != "doi:10.1038/s41586-024-00001-1" {
		t.Fatalf("correction object = %q", corr.Object.Key())
	}
	if corr.Predicate.Normalized != relation.TermCorrects {
		t.Fatalf("correction predicate = %+v", corr.Predicate)
	}
	if corr.Subject.Type != relation.TypeCorrection {
		t.Fatalf("correcting work type = %q", corr.Subject.Type)
	}
}

// clinicalTrialsRelationFixture is a v2 study record with a results
// publication and a background one, a third relation vocabulary again.
const clinicalTrialsRelationFixture = `{
  "studies": [
    {"protocolSection": {
      "identificationModule": {"nctId": "NCT01234567"},
      "designModule": {"studyType": "INTERVENTIONAL"},
      "referencesModule": {"references": [
        {"pmid": "31234567", "type": "RESULT", "citation": "Doe J. Results of the trial. 2024."},
        {"pmid": "29876543", "type": "BACKGROUND", "citation": "Roe R. Prior work. 2019."}
      ]}
    }}
  ]
}`

func TestClinicalTrialsObjectRelations_WorkToTrial(t *testing.T) {
	recs := ClinicalTrialsSearchNormalizer{}.Normalize([]byte(clinicalTrialsRelationFixture))
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if recs[0].ID != "NCT01234567" || recs[0].CanonicalURL != "https://clinicaltrials.gov/study/NCT01234567" {
		t.Fatalf("normalizer output changed: %+v", recs[0])
	}
	rels := clinicalTrialsRelations{}.ObjectRelations(recs[0], relAt)
	if len(rels) != 2 {
		t.Fatalf("want 2 relations, got %d", len(rels))
	}
	result, ok := findRelation(rels, "result")
	if !ok {
		t.Fatal("RESULT reference missing")
	}
	if result.Object.Type != relation.TypeTrial || result.Object.Key() != "raw:nct01234567" {
		t.Fatalf("trial object = %+v", result.Object)
	}
	if result.Subject.Key() != "pmid:31234567" {
		t.Fatalf("publication subject = %q", result.Subject.Key())
	}
	if result.Predicate.Normalized != relation.TermResultsFrom {
		t.Fatalf("predicate = %+v", result.Predicate)
	}
	if result.Provider != "clinicaltrials" {
		t.Fatalf("provider = %q", result.Provider)
	}
	if result.Annotations["citation"] == "" {
		t.Fatal("the provider's own citation string was dropped")
	}
}

func TestObjectRelations_NeverPanicOnUnknownBody(t *testing.T) {
	junk := NormalizedRecord{ID: "x", Raw: []byte(`{"nope":true}`)}
	empty := NormalizedRecord{ID: "x"}
	for _, rec := range []NormalizedRecord{junk, empty} {
		if got := (dataciteIdentity{}).ObjectRelations(rec, relAt); got != nil {
			t.Fatalf("datacite returned %v", got)
		}
		if got := (crossrefIdentity{}).ObjectRelations(rec, relAt); got != nil {
			t.Fatalf("crossref returned %v", got)
		}
		if got := (clinicalTrialsRelations{}).ObjectRelations(rec, relAt); got != nil {
			t.Fatalf("clinicaltrials returned %v", got)
		}
	}
}

func TestRelationCapabilityReported(t *testing.T) {
	for _, a := range []*Adapter{DataCiteSearchAdapter, DataCiteFetchAdapter,
		CrossrefSearchAdapter, CrossrefFetchAdapter, ClinicalTrialsSearchAdapter} {
		if !a.Supports(CapRelations) {
			t.Fatalf("%s does not report the relations capability", a.ID)
		}
	}
	// A provider that publishes no object relations must not claim the
	// capability.
	if PubMedSearchAdapter.Supports(CapRelations) {
		t.Fatal("pubmed-search claims a relations capability it does not implement")
	}
}
