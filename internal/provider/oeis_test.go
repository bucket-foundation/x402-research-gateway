package provider

import "testing"

// oeisFixture is trimmed from a live oeis.org/search response verified
// 2026-08-18 (see oeis.go's doc comment).
const oeisFixture = `[
  {"number":45,"data":"0,1,1,2,3,5,8,13,21,34,55","name":"Fibonacci numbers: F(n) = F(n-1) + F(n-2) with F(0) = 0 and F(1) = 1.","offset":"0,4","author":"_N. J. A. Sloane_, 1964","keyword":"nonn,core,nice,easy,hear","xref":["Cf. A001622 (phi), A000032, A000032 (dup)."]},
  {"number":32,"data":"2,1,3,4,7,11,18,29","name":"Lucas numbers","offset":"0,3","author":"_N. J. A. Sloane_","keyword":"nonn,core","xref":[]}
]`

func TestOEISNormalizer(t *testing.T) {
	recs := OEISNormalizer{}.Normalize([]byte(oeisFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "A000045" {
		t.Errorf("id = %q, want zero-padded A000045", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://oeis.org/A000045" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if recs[1].ID != "A000032" {
		t.Errorf("id = %q, want zero-padded A000032", recs[1].ID)
	}
}

func TestOEISNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (OEISNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestOEISIdentity_RecordRights(t *testing.T) {
	recs := OEISNormalizer{}.Normalize([]byte(oeisFixture))
	rights := oeisIdentity{}.RecordRights(recs[0])
	if rights.License != "CC-BY-SA-4.0" {
		t.Errorf("license = %q", rights.License)
	}
	if !rights.Permits() {
		t.Error("CC BY-SA should permit redistribution (with attribution/share-alike, which Source records)")
	}
}

func TestOEISIdentity_ObjectRelations_DedupesRepeatedMentions(t *testing.T) {
	recs := OEISNormalizer{}.Normalize([]byte(oeisFixture))
	rels := oeisIdentity{}.ObjectRelations(recs[0], testTime)
	// The fixture's xref line mentions A001622 once and A000032 twice
	// ("A000032, A000032 (dup)"); the second A000032 mention must not
	// produce a duplicate relation.
	if len(rels) != 2 {
		t.Fatalf("got %d relations, want 2 (A001622, A000032 deduped): %+v", len(rels), rels)
	}
	targets := map[string]bool{rels[0].Object.RawID: true, rels[1].Object.RawID: true}
	if !targets["A001622"] || !targets["A000032"] {
		t.Errorf("relation targets = %v, want A001622 and A000032", targets)
	}
	for _, r := range rels {
		if r.Predicate.Recognized {
			t.Error("oeis:xref is prose commentary, never a recognized typed predicate")
		}
	}
}

func TestOEISIdentity_ObjectRelations_NoXref(t *testing.T) {
	recs := OEISNormalizer{}.Normalize([]byte(oeisFixture))
	if rels := (oeisIdentity{}).ObjectRelations(recs[1], testTime); rels != nil {
		t.Errorf("empty xref should assert no relations, got %+v", rels)
	}
}
