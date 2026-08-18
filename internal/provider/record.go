package provider

import (
	"encoding/json"
	"strconv"
)

// Helpers shared by adapters whose upstream is not JSON. A NormalizedRecord
// carries its provider bytes in a json.RawMessage, so an adapter parsing
// Atom XML or any other wire format projects each record to JSON rather
// than dropping the raw data it cannot store verbatim.

func marshalRecord(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func unmarshalRecord(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }

// atoiSafe parses a year-shaped string, returning 0 for anything it cannot
// read. A zero year disables year-gap similarity rather than asserting one.
func atoiSafe(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
