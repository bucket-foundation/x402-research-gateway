package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/asset"
	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// assetTestHandler wires Unpaywall and Crossref at fake upstreams. Both are
// asset-capable, and the fan-out is restricted to them so no unit test
// reaches a live provider.
func assetTestHandler(t *testing.T, unpaywallURL, crossrefURL string) *Handler {
	t.Helper()
	cfg := testCfg()
	cfg.Feed402.Assets = config.AssetsConfig{
		Enabled: true, Path: "/research/assets", Price: "0.005",
		MaxConcurrency: 2, TimeoutSeconds: 5,
		ProviderRouteIDs: []string{"unpaywall-fetch", "crossref-fetch"},
	}
	cfg.Routes = append(cfg.Routes,
		config.RouteConfig{
			ID: "unpaywall-fetch", Path: "/research/unpaywall/fetch", Method: "GET",
			Price: "0.001", Feed402Tier: "raw",
			Citation: config.RouteCitation{SourcePrefix: "unpaywall", ProviderURL: "https://unpaywall.org/", License: "per-location"},
			Upstream: config.UpstreamConfig{
				BaseURL: unpaywallURL, Path: "/v2", PassThrough: []string{"id"}, Timeout: 5,
				// A key-bearing param, so the leakage assertion exercises
				// the real code path.
				QueryParams: map[string]string{"email": testUpstreamKey},
			},
		},
		config.RouteConfig{
			ID: "crossref-fetch", Path: "/research/crossref/fetch", Method: "GET",
			Price: "0.001", Feed402Tier: "raw",
			Citation: config.RouteCitation{SourcePrefix: "crossref", ProviderURL: "https://crossref.org/", License: "CC0"},
			Upstream: config.UpstreamConfig{
				BaseURL: crossrefURL, Path: "/works", PassThrough: []string{"id"}, Timeout: 5,
			},
		},
		config.RouteConfig{
			ID: "feed402-assets", Path: "/research/assets", Method: "POST",
			Price: "0.005", Feed402Tier: "query",
			Citation: config.RouteCitation{SourcePrefix: "assets", License: "mixed"},
		},
	)
	h := newTestHandler(cfg)
	h.httpClient = &http.Client{Timeout: 5 * time.Second}
	return h
}

// unpaywallAssetBody carries one CC-BY repository location and one location
// with no licence at all.
const unpaywallAssetBody = `{
  "doi": "10.7717/peerj.4375",
  "title": "The state of OA",
  "is_oa": true,
  "oa_status": "gold",
  "oa_locations": [
    {"url": "https://repo.example.edu/oa.pdf", "url_for_pdf": "https://repo.example.edu/oa.pdf",
     "host_type": "repository", "version": "publishedVersion", "license": "cc-by", "is_best": true},
    {"url": "https://other.example.edu/copy.pdf", "url_for_pdf": "https://other.example.edu/copy.pdf",
     "host_type": "repository", "version": "submittedVersion"}
  ]
}`

// crossrefAssetBody carries a publisher link with no deposited licence: a
// locator that grants nothing.
const crossrefAssetBody = `{"message": {
  "DOI": "10.7717/peerj.4375",
  "URL": "https://doi.org/10.7717/peerj.4375",
  "type": "journal-article",
  "title": ["The state of OA"],
  "link": [{"URL": "https://publisher.example/full.pdf", "content-type": "application/pdf",
            "intended-application": "text-mining", "content-version": "vor"}]
}}`

func TestFanOutAssets_RightsPerAssetAndUnknownNeverPermitted(t *testing.T) {
	h := assetTestHandler(t,
		serve(t, 200, unpaywallAssetBody).URL,
		serve(t, 200, crossrefAssetBody).URL)

	assets, reports := h.fanOutAssets(context.Background(), "10.7717/peerj.4375")
	if len(reports) != 2 {
		t.Fatalf("want a report per provider, got %+v", reports)
	}
	if len(assets) != 3 {
		t.Fatalf("want 3 assets (2 unpaywall + 1 crossref), got %d", len(assets))
	}

	byURL := map[string]asset.Asset{}
	for _, a := range assets {
		byURL[a.CanonicalURL] = a
	}
	if licensed := byURL["https://repo.example.edu/oa.pdf"]; !licensed.Rights.Permits() {
		t.Fatal("a cc-by location was not reported as redistributable")
	}
	for _, url := range []string{
		"https://other.example.edu/copy.pdf", // unpaywall location, no licence
		"https://publisher.example/full.pdf", // crossref link, no deposited licence
	} {
		a, ok := byURL[url]
		if !ok {
			t.Fatalf("%s missing from the asset set", url)
		}
		if a.Rights.Permits() {
			t.Fatalf("%s: unknown rights rendered as permitted", url)
		}
		if a.Rights.Redistribution != asset.RedistributionUnknown {
			t.Fatalf("%s: redistribution = %q", url, a.Rights.Redistribution)
		}
	}

	// Crossref's metadata is CC0 and its content rights are unknown. The
	// two must not be conflated.
	for _, r := range reports {
		if r.Provider != "crossref-fetch" {
			continue
		}
		if r.MetadataRights.License != "CC0" {
			t.Fatalf("metadata rights = %+v", r.MetadataRights)
		}
	}
}

