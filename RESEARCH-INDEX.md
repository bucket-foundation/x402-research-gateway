# RESEARCH-INDEX.md

**Catalog of research APIs + retrieval methods for `gianyrox/x402-research-gateway` (feed402/0.2)**
**Last reviewed:** 2026-04-21 · **Maintainer:** Gian · **Scope:** universal research gateway, one x402 paywall, organized against Bucket Foundation canon branches + earth sciences + cross-cutting discovery + grey-literature.

**Conventions used below:**
- `Auth`: `none` (open, polite-header recommended) · `email-header` (NCBI/OpenAlex polite pool) · `api-key` (free key) · `oauth` (negotiated) · `paid` (licensed).
- `Tier fit`: **raw** = single-record passthrough (GET/{id}); **query** = search / list endpoints returning N hits; **insight** = LLM-synthesized answer over raw+query output.
- `License` refers to the metadata license the gateway would redistribute. Full-text rights are always narrower; default posture is **citation-only** (snippets + canonical_url).
- `source_prefix` is the feed402 `citation.source_prefix`. Canonical URL template uses `{id}` placeholders.
- All Base URLs verified current as of 2026-04 unless noted "sunset".

---

## Section 1 — Canonical APIs (clean legal status)

### 1.1 — Mathematics

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **arXiv (math.*)** | Preprints in all math subj classes (AG, AT, CO, DG, NT, PR, ST…) | ~600k math preprints | `http://export.arxiv.org/api/query?search_query=cat:math.*` (Atom) | none | 1 req/3s polite | arXiv non-exclusive (metadata free; PDF per-paper) | raw (by id), query (search) | `arxiv` | `https://arxiv.org/abs/{id}` | Atom XML not JSON; parser new. Rate-limit strict — cache aggressively. Docs: https://info.arxiv.org/help/api/ |
| **zbMATH Open REST** | Reviewed math literature, abstracts, author + classification graph | ~4.5M records | `https://api.zbmath.org/v1/` (endpoints: `/document`, `/author`, `/classification`, `/serial`, `/software`) | none | soft (polite) | CC-BY 4.0 (metadata) | raw (by zbl id), query (`/document?q=`) | `zbmath` | `https://zbmath.org/{id}` | Open since 2021 (post–zbMATH-classic). OAI-PMH also at https://oai.zbmath.org/. Docs: https://api.zbmath.org/v1/ |
| **OEIS (Sloane)** | Integer sequences, formulae, cross-refs | ~380k sequences | `https://oeis.org/search?q={q}&fmt=json`, record at `https://oeis.org/{Axxxxxx}` | none | polite | CC-BY-NC 3.0 | raw (`/Axxxxxx`), query (`/search`) | `oeis` | `https://oeis.org/{id}` | The canonical combinatorics lookup. Clean JSON. |
| **MathSciNet (AMS)** | AMS-reviewed math literature, MR-numbers, review text | ~4M records | `https://mathscinet.ams.org/mathscinet/api` | paid (institutional) | contractual | proprietary | ⚠️ cannot resell — skip | — | — | **License-blocked** for a public gateway. Mention only as "premium backdoor via institutional OAuth" — do NOT proxy. |
| **arXiv math-ph** | Mathematical physics preprints | subset of arXiv | same as arXiv | none | 1/3s | as arXiv | raw, query | `arxiv` | `https://arxiv.org/abs/{id}` | Shares parser with arXiv. |
| **PolyDB** | Polytopes / discrete geometry database | ~100k objects | `https://db.polymake.org/` (Mongo HTTP bridge) | none | polite | CC-BY | raw | `polydb` | `https://polydb.org/explore/{id}` | Niche but canon-tier for discrete geometry. |
| **LMFDB** | L-functions, modular forms, number-theory objects | 10M+ objects | `https://www.lmfdb.org/api/` | none | polite | CC-BY-SA | raw, query | `lmfdb` | `https://www.lmfdb.org/{path}` | Number-theory canon. API still beta; HTML scrape fallback viable. |
| **FindStat** | Combinatorial statistics / bijections | 1k+ statistics | `http://www.findstat.org/api/` | none | polite | CC-BY-SA | raw | `findstat` | `http://www.findstat.org/{id}` | Niche but cite-worthy. |

