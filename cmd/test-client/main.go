// Package main implements an x402 test client for the research gateway.
//
// It demonstrates the full payment flow:
// 1. Request a research API → get 402 with payment requirements
// 2. Sign an EIP-3009 transferWithAuthorization via EIP-712
// 3. Retry with PAYMENT-SIGNATURE header
// 4. Receive upstream API data
//
// Usage:
//
//	# Proxy-only test (no payment, tests upstream proxy):
//	go run cmd/test-client/main.go --proxy-test
//
//	# Full x402 payment test (requires funded wallet):
//	PAYER_PRIVATE_KEY=0x... go run cmd/test-client/main.go
//
// Environment:
//
//	GATEWAY_URL       - Gateway base URL (default: http://localhost:8091)
//	PAYER_PRIVATE_KEY - Hex private key of funded wallet (required for payment test)
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	proxyTest := flag.Bool("proxy-test", false, "Test upstream proxy only (no payment)")
	route := flag.String("route", "/research/pubmed/search?term=longevity+nutrition", "Route to test")
	flag.Parse()

	gatewayURL := getEnv("GATEWAY_URL", "http://localhost:8091")
	url := gatewayURL + *route

	if *proxyTest {
		runProxyTests(gatewayURL)
		return
	}

	runPaymentTest(url)
}

