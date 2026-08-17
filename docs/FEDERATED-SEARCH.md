# Federated search

Implements x402-research-gateway#4. Code lives in
[`internal/federate`](../internal/federate) and
[`internal/handler/federated.go`](../internal/handler/federated.go).

One request fans out to every provider declaring the requested capability
and returns a merged result set. Every existing single-provider route keeps
working exactly as it did; federation is an operation alongside them.

## Provenance survives the merge

Every result carries the provider that produced it, that provider's rank,
that provider's score, and that provider's raw record. A merged result is
never a new anonymous record.

Filtering on `provider` and sorting by `provider_rank` reconstructs that
provider's own list exactly. A provider that reported no score carries no score
at all. A default score would be an invention.

## Fused order is labeled

When results are merged the response carries a `fusion` block naming the
method (`reciprocal_rank_fusion`), its constant, and a note stating the limit
of what the order means. `fused_rank` sits beside `provider_rank` on every
result, never in place of it.

Fusion here is ranking mechanics over positions in provider lists. It makes
no claim about cross-disciplinary relevance and encodes no model of meaning.
An agent that distrusts the fusion ignores `fused_rank` and loses nothing.

An empty result set carries no `fusion` block, because nothing was fused.

## Partial failure

`providers` lists every provider considered, answered or not, with an
outcome:

| Outcome | Meaning |
|---|---|
| `ok` with `result_count: 0` | asked, answered, found nothing |
| `unsupported_capability` | not asked; does not declare this capability |
| `cost_cap_exceeded` | not asked; the caller's cap could not afford it |
| `timeout` / `upstream_error` / `upstream_status` | asked, did not answer |

A response never silently omits a provider. A missing provider would read as
a negative result, and it is not one.

## Timeout isolation

Each provider runs under its own deadline: the route's `timeoutSeconds` from
`config/routes.yaml` when set, otherwise `feed402.federated.timeoutSeconds`.
One slow upstream cannot extend the whole request past its own deadline, and
its failure is recorded rather than rendered as an empty result. Concurrency
is bounded by `maxConcurrency`, default 4.

## Duplicate candidates

Results from different providers sharing an exact identifier are surfaced in
`duplicate_candidates` with the identifier scheme that matched and
`identity.Evidence` attached. Nothing is collapsed: two records stay two
records at their original positions, and the caller decides.

Only exact identifier agreement produces a candidate here. Similarity-based
resolution belongs to `/research/resolve` (#5), which carries the descriptor
metadata that judgment needs. Results from a single provider are never
paired with each other.

## Cost before payment

`GET /research/federated` is free, makes no upstream call, and returns the
estimate: a per-provider price line, the total, and whether it fits the cap.

`POST /research/federated` runs the paid fan-out. Both accept
`max_cost_usd`. A cap admits providers in ascending price order, so it buys
the most coverage it can afford, and every provider it excludes appears in
the response with `cost_cap_exceeded`.

## Capability routing

`capability` defaults to `search`. Provider selection reads the adapter
registry's declared capabilities, so asking for `cited_by` never reaches a
provider that cannot answer it. A capability nothing implements selects
nobody and prices at zero rather than erroring.

## Request

```json
{"query": "photosynthesis", "capability": "search", "max_cost_usd": 0.003}
```

`query`, `capability`, and `max_cost_usd` are also accepted as query
parameters, with `q` and `term` as aliases for `query`.

## Compatibility

Additive. `/research/federated` is off unless `feed402.federated.enabled`.
No existing route, envelope field, or manifest field changed shape, and the
declarative config-driven proxy path is untouched.