### 1.2 — Physics

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **arXiv (physics, cond-mat, hep-*, quant-ph, gr-qc, astro-ph, nlin)** | All physics preprints | ~1.7M | `http://export.arxiv.org/api/query` | none | 1/3s | arXiv non-exclusive | raw, query | `arxiv` | `https://arxiv.org/abs/{id}` | Single parser covers all arXiv categories. |
| **INSPIRE-HEP** | HEP literature, authors, institutions, conferences, jobs | ~1.6M records | `https://inspirehep.net/api/literature?q=...`, also `/authors`, `/conferences`, `/institutions`, `/jobs` | none | polite | CC0 (metadata) | raw (`/literature/{id}`), query | `inspire` | `https://inspirehep.net/literature/{id}` | Cleanest physics API. JSON-Schema documented. Docs: https://github.com/inspirehep/rest-api-doc |
| **NASA ADS** | Astronomy + physics bibliographic (exhaustive back to 1800s) | ~16M records | `https://api.adsabs.harvard.edu/v1/search/query` | api-key (free) | 5000/day | CC-BY (metadata) | raw, query | `ads` | `https://ui.adsabs.harvard.edu/abs/{bibcode}` | Bibcode is primary id. Required header: `Authorization: Bearer <token>`. Also covers cosmology + earth & planetary. |
| **CERN Document Server (CDS)** | CERN papers, notes, photos, multimedia | ~3M records | `https://cds.cern.ch/search?of=hx&...` + OAI-PMH at `/oai2d` | none | polite | CC-BY | raw, query | `cds` | `https://cds.cern.ch/record/{id}` | Invenio-backed. OAI-PMH most reliable for bulk. |
| **APS Harvest API** | American Physical Society (PhysRev family) metadata | ~700k articles | `https://harvest.aps.org/v2/journals/articles` | api-key (free on request) | 3600/hr | CC-BY (meta), full-text paywalled | raw, query | `aps` | `https://doi.org/{doi}` | Metadata + abstract only. Full-text via publisher agreement. |
| **IOP Publishing** | IOP journals | ~1M articles | via CrossRef + publisher site | none (via CrossRef) | — | CC-BY where OA | raw, query | `iop` | `https://iopscience.iop.org/{doi}` | No dedicated metadata API — use CrossRef. |
| **OSTI.gov** | US DOE scientific/technical information | ~3M records | `https://www.osti.gov/api/v1/records` | none | polite | US-gov public domain | raw, query | `osti` | `https://www.osti.gov/biblio/{id}` | Under-used. Rich for applied physics, fusion, energy. |
| **SAO/NASA ADS Classic Export** | Same as ADS but bulk bibcode export | — | `https://api.adsabs.harvard.edu/v1/export/bibtex` | api-key | — | — | insight helper | `ads` | — | Use as tool-for-tool inside `insight` tier, not a raw route. |

### 1.3 — Chemistry

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **PubChem PUG-REST** *(already integrated)* | Compounds, substances, bioassays | 119M compounds | `https://pubchem.ncbi.nlm.nih.gov/rest/pug/compound/...` | none | 5 req/s | public-domain (US-gov) | raw, query | `pubchem` | `https://pubchem.ncbi.nlm.nih.gov/compound/{cid}` | Keep. Reuse existing parser. |
| **ChEMBL** | Bioactive molecules, bioactivity, targets, drugs | ~2.4M compounds | `https://www.ebi.ac.uk/chembl/api/data/` | none | polite | CC-BY-SA | raw, query | `chembl` | `https://www.ebi.ac.uk/chembl/compound_report_card/{chembl_id}/` | Complement to PubChem, bioactivity-focused. |
| **ChemSpider** | Chemical structures, names, props | ~125M | `https://api.rsc.org/compounds/v1/` | api-key (free) | 1000/mo free | Royal Society of Chemistry ToS | raw, query | `chemspider` | `https://www.chemspider.com/Chemical-Structure.{id}.html` | Free tier tight — budget route. |
| **RCSB PDB** | Protein/biomolecular structures | ~220k structures | `https://data.rcsb.org/rest/v1/core/entry/{pdbid}`, search at `https://search.rcsb.org/rcsbsearch/v2/query` | none | polite | CC0 | raw (`/entry/{id}`), query | `pdb` | `https://www.rcsb.org/structure/{id}` | GraphQL also at `https://data.rcsb.org/graphql`. |
| **PDBe (EBI)** | Mirror of PDB + annotations | same | `https://www.ebi.ac.uk/pdbe/api/` | none | polite | CC0 | raw, query | `pdbe` | `https://www.ebi.ac.uk/pdbe/entry/pdb/{id}` | European mirror, richer annotations. |
| **MassBank** | Mass spectra | ~90k spectra | `https://massbank.eu/MassBank/api/` | none | polite | CC-BY | raw, query | `massbank` | `https://massbank.eu/MassBank/RecordDisplay?id={id}` | Chemoinformatics niche. |
| **NIST WebBook** | Thermochem, IR/Mass/UV spectra | 150k compounds | `https://webbook.nist.gov/cgi/cbook.cgi` | none | polite (no formal API) | US-gov public domain | raw (scrape) | `nist-webbook` | `https://webbook.nist.gov/cgi/cbook.cgi?ID={id}` | No JSON API — HTML scrape; build careful parser. |
| **CrossRef Chemistry subset** | Chemistry DOIs | — | `https://api.crossref.org/works?filter=category-name:...` | none (email polite) | 50/s polite | CC0 (meta) | query | `crossref` | `https://doi.org/{doi}` | Use CrossRef filter by subject category. |
| **Reaxys** | Reactions, synthesis | ~60M reactions | Elsevier API | paid | contractual | proprietary | ⚠️ skip | — | — | License-blocked. |

