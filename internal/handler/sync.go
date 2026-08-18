package handler

// Bulk and incremental sync discovery (x402-research-gateway#11).
//
// An agent asking "should I page through this API two million times or
// download the snapshot" gets the answer here: which providers publish a
// whole-corpus artifact, where it is, what format it is in, how often it is
// republished, what it costs to access, and what its licence permits.
//
// The gateway describes. It is not a mirror, a CDN, or a snapshot host, and
// no route here serves an artifact. Streaming a multi-gigabyte dump through
// a metered HTTP endpoint would multiply cost, add a failure point, and in
// several cases breach the provider's own redistribution terms.
//
// Discovery is free. Pricing a decision about whether to pay must not
// itself cost anything, which is the same rule the federated cost estimate
// follows.

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gianyrox/x402-research-gateway/internal/registry"
)

// SyncNotice is emitted in every sync-discovery response.
const SyncNotice = "This is a description of what each provider publishes. The gateway serves no snapshot, " +
	"mirrors nothing, and hosts nothing: fetch an artifact from the provider directly. Snapshot rights are " +
	"stated apart from the API's rights and are routinely stricter; unknown grants nothing. An entry with " +
	"verified false was read from the provider's documentation rather than checked against a live fetch."

// syncProviderView is one provider's sync capability as the discovery
// endpoint reports it.
type syncProviderView struct {
	ProviderID string        `json:"provider_id"`
	Name       string        `json:"name"`
	Status     string        `json:"status"`
	Sync       registry.Sync `json:"sync"`
	// APIRights is the provider's rights over its query API, carried here
	// so a consumer can see it beside the snapshot rights and never read
	// one as the other.
	APIRights registry.Rights `json:"api_rights"`
	// RouteIDs are the gateway routes serving this provider, so an agent
	// choosing between paging and downloading knows what paging would cost.
	RouteIDs []string `json:"route_ids,omitempty"`
	// AdapterSyncCapability is what the running adapter reports, which can
	// differ from the registry entry when the adapter is deliberately
	// conservative about a channel this deployment cannot exercise.
	AdapterBulk        *bool `json:"adapter_bulk,omitempty"`
	AdapterIncremental *bool `json:"adapter_incremental,omitempty"`
}

// handleSyncDiscovery serves the free sync-capability listing.
func (h *Handler) handleSyncDiscovery(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "no provider registry is loaded on this deployment",
		})
		return
	}
	wantMode := registry.SyncMode(strings.TrimSpace(r.URL.Query().Get("mode")))
	if wantMode != "" && !wantMode.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       "unknown sync mode",
			"mode":        string(wantMode),
			"valid_modes": registry.SyncModes,
		})
		return
	}
	wantProvider := strings.TrimSpace(r.URL.Query().Get("provider"))

	views := []syncProviderView{}
	for i := range h.registry.Providers {
		p := &h.registry.Providers[i]
		if !p.Sync.Declared() {
			continue
		}
		if wantProvider != "" && p.ProviderID != wantProvider {
			continue
		}
		if wantMode != "" && !p.Sync.HasMode(wantMode) {
			continue
		}
		v := syncProviderView{
			ProviderID: p.ProviderID,
			Name:       p.Name,
			Status:     string(p.Status),
			Sync:       p.Sync,
			APIRights:  p.Rights,
			RouteIDs:   p.RouteIDs,
		}
		for _, routeID := range p.RouteIDs {
			adapter := h.providers[routeID]
			if adapter == nil || adapter.SyncProvider == nil {
				continue
			}
			cap := adapter.SyncProvider.SyncCapability()
			bulk, incremental := cap.Bulk, cap.Incremental
			v.AdapterBulk, v.AdapterIncremental = &bulk, &incremental
			break
		}
		views = append(views, v)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ProviderID < views[j].ProviderID })

	writeJSON(w, http.StatusOK, map[string]any{
		"providers":      views,
		"provider_count": len(views),
		"valid_modes":    registry.SyncModes,
		"notice":         SyncNotice,
	})
}
