## Section 4 — Implementation patterns observed (parser reuse map)

For each shape, I note which existing parser applies or whether a new one is needed.

| # | Pattern | Shape summary | Examples in this index | Existing parser reuse | New parser? |
|---|---|---|---|---|---|
| 1 | **NCBI E-utilities (ESearch / ESummary / EFetch)** | 2–3 step: ESearch (query→ID list) → ESummary/EFetch (IDs→records); XML or JSON; email polite header + api-key. | PubMed, PMC, GEO, ClinVar, dbSNP, many NCBI DBs | ✅ reuse existing **pubmed-style** | no (just parameterize `db=`) |
| 2 | **`?query=X` GET → JSON hit array** | Single call, flat `results[]`; maybe `cursor`/`page`. | OpenAlex, CrossRef, DataCite, S2, Europe PMC, CORE, DOAJ, DOAB, Zenodo, Neurovault, DANDI, GBIF, OBIS, PubChem PUG-view, Wikipedia REST, PsyArXiv (via OSF), RCSB (search), ChEMBL, UniProt | ✅ reuse **openalex-style** / **s2-style** (same shape under the hood) | no |
| 3 | **GraphQL** | POST JSON `{query, variables}`; response `data.{root}` needs field-picking per-query. | OpenTargets, OpenNeuro, RCSB PDB (data.graphql), DBLP (optional) | partial — ClinicalTrials v2 is REST not GraphQL | **yes** — new `graphql-style` parser that takes a query-template per route |
| 4 | **OpenSearch / Atom-XML** | Atom feed with `<entry>` elements; mostly used by arXiv and institutional repos. | arXiv, some OAI-PMH respondents | none existing | **yes** — `atom-style` parser (xml→hit array) |
| 5 | **OAI-PMH** | Harvester protocol: `verb=ListRecords&metadataPrefix=oai_dc`; resumption tokens; XML. | zbMATH OAI, CDS, PMC OAI, many repositories | none existing | **yes** — `oai-pmh-style` parser (only needed if we want harvesting mode) |
| 6 | **TAP / ADQL (VO astronomy)** | SQL-flavored queries over astro catalogs; VOTable XML or JSON. | SIMBAD TAP, VizieR TAP, Gaia, NASA Exoplanet Archive, ESO | none existing | **yes** — `tap-adql-style` (VOTable parser; heavier lift) |
| 7 | **DOI-resolver style** | Single-shot `GET /{doi}` → single record. | CrossRef `/works/{doi}`, DataCite `/dois/{doi}`, Unpaywall `/{doi}`, OpenCitations | ✅ reuse **clinicaltrials-style** (single-record by id) — conceptually identical | no |
| 8 | **Bulk-download / TAP-sync / STAC** | Not a hit-array; returns data streams / grid URIs / STAC items. | NASA CMR, USGS M2M, Copernicus CDS, MAST, Sentinel-Hub, NOAA PSL (OPeNDAP) | none existing | **yes** — `stac-style` parser + treat as `insight`-tier helper, never raw-tier passthrough |
| 9 | **Authenticated OAuth (2-legged or 3-legged)** | Token negotiation before each call; rotating. | Dimensions, Elsevier ScienceDirect, Sentinel-Hub, Planet, IEEE | none existing | **yes** — `oauth-upstream` middleware around any other shape |
| 10 | **Email-in-header polite pool** | Unauth but *highly* recommended to send `mailto:` param or UA. | NCBI (`&email=`), OpenAlex (`&mailto=`), CrossRef (`User-Agent: ...; mailto:...`), Unpaywall (`?email=`) | ✅ middleware already present for OpenAlex/NCBI | generalize into a per-route `politeIdentity` config |

**Summary:** with **two new parsers** (`graphql-style`, `atom-style`) and **one new middleware** (`tap-adql-style` — treated as optional, used only for astronomy depth), the gateway can cover all of Section 3's top-20 with mostly-existing machinery. OAI-PMH, STAC, and OAuth are deferred to v0.3+.

---
