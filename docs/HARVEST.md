# Resumable pagination and harvest provenance

Implements x402-research-gateway#10. Code lives in
[`internal/harvest`](../internal/harvest), the paginators in
[`internal/provider/paginators.go`](../internal/provider/paginators.go), and
the paid operation in
[`internal/handler/harvest.go`](../internal/handler/harvest.go).

## The problem it removes

An agent walking a large result set that failed on page four hundred had to
restart and pay again for every page it already held. The gateway proxied
four pagination models as opaque passthrough parameters and kept no state
about any of them.

## How a harvest runs

```
POST /research/harvest  {"route": "pubmed-search", "query": "mitochondria", "page_size": 100}
  -> {"records": [...], "cursor": "<token>", "cursor_state": {...}}

POST /research/harvest  {"cursor": "<token>"}
  -> the next page, from the exact position the last one ended at
```

The gateway runs no harvest and stores no harvest state. It hands the client
what it needs to run its own. Every page is its own paid call.

## Cursor state

`cursor_state` is the readable form of the token: `request_fingerprint`,
`query_fingerprint`, `provider`, `pagination_model`, `provider_cursor`,
`next_cursor`, `exhausted`, `result_count`, `page_result_count`,
`upstream_request_id`, `response_sha256`, `rate_limit_remaining`,
`retry_after`, `retrieved_at`, `provider_release`, `started_at`,
`started_release`, `release_changed`.

The token itself is `base64url(payload).base64url(hmac_tag)`. A client reads
its own position; only the gateway can produce a tag. A rewritten payload
fails verification and is refused, and refusing costs the client nothing it
had: each page is paid for on its own, so a forged cursor buys no page.

`cursor_ephemeral` reports whether cursors survive a gateway restart. They
do when the operator sets `HARVEST_CURSOR_SECRET`; without it the key is
random per process and the response says so rather than letting a client
discover it hours into a harvest.

## Fingerprints

`query_fingerprint` and `request_fingerprint` are the feed402 SPEC §3.6
constructions, not a second spelling of them: keyed HMAC-SHA256 over the
normalized query and over the upstream request. §3.6 forbids an unsalted
digest of a query, which is reversible by dictionary.

`request_fingerprint` is computed **after** the credential exclusion, which
is the whole point: hashing the raw outgoing request would produce a digest
a determined holder could brute-force back to a live key. `SanitizeURL`
strips userinfo and every parameter in `CredentialParams`, which covers API
keys, tokens, signatures, session identifiers, and the polite-pool
`email` / `mailto` / `tool` values `config/routes.yaml` carries.

The upstream URL itself never travels in a cursor.
`TestCursorState_NoCredentialLeakage` and
`TestHarvestCursor_NoCredentialLeakage` assert this over emitted cursor
state rather than leaving it to review.

## Release boundaries

`provider_release` travels in the cursor. A page fetched against a different
release than the harvest started on sets `release_changed` and the response
carries `release_notice`: the pages you hold were not consistent at any
single point in time. The pages are still returned. What is refused is
presenting them as a snapshot.

A provider that publishes no release identifier reports none, and a harvest
across it cannot detect a boundary. That is stated rather than papered over.

## Pagination models

| Route | Model | Provider mechanism |
|---|---|---|
| `pubmed-search` | offset | `retstart` / `retmax`, stopping at the count ESearch reports |
| `openalex-works` | page | `page` / `per_page`, stopping on the `meta` block's own count |
| `semantic-scholar-search` | offset | `offset` / `limit`, stopping when the API omits `next` |
| `clinicaltrials-search` | token | `pageToken`, stopping when no `nextPageToken` comes back |
| `epmc-search` | cursor | `cursorMark`, stopping when the mark repeats |

No paginator invents a next page. A response with no next-page handle ends
the set and reports `exhausted`. For Semantic Scholar that means the end of
what the provider will serve, which is not the end of the literature; the
adapter documents which.
