# x402-research-gateway

**Paid research endpoints over x402, on Base.**

A small Go gateway that exposes scientific/biomedical research APIs as
metered, pay-per-call endpoints using the [x402](https://x402.org) payment
protocol. An AI agent (or any client) with a Base wallet can discover, pay
for, and query research data with a single HTTP request.

It is also a reference [feed402](https://github.com/bucket-foundation/feed402)
merchant speaking canonical **feed402/0.3**: when enabled, it serves
`/.well-known/feed402.json` and wraps every paid response in the feed402
citation envelope (data + citation array + receipt), including a feed402
`insight` tier. See [`DEPRECATIONS.md`](DEPRECATIONS.md) for the 0.2 → 0.3
field-by-field migration and deprecation window.

## Endpoints

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
| `/research/resolve` | Cross-provider scholarly identity resolution | query |
| `/research/citations` | Citation graph across four providers | query |
| `/research/federated` | Federated search, POST paid and GET free estimate | query |
| `/research/openalex/references` | OpenAlex outbound citations | query |
| `/research/openalex/cited-by` | OpenAlex inbound citations | query |
| `/research/semantic-scholar/references` | Semantic Scholar outbound citations | query |
| `/research/semantic-scholar/cited-by` | Semantic Scholar inbound citations | query |
| `/research/opencitations/references` | OpenCitations outbound references | query |
| `/research/opencitations/cited-by` | OpenCitations inbound citations | query |
| `/research/crossref/references` | Crossref deposited reference list | query |
| `/research/crossref/search` | Crossref DOI metadata search | query |
| `/research/crossref/fetch` | Crossref work record by DOI | raw |
| `/research/assets` | Rights-aware asset discovery across open-access sources | query |
| `/research/core/search` | CORE open-access aggregator search | query |
| `/research/core/fetch` | CORE work record | raw |

Routes, prices, and citation policies are declared in
[`config/routes.yaml`](config/routes.yaml).

## Architecture: declarative routes + adapters

Provider semantics are separated from HTTP routing, x402 payment, and
feed402 enveloping behind small composable Go interfaces in
[`internal/provider`](internal/provider) (`Searcher`, `Fetcher`,
`Normalizer`, `CitationProvider`, `AssetProvider`, `VocabularyProvider`,
`SyncProvider`, `IdentityProvider`, `DescriptorProvider`,
`ObjectRelationProvider`, `AvailabilityReporter`, `RecordRightsProvider`,
`CitationGraphProvider`). A route needs no adapter at all: `config/routes.yaml`'s
declarative fields (`baseUrl`, `pathTemplate`, `queryParams`, `passThrough`)
remain the cheapest way to add a simple REST upstream, proxied unchanged by
[`internal/handler/proxy.go`](internal/handler/proxy.go).

An adapter is what a provider graduates to when it needs normalized
per-record citations for a search-tier `hits` array, or when it will need a
capability declarative config cannot express (a non-JSON body, a multi-step
call, pagination semantics). [`internal/provider/registry.go`](internal/provider/registry.go)
maps a route ID to its adapter; a route with no entry is served purely by
the declarative path. `Adapter.Capabilities()` reports what a provider
implements by presence, never by guessing, so an unimplemented capability is
always a clean "not supported" rather than a silent gap — the gateway's
`/.well-known/feed402.json` manifest surfaces this per route as an additive
`capabilities` array.

**Adding a new adapter** is one new file in `internal/provider/` — a
`Normalizer` (upstream body → normalized records) and a `CitationProvider`
(usually `GenericCitationProvider{}`, which handles the common
prefix-plus-id case) — plus one entry in `DefaultRegistry()`. No other file
changes.

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
