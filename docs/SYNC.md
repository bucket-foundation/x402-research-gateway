# Bulk and incremental synchronization

Implements x402-research-gateway#11. The model lives in
[`internal/registry/types.go`](../internal/registry/types.go), the data in
[`config/providers.yaml`](../config/providers.yaml), and the free discovery
endpoint in [`internal/handler/sync.go`](../internal/handler/sync.go).

## The question it answers

Should an agent page through an API two million times, or download the
snapshot. `GET /research/sync` answers it: which providers publish a
whole-corpus artifact, where it is, what format it is in, how often it is
republished, what access it needs, and what its licence permits.

Discovery is free. Pricing the decision about whether to pay must not itself
cost anything.

```
GET /research/sync
GET /research/sync?mode=oai_pmh
GET /research/sync?provider=openalex
```

## Represent, do not proxy

The gateway is not a mirror, a CDN, or a snapshot host. No route serves an
artifact, and none will: streaming a multi-gigabyte dump through a metered
HTTP endpoint multiplies cost, adds a failure point, and in several cases
would breach the provider's redistribution terms. The agent fetches from the
provider directly.

`sync.serve_direct` records the exception: a provider whose incremental feed
is small and permissive enough to serve through the normal metered path.
It is a per-provider decision on size and rights, and registry validation
refuses to record it without `serve_direct_rationale` beside it. No provider
sets it today.

## Sync modes

`bulk_snapshot`, `dump`, `oai_pmh`, `change_feed`, `incremental_cursor`,
`release_based`, `date_window`. The set is closed, so an unreviewed string
cannot enter the registry and be read by an agent as a promise.

## Per-snapshot metadata

`url`, `version`, `release`, `checksum` with `checksum_algorithm`, `size`,
`format`, `last_modified`, `update_frequency`, `auth`, `rights`, `notes`.

Absent fields are absent facts. A snapshot whose size or checksum the
provider does not publish carries neither, rather than an estimate an agent
would act on.

## Rights

A snapshot's licence is recorded apart from the API's, because it is
routinely stricter. Crossref's metadata is CC0 and its monthly dump is a
paid Metadata Plus product. OpenAlex publishes both under CC0. arXiv's
requester-pays S3 channel is permission to download and not permission to
redistribute.

Unknown grants nothing, here as everywhere. A test asserts no gated snapshot
in the registry (`auth` other than none) is marked redistributable.

## Verification

`sync.verified` says whether a human checked the facts against the provider
rather than transcribing its documentation. Every entry populated in this
revision is `verified: false` with an `unverified_reason` saying so, and
registry validation refuses an unverified entry that does not explain
itself. An agent planning a whole-corpus download needs to know which it is
reading.

Verifying them live is follow-up work, and it is what `make verify-providers`
exists for.

## OAI-PMH

Endpoints are registered for arXiv, Europe PMC, DataCite, and zbMATH Open
even though no harvester exists yet, with their metadata prefixes. An agent
asking whether an endpoint exists deserves the answer regardless of whether
this gateway can drive it.

## Adapter capability beside registry capability

The listing reports the running adapter's `SyncCapability` next to the
registry entry. They can differ, and the difference is information: arXiv's
registry entry describes a bulk S3 channel while its adapter reports
`bulk: false`, because this deployment exercises no requester-pays channel.
Neither reading overwrites the other.
