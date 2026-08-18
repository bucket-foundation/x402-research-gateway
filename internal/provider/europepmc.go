package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/citation"
	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// Europe PMC adapters (x402-research-gateway#26).
//
// Verified against europepmc.org/RestfulWebService on 2026-08-17:
//
//	GET /europepmc/webservices/rest/search?query=…&format=json
//	    &pageSize=&cursorMark=&resultType=core
//	GET /europepmc/webservices/rest/{source}/{id}/references?format=json
//	GET /europepmc/webservices/rest/{source}/{id}/citations?format=json
//	GET /europepmc/webservices/rest/{source}/{id}/fullTextXML
//
// Base https://www.ebi.ac.uk/europepmc/webservices/rest. No API key.
// Published guidance is around 30 requests per minute, so the route carries
// a generous timeout and the operator is expected to cache.
//
// Pagination is cursor-based on `cursorMark`, with `*` starting a scan and
// the response's `nextCursorMark` continuing it.
//
// Rights: the open-access subset contains several licences, and some
// records are free to read without being redistributable. Those are
// different facts, so `isOpenAccess` and `license` are read per record and
// never merged into one provider-level string.

type epmcSearchBody struct {
	HitCount       int    `json:"hitCount"`
	NextCursorMark string `json:"nextCursorMark"`
	ResultList     struct {
		Result []json.RawMessage `json:"result"`
	} `json:"resultList"`
}

