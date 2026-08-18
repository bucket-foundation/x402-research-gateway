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
| Crossref | `updated-by`, `update-to`, `update-policy` | Crossmark update relations in both directions, with the notice DOI and date |
| Europe PMC | `commentCorrectionList` | the PubMed correction, retraction, and expression-of-concern relations |
| DataCite | `relatedIdentifiers` | depositor-declared version history and obsoletion |

Direction follows the assertion. A Crossref record naming works it updates
produces assertions about those works, annotated `notice_is_queried_record`.
A record naming works that update it produces assertions about itself.

No commercially licensed retraction dataset is integrated. Retraction Watch
data reaches Crossref under Crossref's own terms and is read there; any
separate licensed dataset would need its access and licence terms verified
before implementation, and none has been.

## Coverage

`providers_consulted[].coverage` states what each provider's integrity data
covers, so a zero count is readable. DataCite has no retraction vocabulary
at all, and its report says so rather than leaving a consumer to infer that
a DataCite record with no notice is a work with no retraction.