### 1.4 — Information / Computation

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **arXiv (cs.*, stat.ML)** | CS preprints | ~900k | `http://export.arxiv.org/api/query?search_query=cat:cs.*` | none | 1/3s | arXiv non-exclusive | raw, query | `arxiv` | `https://arxiv.org/abs/{id}` | Shares arXiv parser. |
| **DBLP** | CS bibliographic, authors, venues | ~7M publications | `https://dblp.org/search/publ/api?q=...&format=json`; person at `/pid/{id}.json` | none | polite | ODC-BY | raw, query | `dblp` | `https://dblp.org/rec/{key}.html` | Canon for CS publication graph. Also a SPARQL endpoint. |
| **Papers With Code** | ML papers + code + SOTA benchmarks | ~500k papers | `https://paperswithcode.com/api/v1/` (OpenAPI) | none | polite | CC-BY-SA | raw, query | `pwc` | `https://paperswithcode.com/paper/{slug}` | Benchmarks endpoint uniquely valuable for agents. |
| **ACM Digital Library** | ACM pubs | ~700k | via CrossRef + `https://dl.acm.org/action/doSearch` (HTML) | none (meta via CrossRef) | — | CC-BY where OA | query (CrossRef) | `acm` | `https://doi.org/{doi}` | No dedicated public REST — route via CrossRef. |
| **IEEE Xplore** | IEEE metadata | ~6M | `https://developer.ieee.org/` (metadata API) | api-key (free, approval) | 200/day free | proprietary (meta summary OK) | query | `ieee` | `https://ieeexplore.ieee.org/document/{id}` | Metadata-only; full-text paywalled. |
| **USENIX (open)** | Systems/security papers | ~15k | HTML + CrossRef | none | — | CC-BY | raw (via CrossRef) | `usenix` | `https://www.usenix.org/{path}` | Most USENIX papers open — fetch PDF via DOI. |
| **CORE** | Open-access aggregator | ~290M articles, 30M full-text | `https://api.core.ac.uk/v3/` | api-key (free) | 10k/day | CC-BY where upstream OA | raw, query | `core` | `https://core.ac.uk/display/{id}` | OA full-text search — major asset for insight tier. |
| **OpenReview** | Peer review archives (ICLR, NeurIPS, etc.) | ~200k submissions | `https://api.openreview.net/` | none (polite) | polite | CC-BY | raw, query | `openreview` | `https://openreview.net/forum?id={id}` | Only public source of peer-review text at scale. |
| **Zenodo (CERN)** | Research outputs, datasets, code | ~5M records | `https://zenodo.org/api/records` | api-key optional | 100/min | per-record (often CC) | raw, query | `zenodo` | `https://zenodo.org/records/{id}` | Covers all sciences; CS + long-tail software prominent. |
| **Software Heritage** | Source code archive | ~20B files | `https://archive.softwareheritage.org/api/1/` | none | polite | CC-BY (meta) | raw | `swh` | `https://archive.softwareheritage.org/{swhid}` | For code citation via SWHID — unique asset. |
| **Microsoft Academic Graph** | ~sunset~ | — | — | — | — | — | sunset 2022 | — | — | **Do NOT integrate.** MAG shut down 2021-12-31. OpenAlex is the successor. |
| **Google Scholar** | — | — | — | — | — | — | **no public API** | — | — | **Do NOT integrate.** No API, ToS forbids scraping. |

