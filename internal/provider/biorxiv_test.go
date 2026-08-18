package provider

import (
	"testing"
	"time"
)

var testTime = time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)

// biorxivFixture is trimmed from a live api.biorxiv.org/details response
// verified 2026-08-18 (see biorxiv.go's doc comment).
const biorxivFixture = `{"messages":[{"status":"ok"}],"collection":[
  {"title":"KCNQ2/3 regulates efferent mediated slow excitation","authors":"Sinha, A. K.; Lee, C.","doi":"10.1101/2023.12.30.573731","date":"2024-01-01","version":"1","license":"cc_no","category":"neuroscience","jatsxml":"https://www.biorxiv.org/content/early/2024/01/01/2023.12.30.573731.source.xml","published":"NA","server":"bioRxiv"},
  {"title":"A preprint that has since been published","authors":"Doe, J.","doi":"10.1101/2024.01.01.000001","date":"2024-01-01","version":"2","license":"cc_by","category":"genetics","jatsxml":"","published":"10.1038/s41586-024-00000-0","server":"bioRxiv"}
]}`

func TestBioRxivNormalizer_Listing(t *testing.T) {
	recs := BioRxivNormalizer{}.Normalize([]byte(biorxivFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "10.1101/2023.12.30.573731v1" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doi.org/10.1101/2023.12.30.573731" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestBioRxivNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (BioRxivNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestBioRxivIdentity_AssertedRelations_UnpublishedIsNil(t *testing.T) {
	recs := BioRxivNormalizer{}.Normalize([]byte(biorxivFixture))
	rels := biorxivIdentity{}.AssertedRelations("biorxiv:"+recs[0].ID, recs[0], testTime)
	if rels != nil {
		t.Errorf("published=NA should assert no relation, got %+v", rels)
	}
}

func TestBioRxivIdentity_AssertedRelations_PublishedVersion(t *testing.T) {
	recs := BioRxivNormalizer{}.Normalize([]byte(biorxivFixture))
	rels := biorxivIdentity{}.AssertedRelations("biorxiv:"+recs[1].ID, recs[1], testTime)
	if len(rels) != 1 {
		t.Fatalf("got %d relations, want 1", len(rels))
	}
	if rels[0].To != "doi:10.1038/s41586-024-00000-0" {
		t.Errorf("relation target = %q", rels[0].To)
	}
	if rels[0].Type != "preprint_of" {
		t.Errorf("relation type = %q", rels[0].Type)
	}
}

func TestBioRxivIdentity_RecordRights_PerRecordLicense(t *testing.T) {
	recs := BioRxivNormalizer{}.Normalize([]byte(biorxivFixture))
	r0 := biorxivIdentity{}.RecordRights(recs[0])
	if r0.License != "cc_no" {
		t.Errorf("license = %q, want cc_no", r0.License)
	}
	r1 := biorxivIdentity{}.RecordRights(recs[1])
	if r1.License != "cc_by" {
		t.Errorf("license = %q, want cc_by", r1.License)
	}
}
