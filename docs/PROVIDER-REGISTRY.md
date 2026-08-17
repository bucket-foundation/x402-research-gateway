# Provider registry

`config/providers.yaml` is the machine-readable source of truth for every
research source this gateway knows about. `RESEARCH-INDEX.md` is generated
from it.

## Workflow

```bash
make validate-providers      # parse, validate, reconcile against config/routes.yaml
make research-index          # regenerate RESEARCH-INDEX.md
make research-index-check    # fail if the committed document has drifted (CI)
make provider-coverage       # lifecycle and provider-type summary
make verify-providers        # cheap liveness and drift check
```

Edit the registry or the prose partials in `docs/research-index/`, then
regenerate. Never hand-edit the generated tables in `RESEARCH-INDEX.md`: the
next regeneration overwrites them.

## What is generated and what is not

Section 1's per-domain tables are generated. The judgment is hand-written and
preserved verbatim from `docs/research-index/`:

| Partial | Content |
|---|---|
| `00-prologue.md` | Title, scope, column conventions |
| `20-section-2-grey-literature.md` | Legal-posture disclaimer and the exclusion recommendation |
| `30-section-3-priority.md` | Top-20 integration ranking and its rationale |
| `40-section-4-parser-reuse.md` | Parser-reuse map across upstream shapes |
| `90-housekeeping.md` | Maintenance notes |

## Lifecycle

`status` stages the backlog, so a source can be described long before an
adapter exists.

| Status | Meaning |
|---|---|
| `discovered` | Known to exist, nothing more |
| `researched` | API, licence, and rights have been read |
| `registered` | Complete registry entry |
| `verified` | Endpoints and licence checked live |
| `adapter_planned` | Adapter scheduled |
| `implemented` | Adapter exists, not serving traffic |
| `production` | Serving traffic through a configured route |
| `deprecated` | Still works, on the way out |
| `sunset` | Upstream is gone |
| `excluded` | Deliberate decision not to operate this source |

Only `implemented`, `production`, and `deprecated` are operational. An
`excluded` entry keeps its research note and registers no endpoints, so no
later code can route to it by accident. Validation enforces both rules.

## Rights

Rights are data, and **unknown is not permission**. `Rights.Redistribution`
must read exactly `allowed` before the gateway may serve records on; anything
else, including an empty block, denies. Metadata rights and content rights are
separate fields because they routinely differ: an open metadata licence says
nothing about full text.

Openness, discoverability, and popularity are never evidence of permission.
Record the terms URL and the date a human read it.

## Historical migrations

`historical_successor` / `historical_predecessor` link a source to what
replaced it. **The predecessor entry is never deleted**, because historical
material still references it: pre-2010 physics literature is classified with
PACS, so PACS stays retrievable even though PhySH superseded it. Validation
rejects a dangling link in either direction.

Recorded migrations: Microsoft Academic Graph to OpenAlex, PACS to PhySH,
the retired PatentsView API to the USPTO Open Data Portal.

## Verification

`make verify-providers` is a liveness and drift probe, not a crawler. Per
provider it issues one lightweight request per recorded URL (`HEAD`, falling
back to a one-byte ranged `GET`), identifies the gateway with a contact URL,
and pauses between providers.

- A `401` or `403` counts as alive: the endpoint exists and wants credentials.
- Excluded and sunset sources are never contacted.
- Failure flags the entry `stale` with a reason and records `last_verified`.
  **It never deletes an entry** — an upstream being briefly down is not
  evidence that a source should be forgotten.
- The command exits zero even when entries are stale, so a flaky upstream
  cannot break CI. `make validate-providers` is the gate.

Pass `-write` to persist `last_verified`, and `-only` to scope a run:

```bash
make verify-providers ARGS="-only crossref,ror -write"
```

## Credentials

The registry records *that* a source needs a key (`auth: api-key`), never the
key itself. Credentials belong in the environment. A test asserts the registry
file contains no credential-shaped strings.

The USPTO Open Data Portal is registered and fully described, but its
verification is deployment-blocked until an ODP key is provisioned.
