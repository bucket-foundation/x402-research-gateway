package provider

import "testing"

// figshareSearchFixture and figshareSingleFixture are trimmed from live
// api.figshare.com responses verified 2026-08-18 (see figshare.go's doc
// comment). The search shape omits `license`; the single-article shape
// carries it.
const figshareSearchFixture = `[{"id":32826848,"doi":"10.6084/m9.figshare.32826848.v7","title":"Approaching ORBIT","url_public_html":"https://figshare.com/articles/32826848"}]`

const figshareSingleFixture = `{"id":32826848,"doi":"10.6084/m9.figshare.32826848.v7","title":"Approaching ORBIT","published_date_year":2026,"license":{"value":6,"name":"GPL 3.0+","url":"https://www.gnu.org/licenses/gpl-3.0.html"}}`

const figshareCC0Fixture = `{"id":11111,"doi":"10.6084/m9.figshare.11111.v1","title":"A CC0 dataset","license":{"value":1,"name":"CC0","url":"https://creativecommons.org/publicdomain/zero/1.0/"}}`

const figshareCCBYNCFixture = `{"id":22222,"doi":"10.6084/m9.figshare.22222.v1","title":"A CC BY-NC dataset","license":{"value":2,"name":"CC BY-NC 4.0","url":"https://creativecommons.org/licenses/by-nc/4.0/"}}`

func TestFigshareNormalizer_SearchShape(t *testing.T) {
	recs := FigshareNormalizer{}.Normalize([]byte(figshareSearchFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "32826848" {
		t.Errorf("id = %q", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://doi.org/10.6084/m9.figshare.32826848.v7" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
}

func TestFigshareNormalizer_SingleRecordShape(t *testing.T) {
	recs := FigshareNormalizer{}.Normalize([]byte(figshareSingleFixture))
	if len(recs) != 1 || recs[0].ID != "32826848" {
		t.Fatalf("single-record shape not handled: %+v", recs)
	}
}

func TestFigshareNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`[]`)} {
		if recs := (FigshareNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestFigshareIdentity_RecordRights_AbsentOnSearchShape(t *testing.T) {
	recs := FigshareNormalizer{}.Normalize([]byte(figshareSearchFixture))
	rights := figshareIdentity{}.RecordRights(recs[0])
	if rights.Redistribution != RedistributionUnknown {
		t.Errorf("search-summary shape carries no license field, want unknown, got %q", rights.Redistribution)
	}
}

func TestFigshareIdentity_RecordRights_CC0Allowed(t *testing.T) {
	recs := FigshareNormalizer{}.Normalize([]byte(figshareCC0Fixture))
	rights := figshareIdentity{}.RecordRights(recs[0])
	if !rights.Permits() {
		t.Errorf("CC0 article should permit redistribution, got %+v", rights)
	}
}

func TestFigshareIdentity_RecordRights_CCBYNCNotAllowed(t *testing.T) {
	recs := FigshareNormalizer{}.Normalize([]byte(figshareCCBYNCFixture))
	rights := figshareIdentity{}.RecordRights(recs[0])
	if rights.Permits() {
		t.Errorf("CC BY-NC article must not report unconditional redistribution, got %+v", rights)
	}
}

func TestFigshareIdentity_RecordRights_GPLNotAllowed(t *testing.T) {
	recs := FigshareNormalizer{}.Normalize([]byte(figshareSingleFixture))
	rights := figshareIdentity{}.RecordRights(recs[0])
	if rights.Permits() {
		t.Errorf("GPL is a software licence, not a CC0/CC-BY redistribution grant, got %+v", rights)
	}
	if rights.License != "GPL 3.0+" {
		t.Errorf("license = %q", rights.License)
	}
}

func TestFigshareAdapters_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["figshare-search"] != FigshareSearchAdapter {
		t.Error("figshare-search not wired")
	}
	if reg["figshare-fetch"] != FigshareFetchAdapter {
		t.Error("figshare-fetch not wired")
	}
}
