package provider

import "testing"

// openaireSearchFixture is trimmed from a live
// api.openaire.eu/graph/v3/research-products response verified 2026-08-18
// (see openaire.go's doc comment).
const openaireSearchFixture = `{"header":{"numFound":1162738,"page":1,"pageSize":2},"results":[
  {"id":"doi_dedup___::4807efad8ff855adaa51d3c5c5390481","type":"publication","mainTitle":"The Changing Landscape of Machine Learning","publicationDate":"2024-01-01","authors":[{"fullName":"Dishita Naik"},{"fullName":"Nitin Naik"}],"pids":[{"scheme":"doi","value":"10.1007/978-3-031-47508-5_2"}],"instances":[{"type":"Article","urls":["https://doi.org/10.1007/978-3-031-47508-5_2"],"accessRight":{"label":"CLOSED"}}]},
  {"id":"openaire____::noident","type":"publication","mainTitle":"A record with no DOI","publicationDate":"2020-01-01","authors":[],"pids":[]}
]}`

const openaireSingleFixture = `{"id":"doi_dedup___::4807efad8ff855adaa51d3c5c5390481","type":"publication","mainTitle":"The Changing Landscape of Machine Learning","publicationDate":"2024-01-01","pids":[{"scheme":"doi","value":"10.1007/978-3-031-47508-5_2"}],"instances":[{"type":"Article","urls":["https://doi.org/10.1007/978-3-031-47508-5_2"],"license":"https://creativecommons.org/licenses/by/4.0/","accessRight":{"label":"OPEN"}}]}`

func TestOpenAIRENormalizer_SearchShape(t *testing.T) {
	recs := OpenAIRENormalizer{}.Normalize([]byte(openaireSearchFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "doi_dedup___::4807efad8ff855adaa51d3c5c5390481" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doi.org/10.1007/978-3-031-47508-5_2" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	// A record with no DOI falls back to OpenAIRE's own explore.openaire.eu
	// resolver rather than being dropped.
	if recs[1].CanonicalURL != "https://explore.openaire.eu/search/publication?pid=openaire____::noident" {
		t.Errorf("fallback canonical url = %q", recs[1].CanonicalURL)
	}
}

func TestOpenAIRENormalizer_SingleRecordShape(t *testing.T) {
	recs := OpenAIRENormalizer{}.Normalize([]byte(openaireSingleFixture))
	if len(recs) != 1 || recs[0].ID != "doi_dedup___::4807efad8ff855adaa51d3c5c5390481" {
		t.Fatalf("single-record shape not handled: %+v", recs)
	}
}

func TestOpenAIRENormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`)} {
		if recs := (OpenAIRENormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestOpenAIREIdentity_Identifiers(t *testing.T) {
	recs := OpenAIRENormalizer{}.Normalize([]byte(openaireSearchFixture))
	ids := openaireIdentity{}.Identifiers(recs[0])
	var sawDOI, sawOpenAIRE bool
	for _, id := range ids {
		if id.Scheme == "doi" && id.Raw == "10.1007/978-3-031-47508-5_2" {
			sawDOI = true
		}
		if id.Scheme == "openaire" {
			sawOpenAIRE = true
		}
	}
	if !sawDOI || !sawOpenAIRE {
		t.Errorf("expected both a doi and an openaire identifier in %+v", ids)
	}
}

func TestOpenAIREIdentity_RecordRights_UnknownWithoutLicense(t *testing.T) {
	recs := OpenAIRENormalizer{}.Normalize([]byte(openaireSearchFixture))
	rights := openaireIdentity{}.RecordRights(recs[0])
	if rights.Permits() {
		t.Errorf("no license field present; redistribution must stay unknown, got %+v", rights)
	}
}

func TestOpenAIREIdentity_RecordRights_ReadsPerInstanceLicense(t *testing.T) {
	recs := OpenAIRENormalizer{}.Normalize([]byte(openaireSingleFixture))
	rights := openaireIdentity{}.RecordRights(recs[0])
	if rights.License != "https://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("license = %q, want the per-instance CC-BY URL", rights.License)
	}
	if !rights.FreeToRead {
		t.Error("accessRight.label OPEN should set FreeToRead")
	}
}

func TestOpenAIREIdentity_Assets(t *testing.T) {
	recs := OpenAIRENormalizer{}.Normalize([]byte(openaireSingleFixture))
	assets := openaireIdentity{}.Assets(recs[0])
	if len(assets) != 1 {
		t.Fatalf("got %d assets, want 1", len(assets))
	}
	if assets[0].CanonicalURL != "https://doi.org/10.1007/978-3-031-47508-5_2" {
		t.Errorf("asset url = %q", assets[0].CanonicalURL)
	}
}
