package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	routes := make([]map[string]string, 0, len(h.cfg.Routes))
	for _, r := range h.cfg.Routes {
		routes = append(routes, map[string]string{
			"id":          r.ID,
			"path":        r.Path,
			"method":      r.Method,
			"price":       r.Price,
			"description": r.Description,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":    "healthy",
		"service":   "x402-research-gateway",
		"protocol":  "x402-v2",
		"network":   h.cfg.CAIP2Network(),
		"recipient": h.cfg.RecipientAddress,
		"routes":    routes,
	}); err != nil {
		slog.Warn("Failed to encode health response", "error", err)
	}
}