### 1.5 — Biophysics / Life Sciences

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **PubMed (NCBI E-utilities)** *(already integrated)* | Biomed literature | ~37M | `https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi` | email-header polite; api-key optional (10/s) | 3/s no key, 10/s key | US-gov public domain (meta) | raw, query | `pubmed` | `https://pubmed.ncbi.nlm.nih.gov/{pmid}/` | Keep. Parser = `pubmed-style`. |
| **Europe PMC** | PubMed + preprints + Agricola + patents | ~43M (2026) | `https://www.ebi.ac.uk/europepmc/webservices/rest/search?query=...&format=json` | none | polite | mixed (per-record) | raw, query | `epmc` | `https://europepmc.org/article/{src}/{id}` | No API key, simpler than NCBI. Has annotation API for NER. Docs: https://europepmc.org/RestfulWebService |
| **bioRxiv / medRxiv** | Life-sci preprints | ~400k (bioRxiv) + ~60k (medRxiv) | `https://api.biorxiv.org/details/biorxiv/{doi}`, `/pubs/biorxiv/{interval}` | none | polite | CC-BY (most) | raw, query | `biorxiv` / `medrxiv` | `https://www.biorxiv.org/content/{doi}` | Covers both with same API shape. |
| **UniProt** | Protein sequences + annotation | ~250M entries | `https://rest.uniprot.org/uniprotkb/search?query=...&format=json` | none | polite | CC-BY | raw (`/{accession}`), query | `uniprot` | `https://www.uniprot.org/uniprotkb/{id}/entry` | Massive, well-versioned REST. |
| **Ensembl REST** | Genomes, variants, comparative genomics | — | `https://rest.ensembl.org/` | none | 15/s | Apache-2 | raw, query | `ensembl` | `https://www.ensembl.org/{species}/Gene/Summary?g={id}` | Species-parametric. |
| **NCBI GEO (Gene Expression Omnibus)** | Gene-expression datasets | ~200k series | E-utilities `db=gds` | email-header | 3/10/s | US-gov PD | raw, query | `geo` | `https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc={gse}` | Reuse pubmed parser with different `db`. |
| **OpenTargets** | Target–disease associations | — | `https://api.platform.opentargets.org/api/v4/graphql` | none | polite | CC0 | raw, query (GraphQL) | `opentargets` | `https://platform.opentargets.org/target/{ensgid}` | GraphQL — needs a new parser shape. |
| **BioModels** | Curated mathematical models of biology | ~3k curated, ~2M auto | `https://www.ebi.ac.uk/biomodels/search?query=...&format=json` | none | polite | CC0 | raw, query | `biomodels` | `https://www.ebi.ac.uk/biomodels/{id}` | SBML models, quantitative. |
| **Human Protein Atlas** | Protein expression in tissues | ~20k proteins | `https://www.proteinatlas.org/api/search_download.php` | none | polite | CC-BY-SA | query | `hpa` | `https://www.proteinatlas.org/{ensg}-{gene}` | Tab-delimited export; no pure JSON. |
| **RCSB PDB** | *(see Chemistry)* | — | — | — | — | — | — | `pdb` | — | Cross-branch. |
| **MGI (Mouse Genome Informatics)** | Mouse genetics | — | `http://www.informatics.jax.org/` (limited API) | none | polite | CC-BY | raw | `mgi` | `http://www.informatics.jax.org/marker/{id}` | Partial API — scrape fallback. |
| **ClinicalTrials.gov v2** *(already integrated)* | Clinical trials | ~500k studies | `https://clinicaltrials.gov/api/v2/studies` | none | polite | US-gov PD | raw, query | `clinicaltrials` | `https://clinicaltrials.gov/study/{nctid}` | Keep. V1 deprecated 2024-06; ensure using v2. |
| **PubMed Central (PMC OA subset)** | OA full-text biomed | ~9M OA | E-utilities + `https://www.ncbi.nlm.nih.gov/pmc/oai/oai.cgi` | email-header | 3/10/s | per-record (CC-BY dominant) | raw (full-text XML), query | `pmc` | `https://www.ncbi.nlm.nih.gov/pmc/articles/{pmcid}/` | Full-text XML; big win for insight tier. |
| **DrugBank (open)** | Drug + drug-target | ~14k drugs | `https://go.drugbank.com/releases/latest/downloads/api` | paid (commercial) | contract | proprietary (open subset CC-BY-NC-SA) | raw (open subset only) | `drugbank` | `https://go.drugbank.com/drugs/{id}` | Watch the NC clause — commercial gateway may be restricted. |

