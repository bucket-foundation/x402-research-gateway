// Package main implements the x402 research API gateway.
//
// This server wraps free upstream research APIs (PubMed, Semantic Scholar,
// OpenAlex, ClinicalTrials.gov, PubChem) behind x402 micropayments on
// Base Sepolia testnet.
//
// Usage:
//
//	go run cmd/gateway/main.go
//
// Environment variables:
//
//	PORT               - Server port (default: 8091)
//	RECIPIENT_ADDRESS  - Wallet to receive payments (required)
//	NETWORK            - Target network (default: base-sepolia)
//	FACILITATOR_URL    - x402 facilitator (default: https://facilitator.x402.rs)
//	GATEWAY_CONFIG     - Path to routes YAML (default: ./config/routes.yaml)
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/config"
	"github.com/gianyrox/x402-research-gateway/internal/handler"
)

func main() {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	configPath := os.Getenv("GATEWAY_CONFIG")
	if configPath == "" {
		configPath = "./config/routes.yaml"
	}

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	slog.Info("Loaded gateway configuration",
		"routes", len(cfg.Routes),
		"network", cfg.Network,
		"recipient", cfg.RecipientAddress,
	)

	h := handler.NewHandler(cfg)

	ctx := context.Background()
	if err := h.Initialize(ctx); err != nil {
		slog.Warn("Failed to initialize x402 facilitator (will retry on first request)", "error", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      h.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("Starting x402 Research Gateway",
			"addr", addr,
			"network", cfg.CAIP2Network(),
			"facilitator", cfg.FacilitatorURL,
			"routes", len(cfg.Routes),
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		cancel()
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	slog.Info("Server stopped")
}
