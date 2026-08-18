# Research-object relations

Implements x402-research-gateway#7. Code lives in
[`internal/relation`](../internal/relation), the feed402 lineage shape in
[`internal/lineage`](../internal/lineage), and the provider seam in
[`internal/provider/object_relations.go`](../internal/provider/object_relations.go).

## The premise

A paper has an underlying dataset, the software that produced it, the trial
it reports, a correction issued against it, and supplementary material.
Upstreams publish those links. Before this, the gateway read them only where
one happened to map onto an identity relation and discarded the rest.

A relation here is `(subject, provider_term, object, provider,
retrieved_at)`. Both ends are typed, and both type sets are open.

## Provider terms are the record

DataCite's `relationType`, Crossref's update types and `relation` block, and
ClinicalTrials.gov's reference types are three vocabularies that do not map
onto each other. Coercing them into one house vocabulary would lose
information a consumer may need, so `predicate.provider_term` carries the
upstream's own string verbatim and `predicate.normalized_term` is the
gateway's annotation on top of it.

A term with no gateway equivalent is stored and returned with
`predicate.recognized: false` and no `normalized_term`. It is never dropped.
`Set.unrecognized_terms` indexes every such term in a response, so a
consumer sees at a glance where its own vocabulary work sits.

This is feed402 SPEC §2.3's unknown-field rule applied to relation
vocabularies.

## Open type sets

`relation.ObjectType` covers `work`, `preprint`, `dataset`, `software`,
`patent`, `trial`, `model`, `correction`, `supplement`, `organization`,
`person`, `collection`, and `unknown`. Provider terms map onto it through
`RegisterObjectTypeTerm`, which is runtime-extensible. An unmapped provider
term resolves to `unknown` with `provider_type` retained and
`type_recognized: false`.

Relation terms work the same way through `RegisterPredicateTerm`.
Registration is additive: adding a mapping later never rewrites what a
provider said, because the provider's string was stored, not replaced.

## Derivation goes through feed402 lineage

A provider asserting that one object was produced from another (DataCite
`IsDerivedFrom`, `IsCompiledBy`, the version relations) produces a feed402
SPEC §3.7 lineage entry on the relation rather than a second derivation
model beside it. `Set.lineage` collects those entries, renumbered, in the
array form an envelope carries at top level.

The transformation on such an entry is `provider_asserted_derivation`, and
its notes say so: the gateway performed no transformation, it is reporting
one an upstream published.

## Providers

| Provider | Source field | Vocabulary |
|---|---|---|
| DataCite | `relatedIdentifiers` | `IsSupplementTo`, `IsDerivedFrom`, `IsVersionOf`, `Describes`, and the rest of the DataCite set, camel-cased |
| Crossref | `relation`, `update-to`, `updated-by` | `is-supplement-to`, `has-preprint`, hyphenated; plus the Crossmark update types `correction`, `retraction`, `withdrawal` |
| ClinicalTrials.gov | `referencesModule` | `RESULT`, `DERIVED`, `BACKGROUND` |

Direction follows the assertion. Crossref `updated-by` names works that
update this record, so the relation is emitted with the updating work as
subject. ClinicalTrials.gov asserts from the study side, and the emitted
relation reads "this publication results from this trial."

## Boundary

The gateway carries relations upstream providers assert about research
objects. Nothing here relates a work to a problem instance, an equation, a
mathematical structure, or an algorithm; derived scientific relations belong
to a downstream repository. A test enforces the vocabulary boundary.

There is no relation inference. Nothing in this package computes a relation
from similarity, co-citation, or any other signal. Every relation in a
response is one an upstream published.
