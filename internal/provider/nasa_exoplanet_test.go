package provider

import "testing"

// nasaExoplanetFixture is a live TAP/sync response body verified 2026-08-18
// (see nasa_exoplanet.go's doc comment): a bare JSON array, no envelope.
const nasaExoplanetFixture = `[
{"pl_name": "Kepler-370 b", "hostname": "Kepler-370", "disc_year": 2014},
{"pl_name": "Kepler-317 c", "hostname": "Kepler-317", "disc_year": 2014}
]`

func TestNASAExoplanetNormalizer(t *testing.T) {
	recs := NASAExoplanetNormalizer{}.Normalize([]byte(nasaExoplanetFixture))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].ID != "Kepler-370 b" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://exoplanetarchive.ipac.caltech.edu/overview/Kepler-370%20b" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestNASAExoplanetNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`[]`), []byte(`[{"hostname":"x"}]`)} {
		if recs := (NASAExoplanetNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestNASAExoplanetIdentity_Descriptor(t *testing.T) {
	recs := NASAExoplanetNormalizer{}.Normalize([]byte(nasaExoplanetFixture))
	d := nasaExoplanetIdentity{}.Descriptor(recs[0])
	if d.Title != "Kepler-370 b" || d.Year != 2014 {
		t.Errorf("descriptor = %+v", d)
	}
}

func TestNASAExoplanetIdentity_RecordRights_Unknown(t *testing.T) {
	recs := NASAExoplanetNormalizer{}.Normalize([]byte(nasaExoplanetFixture))
	rights := nasaExoplanetIdentity{}.RecordRights(recs[0])
	if rights.Redistribution != RedistributionUnknown {
		t.Errorf("redistribution = %q, want unknown (no site-wide statement found)", rights.Redistribution)
	}
}

func TestNASAExoplanetAdapter_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["nasa-exoplanet-archive-tap"] != NASAExoplanetArchiveAdapter {
		t.Error("nasa-exoplanet-archive-tap not wired")
	}
	if NASAExoplanetArchiveAdapter.Searcher.PaginationModel() != "none" {
		t.Error("TAP/sync returns its full bounded result set in one call")
	}
}
