# Coverage and gap reporting

Implements x402-research-gateway#20. Code lives in
[`internal/coverage`](../internal/coverage) and the free endpoint in
[`internal/handler/sync.go`](../internal/handler/sync.go).

## A gap is not a statement about the science

Everything in this report describes this gateway. A field at
`not_researched` means nobody here has looked. It is not evidence that no
source exists, that the field is small, or that its literature is thin.
Reading "we have not implemented this" as "this source does not exist" is
how a coverage gap becomes a false negative in someone's research, and this
report exists to make that misreading impossible.

```
GET /research/coverage
GET /research/coverage?field=mathematics
```

Free, and derived from `config/providers.yaml` on every call rather than
maintained beside it, so it cannot drift from the registry it describes.

## States

| State | Meaning |
|---|---|
| `not_researched` | nobody has looked at this field on this dimension |
| `source_known` | a source is known to exist and nothing more is established |
| `registered` | a complete reviewed registry entry exists, and no adapter |
| `license_blocked` | a source exists and this gateway cannot serve it |
| `coverage_incomplete` | an adapter exists and does not reach everything the source offers |
| `adapter_implemented` | an adapter exists and serves this dimension |

`license_blocked` and `coverage_incomplete` carry a reason. A shadow library
excluded on legal posture reports `license_blocked` rather than vanishing
from the report, because the source exists and the honest answer is that the
gateway will not operate it.

## Dimensions

`literature_metadata`, `citation_graph`, `full_text`, `ontology`, `dataset`,
`software`, `patent`, `historical_depth`, `language`.

Every field carries every dimension. A gap is a row that says
`not_researched`, never a missing row, and the summary counts every state
including the zeroes.

## Historical depth

A provider covering 1996 onward leaves a century of literature unreachable,
and a source reaching the 1860s is a different capability. Each field
reports two numbers: `historical_from`, the earliest year an implemented
provider reaches, and `historical_from_any_source`, the earliest any known
source reaches. Where they differ, an adapter is missing and the gap is
visible as a number rather than as prose.

`historical_depth_known` false means no provider in the field records a
start year. That is a gap in this registry, never a claim that the field is
shallow. `coverage_depth_note` qualifies a year that is a floor rather than
a measured minimum.

## Language

`languages` lists what the implemented providers in a field declare, and
`languages_known` says whether any of them declared anything. An empty list
is an unrecorded fact, never a claim that the field is English-only.

## Deriving, not maintaining

Nothing in the report is hand-written. States come from each provider's
registry lifecycle, its rights block, and what it declares about full text,
depth, and language. Adding a provider or moving one to `implemented`
changes the report on the next call with no second edit anywhere.
