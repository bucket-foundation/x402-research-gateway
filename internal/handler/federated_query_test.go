package handler

import (
	"context"
	"testing"

	"github.com/gianyrox/x402-research-gateway/internal/config"
)

func TestFederatedUpstreamRequestRoutesOnlyProviderSearchParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		passThrough []string
		wantKey     string
		wantValue   string
	}{
		{"openalex", []string{"search", "filter", "sort", "per_page", "page", "select"}, "search", "spectral inverse problems"},
		{"pubmed", []string{"term", "retmax", "retstart", "sort", "mindate", "maxdate", "datetype"}, "term", "spectral inverse problems"},
		{"semantic scholar", []string{"query", "limit", "offset", "fields", "year", "fieldsOfStudy"}, "query", "spectral inverse problems"},
		{"clinical trials", []string{"query.term", "query.cond", "query.intr", "filter.overallStatus", "pageSize", "pageToken", "sort"}, "query.term", "spectral inverse problems"},
		{"arxiv", []string{"search_query", "id_list", "start", "max_results", "sortBy", "sortOrder"}, "search_query", "all:spectral inverse problems"},
		{"zbmath", []string{"search_string", "results_per_page", "page"}, "search_string", "spectral inverse problems"},
		{"dblp", []string{"q", "h", "f"}, "q", "spectral inverse problems"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route := &config.RouteConfig{
				Path: "/search",
				Upstream: config.UpstreamConfig{
					PassThrough: tc.passThrough,
				},
			}

			req := federatedUpstreamRequest(
				context.Background(),
				route,
				"spectral inverse problems",
			)

			got := req.URL.Query()

			if got.Get(tc.wantKey) != tc.wantValue {
				t.Fatalf("%s = %q, want %q", tc.wantKey, got.Get(tc.wantKey), tc.wantValue)
			}

			if len(got) != 1 {
				t.Fatalf("federated query populated non-search controls: %v", got)
			}
		})
	}
}
