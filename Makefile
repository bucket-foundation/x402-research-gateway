.PHONY: build run test clean fmt vet smoke test-402 mock-facilitator e2e proxy-test \
        registry validate-providers research-index research-index-check \
        verify-providers provider-coverage

# Build all binaries
build:
	go build -o bin/gateway ./cmd/gateway/
	go build -o bin/test-client ./cmd/test-client/
	go build -o bin/mock-facilitator ./cmd/mock-facilitator/

# Run the gateway locally (requires .env)
run: build
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && ./bin/gateway

# Run directly with go run
dev:
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && go run ./cmd/gateway/

# Start mock facilitator for local testing
mock-facilitator:
	go run ./cmd/mock-facilitator/

# Run Go tests
test:
	go test ./...

# Run proxy integration tests (verifies 402 responses + upstream APIs)
proxy-test:
	go run ./cmd/test-client/ --proxy-test

# Format and vet
fmt:
	go fmt ./...
vet:
	go vet ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Test that the health endpoint responds
smoke:
	curl -s http://localhost:8091/health | jq .

# Test that a route returns 402
test-402:
	@echo "Testing PubMed search (expect 402)..."
	@curl -s -o /dev/null -w "%{http_code}" http://localhost:8091/research/pubmed/search?term=longevity
	@echo ""

# ---------- Research source registry ----------
# config/providers.yaml is the source of truth. RESEARCH-INDEX.md is generated
# from it; the hand-written analysis lives in docs/research-index/.

registry:
	go build -o bin/registry ./cmd/registry/

# Parse and validate the registry, and reconcile it against config/routes.yaml.
validate-providers:
	go run ./cmd/registry validate

# Regenerate RESEARCH-INDEX.md from the registry.
research-index:
	go run ./cmd/registry research-index

# Fail if the committed document has drifted from the registry. For CI.
research-index-check:
	go run ./cmd/registry research-index -check

# Cheap liveness and drift check: one lightweight request per URL, polite
# identification, no scraping and no bulk downloads. A failure flags an entry
# stale, it never deletes one. Pass -write to record last_verified.
#   make verify-providers ARGS="-only crossref,ror -write"
verify-providers:
	go run ./cmd/registry verify $(ARGS)

# Lifecycle and provider-type summary.
provider-coverage:
	go run ./cmd/registry coverage
