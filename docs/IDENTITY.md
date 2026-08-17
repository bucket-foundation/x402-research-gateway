# Scholarly identity resolution

Implements x402-research-gateway#5. Code lives in
[`internal/identity`](../internal/identity), the provider seam in
[`internal/provider/identity_adapters.go`](../internal/provider/identity_adapters.go),
and the paid operation in
[`internal/handler/resolve.go`](../internal/handler/resolve.go).

## The premise

A DOI does not equal a paper. A DOI identifies a registered record. One work
can carry several: one for the preprint, one per version, one at the
publisher. A preprint and its published article are related without being
the same object.

So the gateway emits a graph. Every provider record stays its own
independently addressable node with its raw upstream bytes attached, and the
answer is the typed, evidenced relations between those nodes. There is no
merged canonical entity, and the gateway never picks a winner when providers
disagree. Disagreement is data.

## Identifier schemes

`identity.Scheme` covers `doi`, `pmid`, `pmcid`, `arxiv`, `openalex`,
`semantic_scholar`, `dblp`, `zbmath`, `orcid`, `ror`. The set is extended at
runtime with `identity.RegisterScheme`, and the per-provider
`identifier_schemes` field in `internal/registry` drives which schemes a
provider contributes.

Normalization is additive. `Identifier.Raw` holds the exact string the
provider sent; `Identifier.Value` holds the matching form. An identifier a
scheme rejects keeps its raw string and drops out of matching, never out of
the record.

arXiv versions are split: `Value` holds the version-stripped base and
`Version` holds the suffix, so `2101.00001v1` and `2101.00001v3` relate as
versions rather than reading as two unrelated papers.

## Relations

`same_work`, `version_of`, `preprint_of`, `published_as`, `corrects`,
`retracts`, `withdraws`, `supplement_to`, `translation_of`,
`possible_same_work`.

`possible_same_work` is the one that matters. Title-and-author similarity is
evidence. It gets its own type so a consumer decides whether to act on it,
and `Graph.Add` rewrites any attempt to record similarity-backed
`same_work` down to `possible_same_work`. There is no confidence threshold
at which promotion happens.

## Evidence

Every relation carries `Evidence`, with `kind` either `provider_asserted` or
`gateway_inferred`, plus `retrieved_at`.

| Kind | Carries | Example |
|---|---|---|
| `provider_asserted` | `provider` | Crossref publishing a `relation` block |
| `gateway_inferred` | `method`, `detail`, `score` | two records sharing an exact DOI |

`Evidence.Valid()` rejects an entry carrying both a provider and a method,
so the two classes of fact cannot blur.

Inference methods this revision emits: `shared_exact_identifier` (detail
names the matching scheme), `arxiv_version_suffix`, and
`title_author_similarity` (the only one that carries a score).

Similarity is deliberately conservative. A record with no title scores zero.
A publication-year gap over one year halves the score. Author evidence
absent on either side discounts the title score. The default threshold is
0.82, configurable via `feed402.resolve.similarityThreshold`.

## The operation

`POST /research/resolve` with `{"identifier": "10.7717/peerj.4375"}`.

The gateway fans out to every route whose adapter implements
`provider.IdentityProvider`, bounded by `maxConcurrency` (default 4) with a
per-provider `timeoutSeconds` (default 10). Response is a feed402 §3
envelope:

- `data.identifier`: the query identifier, its sniffed scheme, its
  normalized value, and whether any scheme claimed it.
- `data.graph`: `nodes`, `relations`, `providers`.
- `data.providers_attempted`: every route that was asked.
- `data.providers_failed`: every route that was asked and did not answer,
  with a coarse reason (`upstream_error`, `upstream_status`, `timeout`,
  `route_not_configured`) and the upstream status when there was one. This
  array is always present, empty included.
- `citation`: one entry per contributing provider, each bound by
  `result_index` to the nodes it supplied.

A provider that times out is reported as a timeout. It never renders as a
provider that returned zero results.

Failure records carry no upstream URL and no transport error text, because
for a key-bearing provider the upstream URL carries the key. Detail goes to
the gateway log; the response body carries the route id and the reason.

## Manifest

The resolve route advertises capability `identity_resolution` with every
normalizable scheme in `identifier_schemes`, so an agent can tell before
paying whether its identifier is one this gateway handles. The capability
name is an extension under feed402 SPEC §1.1's open-vocabulary rule: an
agent that does not know the string degrades it to an operation it cannot
drive, which the spec requires.

## Adding a provider

Implement `provider.IdentityProvider` on the adapter and set the field.
`Identifiers` reports what the provider says the record is; `AssertedRelations`
reports relations the provider itself published. Neither computes a
similarity: inference belongs to the resolver, so a provider cannot smuggle
a guess in as a fact.

`provider.DescriptorProvider` is separate and optional. An adapter that
cannot supply title, authors, and year disables fuzzy matching for its
records rather than producing a weaker guess.

Implemented today: OpenAlex (`ids` block, plus descriptor), Semantic Scholar
(`externalIds`, plus descriptor), PubMed ESearch (PMID only, which is all
that tier asserts).

## Compatibility

Additive throughout. The resolve block is off unless
`feed402.resolve.enabled` is set, no existing route, envelope field, or
manifest field changed shape, and the declarative config-driven proxy path
is untouched. The OpenAlex and Semantic Scholar normalizers now retain each
record's raw bytes; their existing `ID` and `CanonicalURL` output is
byte-identical, covered by test.
