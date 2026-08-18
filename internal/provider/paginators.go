package provider

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gianyrox/x402-research-gateway/internal/harvest"
)

// Paginator implementations (x402-research-gateway#10).
//
// Four providers, four pagination models, one interface. Before this, each
// model was an opaque passthrough parameter in config and the gateway held
// no state about any of them, so an interrupted walk of a large result set
// could only restart and pay again for pages it already had.
//
// Each implementation does two things: render the upstream parameters for a
// position, and read the provider's own next-page handle out of a response.
// Neither touches credentials, and no implementation invents a next page:
// a response with no handle is the end of the set, reported as such.

// ---------- PubMed ESearch: offset on retstart ----------

type pubMedPaginator struct{}

func (pubMedPaginator) Model() string { return harvest.ModelOffset }

func (pubMedPaginator) PageParams(pos harvest.Position, pageSize int) map[string]string {
	return map[string]string{
		"retstart": strconv.Itoa(pos.Offset),
		"retmax":   strconv.Itoa(pageSize),
	}
}

// NextPosition advances by the page's own record count, and stops at the
// count ESearch reports. The count is the provider's, so a set that grew
// between pages is the provider's statement rather than this gateway's
// guess.
func (pubMedPaginator) NextPosition(body []byte, pos harvest.Position, pageSize int) (harvest.Position, bool) {
	var parsed struct {
		ESearchResult struct {
			Count  string   `json:"count"`
			RetMax string   `json:"retmax"`
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.ESearchResult.IDList) == 0 {
		return harvest.Position{}, false
	}
	next := pos.Offset + len(parsed.ESearchResult.IDList)
	total, err := strconv.Atoi(strings.TrimSpace(parsed.ESearchResult.Count))
	if err == nil && next >= total {
		return harvest.Position{}, false
	}
	return harvest.Position{Offset: next}, true
}

// ---------- OpenAlex: page number ----------

type openAlexPaginator struct{}

func (openAlexPaginator) Model() string { return harvest.ModelPage }

func (openAlexPaginator) PageParams(pos harvest.Position, pageSize int) map[string]string {
	page := pos.Page
	if page < 1 {
		page = 1
	}
	return map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(pageSize),
	}
}

// NextPosition reads OpenAlex's own meta block: it publishes count, page,
// and per_page, so the last page is a fact rather than an inference from an
// empty result list.
func (openAlexPaginator) NextPosition(body []byte, pos harvest.Position, pageSize int) (harvest.Position, bool) {
	var parsed struct {
		Meta struct {
			Count   int `json:"count"`
			Page    int `json:"page"`
			PerPage int `json:"per_page"`
		} `json:"meta"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Results) == 0 {
		return harvest.Position{}, false
	}
	page := parsed.Meta.Page
	if page < 1 {
		page = pos.Page
		if page < 1 {
			page = 1
		}
	}
	perPage := parsed.Meta.PerPage
	if perPage < 1 {
		perPage = pageSize
	}
	if parsed.Meta.Count > 0 && page*perPage >= parsed.Meta.Count {
		return harvest.Position{}, false
	}
	return harvest.Position{Page: page + 1}, true
}

// ---------- Semantic Scholar: offset ----------

type semanticScholarPaginator struct{}

func (semanticScholarPaginator) Model() string { return harvest.ModelOffset }

func (semanticScholarPaginator) PageParams(pos harvest.Position, pageSize int) map[string]string {
	return map[string]string{
		"offset": strconv.Itoa(pos.Offset),
		"limit":  strconv.Itoa(pageSize),
	}
}

// NextPosition uses the Graph API's own `next`, which the API omits at the
// end of a set and at its deep-paging ceiling. An omitted `next` is the end
// of what this provider will serve, which is not the same as the end of the
// literature, and the response says which.
func (semanticScholarPaginator) NextPosition(body []byte, pos harvest.Position, pageSize int) (harvest.Position, bool) {
	var parsed struct {
		Next *int              `json:"next"`
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Data) == 0 {
		return harvest.Position{}, false
	}
	if parsed.Next == nil {
		return harvest.Position{}, false
	}
	return harvest.Position{Offset: *parsed.Next}, true
}

// ---------- ClinicalTrials.gov v2: opaque page token ----------

type clinicalTrialsPaginator struct{}

func (clinicalTrialsPaginator) Model() string { return harvest.ModelToken }

func (clinicalTrialsPaginator) PageParams(pos harvest.Position, pageSize int) map[string]string {
	params := map[string]string{"pageSize": strconv.Itoa(pageSize)}
	if pos.Token != "" {
		params["pageToken"] = pos.Token
	}
	return params
}

func (clinicalTrialsPaginator) NextPosition(body []byte, pos harvest.Position, pageSize int) (harvest.Position, bool) {
	var parsed struct {
		NextPageToken string            `json:"nextPageToken"`
		Studies       []json.RawMessage `json:"studies"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return harvest.Position{}, false
	}
	if parsed.NextPageToken == "" || len(parsed.Studies) == 0 {
		return harvest.Position{}, false
	}
	return harvest.Position{Token: parsed.NextPageToken}, true
}

// ---------- Europe PMC: cursorMark ----------

type europePMCPaginator struct{}

func (europePMCPaginator) Model() string { return harvest.ModelCursor }

func (europePMCPaginator) PageParams(pos harvest.Position, pageSize int) map[string]string {
	token := pos.Token
	if token == "" {
		token = "*"
	}
	return map[string]string{
		"cursorMark": token,
		"pageSize":   strconv.Itoa(pageSize),
		"format":     "json",
	}
}

// NextPosition stops when Europe PMC returns the cursor it was given, which
// is how a Solr cursorMark scan signals the end of a set.
func (europePMCPaginator) NextPosition(body []byte, pos harvest.Position, pageSize int) (harvest.Position, bool) {
	var parsed struct {
		NextCursorMark string `json:"nextCursorMark"`
		ResultList     struct {
			Result []json.RawMessage `json:"result"`
		} `json:"resultList"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return harvest.Position{}, false
	}
	if parsed.NextCursorMark == "" || len(parsed.ResultList.Result) == 0 {
		return harvest.Position{}, false
	}
	current := pos.Token
	if current == "" {
		current = "*"
	}
	if parsed.NextCursorMark == current {
		return harvest.Position{}, false
	}
	return harvest.Position{Token: parsed.NextCursorMark}, true
}
