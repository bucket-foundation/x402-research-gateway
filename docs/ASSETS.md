# Rights-aware asset discovery

Implements x402-research-gateway#8. Code lives in
[`internal/asset`](../internal/asset), the paid operation in
[`internal/handler/assets.go`](../internal/handler/assets.go), and the
provider seams in each adapter's `Assets`, `RecordRights`, and
`Availability` methods.

## The question

What representations of this work are legally discoverable, and where.

`POST /research/assets` with `{"identifier": "10.7717/peerj.4375"}` returns
every representation the consulted providers published, each with its own
rights and its own availability, plus a per-provider account of who was
asked and what they said.

## Discovery only

The gateway discovers locations. It does not fetch, mirror, cache, or
re-serve content, under any configuration flag. The only upstream calls this
endpoint makes are to the configured provider metadata routes.
`TestFanOutAssets_NeverFetchesAnAssetLocation` points every discovered
location at a counting host and asserts it receives zero requests.

No shadow-library source is registered in any lifecycle state other than
`excluded`. The `sci-hub` and `libgen` registry entries carry the legal
posture and produce no operational endpoints; registry validation refuses
endpoints on an excluded provider.

Where a repository or publisher permits redistribution, that permission is
recorded per asset and honored per asset. It is never assumed from the
provider's metadata licence.

## Rights

Two statements, kept apart:

- `providers_consulted[].metadata_rights` covers the records the provider
  serves. Crossref's is CC0.
- `assets[].rights` covers the representation at that location. Crossref's
  CC0 says nothing about it, and the field reports unknown on most records.

`redistribution` is one of `allowed`, `prohibited`, `unknown`. Unknown is
the zero value and grants nothing. `Rights.Permits()` returns true only for
an explicit `allowed`, a value from nowhere degrades to unknown with the
rejected string recorded in `source`, and `TestRights_UnknownIsNeverPermitted`
covers every construction path.

`free_to_read` is separate from redistribution. A CORE-hosted copy with no
licence field is free to read and unknown to redistribute, which is exactly
what CORE publishes.

## Availability

| Value | Meaning |
|---|---|
| `retrievable` | at least one representation is reachable at a published location |
| `restricted` | the work exists in a representation the consulted providers could not hand over |
| `absent` | no consulted provider published any discoverable representation |
| `unknown` | no provider answered, so nothing is known either way |

`absent` is a result. `open_access_copy_found` restates it as its own field,
so a consumer never infers a negative answer from an empty array. A fan-out
where every provider failed reports `unknown`, never `absent`.

## Sources

| Provider | What it contributes |
|---|---|
| Unpaywall | open-access locations per DOI, with per-location licence, host type, and version |
| Europe PMC | abstract, JATS full-text XML where held, PDF, with per-record rights |
| PMC | reached through Europe PMC's `inPMC` / `PMCID` records |
| CORE | CORE-hosted copies and source repository locations across roughly 290M records |
| arXiv | abstract page, PDF, TeX source, with the per-submission licence |
| DataCite | landing page and depositor `contentUrl` entries, with the object's own rights |
| Crossref | publisher `link` metadata, with the deposited licence where one exists |

CORE needs a free API key from core.ac.uk. The gateway holds no key: the
route reads `CORE_API_KEY` from the operator's environment. On a deployment
where it is unset, comment the two `core-*` routes out of `config/routes.yaml`;
left in place with no key, CORE answers 401 and the response reports an
upstream status rather than a negative answer.

## Boundary

No full-text extraction, no parsing, no indexing of retrieved documents.
CORE returns inline `fullText` on some records and no asset ever carries it;
`TestCORENeverReservesFullText` asserts that.
