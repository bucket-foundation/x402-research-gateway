.PHONY: build run test clean fmt vet smoke test-402 mock-facilitator e2e proxy-test

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