### 1.6 — Cosmology / Astronomy / Space

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **NASA ADS** *(see Physics)* | Astro literature | ~16M | — | api-key | 5000/day | CC-BY | raw, query | `ads` | `https://ui.adsabs.harvard.edu/abs/{bibcode}` | Primary astro bibliographic source. |
| **SIMBAD** | Astronomical objects, identifiers, basic data | ~17M objects | `http://simbad.u-strasbg.fr/simbad/sim-id?Ident={name}&output.format=ASCII`; TAP at `http://simbad.u-strasbg.fr/simbad/sim-tap` | none | polite | CDS terms (CC-BY) | raw, query | `simbad` | `http://simbad.u-strasbg.fr/simbad/sim-id?Ident={id}` | TAP = ADQL SQL over astro catalogs. |
| **VizieR** | Astronomical catalogs aggregator | ~23k catalogs | `https://vizier.u-strasbg.fr/viz-bin/votable`, TAP at `http://tapvizier.u-strasbg.fr/TAPVizieR/tap` | none | polite | per-catalog | raw, query | `vizier` | `https://vizier.u-strasbg.fr/viz-bin/VizieR-S?{id}` | TAP/ADQL over 23k catalogs — huge leverage. |
| **NED (NASA/IPAC Extragalactic Database)** | Extragalactic objects | ~1B objects | `https://ned.ipac.caltech.edu/cgi-bin/objsearch?...` + new NED-REST | none | polite | NASA terms (PD-ish) | raw, query | `ned` | `https://ned.ipac.caltech.edu/byname?objname={name}` | XML + JSON. |
| **MAST (Space Telescope)** | HST, JWST, TESS, Kepler archives | petabytes | `https://mast.stsci.edu/api/v0/invoke`, `/api/v0.1/Download/` | none (token for large) | polite | PD (NASA) | raw, query | `mast` | `https://mast.stsci.edu/api/v0.1/Download/file?uri={uri}` | Data access — stream URIs not JSON. |
| **Gaia archive (ESA)** | Gaia DR3 + future | 1.8B sources | TAP at `https://gea.esac.esa.int/tap-server/tap` | none | polite | ESA CC-BY-SA | query (ADQL) | `gaia` | `https://gea.esac.esa.int/archive/` | ADQL. |
| **ESASky / ESO archive** | ESO telescopes, archives | petabytes | `https://archive.eso.org/programmatic/` (TAP) | none (token for downloads) | polite | ESO terms | query | `eso` | `https://archive.eso.org/dataset/{id}` | TAP-based. |
| **JPL Horizons** | Solar-system ephemerides | — | `https://ssd.jpl.nasa.gov/api/horizons.api` | none | polite | NASA PD | raw | `horizons` | `https://ssd.jpl.nasa.gov/horizons/` | Deterministic physics — natural `insight` helper. |
| **NASA Exoplanet Archive** | Confirmed exoplanets | 5k+ | `https://exoplanetarchive.ipac.caltech.edu/TAP/sync?query=...` | none | polite | NASA PD | query (ADQL) | `nea` | `https://exoplanetarchive.ipac.caltech.edu/overview/{name}` | TAP. |
| **IVOA VO Registry** | Astro service discovery | — | `https://registry.euro-vo.org/tap` | none | polite | IVOA terms | meta-query | `ivoa` | — | Discovery layer — lists the above and hundreds more. |

### 1.7 — Mind / Neuroscience / Psychology

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **PubMed (neuro slice)** | via MeSH `Nervous System`, `Psychology` filters | — | E-utilities | email-header | 3/10/s | PD | raw, query | `pubmed` | — | Reuse pubmed parser with filter. |
| **PsyArXiv (OSF)** | Psychology preprints | ~50k | `https://api.osf.io/v2/preprints/?filter[provider]=psyarxiv` | none | 1/s | CC-BY (most) | raw, query | `psyarxiv` | `https://osf.io/preprints/psyarxiv/{id}` | OSF-hosted. |
| **OSF API (full)** | Open science projects across fields | ~500k projects | `https://api.osf.io/v2/` | api-key optional | 100/s auth | CC-BY | raw, query | `osf` | `https://osf.io/{id}` | Superset of PsyArXiv; covers all preprint servers on OSF (SocArXiv, BITSS, etc.). |
| **OpenNeuro** | MRI/EEG/MEG datasets | ~1k datasets | `https://openneuro.org/crn/graphql` | none | polite | CC0 | raw, query (GraphQL) | `openneuro` | `https://openneuro.org/datasets/{id}` | BIDS-format datasets. |
| **Neurosynth** | fMRI meta-analysis | ~15k studies | `https://neurosynth.org/api/v1/` (limited) | none | polite | CC0 | raw, query | `neurosynth` | `https://neurosynth.org/studies/{id}` | Recently migrating → NeuroVault + NiMARE. |
| **NeuroVault** | Unthresholded statistical brain maps | ~50k maps | `https://neurovault.org/api/` | none | polite | CC0 | raw, query | `neurovault` | `https://neurovault.org/images/{id}` | Clean DRF API. |
| **BrainMap** | fMRI coordinate meta | ~20k papers | Sleuth desktop + limited REST | api-key (request) | polite | research-only | raw | `brainmap` | — | Legal posture: research use only. |
| **Allen Brain Atlas** | Mouse + human brain expression, connectivity, cell types | petabytes | `http://api.brain-map.org/api/v2/data/query.json` | none | polite | Allen Institute Terms (mostly CC-BY-NC) | raw, query | `allen` | `http://atlas.brain-map.org/{id}` | RMA query language — new parser shape. |
| **Cognitive Atlas** | Ontology of mental processes | ~800 concepts | `https://www.cognitiveatlas.org/api/v-alpha/` | none | polite | CC-BY | raw | `cogatlas` | `https://www.cognitiveatlas.org/concept/id/{id}` | Small but canonical. |
| **DANDI Archive** | Neurophysiology datasets (NWB) | ~500 dandisets | `https://api.dandiarchive.org/api/` | none | polite | CC-BY | raw, query | `dandi` | `https://dandiarchive.org/dandiset/{id}` | NIH-funded, BRAIN Initiative. |