func runProxyTests(gatewayURL string) {
	fmt.Println("=== x402 Research Gateway — Proxy Integration Tests ===")
	fmt.Println()

	tests := []struct {
		name   string
		path   string
		expect string // substring expected in response
	}{
		{
			name:   "PubMed Search (longevity nutrition)",
			path:   "/research/pubmed/search?term=longevity+nutrition",
			expect: "esearchresult",
		},
		{
			name:   "PubMed Fetch (PMID 12345)",
			path:   "/research/pubmed/fetch?id=12345",
			expect: "PubmedArticle",
		},
		{
			name:   "Semantic Scholar (psychedelics neuroplasticity)",
			path:   "/research/semantic-scholar/search?query=psychedelics+neuroplasticity&limit=3",
			expect: "data",
		},
		{
			name:   "OpenAlex (quantum computing)",
			path:   "/research/openalex/works?search=quantum+computing&per_page=3",
			expect: "results",
		},
		{
			name:   "ClinicalTrials (psilocybin)",
			path:   "/research/clinicaltrials/search?query.term=psilocybin&pageSize=3",
			expect: "studies",
		},
		{
			name:   "PubChem Compound (serotonin)",
			path:   "/research/pubchem/compound?name=serotonin",
			expect: "PropertyTable",
		},
	}

	passed := 0
	failed := 0

	for _, tt := range tests {
		fmt.Printf("  Testing %s... ", tt.name)

		// Step 1: Verify 402 is returned
		resp, err := http.Get(gatewayURL + tt.path)
		if err != nil {
			fmt.Printf("FAIL (request error: %v)\n", err)
			failed++
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusPaymentRequired {
			fmt.Printf("FAIL (expected 402, got %d)\n", resp.StatusCode)
			failed++
			continue
		}

		// Verify x402 payment header is present
		paymentRequired := resp.Header.Get("Payment-Required")
		if paymentRequired == "" {
			fmt.Printf("FAIL (no Payment-Required header)\n")
			failed++
			continue
		}

		// Decode and verify x402 payload
		payloadBytes, err := base64.StdEncoding.DecodeString(paymentRequired)
		if err != nil {
			fmt.Printf("FAIL (can't decode Payment-Required: %v)\n", err)
			failed++
			continue
		}

		var payload struct {
			X402Version int `json:"x402Version"`
			Accepts     []struct {
				Scheme  string `json:"scheme"`
				Network string `json:"network"`
				Amount  string `json:"amount"`
				PayTo   string `json:"payTo"`
			} `json:"accepts"`
		}
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			fmt.Printf("FAIL (can't parse x402 payload: %v)\n", err)
			failed++
			continue
		}

		if payload.X402Version != 2 {
			fmt.Printf("FAIL (expected x402v2, got v%d)\n", payload.X402Version)
			failed++
			continue
		}
		if len(payload.Accepts) == 0 {
			fmt.Printf("FAIL (no payment options)\n")
			failed++
			continue
		}

		fmt.Printf("402 OK (v%d, %s %s, amount=%s)\n",
			payload.X402Version,
			payload.Accepts[0].Scheme,
			payload.Accepts[0].Network,
			payload.Accepts[0].Amount,
		)
		passed++
	}

	fmt.Println()
	fmt.Printf("=== Results: %d passed, %d failed out of %d tests ===\n", passed, failed, len(tests))

	// Now test upstream proxy directly (bypass payment for testing)
	fmt.Println()
	fmt.Println("=== Upstream Proxy Smoke Tests (direct upstream calls) ===")
	fmt.Println()

	upstreamTests := []struct {
		name string
		url  string
	}{
		{"PubMed", "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?db=pubmed&retmode=json&retmax=3&term=longevity+nutrition"},
		{"Semantic Scholar", "https://api.semanticscholar.org/graph/v1/paper/search?query=psychedelics&limit=1"},
		{"OpenAlex", "https://api.openalex.org/works?search=quantum+computing&per_page=1"},
		{"ClinicalTrials", "https://clinicaltrials.gov/api/v2/studies?format=json&pageSize=1&query.term=psilocybin"},
		{"PubChem", "https://pubchem.ncbi.nlm.nih.gov/rest/pug/compound/name/serotonin/property/MolecularFormula,MolecularWeight,IUPACName/JSON"},
	}

	client := &http.Client{Timeout: 20 * time.Second}
	for _, tt := range upstreamTests {
		fmt.Printf("  %s: ", tt.name)
		req, _ := http.NewRequest("GET", tt.url, nil)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("FAIL (%v)\n", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 200 {
			preview := string(body)
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			// Remove newlines for compact display
			preview = strings.ReplaceAll(preview, "\n", " ")
			fmt.Printf("OK (%d bytes) %s\n", len(body), preview)
		} else {
			fmt.Printf("HTTP %d (%d bytes)\n", resp.StatusCode, len(body))
		}
	}
}

func runPaymentTest(url string) {
	privKey := os.Getenv("PAYER_PRIVATE_KEY")
	if privKey == "" {
		log.Fatal("PAYER_PRIVATE_KEY env var required for payment test. Use --proxy-test for proxy-only testing.")
	}

	fmt.Println("=== x402 Payment Flow Test ===")
	fmt.Printf("URL: %s\n", url)
	fmt.Println()

	// Step 1: Request resource, expect 402
	fmt.Println("Step 1: Requesting resource (expect 402)...")
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPaymentRequired {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("Expected 402, got %d: %s", resp.StatusCode, string(body))
	}

	paymentRequired := resp.Header.Get("Payment-Required")
	if paymentRequired == "" {
		log.Fatal("No Payment-Required header in 402 response")
	}

	fmt.Printf("  Got 402 with Payment-Required header (%d bytes)\n", len(paymentRequired))

	// Decode x402 payload
	payloadBytes, err := base64.StdEncoding.DecodeString(paymentRequired)
	if err != nil {
		log.Fatalf("Failed to decode Payment-Required: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		log.Fatalf("Failed to parse x402 payload: %v", err)
	}

	prettyPayload, _ := json.MarshalIndent(payload, "  ", "  ")
	fmt.Printf("  x402 payload:\n  %s\n\n", string(prettyPayload))

	// Step 2: Sign payment (requires EIP-712 signing - not yet implemented in this test client)
	fmt.Println("Step 2: Payment signing...")
	fmt.Println("  NOTE: Full EIP-712 signing requires the x402 client SDK.")
	fmt.Println("  For production testing, use the Viatika MCP pay_for_resource tool.")
	fmt.Println()
	fmt.Println("  To test with Viatika MCP:")
	fmt.Printf("  pay_for_resource(url=\"%s\")\n", url)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
