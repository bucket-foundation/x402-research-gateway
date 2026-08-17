## Housekeeping

- **Sunset / do-not-integrate:** Microsoft Academic Graph (closed 2021-12-31), Google Scholar (no public API, ToS-hostile), Connected Papers (no API), MathSciNet (licensed), Reaxys (licensed), Dimensions/Scite (paid; revisit if contract), Planet Labs (paid).
- **Legal-blocked / posture-excluded:** Sci-Hub, LibGen, Anna's Archive, Z-Library (see Section 2).
- **Cross-branch cites:** RCSB PDB (chem + biophysics), NASA ADS (physics + cosmology), PubMed (biophysics + mind), arXiv (math + physics + CS), Europe PMC (biophysics + cross-cutting). Pick a primary `source_prefix` and allow secondary tags in citation metadata.
- **Polite identity** for the gateway: `User-Agent: x402-research-gateway/0.2 (+https://nucleus.agfarms.dev; mailto:gian@agfarms.dev)`. Use this across all routes that honor a polite pool.

---

**Sources (official API docs, verified 2026-04-21):**
- zbMATH Open REST: https://api.zbmath.org/v1/
- INSPIRE-HEP: https://github.com/inspirehep/rest-api-doc
- Europe PMC REST: https://europepmc.org/RestfulWebService
- Anna's Archive (legal posture reference): https://en.wikipedia.org/wiki/Anna's_Archive
- arXiv API: https://info.arxiv.org/help/api/
- NASA ADS: https://ui.adsabs.harvard.edu/help/api/
- CrossRef: https://api.crossref.org
- OpenAlex: https://docs.openalex.org
- Unpaywall: https://unpaywall.org/products/api
- Semantic Scholar: https://api.semanticscholar.org/api-docs
- Others: see per-row URL notes.
