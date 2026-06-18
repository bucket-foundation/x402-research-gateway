# Deploy: x402-research.agfarms.dev

One-page runbook for hosting this gateway on the Hetzner CPX42 box
(`5.161.236.151`, also reachable as `prod-hetzner-1` inside AGFarms infra).

**Target URL:** `https://x402-research.agfarms.dev`
**Health check:** `curl https://x402-research.agfarms.dev/.well-known/feed402.json`

## Status (2026-04-23)

- DNS: `x402-research.agfarms.dev` → `5.161.236.151` ✅ (already resolves, same
  A record as `research.agfarms.dev` — both Cloudflare-managed).
- SSH from founder's laptop: **blocked** (public-key + password auth both
  refused for `root@5.161.236.151`). Deploy must run from a host whose key
  is in `/root/.ssh/authorized_keys` on the box, or the founder runs it by
  hand via `ssh root@5.161.236.151` in an interactive shell.

Everything below is copy-paste once SSH is restored.

## 1. Cloudflare DNS (verify)

```bash
dig +short x402-research.agfarms.dev
# expect: 5.161.236.151
```

If no record exists, add one in Cloudflare dashboard for zone `agfarms.dev`:
- Type `A`, name `x402-research`, content `5.161.236.151`, proxy OFF
  (Caddy terminates TLS directly — Cloudflare proxy breaks the ACME
  HTTP-01 challenge the first time).

## 2. Update Caddyfile in this repo

Edit `Caddyfile` in this repo so the gateway answers on the new hostname:

```caddyfile
x402-research.agfarms.dev {
    reverse_proxy gateway:8091
}
```

(Also acceptable: keep `research.agfarms.dev` and add a second block.)

## 3. Wallet env (on the server, never in git)

```bash
# on Hetzner box
mkdir -p /opt/x402-research/gateway
cat > /opt/x402-research/gateway/.env <<'EOF'
RECIPIENT_ADDRESS=0x4daF1378F862A58fe2C4C534d4d105A29D2B29Ff
NETWORK=base-sepolia
FACILITATOR_URL=https://facilitator.x402.rs
EOF
chmod 600 /opt/x402-research/gateway/.env
```

Same wallet is mirrored at `~/.bucket-wallet.env` on the founder's box (chmod
600). Private key is NOT required on the gateway side — gateway only receives.

**Funding:** wallet is UNFUNDED as of 2026-04-23. To fund ~$5 USDC on Base
Sepolia:
- https://www.coinbase.com/faucets/base-ethereum-sepolia-faucet (ETH, then swap)
- https://faucet.circle.com/ (USDC direct — recommended)

Until funded, clients will see 402 challenges that resolve to empty receipts.
The server itself runs fine without funding because the gateway is the
*receiver*, not the payer — clients fund their own wallets and settle to
this address.

## 4. One-shot deploy (founder runs this)

```bash
# from founder's laptop, once SSH is working:
cd ~/agfarms/x402-research-gateway
./deploy.sh 5.161.236.151
```

`deploy.sh` already exists in this repo — it rsyncs the source, rsyncs the
Kruse corpus at `/home/gian/jackkruse/`, copies `.env`, and runs
`docker compose -f docker-compose.prod.yml up -d --build`.

## 5. Verify

```bash
# DNS + TLS
curl -vI https://x402-research.agfarms.dev/.well-known/feed402.json 2>&1 | head -20
openssl s_client -connect x402-research.agfarms.dev:443 -servername x402-research.agfarms.dev </dev/null 2>/dev/null | openssl x509 -noout -subject -issuer -dates

# Manifest (expect spec: "feed402/0.2")
curl -s https://x402-research.agfarms.dev/.well-known/feed402.json | python3 -m json.tool

# 402 challenge on the paid rail
curl -i https://x402-research.agfarms.dev/pubmed/query?q=caloric+restriction
# expect: HTTP/2 402 + x-payment-required header
```

## 6. Route list (expected after deploy)

Six endpoints × three tiers (from `config/routes.yaml`):
`pubmed`, `semantic-scholar`, `openalex`, `clinicaltrials`, `pubchem`, `kruse`.

## Blockers

- **SSH access**: founder must run `ssh-copy-id root@5.161.236.151` from the
  laptop that will deploy, OR run the deploy interactively from a shell
  that already has root access (the K3s control plane box).
- **Wallet funding**: needs a manual CAPTCHA-gated faucet drip. ≤ $5.