### 1.8 — Earth Sciences

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **NASA CMR (Common Metadata Repository)** | NASA Earth-observation granules, collections | ~10B granules | `https://cmr.earthdata.nasa.gov/search/collections.json?keyword=...` | none (EDL token for downloads) | polite | NASA PD | query | `cmr` | `https://cmr.earthdata.nasa.gov/search/concepts/{id}` | CMR Search API — discovery layer for all NASA EOSDIS. |
| **USGS Earth Explorer (M2M)** | Landsat, hyperspectral, aerial | petabytes | `https://m2m.cr.usgs.gov/api/api/json/stable/` | api-key (free w/ account) | polite | US-gov PD | query, raw | `usgs-ee` | `https://earthexplorer.usgs.gov/scene/metadata/full/{id}/{did}/` | Auth token rotates. |
| **USGS ScienceBase** | Scientific data catalog | ~2M items | `https://www.sciencebase.gov/catalog/items?format=json` | none | polite | US-gov PD | query, raw | `sciencebase` | `https://www.sciencebase.gov/catalog/item/{id}` | |
| **NOAA NCEI** | Weather/climate records | petabytes | `https://www.ncei.noaa.gov/access/services/data/v1` | token (free) | polite | US-gov PD | query | `ncei` | `https://www.ncei.noaa.gov/access/metadata/landing-page/bin/iso?id={id}` | Multiple services (CDO, NCEP, etc.). |
| **NOAA PSL** | Climate reanalyses | — | OPeNDAP + THREDDS | none | polite | US-gov PD | raw (grid) | `noaa-psl` | `https://psl.noaa.gov/data/gridded/{dataset}` | Not JSON — grids. Insight-tier helper only. |
| **Copernicus CDS / ADS** | Climate + atmosphere (ERA5, CAMS) | petabytes | `https://cds.climate.copernicus.eu/api/v2` | api-key (free, account) | polite | Copernicus license (free redistribute w/ attribution) | query, raw | `copernicus-cds` | `https://cds.climate.copernicus.eu/datasets/{id}` | Queue-based (jobs). Not synchronous. |
| **PANGAEA** | Earth & env data publisher | ~400k datasets | `https://www.pangaea.de/advanced/search.php?q=...&format=json` | none | polite | CC-BY (most) | query, raw | `pangaea` | `https://doi.pangaea.de/{doi}` | DOI-cited. |
| **GBIF** | Biodiversity occurrences | ~3B records | `https://api.gbif.org/v1/` | none | polite | CC-BY / CC0 | raw, query | `gbif` | `https://www.gbif.org/occurrence/{id}` | Species/taxonomy canonical. |
| **OBIS** | Marine biodiversity | ~130M | `https://api.obis.org/v3/` | none | polite | CC-BY | raw, query | `obis` | `https://obis.org/occurrence/{id}` | Marine-slice of GBIF, but independent. |
| **EPA Envirofacts** | US EPA regulatory data | — | `https://data.epa.gov/efservice/{table}/{field}/{value}/json` | none | polite | US-gov PD | query | `epa` | `https://enviro.epa.gov/` | Composable URL API — quirky. |
| **Sentinel Hub** | Copernicus Sentinel imagery | petabytes | `https://services.sentinel-hub.com/api/v1/` | oauth | paid tiers | Copernicus | query, raw | `sentinel` | — | Paid above eval tier. |
| **Planet Labs** | Commercial EO | — | `https://api.planet.com/` | oauth, paid | contract | proprietary | ⚠️ skip | — | — | Paid-only — defer. |
| **OpenStreetMap Overpass** | OSM features | 9B nodes | `https://overpass-api.de/api/interpreter` | none | polite (strict) | ODbL | query | `osm` | `https://www.openstreetmap.org/{type}/{id}` | Overpass QL — new parser shape. |
| **WorldClim** | Climate layers | static | HTTPS downloads | none | polite | CC-BY | raw | `worldclim` | `https://worldclim.org/data/{version}` | Static files; best as insight helper. |
| **IRIS (Incorporated Research Institutions for Seismology)** *now EarthScope* | Seismograms | petabytes | `https://service.iris.edu/fdsnws/` | none | polite | PD | query, raw | `iris` | — | FDSN standard shared across global seismo networks. |

