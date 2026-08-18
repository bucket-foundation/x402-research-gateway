# Vocabulary and ontology layer

The gateway can find papers about a concept; this layer resolves the
concept itself (x402-research-gateway#14, #15).

## Capability, not one interface

A thesaurus, an OWL ontology, a classification code, and a document
standard are different kinds of object. `internal/provider/vocabulary.go`
splits the generic operations `search_terms` / `get_concept` / `broader` /
`narrower` / `related` / `synonyms` / `mappings` / `historical_terms` /
`deprecated_terms` / `release` into small interfaces (`TermSearcher`,
`ConceptGetter`, `BroaderNarrowerProvider`, …), matching the
`Searcher`/`Fetcher`/`Paginator` composability convention already in this
package. A provider implements only what its upstream supports; the rest
is absent, not stubbed to return empty-but-successful.

`Concept` carries a normalized view (`PrefLabel`, `AltLabels`,
`Definition`, temporal/lifecycle fields) alongside `Native` — the source's
own serialization, untouched. Normalization is additive; nothing is
coerced into a stronger claim than the source made (a thesaurus's
`skos:broader` never becomes an ontological `subClassOf`).

## Implemented adapters

| Provider | File | Ops | Verified |
|---|---|---|---|
| MeSH | `internal/provider/mesh.go` | SearchTerms, GetConcept, Broader, HistoricalTerms, CurrentRelease | 2026-08-18, live |
| Gene Ontology (via EBI OLS) | `internal/provider/ols.go` | SearchTerms, GetConcept, Broader, Narrower, Synonyms, CurrentRelease | 2026-08-18, live |

`OLSProvider` is parameterized by ontology id and works unmodified against
any OBO Foundry ontology OLS4 indexes (ChEBI, HP, MONDO, …); only Gene
Ontology is instantiated so far. Adding another is a new `OLSProvider{Ontology: "..."}` plus a registry entry with that ontology's own verified
licence — see `obo-foundry-ols` in `config/providers.yaml`.

## Historical terminology (#15)

Every `Concept` carries `SourceRelease`. `HistoricalAliases`,
`SupersededBy`, `Predecessor`, and `Successor` are directional and never
imply equivalence:

- **MeSH** exposes prior labels via its own `previousIndexing` field —
  today's "Apoptosis" (`D017209`) carries `previousIndexing: "Cell
  Survival (1972-1992)"`, a fact read live, not invented.
- **Gene Ontology** exposes deprecation via `is_obsolete` +
  `term_replaced_by` (exact successor) and `consider` (non-exact
  candidates) — the two are kept in separate `Concept` fields
  (`SupersededBy` vs. `Successor`) so a caller never reads a candidate list
  as a stated equivalence.
- **PACS → PhySH** is registered as a `historical_vocabulary` /
  `classification` pair via the registry's `historical_successor` /
  `historical_predecessor` fields (`config/providers.yaml`, no adapter
  needed for the relation itself: PACS has no live endpoint, and the
  relation is the fact worth keeping).

## Licensing is visible before it's discovered at runtime

UMLS and SNOMED CT are registered `researched` with `rights.redistribution:
unknown` and a `notes` field naming the concrete gate (a signed UMLS
license + NLM account; SNOMED's territorial member-country licensing).
Neither is implementable without provisioning credentials nobody has; the
gateway does not invent them, and does not serve restricted content on the
assumption that "unknown" means "allowed."

## Registered, not yet implemented

`config/providers.yaml` section 1.10 carries ~25 additional entries across
the families #14 named (Getty AAT/TGN/ULAN, LOC vocabularies, BIBFRAME,
OBO Foundry catalog, LOINC, IUPAC Gold Book, AGROVOC, CF Standard Names,
NASA GCMD Keywords, SWEET, GeoSciML, IUCr CIF dictionaries, NOMAD
MetaInfo, OPTIMADE, MSC2020) at accurate lifecycle states
(`discovered`/`researched`/`registered`), so the backlog is described
before it is built. Document and mathematical-semantics standards (JATS,
TEI, MathML, OpenMath CDs, BIBFRAME) are registered as `standard` /
`document_standard` / `mathematical_semantics_standard`, not as searchable
sources — they describe a shape other providers' data conforms to.
