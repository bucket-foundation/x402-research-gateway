# Deprecations: feed402/0.2 -> feed402/0.3

This gateway upgraded its wire protocol from `feed402/0.2` to the canonical
`feed402/0.3` (x402-research-gateway#1, depends on
[feed402#1](https://github.com/bucket-foundation/feed402/issues/1)). This
document records the break, the field-by-field migration, the deprecation
window, and the sunset — the artifact x402-research-gateway#1's acceptance
criteria calls for.

## The break

`feed402/0.2` shipped `Envelope.citation` as a single object. `feed402/0.3`
makes it an array (feed402 SPEC.md §3, §7.1). This is the only breaking
change in the protocol revision, and the only breaking change in this
gateway's migration.

A 0.2 client reading `envelope.citation.source_id` will get `undefined`
against a 0.3 response, because `citation` is now `citation[0].source_id`.

## Deprecated fields and their sunset

Every field below is retained during the deprecation window. All sunset at
**`feed402/0.5`** — a conformant client stops reading them and a conformant
merchant (this gateway) stops emitting them at that point. Until then they
are duplicates of authoritative data, never the only copy.

| Field | Where (0.3) | Replacement | Mapping | Sunset |
|---|---|---|---|---|
| `citation` as a singular object | envelope | `citation[]` | `citation` → `citation[0]` | `feed402/0.5` |
| `citation_legacy` | envelope | `citation[0]` | identity (this gateway emits it as an advisory copy) | `feed402/0.5` |
| `hits[]` | `data` (moved from the top-level `envelope.hits` field in 0.2) | `citation[]` | see below | `feed402/0.5` |
| `routes[]` | manifest | `operations[]` | §1.3 heuristic; declarative-only routes without an adapter fall back to a name-based search/fetch guess | `feed402/0.5` |
| `tier_routes{}` | manifest | `operations[]` grouped by `tier` | same as `routes[]` | `feed402/0.5` |

### `hits[]` → `citation[]` mapping

Per feed402 SPEC §7.2's mapping table:

- `hits[i].source_id` → `citation[i].source_id`
- `hits[i].canonical_url` → `citation[i].canonical_url`
- `hits[i].rank` → preserved verbatim in the retained `data.hits[i].rank`
  alias (not folded into a `retrieval` object, since the gateway has no
  `model`/`score` to pair with it — SPEC §3.2's `retrieval` block requires
  all three fields together, and inventing `model`/`score` would be
  publishing a determination the gateway never made)
- `hits[i]` position → `citation[i+1].result_index` (see shape below)

### The citation array's shape for search-tier responses

A search-tier response whose route has an adapter-derived hit list (see
x402-research-gateway#2's `internal/provider` registry) now emits:

```jsonc
{
  "data": {
    "esearchresult": { /* ...unchanged upstream body... */ },
    "rows": [ /* same content as hits[], added so SPEC §3.3's resultList()
                 recognizes a multi-record response */ ],
    "hits": [ /* deprecated alias, same shape as pre-0.3 envelope.hits */ ]
  },
  "citation": [
    { "source_id": "pubmed:query:<hash>", "...": "...", "result_index": [0, 1, 2] },
    { "source_id": "pubmed:38831607",     "...": "...", "result_index": [0] },
    { "source_id": "pubmed:34588695",     "...": "...", "result_index": [1] },
    { "source_id": "pubmed:11111111",     "...": "...", "result_index": [2] }
  ],
  "citation_legacy": { "source_id": "pubmed:query:<hash>", "...": "..." },
  "receipt": { "...": "..." }
}
```

The provider-level query citation (`<prefix>:query:<hash>`, unchanged
construction from 0.2's `buildCitationFor()`) stays first in the array and
grounds every result via an explicit `result_index` spanning the whole
result set. One citation per hit follows, each grounding only its own
result. This is SPEC §3.3 rule 3 (explicit binding: once any citation
carries `result_index`, every citation in the array must), applied so both
"the query as a whole produced this" and "this specific record is at this
specific position" are simultaneously true and simultaneously provable —
neither statement is dropped in favor of the other.

`data.rows` exists purely so SPEC §3.3's `resultList()` recognizes this as a
multi-record response; its content is identical to `data.hits`. A future
revision may collapse the two once `hits` sunsets at `feed402/0.5`.

**Single-record and insight responses are unaffected in shape** beyond the
array-wrapping: they carry exactly one citation, per SPEC §3.3 rule 1. The
insight tier's `data.hits` (also newly relocated from the top-level
`envelope.hits`) still carries the full retrieval hit list for agents that
want it, but the citation array stays one element — the top hit, or a
synthetic fallback — because an insight response is a single synthesized
answer, not an enumerable result list. Expanding insight's citation array to
one-per-source-snippet is a documented non-goal of this migration; a future
issue can revisit it if agents show demand.

## What did not change

- `source_id` construction per source prefix (`buildCitationFor()`).
- `canonical_url` templating from `RouteCitation.CanonicalURLTemplate` and
  its passthrough substitution.
- Per-route `license` with fallback to `citationPolicy` via `licenseFor()`.
- `retrieved_at` construction.
- `receipt.tx` from `settleWithTimeout()`, including the `pending:`
  placeholder path for async settlement.
- The four hit-deriving adapters' underlying facts (source_id, canonical
  URL, rank) — every one survives, just re-shaped per the mapping above.
- The declarative config-driven route path (`config/routes.yaml`,
  `proxyToUpstream`) — untouched.

## Manifest: `spec` version

`config/routes.yaml` and `config/routes.hetzner.yaml` now declare
`spec: "feed402/0.3"`; `internal/config/config.go`'s default (when `spec` is
unset) moved from `feed402/0.2` to `feed402/0.3` to match.

## Manifest: `operations[]`

The manifest gains `operations[]` (feed402 SPEC §1.2) and a manifest-level
`capabilities[]` summary (SPEC §1.1), built from the same
`internal/provider.Registry` x402-research-gateway#2 introduced: a route
backed by a `Searcher` reports `capability: "search"` plus its
`pagination_model`; a route backed by a `Fetcher` reports `capability:
"fetch"` plus `identifier_schemes`; a declarative-only route with no adapter
falls back to a name-based search/fetch heuristic (mirrors feed402
`types.ts`'s `inferCapabilityFromRoute()`). `routes[]`/`tier_routes{}` stay
populated, unchanged, for the deprecation window.

## Testing

`internal/handler/feed402_test.go` covers, for both shapes during the
window:

- `citation` is always an array, length >= 1.
- `citation_legacy` equals `citation[0]` byte-for-byte.
- The explicit `result_index` binding rule: every citation in a multi-entry
  array carries it, and every result index is covered by at least one
  citation.
- `data.rows` / `data.hits` carry identical content, in the pre-0.3 `hits[]`
  field shape.
- A raw-tier (single-record) response carries exactly one citation and no
  `data.rows`/`data.hits`.
- The insight tier's citation array has exactly one element, with
  `citation_legacy` mirroring it.
- The manifest's `operations[]` and manifest-level `capabilities[]`.
