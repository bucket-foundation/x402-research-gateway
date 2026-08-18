# Scholarly integrity and update graph

Implements x402-research-gateway#9. Code lives in
[`internal/integrity`](../internal/integrity), the paid operation in
[`internal/handler/integrity.go`](../internal/handler/integrity.go), and the
provider seam in
[`internal/provider/integrity_adapters.go`](../internal/provider/integrity_adapters.go).

## Absence is not clearance

A work no consulted provider published a notice for is a work those
providers reported nothing about. It has not been checked and found sound.
Every response carries that sentence in `absence_notice`, names every
provider consulted, and states what each one returned, zero included.

A provider that timed out or answered 503 reports that outcome. It never
renders as a provider with no notices, because that would read as a
clearance the gateway cannot give.

## Disagreement coexists

Providers ingest notices at different times. Crossref carrying a retraction
Europe PMC has not yet ingested is the most informative fact in the
response, so both assertions are returned side by side.

There is no single status field. `assertions` holds every provider's
statement whole, `status_summary` indexes them by status without collapsing
them, and `providers_disagree` flags that the answering providers asserted
different status sets. The flag is a statement about the data, never a
judgment about which provider is right. The gateway does not adjudicate.

## Statuses

`correction`, `erratum`, `retraction`, `withdrawal`,
`expression_of_concern`, `new_version`.

Each assertion carries the asserting provider, the upstream's own term
verbatim in `provider_term`, the notice identifier where one exists, the
date the provider published, the upstream field it was read from, and
`retrieved_at`.

A provider term that maps to no status is emitted with `recognized: false`
and no status. Calling a correction a retraction is worse than saying
nothing, so an unmapped term is carried rather than coerced.

## Sources

| Provider | Source field | What it contributes |
|---|---|---|
| Crossref / Crossmark | `updated-by`, `update-to`, `update-policy` | Crossmark update relations in both directions, with the notice DOI and date |
| Europe PMC | `commentCorrectionList` | the PubMed correction, retraction, and expression-of-concern relations |
| DataCite | `relatedIdentifiers` | depositor-declared version history and obsoletion |
| arXiv | the version number already carried in the record id | new_version when the fetched record is v2 or later, naming the base submission as the affected work and the current version as the notice |

Direction follows the assertion. A Crossref record naming works it updates
produces assertions about those works, annotated `notice_is_queried_record`.
A record naming works that update it produces assertions about itself.

## Retraction Watch (x402-research-gateway#19)

No commercially licensed retraction dataset is integrated, and none is
scraped. Retraction Watch's original database was acquired by Crossref from
the Center for Scientific Integrity in 2023. Verified live 2026-08-18 against
Crossref's own documentation, retractionwatch.com's user guide, and the
dataset's README at `gitlab.com/crossref/retraction-watch-data`:

- **Per-DOI retraction signal**: already covered, no separate integration
  needed. Crossref's own documentation states retractions from this
  acquisition "appear in the update-to field via the Crossref REST API" —
  the same `update-to`/`updated-by` field the Crossref/Crossmark adapter
  above already reads, under Crossref's CC0 metadata terms.
- **Standalone bulk dataset** (the full CSV/git history at
  `gitlab.com/crossref/retraction-watch-data`): registry-only
  (`config/providers.yaml`, provider_id `retraction-watch-crossref-dataset`).
  No page consulted — Crossref's announcement, Crossref's REST API docs,
  retractionwatch.com's user guide, or the dataset's own README — states an
  explicit licence (no CC0/CC-BY/SPDX identifier, no LICENSE file content
  found) or an explicit redistribution permission for the bulk file.
  "Publicly available" is not a licence grant, and this registry treats an
  unstated licence as unknown, which forbids redistribution. The registry
  entry records what the source is and where it lives and claims no route
  and ingests nothing. Do not scrape or bulk-download it without a human
  confirming an explicit licence first.

## Coverage

`providers_consulted[].coverage` states what each provider's integrity data
covers, so a zero count is readable. DataCite has no retraction vocabulary
at all, and its report says so rather than leaving a consumer to infer that
a DataCite record with no notice is a work with no retraction.