type epmcResult struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	PMID         string `json:"pmid"`
	PMCID        string `json:"pmcid"`
	DOI          string `json:"doi"`
	Title        string `json:"title"`
	AuthorString string `json:"authorString"`
	PubYear      string `json:"pubYear"`
	IsOpenAccess string `json:"isOpenAccess"`
	InEPMC       string `json:"inEPMC"`
	InPMC        string `json:"inPMC"`
	License      string `json:"license"`
	PubType      string `json:"pubType"`
	// CommentCorrectionList carries the published-version relation Europe
	// PMC declares on a preprint record.
	CommentCorrectionList struct {
		CommentCorrection []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"commentCorrection"`
	} `json:"commentCorrectionList"`
}

// EuropePMCNormalizer parses the search response, keeping each result's raw
// bytes.
type EuropePMCNormalizer struct{}

func (EuropePMCNormalizer) Normalize(body []byte) []NormalizedRecord {
	var parsed epmcSearchBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(parsed.ResultList.Result))
	for _, raw := range parsed.ResultList.Result {
		var r epmcResult
		if err := json.Unmarshal(raw, &r); err != nil || r.ID == "" {
			continue
		}
		// The record id is source:id, which is how Europe PMC addresses a
		// record and what its own endpoints take back.
		id := r.ID
		if r.Source != "" {
			id = r.Source + ":" + r.ID
		}
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://europepmc.org/article/" + strings.ToUpper(r.Source) + "/" + r.ID,
			Raw:          raw,
		})
	}
	return recs
}

type epmcCursorPagination struct{}

func (epmcCursorPagination) PaginationModel() string { return "cursor" }

type epmcFetchByID struct{}

func (epmcFetchByID) IdentifierSchemes() []string { return []string{"pmid", "pmcid", "doi"} }

type epmcIdentity struct{}

func (epmcIdentity) parse(rec NormalizedRecord) (epmcResult, bool) {
	var r epmcResult
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := json.Unmarshal(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (e epmcIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	r, ok := e.parse(rec)
	if !ok {
		return nil
	}
	var out []identity.Identifier
	out = appendID(out, identity.SchemePMID, r.PMID)
	out = appendID(out, identity.SchemePMCID, r.PMCID)
	out = appendID(out, identity.SchemeDOI, r.DOI)
	return out
}

// AssertedRelations surfaces the preprint-to-published relations Europe PMC
// publishes in commentCorrectionList. Only the types this mapping knows
// become typed relations; an unmapped type is dropped rather than guessed.
func (e epmcIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	r, ok := e.parse(rec)
	if !ok {
		return nil
	}
	ev := identity.ProviderAsserted("europepmc", at)
	var out []identity.Relation
	for _, cc := range r.CommentCorrectionList.CommentCorrection {
		if cc.ID == "" {
			continue
		}
		var rel identity.RelationType
		switch strings.ToLower(strings.TrimSpace(cc.Type)) {
		case "preprint of this article", "preprint":
			rel = identity.RelPreprintOf
		case "retraction in", "retracted publication":
			rel = identity.RelRetracts
		case "erratum in", "corrected and republished in", "correction":
			rel = identity.RelCorrects
		default:
			continue
		}
		out = append(out, identity.Relation{
			From: nodeID, To: "pmid:" + cc.ID, Type: rel, Evidence: ev,
		})
	}
	return out
}

func (e epmcIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := e.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: r.Title, Year: atoiSafe(r.PubYear)}
	for _, a := range strings.Split(r.AuthorString, ",") {
		if n := strings.TrimSpace(strings.TrimSuffix(a, ".")); n != "" {
			d.Authors = append(d.Authors, n)
		}
	}
	return d
}

// RecordRights reads the per-record rights. Free to read and open to
// redistribute are separate facts here: `isOpenAccess` and `inEPMC` say the
// content is readable, and only the `license` field says anything about
// redistribution. A record with no licence reports unknown even when it is
// openly readable, because readable is not redistributable.
func (e epmcIdentity) RecordRights(rec NormalizedRecord) Rights {
	r, ok := e.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "europepmc (unparseable record)"}
	}
	rights := Rights{
		License:        r.License,
		Redistribution: RedistributionUnknown,
		Source:         "europepmc:license",
		FreeToRead:     strings.EqualFold(r.IsOpenAccess, "Y") || strings.EqualFold(r.InEPMC, "Y"),
	}
	switch strings.ToLower(strings.TrimSpace(r.License)) {
	case "cc0", "cc by", "cc-by", "cc by-sa", "cc by-nc", "cc by-nc-sa", "cc by-nc-nd", "cc by-nd":
		rights.Redistribution = RedistributionAllowed
	}
	if rights.License == "" {
		rights.Source = "europepmc:license (absent)"
	}
	return rights
}

// Assets reports the representations Europe PMC holds for a record. The
// full-text XML representation appears only when the record is in Europe
// PMC's own full-text store, and every representation carries the record's
// own rights rather than a provider default.
func (e epmcIdentity) Assets(rec NormalizedRecord) []Asset {
	r, ok := e.parse(rec)
	if !ok {
		return nil
	}
	rights := e.RecordRights(rec)
	base := "https://www.ebi.ac.uk/europepmc/webservices/rest/" + r.Source + "/" + r.ID
	assets := []Asset{{
		AssetID:        "epmc:" + rec.ID + "#abstract",
		Representation: "text/html; role=abstract",
		CanonicalURL:   rec.CanonicalURL,
		Rights:         rights,
	}}
	if strings.EqualFold(r.InEPMC, "Y") || strings.EqualFold(r.InPMC, "Y") {
		// JATS XML, structure preserved. The gateway surfaces the address
		// of the structured document rather than flattening it to a blob,
		// because the structure is the reason to want it.
		assets = append(assets, Asset{
			AssetID:        "epmc:" + rec.ID + "#fulltext-xml",
			Representation: "application/xml; role=full-text; schema=JATS",
			CanonicalURL:   base + "/fullTextXML",
			Rights:         rights,
		})
	}
	if r.PMCID != "" {
		assets = append(assets, Asset{
			AssetID:        "epmc:" + rec.ID + "#pdf",
			Representation: "application/pdf",
			CanonicalURL:   "https://europepmc.org/articles/" + r.PMCID + "?pdf=render",
			Rights:         rights,
		})
	}
	return assets
}

type epmcSync struct{}

func (epmcSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: true, Incremental: true}
}

// ---------- Citation graph ----------

// epmcCitationGraph serves both directions off the article endpoints. The
// response is a list of far-end records; Europe PMC returns `id` plus
// `source` per entry.
type epmcCitationGraph struct {
	direction citation.Direction
}

func (e epmcCitationGraph) Direction() citation.Direction { return e.direction }

// epmcSourceFor renders an identifier as the source/id pair the article
// endpoints take. PMIDs address the MED source and PMCIDs the PMC source.
func epmcSourceFor(id identity.Identifier) (source, value string, ok bool) {
	switch id.Scheme {
	case identity.SchemePMID:
		return "MED", id.Value, true
	case identity.SchemePMCID:
		return "PMC", strings.TrimPrefix(id.Value, "PMC"), true
	default:
		return "", "", false
	}
}

func (e epmcCitationGraph) EdgeQuery(id identity.Identifier) (map[string]string, bool) {
	source, value, ok := epmcSourceFor(id)
	if !ok || value == "" {
		return nil, false
	}
	return map[string]string{
		"source": source, "id": value, "format": "json", "pageSize": "100",
	}, true
}

type epmcEdgeBody struct {
	HitCount     int `json:"hitCount"`
	CitationList struct {
		Citation []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			DOI    string `json:"doi"`
		} `json:"citation"`
	} `json:"citationList"`
	ReferenceList struct {
		Reference []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			DOI    string `json:"doi"`
		} `json:"reference"`
	} `json:"referenceList"`
}

func (e epmcCitationGraph) Edges(query identity.Identifier, body []byte, at time.Time) []citation.Edge {
	var b epmcEdgeBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil
	}
	type far struct{ id, source, doi string }
	var fars []far
	for _, c := range b.CitationList.Citation {
		fars = append(fars, far{c.ID, c.Source, c.DOI})
	}
	for _, r := range b.ReferenceList.Reference {
		fars = append(fars, far{r.ID, r.Source, r.DOI})
	}

	queryEnd := citation.Endpoint{
		Identifiers: []identity.Identifier{query},
		RawID:       query.Raw,
	}
	stamp := at.UTC().Format(time.RFC3339)
	var edges []citation.Edge
	for _, f := range fars {
		end := citation.Endpoint{RawID: f.source + ":" + f.id}
		if strings.EqualFold(f.source, "MED") {
			end.Identifiers = appendID(end.Identifiers, identity.SchemePMID, f.id)
		}
		end.Identifiers = appendID(end.Identifiers, identity.SchemeDOI, f.doi)
		if len(end.Identifiers) == 0 && f.id == "" {
			continue
		}
		edge := citation.Edge{Direction: e.direction, RetrievedAt: stamp}
		if e.direction == citation.DirectionReferences {
			edge.Source, edge.Target = queryEnd, end
		} else {
			edge.Source, edge.Target = end, queryEnd
		}
		edges = append(edges, edge)
	}
	return edges
}

func (e epmcCitationGraph) EdgePagination(body []byte) (string, bool, string) {
	var b epmcEdgeBody
	if err := json.Unmarshal(body, &b); err != nil {
		return "page", false, ""
	}
	got := len(b.CitationList.Citation) + len(b.ReferenceList.Reference)
	return "page", b.HitCount > got, ""
}

// Coverage names what answered, so a zero edge count reads as this
// collection's view rather than as an uncited work.
func (epmcCitationGraph) Coverage() string {
	return "Europe PMC citation network over its indexed corpus, which covers PubMed plus " +
		"preprints and the PMC open-access subset. Verified 2026-08-17."
}

// EuropePMCSearchAdapter backs route ID "epmc-search".
var EuropePMCSearchAdapter = &Adapter{
	ID:                   "epmc-search",
	Description:          "Europe PMC search across life-science literature, preprints, and the PMC open-access subset.",
	Searcher:             epmcCursorPagination{},
	Normalizer:           EuropePMCNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     epmcIdentity{},
	DescriptorProvider:   epmcIdentity{},
	AssetProvider:        epmcIdentity{},
	RecordRightsProvider: epmcIdentity{},
	SyncProvider:         epmcSync{},
}

// EuropePMCFetchAdapter backs route ID "epmc-fetch".
var EuropePMCFetchAdapter = &Adapter{
	ID:                   "epmc-fetch",
	Description:          "Europe PMC single record by PMID, PMCID, or DOI.",
	Fetcher:              epmcFetchByID{},
	Normalizer:           EuropePMCNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     epmcIdentity{},
	DescriptorProvider:   epmcIdentity{},
	AssetProvider:        epmcIdentity{},
	RecordRightsProvider: epmcIdentity{},
	SyncProvider:         epmcSync{},
}

// EuropePMCReferencesAdapter backs route ID "epmc-references".
var EuropePMCReferencesAdapter = &Adapter{
	ID:                    "epmc-references",
	Description:           "Europe PMC outbound references for an article.",
	CitationGraphProvider: epmcCitationGraph{direction: citation.DirectionReferences},
}

// EuropePMCCitedByAdapter backs route ID "epmc-cited-by".
var EuropePMCCitedByAdapter = &Adapter{
	ID:                    "epmc-cited-by",
	Description:           "Europe PMC inbound citations for an article.",
	CitationGraphProvider: epmcCitationGraph{direction: citation.DirectionCitedBy},
}
