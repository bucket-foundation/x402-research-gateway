package provider

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// CORE adapter (x402-research-gateway#8).
//
// CORE aggregates open-access full text from repositories worldwide,
// roughly 290M records with full text for about 30M of them, which is why
// RESEARCH-INDEX.md ranks it the largest single lever for asset discovery.
//
//	GET https://api.core.ac.uk/v3/search/works?q=…&limit=&offset=
//	GET https://api.core.ac.uk/v3/works/{id}
//
// Access needs a free API key, sent as `Authorization: Bearer …`. The
// gateway holds no key of its own: the route supplies it from the
// operator's environment through config, and the route is absent from a
// deployment where the operator set no key. No key is embedded here, and
// none is ever echoed into a response.
//
// Rights: CORE aggregates from repositories whose deposit terms it does not
// restate uniformly. A record's own `license` field is the only statement
// this adapter treats as one; an absent licence reports unknown, which
// permits nothing, even though the record is free to read. Free to read and
// free to redistribute are different facts and CORE is exactly the provider
// where conflating them would do damage.

type coreWork struct {
	ID    json.RawMessage `json:"id"`
	DOI   string          `json:"doi"`
	Title string          `json:"title"`
	// YearPublished is an int on most records and absent on some.
	YearPublished int `json:"yearPublished"`
	Authors       []struct {
		Name string `json:"name"`
	} `json:"authors"`
	License string `json:"license"`
	// DownloadURL is CORE's own copy of the full text when it holds one.
	DownloadURL string `json:"downloadUrl"`
	// SourceFulltextUrls are the repository locations CORE harvested from.
	SourceFulltextUrls []string `json:"sourceFulltextUrls"`
	Links              []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"links"`
	FullText string `json:"fullText"`
}

func (w coreWork) id() string {
	if len(w.ID) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(w.ID, &s); err == nil {
		return s
	}
	var n int64
	if err := json.Unmarshal(w.ID, &n); err == nil {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

// CORENormalizer handles the search list shape and the single-work shape.
type CORENormalizer struct{}

func (CORENormalizer) Normalize(body []byte) []NormalizedRecord {
	var list struct {
		Results []json.RawMessage `json:"results"`
	}
	items := []json.RawMessage{}
	if err := json.Unmarshal(body, &list); err == nil && len(list.Results) > 0 {
		items = list.Results
	} else {
		var single json.RawMessage = body
		var probe coreWork
		if err := json.Unmarshal(body, &probe); err != nil || probe.id() == "" {
			return nil
		}
		items = []json.RawMessage{single}
	}
	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var w coreWork
		if err := json.Unmarshal(raw, &w); err != nil {
			continue
		}
		id := w.id()
		if id == "" {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://core.ac.uk/works/" + id,
			Raw:          raw,
		})
	}
	return recs
}

type coreOffsetPagination struct{}

func (coreOffsetPagination) PaginationModel() string { return "offset" }

type coreFetchByID struct{}

func (coreFetchByID) IdentifierSchemes() []string { return []string{"doi", "core"} }

type coreIdentity struct{}

func (coreIdentity) parse(rec NormalizedRecord) (coreWork, bool) {
	var w coreWork
	if len(rec.Raw) == 0 {
		return w, false
	}
	if err := json.Unmarshal(rec.Raw, &w); err != nil {
		return w, false
	}
	return w, true
}

func (c coreIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	w, ok := c.parse(rec)
	if !ok {
		return nil
	}
	return appendID(nil, identity.SchemeDOI, w.DOI)
}

// AssertedRelations returns nil: CORE aggregates copies of works and
// publishes no typed relations between works.
func (coreIdentity) AssertedRelations(string, NormalizedRecord, time.Time) []identity.Relation {
	return nil
}

func (c coreIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	w, ok := c.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: w.Title, Year: w.YearPublished}
	for _, a := range w.Authors {
		if a.Name != "" {
			d.Authors = append(d.Authors, a.Name)
		}
	}
	return d
}

// RecordRights reads the licence CORE carried from the source repository.
// An absent licence reports unknown. CORE holding a full-text copy means
// the record is readable; it is not a redistribution grant to this gateway,
// and this method never turns it into one.
func (c coreIdentity) RecordRights(rec NormalizedRecord) Rights {
	w, ok := c.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "core (unparseable record)"}
	}
	if strings.TrimSpace(w.License) == "" {
		return Rights{
			Redistribution: RedistributionUnknown,
			Source:         "core:license (absent); CORE holding a copy is not a licence",
			FreeToRead:     w.DownloadURL != "" || len(w.SourceFulltextUrls) > 0,
		}
	}
	rights := Rights{
		License:        w.License,
		Redistribution: RedistributionUnknown,
		Source:         "core:license",
		FreeToRead:     true,
	}
	l := strings.ToLower(w.License)
	if strings.Contains(l, "cc0") || strings.Contains(l, "publicdomain") ||
		strings.Contains(l, "cc-by") || strings.Contains(l, "cc by") ||
		strings.Contains(l, "/licenses/by") {
		rights.Redistribution = RedistributionAllowed
	}
	return rights
}

// Availability reports what CORE published for this record. A record with a
// location is retrievable; a record CORE holds without publishing a
// location is restricted rather than absent, because the copy exists and
// the caller cannot reach it from here.
func (c coreIdentity) Availability(rec NormalizedRecord) Availability {
	w, ok := c.parse(rec)
	if !ok {
		return AvailabilityAbsent
	}
	if w.DownloadURL != "" || len(w.SourceFulltextUrls) > 0 || coreDownloadLink(w) != "" {
		return AvailabilityRetrievable
	}
	if w.FullText != "" {
		return AvailabilityRestricted
	}
	return AvailabilityAbsent
}

func coreDownloadLink(w coreWork) string {
	for _, l := range w.Links {
		if strings.EqualFold(l.Type, "download") && l.URL != "" {
			return l.URL
		}
	}
	return ""
}

// Assets reports the locations CORE published: its own hosted copy and each
// source repository location, each carrying the record's own rights. The
// gateway does not dereference any of them, and CORE's `fullText` field,
// when present in a response, is never re-served: only the location is.
func (c coreIdentity) Assets(rec NormalizedRecord) []Asset {
	w, ok := c.parse(rec)
	if !ok {
		return nil
	}
	rights := c.RecordRights(rec)
	id := w.id()
	var out []Asset
	if w.DownloadURL != "" {
		out = append(out, Asset{
			AssetID:        "core:" + id + "#download",
			Representation: "application/pdf; role=full-text; host=core",
			CanonicalURL:   w.DownloadURL,
			Rights:         rights,
		})
	}
	if link := coreDownloadLink(w); link != "" && link != w.DownloadURL {
		out = append(out, Asset{
			AssetID:        "core:" + id + "#link-download",
			Representation: "unspecified; role=full-text; host=core",
			CanonicalURL:   link,
			Rights:         rights,
		})
	}
	for i, u := range w.SourceFulltextUrls {
		if u == "" {
			continue
		}
		out = append(out, Asset{
			AssetID:        "core:" + id + "#source-" + strconv.Itoa(i),
			Representation: "unspecified; role=full-text; host=repository",
			CanonicalURL:   u,
			Rights:         rights,
		})
	}
	return out
}

// coreSync reports CORE's data dumps and its incremental scroll. Both exist
// under the same free key, so both are reported.
type coreSync struct{}

func (coreSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: true, Incremental: true}
}

func (coreIdentity) Coverage() string {
	return "CORE aggregates open-access records from repositories worldwide, roughly 290M records " +
		"with full text held for about 30M. Coverage follows repository deposit rather than publication."
}

// CORESearchAdapter backs route ID "core-search".
var CORESearchAdapter = &Adapter{
	ID:                   "core-search",
	Description:          "CORE open-access aggregator search, for full-text location discovery.",
	Searcher:             coreOffsetPagination{},
	Normalizer:           CORENormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     coreIdentity{},
	DescriptorProvider:   coreIdentity{},
	AssetProvider:        coreIdentity{},
	AvailabilityReporter: coreIdentity{},
	RecordRightsProvider: coreIdentity{},
	SyncProvider:         coreSync{},
}

// COREFetchAdapter backs route ID "core-fetch".
var COREFetchAdapter = &Adapter{
	ID:                   "core-fetch",
	Description:          "CORE single work record.",
	Fetcher:              coreFetchByID{},
	Normalizer:           CORENormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     coreIdentity{},
	DescriptorProvider:   coreIdentity{},
	AssetProvider:        coreIdentity{},
	AvailabilityReporter: coreIdentity{},
	RecordRightsProvider: coreIdentity{},
	SyncProvider:         coreSync{},
}
