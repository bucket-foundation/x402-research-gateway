package provider

import (
	"testing"

	"github.com/gianyrox/x402-research-gateway/internal/harvest"
)

func TestPubMedPaginator(t *testing.T) {
	p := pubMedPaginator{}
	if p.Model() != harvest.ModelOffset {
		t.Fatalf("model = %q", p.Model())
	}
	params := p.PageParams(harvest.Position{Offset: 50}, 25)
	if params["retstart"] != "50" || params["retmax"] != "25" {
		t.Fatalf("params = %v", params)
	}
	body := []byte(`{"esearchresult":{"count":"120","retmax":"25","idlist":["1","2","3"]}}`)
	next, more := p.NextPosition(body, harvest.Position{Offset: 50}, 25)
	if !more || next.Offset != 53 {
		t.Fatalf("next = %+v more = %v", next, more)
	}
	// The provider's own count ends the set.
	last := []byte(`{"esearchresult":{"count":"52","retmax":"25","idlist":["1","2"]}}`)
	if _, more := p.NextPosition(last, harvest.Position{Offset: 50}, 25); more {
		t.Fatal("the paginator ran past the provider's reported count")
	}
	if _, more := p.NextPosition([]byte(`{"esearchresult":{"idlist":[]}}`), harvest.Position{}, 25); more {
		t.Fatal("an empty page produced a next position")
	}
}

func TestOpenAlexPaginator(t *testing.T) {
	p := openAlexPaginator{}
	if p.Model() != harvest.ModelPage {
		t.Fatalf("model = %q", p.Model())
	}
	if got := p.PageParams(harvest.Position{}, 25)["page"]; got != "1" {
		t.Fatalf("a zero position must request page 1, got %q", got)
	}
	body := []byte(`{"meta":{"count":100,"page":2,"per_page":25},"results":[{"id":"W1"}]}`)
	next, more := p.NextPosition(body, harvest.Position{Page: 2}, 25)
	if !more || next.Page != 3 {
		t.Fatalf("next = %+v more = %v", next, more)
	}
	last := []byte(`{"meta":{"count":100,"page":4,"per_page":25},"results":[{"id":"W1"}]}`)
	if _, more := p.NextPosition(last, harvest.Position{Page: 4}, 25); more {
		t.Fatal("the paginator ran past the last page")
	}
}

func TestSemanticScholarPaginator(t *testing.T) {
	p := semanticScholarPaginator{}
	if p.Model() != harvest.ModelOffset {
		t.Fatalf("model = %q", p.Model())
	}
	params := p.PageParams(harvest.Position{Offset: 100}, 20)
	if params["offset"] != "100" || params["limit"] != "20" {
		t.Fatalf("params = %v", params)
	}
	next, more := p.NextPosition([]byte(`{"next":120,"data":[{"paperId":"a"}]}`), harvest.Position{Offset: 100}, 20)
	if !more || next.Offset != 120 {
		t.Fatalf("next = %+v more = %v", next, more)
	}
	// An omitted `next` is the end of what this provider will serve.
	if _, more := p.NextPosition([]byte(`{"data":[{"paperId":"a"}]}`), harvest.Position{Offset: 100}, 20); more {
		t.Fatal("a response with no next handle produced a next position")
	}
}

func TestClinicalTrialsPaginator(t *testing.T) {
	p := clinicalTrialsPaginator{}
	if p.Model() != harvest.ModelToken {
		t.Fatalf("model = %q", p.Model())
	}
	if _, present := p.PageParams(harvest.Position{}, 25)["pageToken"]; present {
		t.Fatal("a first page must not send an empty page token")
	}
	if got := p.PageParams(harvest.Position{Token: "abc"}, 25)["pageToken"]; got != "abc" {
		t.Fatalf("pageToken = %q", got)
	}
	next, more := p.NextPosition([]byte(`{"nextPageToken":"xyz","studies":[{}]}`), harvest.Position{}, 25)
	if !more || next.Token != "xyz" {
		t.Fatalf("next = %+v more = %v", next, more)
	}
	if _, more := p.NextPosition([]byte(`{"studies":[{}]}`), harvest.Position{}, 25); more {
		t.Fatal("a response with no token produced a next position")
	}
}

func TestEuropePMCPaginator(t *testing.T) {
	p := europePMCPaginator{}
	if p.Model() != harvest.ModelCursor {
		t.Fatalf("model = %q", p.Model())
	}
	if got := p.PageParams(harvest.Position{}, 25)["cursorMark"]; got != "*" {
		t.Fatalf("a scan must start at *, got %q", got)
	}
	body := []byte(`{"nextCursorMark":"AoJw","resultList":{"result":[{"id":"1"}]}}`)
	next, more := p.NextPosition(body, harvest.Position{}, 25)
	if !more || next.Token != "AoJw" {
		t.Fatalf("next = %+v more = %v", next, more)
	}
	// A cursorMark scan ends by returning the mark it was given.
	same := []byte(`{"nextCursorMark":"AoJw","resultList":{"result":[{"id":"1"}]}}`)
	if _, more := p.NextPosition(same, harvest.Position{Token: "AoJw"}, 25); more {
		t.Fatal("a repeated cursorMark did not end the scan")
	}
}

func TestPaginators_NeverPanicOnJunk(t *testing.T) {
	junk := [][]byte{[]byte(``), []byte(`{`), []byte(`[]`), []byte(`{"nope":true}`), nil}
	pagers := []Paginator{pubMedPaginator{}, openAlexPaginator{},
		semanticScholarPaginator{}, clinicalTrialsPaginator{}, europePMCPaginator{}}
	for _, p := range pagers {
		for _, body := range junk {
			if _, more := p.NextPosition(body, harvest.Position{}, 25); more {
				t.Fatalf("%s produced a next position from junk", p.Model())
			}
		}
	}
}

// The four models the gateway proxies must all be reachable through a
// configured adapter, which is the point of the seam.
func TestPaginatorsWiredToAdapters(t *testing.T) {
	want := map[string]string{
		"pubmed-search":           harvest.ModelOffset,
		"openalex-works":          harvest.ModelPage,
		"semantic-scholar-search": harvest.ModelOffset,
		"clinicaltrials-search":   harvest.ModelToken,
		"epmc-search":             harvest.ModelCursor,
	}
	reg := DefaultRegistry()
	for id, model := range want {
		a, ok := reg[id]
		if !ok || a.Paginator == nil {
			t.Fatalf("%s has no paginator", id)
		}
		if got := a.Paginator.Model(); got != model {
			t.Fatalf("%s model = %q, want %q", id, got, model)
		}
	}
}
