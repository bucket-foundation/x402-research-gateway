package lineage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSourceJSON_BothFeed402Forms(t *testing.T) {
	b, err := json.Marshal([]Source{CitationSource(0), ObjectSource("insight:req#context")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `[0,"insight:req#context"]`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	var back []Source
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back[0].Index != 0 || back[0].DerivedObject != "" {
		t.Fatalf("citation source round-trip: %+v", back[0])
	}
	if back[1].DerivedObject != "insight:req#context" {
		t.Fatalf("object source round-trip: %+v", back[1])
	}
}

func TestSourceJSON_RejectsEmptySource(t *testing.T) {
	if _, err := json.Marshal([]Source{{Index: -1}}); err == nil {
		t.Fatal("a source with neither form encoded without error")
	}
}

func TestEntryValid(t *testing.T) {
	ok := Entry{DerivedObject: "o", Sources: []Source{CitationSource(0)}, Transformation: "merge"}
	if !ok.Valid() {
		t.Fatal("complete entry reported invalid")
	}
	for name, e := range map[string]Entry{
		"no derived object": {Sources: []Source{CitationSource(0)}, Transformation: "merge"},
		"no sources":        {DerivedObject: "o", Transformation: "merge"},
		"no transformation": {DerivedObject: "o", Sources: []Source{CitationSource(0)}},
		"empty source":      {DerivedObject: "o", Sources: []Source{{Index: -1}}, Transformation: "merge"},
	} {
		if e.Valid() {
			t.Fatalf("%s: reported valid", name)
		}
	}
}

func TestNumberAndStamp(t *testing.T) {
	got := Number([]Entry{{Step: 7}, {Step: 7}, {Step: 7}})
	for i, e := range got {
		if e.Step != i {
			t.Fatalf("entry %d numbered %d", i, e.Step)
		}
	}
	e := Stamp(Entry{}, time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC))
	if e.Timestamp != "2026-08-17T10:00:00Z" {
		t.Fatalf("timestamp = %q", e.Timestamp)
	}
}