func TestFanOutAssets_NeverFetchesAnAssetLocation(t *testing.T) {
	var contentHits int64
	contentHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&contentHits, 1)
		_, _ = w.Write([]byte("COPYRIGHTED BODY"))
	}))
	t.Cleanup(contentHost.Close)

	// Both providers point every location at the content host, so any
	// dereference would register.
	body := `{"doi":"10.7717/peerj.4375","is_oa":true,"oa_locations":[
	  {"url":"` + contentHost.URL + `/oa.pdf","url_for_pdf":"` + contentHost.URL + `/oa.pdf",
	   "host_type":"repository","license":"cc-by"}]}`
	h := assetTestHandler(t, serve(t, 200, body).URL, serve(t, 200, crossrefAssetBody).URL)

	assets, _ := h.fanOutAssets(context.Background(), "10.7717/peerj.4375")
	if len(assets) == 0 {
		t.Fatal("no assets discovered, so the no-fetch assertion proves nothing")
	}
	if got := atomic.LoadInt64(&contentHits); got != 0 {
		t.Fatalf("the gateway dereferenced %d asset locations; discovery must never fetch content", got)
	}
}

func TestFanOutAssets_AbsentIsReportedAsAnAnswer(t *testing.T) {
	const noOA = `{"doi":"10.1000/paywalled","is_oa":false,"oa_locations":[]}`
	const noLinks = `{"message":{"DOI":"10.1000/paywalled","type":"journal-article"}}`
	h := assetTestHandler(t, serve(t, 200, noOA).URL, serve(t, 200, noLinks).URL)

	assets, reports := h.fanOutAssets(context.Background(), "10.1000/paywalled")
	set := asset.Build(mustParse(t, "10.1000/paywalled"), assets, reports, time.Now())
	if set.Availability != asset.AvailabilityAbsent {
		t.Fatalf("availability = %q, want absent", set.Availability)
	}
	if set.OpenAccessCopyFound {
		t.Fatal("open_access_copy_found is true with no open-access copy")
	}
	for _, r := range set.ProvidersConsulted {
		if !r.Consulted || r.Outcome != asset.OutcomeOK {
			t.Fatalf("%s: a provider that answered with nothing must report ok: %+v", r.Provider, r)
		}
	}
	if set.DiscoveryNotice == "" {
		t.Fatal("discovery notice missing from a negative answer")
	}
}

func TestFanOutAssets_UpstreamFailureIsNotANegativeAnswer(t *testing.T) {
	h := assetTestHandler(t, serve(t, 500, `{}`).URL, serve(t, 200, crossrefAssetBody).URL)
	_, reports := h.fanOutAssets(context.Background(), "10.7717/peerj.4375")
	for _, r := range reports {
		if r.Provider != "unpaywall-fetch" {
			continue
		}
		if r.Outcome != asset.OutcomeUpstreamStatus || r.UpstreamStatus != 500 {
			t.Fatalf("a 500 was reported as %+v", r)
		}
	}
}

// The upstream credential must never reach a response body, a provider
// report, or an asset. Same rule as TestUnpaywallIdentity_NoEmailLeakage.
func TestAssetResponse_NoCredentialLeakage(t *testing.T) {
	h := assetTestHandler(t, serve(t, 500, `{}`).URL, serve(t, 200, crossrefAssetBody).URL)
	assets, reports := h.fanOutAssets(context.Background(), "10.7717/peerj.4375")
	set := asset.Build(mustParse(t, "10.7717/peerj.4375"), assets, reports, time.Now())
	blob := mustJSON(t, set)
	if strings.Contains(blob, testUpstreamKey) {
		t.Fatal("the upstream credential leaked into the asset response")
	}
	if strings.Contains(blob, h.findRouteByID("unpaywall-fetch").Upstream.BaseURL) {
		t.Fatal("the upstream base URL leaked into the asset response")
	}
}

func mustParse(t *testing.T, raw string) identity.Identifier {
	t.Helper()
	id, ok := identity.Parse(raw)
	if !ok {
		t.Fatalf("fixture identifier %q did not parse", raw)
	}
	return id
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
