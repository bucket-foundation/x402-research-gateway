## Section 3 — Integration priority recommendation (top 20 next upstreams)

Scored against: **(a)** agent demand — would an LLM research agent actually call this; **(b)** legal cleanness; **(c)** implementation cost vs existing 7; **(d)** canon-coverage gap.

| # | Upstream | Branch | Why next |
|---|---|---|---|
| 1 | **CrossRef** | cross-cutting | CC0 metadata for 160M DOIs; the universal DOI-resolver. Every agent workflow needs it. Cheapest integration (polite pool, JSON). |
| 2 | **Unpaywall** | cross-cutting | Given any DOI, returns best OA PDF URL. CC0. Agents need this to *reach full text*. Dead-simple parser. |
| 3 | **Europe PMC** | biophysics + cross | PubMed + preprints + full-text XML in one endpoint; no API key; covers PMC OA better than NCBI directly. |
| 4 | **arXiv** | math + physics + CS | Atom-XML but canonical for 3 canon branches. One parser unlocks many source_prefixes. |
| 5 | **INSPIRE-HEP** | physics | Best-in-class clean JSON, CC0 meta, covers a branch currently zero-covered. |
| 6 | **NASA ADS** | physics + cosmology | Massive bibliographic corpus back to 1800s; covers cosmology branch. Free key. |
| 7 | **Wikidata** | cross-cutting | SPARQL → structured facts for every branch. `insight` tier multiplier. |
| 8 | **OpenCitations** | cross-cutting | Citation graph as CC0 data. Agent workflow: "who cited {doi}" is a top-5 query. |
| 9 | **bioRxiv / medRxiv** | biophysics | Preprints not in PubMed for 3–12 months. Essential for currency. Shared API shape. |
| 10 | **UniProt** | biophysics | Clean REST, 250M protein entries, CC-BY. |
| 11 | **ORCID + ROR** | cross-cutting | Person + institution identifiers glue the entire graph together. Trivial integration. |
| 12 | **DBLP** | CS | Canon for CS publication graph, clean JSON. |
| 13 | **Semantic Scholar extensions** | cross | Already integrated — add authors, citations, recommendations endpoints, not just search. |
| 14 | **zbMATH Open** | math | Only comprehensive math corpus that's legally clean. Covers a branch currently zero-covered. |
| 15 | **CORE** | cross-cutting | 30M OA full-texts searchable — biggest lever for `insight` tier. |
| 16 | **OpenReview** | CS / mind | Only public source of peer-review text; unique. |
| 17 | **NASA CMR** | earth-sciences | Opens the earth-sciences branch; cleanest entrypoint across NASA EOSDIS. |
| 18 | **GBIF** | earth-sciences | Biodiversity canon; used by biology + ecology + conservation agents. |
| 19 | **Papers With Code** | CS | Unique: benchmarks + SOTA + code links. Agents love this. |
| 20 | **RCSB PDB** | chem + biophysics | CC0 structures; bridges branches 3 and 5. |

**Deferred on purpose:** Dimensions, Scite, Reaxys, MathSciNet, Sentinel-Hub, Planet, IEEE full-text (licence); Google Scholar, Connected Papers (no API); Sci-Hub, LibGen, Anna's, Z-Lib (legal posture).

---
