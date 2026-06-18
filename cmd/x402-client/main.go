// Package main implements a real x402 client that signs EIP-712 payments
// and sends them to the research gateway for verification via the actual
// x402 facilitator on Base Sepolia testnet.
//
// Usage:
//
//	PAYER_PRIVATE_KEY=0x... go run cmd/x402-client/main.go [route-url]
//
// Example:
//
//	PAYER_PRIVATE_KEY=0xdee19... go run cmd/x402-client/main.go \
//	  "http://localhost:8091/research/pubmed/search?term=longevity+nutrition"
package main

import (
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// Base Sepolia USDC contract
const baseSepoliaUSDC = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"

func main() {
	privKeyHex := os.Getenv("PAYER_PRIVATE_KEY")
	if privKeyHex == "" {
		log.Fatal("PAYER_PRIVATE_KEY env var required (hex, with or without 0x prefix)")
	}
	privKeyHex = strings.TrimPrefix(privKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		log.Fatalf("Invalid private key: %v", err)
	}
	payerAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	fmt.Printf("Payer address: %s\n\n", payerAddr.Hex())

	url := "http://localhost:8091/research/pubmed/search?term=longevity+nutrition"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// Step 1: Request resource, get 402
	fmt.Printf("[1] GET %s\n", url)
	resp, err := client.Get(url)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPaymentRequired {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("Expected 402, got %d: %s", resp.StatusCode, string(body))
	}

	// Extract x402 payload
	prHeader := resp.Header.Get("Payment-Required")
	if prHeader == "" {
		log.Fatal("No Payment-Required header")
	}

	payloadBytes, err := base64.StdEncoding.DecodeString(prHeader)
	if err != nil {
		log.Fatalf("Decode Payment-Required: %v", err)
	}

	var x402Payload struct {
		X402Version int `json:"x402Version"`
		Accepts     []struct {
			Scheme            string         `json:"scheme"`
			Network           string         `json:"network"`
			Asset             string         `json:"asset"`
			Amount            string         `json:"amount"`
			PayTo             string         `json:"payTo"`
			MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
			Extra             map[string]any `json:"extra"`
		} `json:"accepts"`
	}
	if err := json.Unmarshal(payloadBytes, &x402Payload); err != nil {
		log.Fatalf("Parse x402 payload: %v", err)
	}

	if len(x402Payload.Accepts) == 0 {
		log.Fatal("No payment options in 402 response")
	}

	opt := x402Payload.Accepts[0]
	fmt.Printf("    402 received: scheme=%s network=%s amount=%s payTo=%s\n\n", opt.Scheme, opt.Network, opt.Amount, opt.PayTo)

	// Step 2: Sign EIP-712 transferWithAuthorization
	fmt.Println("[2] Signing EIP-712 transferWithAuthorization...")

	amount := new(big.Int)
	amount.SetString(opt.Amount, 10)

	payTo := common.HexToAddress(opt.PayTo)
	validAfter := big.NewInt(0)
	validBefore := big.NewInt(time.Now().Add(5 * time.Minute).Unix())

	// Generate random nonce
	nonce := crypto.Keccak256Hash([]byte(fmt.Sprintf("%d-%s", time.Now().UnixNano(), payerAddr.Hex())))

	// Get chain ID from network name
	chainID := getChainID(opt.Network)

	// Build EIP-712 typed data for USDC transferWithAuthorization (EIP-3009)
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TransferWithAuthorization": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "validAfter", Type: "uint256"},
				{Name: "validBefore", Type: "uint256"},
				{Name: "nonce", Type: "bytes32"},
			},
		},
		PrimaryType: "TransferWithAuthorization",
		Domain: apitypes.TypedDataDomain{
			Name:              "USDC",
			Version:           "2",
			ChainId:           math.NewHexOrDecimal256(chainID.Int64()),
			VerifyingContract: baseSepoliaUSDC,
		},
		Message: apitypes.TypedDataMessage{
			"from":        payerAddr.Hex(),
			"to":          payTo.Hex(),
			"value":       amount.String(),
			"validAfter":  validAfter.String(),
			"validBefore": validBefore.String(),
			"nonce":       fmt.Sprintf("0x%x", nonce),
		},
	}

	// Hash and sign
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		log.Fatalf("Hash domain: %v", err)
	}

	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		log.Fatalf("Hash message: %v", err)
	}

	rawData := fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(typedDataHash))
	hash := crypto.Keccak256Hash([]byte(rawData))

	sig, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		log.Fatalf("Sign: %v", err)
	}

	// Adjust v value (27/28 for Ethereum) and build canonical signature
	if sig[64] < 27 {
		sig[64] += 27
	}
	// Build canonical signature hex: R (32 bytes) + S (32 bytes) + V (1 byte)
	sigHex := fmt.Sprintf("0x%x", sig)

	fmt.Printf("    Signature: %s...%s\n\n", sigHex[:14], sigHex[len(sigHex)-8:])

	// Step 3: Build x402 v2 payment header
	// Format matches Viatika's buildPaymentHeader in remotemcp/tools.go:
	// - signature is a single hex string (not {v,r,s} object)
	// - nonce is hex-encoded bytes32
	fmt.Println("[3] Building x402 v2 payment header...")

	payment := map[string]any{
		"x402Version": 2,
		"scheme":      opt.Scheme,
		"network":     opt.Network,
		"payload": map[string]any{
			"signature": sigHex,
			"authorization": map[string]any{
				"from":        payerAddr.Hex(),
				"to":          payTo.Hex(),
				"value":       amount.String(),
				"validAfter":  validAfter.String(),
				"validBefore": validBefore.String(),
				"nonce":       fmt.Sprintf("0x%x", nonce),
			},
		},
		"accepted": opt,
	}

	paymentJSON, err := json.Marshal(payment)
	if err != nil {
		log.Fatalf("Marshal payment: %v", err)
	}
	paymentHeader := base64.StdEncoding.EncodeToString(paymentJSON)

	fmt.Printf("    Payment header: %d chars\n\n", len(paymentHeader))

	// Step 4: Retry with payment
	fmt.Printf("[4] Retrying with PAYMENT-SIGNATURE header...\n")

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Create request: %v", err)
	}
	req.Header.Set("PAYMENT-SIGNATURE", paymentHeader)

	resp2, err := client.Do(req)
	if err != nil {
		log.Fatalf("Retry request failed: %v", err)
	}
	defer resp2.Body.Close()

	body, _ := io.ReadAll(resp2.Body)

	fmt.Printf("    HTTP %d (%d bytes)\n\n", resp2.StatusCode, len(body))

	if resp2.StatusCode == 200 {
		fmt.Println("[5] SUCCESS - Research data received:")
		// Pretty print if JSON
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			pretty, _ := json.MarshalIndent(parsed, "    ", "  ")
			// Show first 1500 chars
			output := string(pretty)
			if len(output) > 1500 {
				output = output[:1500] + "\n    ..."
			}
			fmt.Printf("    %s\n", output)
		} else {
			if len(body) > 1500 {
				fmt.Printf("    %s...\n", string(body[:1500]))
			} else {
				fmt.Printf("    %s\n", string(body))
			}
		}
	} else {
		fmt.Printf("[5] Payment verification result (HTTP %d):\n", resp2.StatusCode)
		fmt.Printf("    %s\n", string(body))
		fmt.Println()
		fmt.Println("    Note: If verification failed, the EIP-712 signature was rejected.")
		fmt.Println("    This is expected if the wallet has no USDC — the facilitator may")
		fmt.Println("    check allowance/balance during verify on some networks.")
	}
}

func getChainID(network string) *big.Int {
	switch network {
	case "base-sepolia", "eip155:84532":
		return big.NewInt(84532)
	case "base", "eip155:8453":
		return big.NewInt(8453)
	default:
		return big.NewInt(84532)
	}
}

func getKey(key *ecdsa.PrivateKey) *ecdsa.PrivateKey {
	return key
}