### 1.9 — Cross-cutting discovery APIs

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit (free) | License | Tier fit | source_prefix | Canonical URL template | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **CrossRef** | DOI metadata, refs, funders | ~160M works | `https://api.crossref.org/works?query=...` | none (email polite = "mailto=...") | 50/s polite pool | CC0 | raw (`/works/{doi}`), query | `crossref` | `https://doi.org/{doi}` | Polite pool header `User-Agent: ...mailto:gian@agfarms.dev`. |
| **DataCite** | DOI metadata for datasets | ~65M | `https://api.datacite.org/dois` | none | polite | CC0 | raw, query | `datacite` | `https://doi.org/{doi}` | Dataset/software DOIs (complement to CrossRef). |
| **OpenAlex** *(already integrated)* | Unified research graph (works, authors, venues, institutions, concepts) | ~260M works | `https://api.openalex.org/works?search=...` | email-header polite | 100k/day polite | CC0 | raw, query | `openalex` | `https://openalex.org/{id}` | Keep. MAG successor. Parser = `openalex-style`. |
| **CORE** | OA aggregator (full-text) | ~290M | `https://api.core.ac.uk/v3/` | api-key free | 10k/day | CC-BY per-record | raw, query | `core` | `https://core.ac.uk/display/{id}` | Gives *full text* not just metadata. |
| **BASE (Bielefeld)** | OA discovery | ~400M | `https://api.base-search.net/cgi-bin/BaseHttpSearchInterface.fcgi` | ip-whitelist (free on request) | contract | meta-varies | query | `base` | — | Less friendly than CORE; use only if CORE gaps. |
| **Unpaywall** | OA version lookup for any DOI | — | `https://api.unpaywall.org/v2/{doi}?email=...` | email-param | 100k/day | CC0 | raw | `unpaywall` | — | Resolver; returns best OA URL. Essential companion. |
| **OpenAIRE Graph** | EU-funded OA research graph | ~140M | `https://api.openaire.eu/search/` + graph API `https://graph.openaire.eu/` | none (key optional) | polite | CC-BY | raw, query | `openaire` | `https://explore.openaire.eu/search/publication?pid={doi}` | Funding-aware. |
| **Europe PMC** *(also life-sci)* | — | — | — | — | — | — | — | `epmc` | — | Crosses 1.5 and 1.9. |
| **Dimensions** | Commercial research graph | ~140M | `https://app.dimensions.ai/api/` | oauth, paid | contract | proprietary | ⚠️ skip default | — | — | Paid — only if Gian has licence. |
| **Lens.org** | Patents + scholarly | ~260M scholar + 140M patent | `https://api.lens.org/` | api-key (free tier, limited) | 1000/mo free | mixed | query | `lens` | `https://www.lens.org/lens/scholar/article/{id}` | Rare legitimate patent + scholar link. |
| **ORCID Public API** | Author identifiers | ~20M | `https://pub.orcid.org/v3.0/{orcid}` | none (2-legged oauth for bulk) | polite | CC0 | raw | `orcid` | `https://orcid.org/{id}` | Person-graph primitive. |
| **ROR (Research Org Registry)** | Institution identifiers | ~110k | `https://api.ror.org/organizations` | none | polite | CC0 | raw, query | `ror` | `https://ror.org/{id}` | Institution-graph primitive. |
| **Wikidata** | Structured knowledge graph (everything) | ~110M items | `https://www.wikidata.org/w/api.php`; SPARQL at `https://query.wikidata.org/sparql` | none | 5/s SPARQL polite | CC0 | raw, query (SPARQL) | `wikidata` | `https://www.wikidata.org/wiki/{Qid}` | SPARQL = power tool. Insight-tier heavy user. |
| **Wikipedia REST** | Encyclopedia articles, summaries, refs | ~60M articles all langs | `https://en.wikipedia.org/api/rest_v1/page/summary/{title}` | polite user-agent required | 200/s w/ UA | CC-BY-SA | raw, query | `wikipedia` | `https://en.wikipedia.org/wiki/{title}` | Must send a descriptive `User-Agent`. |
| **DOAJ** | Directory of Open Access Journals | ~20k journals | `https://doaj.org/api/` | none | polite | CC-BY | query | `doaj` | `https://doaj.org/article/{id}` | Journal + article. |
| **DOAB** | Directory of OA Books | ~80k books | `https://directory.doabooks.org/rest/` | none | polite | CC-BY | query | `doab` | `https://directory.doabooks.org/handle/{handle}` | OA monographs. |
| **OpenCitations** | Citation graph | ~2B citations | `https://opencitations.net/index/coci/api/v1/` | none | polite | CC0 | raw, query | `opencitations` | `https://opencitations.net/index/coci/api/v1/references/{doi}` | Citation relations as data. |
| **Scite.ai** | Smart-citation classification (supporting/contrasting) | ~1.2B citations | `https://api.scite.ai/` | api-key, paid | contract | proprietary | ⚠️ skip default | — | — | Paid — defer. |
| **Connected Papers** | Citation-graph visualization | — | no official public API | — | — | — | **no API** | — | — | Skip — HTML only, ToS forbids scraping. |
| **Altmetric** | Social attention scoring | — | `https://api.altmetric.com/v1/` (free tier), commercial above | api-key | 1/s free | proprietary | query | `altmetric` | `https://www.altmetric.com/details/{id}` | Useful adjunct for insight tier. |
| **Semantic Scholar (S2) API** *(already integrated)* | Research graph + TLDRs + embeddings | ~220M papers | `https://api.semanticscholar.org/graph/v1/` | api-key optional | 1/s anon, 100/s keyed | CC-BY-NC (TLDRs), ODC-BY (meta) | raw, query | `s2` | `https://www.semanticscholar.org/paper/{id}` | Keep. TLDRs useful in insight tier. |

---

## Section 2 — Grey-literature retrieval methods (separate legal column)

