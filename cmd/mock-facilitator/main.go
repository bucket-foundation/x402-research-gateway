// Package main implements a lightweight mock x402 facilitator for local testing.
//
// It accepts any payment at /verify and returns success, allowing the full
// x402 flow to be tested locally without real on-chain settlement.
//
// Usage:
//
//	go run cmd/mock-facilitator/main.go
//
// Environment:
//
//	PORT - Server port (default: 4402)
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4402"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /supported", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"x402Version": 1,
			"kinds": []map[string]any{
				{
					"scheme":  "exact",
					"network": "base-sepolia",
					"asset":   "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
				},
			},
		})
	})

	mux.HandleFunc("POST /verify", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		slog.Info("Mock facilitator /verify", "body_size", len(body))

		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"isValid":       false,
				"invalidReason": "parse_error",
			})
			return
		}

		// Extract payer from the payload if available
		payer := "0x0000000000000000000000000000000000000000"
		if pp, ok := req["paymentPayload"].(map[string]any); ok {
			if payload, ok := pp["payload"].(map[string]any); ok {
				if auth, ok := payload["authorization"].(map[string]any); ok {
					if from, ok := auth["from"].(string); ok {
						payer = from
					}
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"isValid": true,
			"payer":   payer,
		})
	})

	mux.HandleFunc("POST /settle", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		slog.Info("Mock facilitator /settle", "body_size", len(body))

		writeJSON(w, http.StatusOK, map[string]any{
			"success":     true,
			"transaction": "0xmock_tx_" + fmt.Sprintf("%d", len(body)),
			"network":     "base-sepolia",
			"payer":       "0xMockPayer",
		})
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "healthy",
			"service": "mock-facilitator",
			"note":    "This is a LOCAL mock for testing. Not connected to any blockchain.",
		})
	})

	addr := ":" + port
	slog.Info("Starting mock x402 facilitator", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
