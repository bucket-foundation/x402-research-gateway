# Provider health, drift, and contract tests

Upstreams drift: endpoints move, response shapes change, sunset notices show
up in a header nobody reads, documentation gets rewritten without a version
bump. This gateway catches that before a caller does, without generating the
kind of traffic that gets a free public API worried about it.

## Two layers, one implementation

**Contract tests** (`go test ./internal/provider/... -run TestNormalizerContracts`)
run against recorded fixtures, generate no upstream traffic, and run in the
regular test suite. `internal/provider/contract_test.go` names every
implemented adapter's `Normalizer` and the response shape its production
parser depends on — the same fixture consts each adapter's own `_test.go`
already exercises, not a parallel copy that could drift on its own.
`TestNormalizerContracts_CoverEveryRegisteredNormalizer` fails the build if a
new adapter ships without a matching entry.

**Scheduled drift checks** (`make verify-providers`, `.github/workflows/provider-health.yml`)
run daily against live endpoints, one lightweight HEAD (falling back to a
1-byte ranged GET) per registered URL, respecting each provider's
politeness conventions via a configurable per-provider delay. Both layers
share `internal/registry/verify.go`'s `Verifier`: the CLI, the scheduled
job, and the unit tests exercising `Verifier` all call the same code, so
"cheap check" and "contract test" never diverge into two implementations
that quietly stop agreeing with each other.

## What gets checked

| Check | How |
|---|---|
| Endpoint reachable | HEAD (or ranged GET) against `base_url`, `documentation_url`, `rights.terms_url` |
| Response shape matches the adapter | Contract test parses a recorded fixture through the adapter's `Normalizer` |
| Sunset / deprecation signal | `Sunset` (RFC 8594) and `Deprecation` response headers |
| Migration announced in docs | Case-insensitive keyword scan of the documentation body ("has been discontinued", "migrating to", …) |
| Documentation drift | sha256 of the documentation body compared against the hash recorded on the last check |

Pagination-parameter and rate-limit-header drift are not yet automated; a
provider's `pagination` and `rate_limit` registry fields are the place a
human records what to check by hand until that lands.

## Registry entries are never deleted or downgraded on outage

`Provider.Status` (lifecycle) is untouched by a failed check. What moves:

- `last_verified` — the date of the **last successful** check. A failure
  never advances it, so an outage cannot look like a fresh verification and
  a source's real track record stays visible.
- `last_checked` — the date of the last **attempt**, success or failure, so
  "never checked" and "checked and failing" are distinguishable.
- `stale` / `stale_reason` — set on failure, cleared on the next success.
- `warnings` — non-failing signals (sunset header, doc drift, keyword hit)
  from the most recent check. Refreshed every run; does not set `stale`.
- `documentation_content_hash` — updated whenever the documentation URL was
  fetched successfully, drift or not.

A provider that has been down for a month still shows exactly when it last
worked. That is the fact a reproducible research campaign needs, and it is
the fact `last_verified = today` on a failed check used to destroy.

## Commands

```bash
make verify-providers                       # sweep everything, print results
make verify-providers ARGS="-only pubmed"    # one provider
make verify-providers ARGS="-write"          # persist last_verified/stale/warnings
go run ./cmd/registry verify -fail-on-stale  # CI gate: nonzero exit if anything is stale
go run ./cmd/registry health                 # last-recorded state, no network calls
```

`registry health` is safe to run as often as wanted (a status page, a
pre-campaign readiness check): it only reads what the last `verify` run
recorded.
