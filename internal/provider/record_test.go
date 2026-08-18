package provider

import (
	"encoding/json"
	"testing"
)

func TestMarshalUnmarshalRecord_RoundTrips(t *testing.T) {
	type sample struct {
		A string `json:"a"`
		B int    `json:"b"`
	}
	raw, err := marshalRecord(sample{A: "x", B: 1})
	if err != nil {
		t.Fatalf("marshalRecord: %v", err)
	}
	var got sample
	if err := unmarshalRecord(raw, &got); err != nil {
		t.Fatalf("unmarshalRecord: %v", err)
	}
	if got.A != "x" || got.B != 1 {
		t.Errorf("round-trip = %+v", got)
	}
}

func TestMarshalRecord_UnmarshalableValue(t *testing.T) {
	// A channel cannot be marshaled to JSON; the error must propagate
	// rather than silently producing an empty or invented record.
	if _, err := marshalRecord(make(chan int)); err == nil {
		t.Error("marshalRecord must error on an unmarshalable value")
	}
}

func TestUnmarshalRecord_MalformedBytes(t *testing.T) {
	var v map[string]any
	if err := unmarshalRecord(json.RawMessage(`not json`), &v); err == nil {
		t.Error("unmarshalRecord must error on malformed bytes")
	}
}

func TestAtoiSafe(t *testing.T) {
	cases := map[string]int{
		"2021":  2021,
		"":      0,
		"abcd":  0,
		"-5":    -5,
		"00042": 42,
	}
	for in, want := range cases {
		if got := atoiSafe(in); got != want {
			t.Errorf("atoiSafe(%q) = %d, want %d", in, got, want)
		}
	}
}
