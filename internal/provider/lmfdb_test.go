package provider

import "testing"

// lmfdbFixture is trimmed from a live lmfdb.org/api/ec_curvedata response
// verified 2026-08-18 (see lmfdb.go's doc comment).
const lmfdbFixture = `{"table":"ec_curvedata","timestamp":"2026-08-18T14:06:07Z","data":[
  {"id":1,"Ciso":"11a","Clabel":"11a2","conductor":11,"lmfdb_label":"11.a1","rank":0},
  {"id":2,"Ciso":"11a","Clabel":"11a1","conductor":11,"lmfdb_label":"11.a2","rank":0}
]}`

func TestLMFDBNormalizer(t *testing.T) {
	recs := LMFDBNormalizer{}.Normalize([]byte(lmfdbFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "ec_curvedata:1" {
		t.Errorf("id = %q, want table-prefixed row id", recs[0].ID)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes (native per-table fields) must be preserved")
	}
}

func TestLMFDBNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"table":"x","data":[]}`)} {
		if recs := (LMFDBNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}
