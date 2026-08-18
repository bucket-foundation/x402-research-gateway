package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/registry"
)

func syncTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := testCfg()
	cfg.Feed402.SyncDiscovery = config.SyncDiscoveryConfig{
		Enabled: true, Path: "/research/sync", RegistryPath: "../../config/providers.yaml",
	}
	h := newTestHandler(cfg)
	reg, err := registry.Load(cfg.Feed402.SyncDiscovery.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	h.registry = reg
	return h
}

func syncResponse(t *testing.T, h *Handler, query string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h.handleSyncDiscovery(rec, httptest.NewRequest("GET", "/research/sync"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSyncDiscovery_ListsBulkProviders(t *testing.T) {
	out := syncResponse(t, syncTestHandler(t), "")
	providers, _ := out["providers"].([]any)
	if len(providers) < 10 {
		t.Fatalf("only %d providers declare sync capability", len(providers))
	}
	if out["notice"] == "" {
		t.Fatal("the not-a-mirror notice is missing")
	}
	if !strings.Contains(out["notice"].(string), "serves no snapshot") {
		t.Fatalf("notice = %q", out["notice"])
	}
}

func TestSyncDiscovery_FilterByMode(t *testing.T) {
	h := syncTestHandler(t)
	out := syncResponse(t, h, "?mode=oai_pmh")
	providers, _ := out["providers"].([]any)
	if len(providers) < 4 {
		t.Fatalf("only %d OAI-PMH providers listed", len(providers))
	}
	for _, raw := range providers {
		p := raw.(map[string]any)
		sync := p["sync"].(map[string]any)
		if sync["oai_pmh_endpoint"] == nil {
			t.Fatalf("%v declares oai_pmh with no endpoint", p["provider_id"])
		}
	}
}

func TestSyncDiscovery_UnknownModeRefused(t *testing.T) {
	h := syncTestHandler(t)
	rec := httptest.NewRecorder()
	h.handleSyncDiscovery(rec, httptest.NewRequest("GET", "/research/sync?mode=torrent", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSyncDiscovery_SnapshotAndAPIRightsStaySeparate(t *testing.T) {
	out := syncResponse(t, syncTestHandler(t), "?provider=crossref")
	providers := out["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("want the crossref entry, got %d", len(providers))
	}
	p := providers[0].(map[string]any)
	api := p["api_rights"].(map[string]any)
	if api["metadata_license"] == nil {
		t.Fatal("the API rights block is missing")
	}
	snapshots := p["sync"].(map[string]any)["snapshots"].([]any)
	snap := snapshots[0].(map[string]any)
	rights := snap["rights"].(map[string]any)
	// Crossref's metadata is CC0 and its dump is a paid product. The two
	// postures must not be conflated.
	if rights["redistribution"] != "unknown" {
		t.Fatalf("snapshot redistribution = %v", rights["redistribution"])
	}
	if snap["auth"] != "subscription" {
		t.Fatalf("snapshot auth = %v", snap["auth"])
	}
}

// The endpoint describes. No response field carries an artifact, and no
// route serves one.
func TestSyncDiscovery_ServesNoArtifact(t *testing.T) {
	out := syncResponse(t, syncTestHandler(t), "")
	blob, _ := json.Marshal(out)
	for _, banned := range []string{"\"body\"", "\"content\"", "\"bytes\"", "\"download_proxy\""} {
		if strings.Contains(string(blob), banned) {
			t.Fatalf("the sync listing carries %s", banned)
		}
	}
}

func TestSyncDiscovery_NoRegistryIsReportedNotEmpty(t *testing.T) {
	h := newTestHandler(testCfg())
	rec := httptest.NewRecorder()
	h.handleSyncDiscovery(rec, httptest.NewRequest("GET", "/research/sync", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; a missing registry must not read as no provider having bulk access", rec.Code)
	}
}

func TestSyncDiscovery_AdapterCapabilityReportedBesideRegistry(t *testing.T) {
	out := syncResponse(t, syncTestHandler(t), "?provider=arxiv")
	p := out["providers"].([]any)[0].(map[string]any)
	if p["adapter_incremental"] != true {
		t.Fatalf("arxiv adapter incremental = %v", p["adapter_incremental"])
	}
	// The arXiv adapter reports Bulk false because this gateway exercises
	// no requester-pays channel, while the registry describes that the
	// channel exists. Both readings are present and neither overwrites the
	// other.
	if p["adapter_bulk"] != false {
		t.Fatalf("arxiv adapter bulk = %v", p["adapter_bulk"])
	}
	sync := p["sync"].(map[string]any)
	modes := sync["modes"].([]any)
	found := false
	for _, m := range modes {
		if m == "bulk_snapshot" {
			found = true
		}
	}
	if !found {
		t.Fatalf("arxiv registry modes = %v", modes)
	}
}
