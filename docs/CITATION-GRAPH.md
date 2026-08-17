# Citation graph

Implements x402-research-gateway#6. Code lives in
[`internal/citation`](../internal/citation), the provider seam in
`internal/provider/citation_*.go`, and the paid operation in
[`internal/handler/citations.go`](../internal/handler/citations.go).

## Absence is not evidence

A provider that returns no edges for a work has no edges for that work.
The citation does not therefore fail to exist. Coverage varies by
discipline, by era, and by whether the publisher deposits references
openly. Crossref carries a reference list only where the publisher
deposited one. OpenCitations covers what it has ingested. OpenAlex and
Semantic Scholar each cover a different slice.

So every response carries `providers_consulted`, one entry per provider
whether it answered or not, and restates the rule in `absence_notice`.
Four outcomes stay distinct:

| Outcome | Meaning |
|---|---|
| `ok` with `edge_count: 0` | asked, answered, holds no edges for this work |
| `unsupported_identifier` | not asked; cannot express a query for this scheme |
| `unsupported_direction` | not asked; does not serve this traversal at all |
| `timeout` / `upstream_error` / `upstream_status` | asked, did not answer |

Reading `ok`/0 as "uncited" is wrong. Reading `unsupported_identifier` as
"uncited" is worse, since nobody was asked.

## The edge model

An `Edge` carries the asserting provider, the direction it was retrieved
under, both endpoints, the provider's own edge status, its edge id when it
has one, and `retrieved_at`. Source always cites Target, whichever direction
the query ran.

An `Endpoint` holds every identifier form the provider expressed, each
retaining its raw string in `Identifier.Raw`, plus `raw_id` for the exact
string the provider sent. An edge asserted in DOIs stays reversible to those
DOIs. An unstructured Crossref reference with no deposited DOI keeps its
text in `raw_id` and carries no identifier, which keeps it visible without
inventing a match.

`status` is the provider's annotation, `retracted`, `corrected`, or
`superseded`. Empty means the provider said nothing, which differs from the
provider saying the edge is current.

## Provider disagreement

Nothing is fused. Two providers asserting the same citation produce two
edges, each attributed. The recognition that they describe one citation is a
separate `Equivalence` entry listing the edge ids and the identifier schemes
that agreed on both ends, with `identity.Evidence` attached.

Equivalence is exact-identifier only. There is no similarity path into one,
so acting on an equivalence means acting on identifiers the providers
themselves published. Edges from a single provider are never equated with
each other; a provider asserting two edges is asserting two edges. Where one
provider has an edge and another does not, both facts stay visible and
neither provider is declared correct.

## The operation

`POST /research/citations` with
`{"identifier": "10.7717/peerj.4375", "direction": "references"}`.
`direction` defaults to `references`; `cited_by` is the other value.

The identifier must match a known scheme. One that matches none is refused
rather than sent upstream as a bare string, because a bare string produces a
plausible-looking wrong answer at every provider.

Fan-out is bounded by `maxConcurrency` (default 4) with a per-provider
`timeoutSeconds` (default 10). The response is a feed402 §3 envelope whose
`data` is the `citation.Result`, with one citation per provider that
contributed edges, bound by `result_index` to those edges.

## Providers

Verified against primary documentation on 2026-08-17.

| Provider | Direction | Accepts | Pagination | Notes |
|---|---|---|---|---|
| `openalex-references` | references | OpenAlex work id | cursor | `filter=cited_by:W…` |
| `openalex-cited-by` | cited_by | OpenAlex work id | cursor | `filter=cites:W…` |
| `semantic-scholar-references` | references | S2 id, DOI, PMID, PMCID, arXiv | offset | `/graph/v1/paper/{id}/references` |
| `semantic-scholar-cited-by` | cited_by | S2 id, DOI, PMID, PMCID, arXiv | offset | `/graph/v1/paper/{id}/citations` |
| `opencitations-references` | references | DOI, PMID | none | `/index/v2/references/{id}` |
| `opencitations-cited-by` | cited_by | DOI, PMID | none | `/index/v2/citations/{id}` |
| `crossref-references` | references | DOI | none | `/works/{doi}` → `message.reference` |

The OpenAlex filter names invert the traversal they perform:
`filter=cited_by:W123` returns the works in W123's `referenced_works`, and
`filter=cites:W123` returns the works citing W123. The mapping is asserted
in `TestOpenAlexCitationGraph_FilterMapping`, because getting it backwards
silently inverts the graph.

Crossref has no cited-by endpoint, so no cited-by adapter exists for it and
a cited-by query reports `unsupported_direction`.

Every route stays independently addressable, so single-provider access is
available without going through the fan-out.

## Credentials

No provider here requires a key. OpenCitations accepts an optional access
token in the `Authorization` header and rate-limits to 180 requests per
minute per IP without one; supply it through the route's headers. Crossref
uses a `mailto` query parameter for the polite pool. Semantic Scholar
accepts an optional key that raises rate limits.

Provider failure records carry the route id, an outcome, and the upstream
status. They carry no URL and no transport error text, because for a
token-bearing provider the upstream URL carries the token. Covered by
`TestCitations_NoCredentialLeakage`.

## Non-goals

No citation-sentiment classification, no influence scoring, no graph
analytics. Edge retrieval only.

## Compatibility

Additive. `/research/citations` is off unless `feed402.citations.enabled`.
Seven new routes were added and no existing route, envelope field, or
manifest field changed shape. `crossref` and `opencitations` moved from
`researched` to `implemented` in `config/providers.yaml`, which is a
lifecycle advance rather than a rewrite; the OpenCitations `base_url` was
corrected from the retired COCI v1 path to Index v2.
