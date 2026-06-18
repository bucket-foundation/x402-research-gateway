# x402-research-gateway

**Paid research endpoints over x402, on Base.**

A small Go gateway that exposes scientific/biomedical research APIs as
metered, pay-per-call endpoints using the [x402](https://x402.org) payment
protocol. An AI agent (or any client) with a Base wallet can discover, pay
for, and query research data with a single HTTP request.

It is also a reference [feed402](https://github.com/bucket-foundation/feed402)
merchant: when enabled, it serves `/.well-known/feed402.json` and wraps every
paid response in the feed402 citation envelope (data + citation + receipt),
including a feed402 `insight` tier.

## Endpoints (7 + insight)

| Path | Source | Tier |
|------|--------|------|
| `/research/pubmed/search` | PubMed biomedical literature (36M+ citations) | query |
| `/research/pubmed/fetch` | PubMed article abstracts by PMID | raw |
| `/research/semantic-scholar/search` | Semantic Scholar cross-disciplinary corpus | query |
| `/research/openalex/works` | OpenAlex scholarly works graph | query |
| `/research/clinicaltrials/search` | ClinicalTrials.gov registry | query |
| `/research/pubchem/compound` | PubChem chemical compound data | raw |
| `/research/kruse/search` | Jack Kruse corpus (biophysics) | query |
| `/research/insight` | LLM-summarized insight over PubMed retrieval | insight |

Routes, prices, and citation policies are declared in
[`config/routes.yaml`](config/routes.yaml).

## Quick start

```bash
cp .env.example .env        # set RECIPIENT_ADDRESS (your Base payout address)
make run                    # build + run on :8091
make smoke                  # health check
```

Payments settle to the `RECIPIENT_ADDRESS` you configure. The default network
is `base-sepolia` (testnet); set `NETWORK=base` for mainnet. No private keys
live in this repo — the gateway only ever needs a public payout address.

## Deploy

See [`DEPLOY.md`](DEPLOY.md). Docker Compose files are provided for local and
Hetzner deployment.

## License & attribution

MIT (code) — see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

**Author:** Gianangelo Dichio. Developed with Viatika as hourly sponsor; no
exclusive rights are granted to any party.