**Posture disclaimer (read first).** The services below distribute full-text scholarly content without consent from copyright holders in most cases. Courts in the US, EU, India, and Russia have issued injunctions or site-blocks against some of them at various times; operating domains rotate. State-of-law varies by jurisdiction and changes frequently. **A public-facing paid gateway that proxies their full-text is high legal risk** and can trigger publisher DMCA, payment-rail termination, and personal liability. A neutral, product-engineering read follows.

| Name | Domain coverage | Corpus size | Base URL / endpoint | Auth | Rate limit | Legal status | Access method | Uptime (2026-04) | Recommended feed402 posture |
|---|---|---|---|---|---|---|---|---|---|
| **Sci-Hub** | Scholarly articles (esp. paywalled) | ~90M papers | rotating mirrors: `sci-hub.ru`, `sci-hub.se`, `sci-hub.st` | none | aggressive 429 | Not authorized in most jurisdictions. Elsevier v. Sci-Hub (SDNY 2017) default judgment; ACS v. Sci-Hub (ED Va 2017) similar; India case (Delhi HC 2020–ongoing) unresolved; site-blocked in UK, France, Germany, Austria, Belgium, Italy, Portugal, Russia (2022), Sweden, Netherlands. Operator Alexandra Elbakyan identified; US criminal investigation reported 2021. New upload paused since Dec 2020. | DOI → PDF via `https://sci-hub.XX/{doi}` (captcha + JS); parser must handle rotating captcha | .ru most reliable; .se intermittent; .st up 2026-04 | **Do NOT proxy full-text.** If included at all, emit a signed "attempted resolution" envelope = `{doi, sci_hub_has_record: bool, retrieved_at: null, legal_notice: "..."}` with no body. Recommend: **exclude entirely from gateway**. Use Unpaywall + CORE + EuropePMC for legitimate OA instead. |
| **LibGen (Library Genesis)** | Books + scholarly + comics + fiction | ~4M books + ~90M papers (scimag) | `libgen.is`, `libgen.rs`, `libgen.li`, `libgen.gs` (rotating) | none | moderate | Not authorized. Multiple publisher suits; Elsevier v. LibGen default judgment 2015; various site-blocks. | MD5 hash → direct download; ISBN → record; scimag collection = Sci-Hub mirror | .is most reliable; .rs intermittent; .gs up 2026-04 | **Do NOT proxy.** Metadata lookups (ISBN → record) are lower risk than full-text proxying but still reputationally/legally exposed. **Exclude by default.** If a user needs a book record, route via **OpenLibrary** (legitimate). |
| **Anna's Archive** | Unified search over LibGen + Sci-Hub + Z-Library + SciDB | ~63M books + ~95M papers (2026-03 per their stats page) | `annas-archive.org`, `.gd`, `.li`, `.se` mirrors | API via paid membership (Bookworm tier+, key at `/account`) | 25 fast DL/day (Bookworm) | Not authorized. Aggregates the above shadow libraries; DMCA-hostile hosting; torrent-seed distribution of corpus via `annas-archive.org/torrents` (~1.1 PB). Operator pseudonymous. No major named judgment as of 2026-04 but publisher industry actively pursuing. | JSON API key: `GET /dyn/api/fast_download.json?md5={md5}&key={key}`; SciDB: `/scidb/{doi}` | .org blocked various; `.gl` `.gd` `.se` rotating; uptime high among the shadow libraries | **Do NOT proxy full-text.** Metadata search (no download URL in response) is *lower* risk — could be surfaced as "discovery only, no content delivered" *if* founder accepts reputational risk. Recommend: **exclude from the gateway by default**; allow a documented, opt-in, off-by-default route that returns metadata + signed "not retrieved" envelope. |
| **Z-Library** | Books (emphasis) + articles | ~13M books | rotating TLDs, TOR hidden services, personal-user domains | account | per-account | Not authorized. US DOJ indictment Nov 2022 of two alleged operators; domain seizure Nov 2022; rebuilt via personal-domain + TOR model; operating. Notable publisher suits pending. | account login + HTML scrape, no stable JSON API | unstable; changes monthly | **Exclude.** The personal-domain-per-user model means no stable endpoint to route to; zero upside for a gateway. |
| **Library.lol / 1lib.* / book4you / etc.** | Z-Lib mirrors & forks | varies | rotating | varies | varies | Same posture as Z-Library. | varies | unstable | **Exclude.** |

**Summary recommendation (Section 2):** exclude grey-literature from the x402 gateway by default. The value prop "one paywall in front of every research source" is better served by *expanding OA coverage* (CORE, Europe PMC, Unpaywall, OpenAIRE, bioRxiv, arXiv, Zenodo) than by proxying shadow libraries. If a "full-text requested" signal is needed for agent workflows, emit a neutral `{status: "not_available_open_access", unpaywall_checked: true, suggestions: [...]}` envelope — no shadow-lib proxying.

---

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
