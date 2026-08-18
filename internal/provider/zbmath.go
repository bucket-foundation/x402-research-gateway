package provider

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// zbMATH Open adapter (x402-research-gateway#32).
//
// Verified live against api.zbmath.org on 2026-08-17:
//
//	GET https://api.zbmath.org/v1/document/_search?search_string=…&results_per_page=&page=
//	GET https://api.zbmath.org/v1/document/{id}
//
// No auth for either endpoint, empirically confirmed by unauthenticated
// queries returning full records. No numeric rate limit is published on
// the API landing page; the route carries a generous timeout and the
// operator is expected to cache. An OAI-PMH endpoint exists and answers
// unauthenticated at https://oai.zbmath.org/v1/ (confirmed live via
// Identify), for incremental harvest.
//
// zbMATH Open is the failure this adapter exists to avoid twice over.
// First: bibliographic metadata and review text are different authored
// works with different rights, and the API's own `license` field applies
// to the bibliographic record, never to a review's prose, which is
// editorial commentary a named reviewer wrote. Second: an MSC
// classification code is meaningless without its edition, since the same
// numeric code can name a different subject across the 2000, 2010, and
// 2020 revisions; every code this adapter reports carries the `scheme`
// zbMATH itself published (e.g. "msc2020") rather than being normalized
// to one edition.

type zbmathAuthor struct {
	Name  string   `json:"name"`
	Codes []string `json:"codes"`
}

type zbmathMSC struct {
	Code   string `json:"code"`
	Scheme string `json:"scheme"`
	Text   string `json:"text"`
}

type zbmathReviewer struct {
	AuthorCode *string `json:"author_code"`
	ReviewerID *string `json:"reviewer_id"`
	Name       *string `json:"name"`
	Sign       string  `json:"sign"`
}

type zbmathEditorialContribution struct {
	Language         string         `json:"language"`
	Reviewer         zbmathReviewer `json:"reviewer"`
	Text             string         `json:"text"`
	ContributionType string         `json:"contribution_type"`
}

type zbmathDocument struct {
	ID         int    `json:"id"`
	Identifier string `json:"identifier"`
	ZbmathURL  string `json:"zbmath_url"`
	Title      struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Addition string `json:"addition"`
	} `json:"title"`
	Year         string `json:"year"`
	Contributors struct {
		Authors []zbmathAuthor `json:"authors"`
	} `json:"contributors"`
	MSC                    []zbmathMSC                   `json:"msc"`
	EditorialContributions []zbmathEditorialContribution `json:"editorial_contributions"`
	License                []json.RawMessage             `json:"license"`
	Links                  []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"links"`
	Source struct {
		Source string `json:"source"`
	} `json:"source"`
}

// zbmathSearchBody unwraps both the search shape (`result` is an array)
// and the single-document fetch shape (`result` is one object).
type zbmathSearchBody struct {
	Result json.RawMessage `json:"result"`
}

// ZbMATHNormalizer handles both /document/_search (result: array) and
// /document/{id} (result: one object).
type ZbMATHNormalizer struct{}

func (ZbMATHNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b zbmathSearchBody
	if err := json.Unmarshal(body, &b); err != nil || len(b.Result) == 0 || string(b.Result) == "null" {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(b.Result, &items); err != nil {
		// Single-document shape: result is one object.
		items = []json.RawMessage{b.Result}
	}
	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var d zbmathDocument
		if err := json.Unmarshal(raw, &d); err != nil || d.Identifier == "" {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           d.Identifier,
			CanonicalURL: firstNonEmpty(d.ZbmathURL, "https://zbmath.org/"+strconv.Itoa(d.ID)),
			Raw:          raw,
		})
	}
	return recs
}

type zbmathPagePagination struct{}

func (zbmathPagePagination) PaginationModel() string { return "page" }

type zbmathFetchByID struct{}

func (zbmathFetchByID) IdentifierSchemes() []string { return []string{"zbmath"} }

type zbmathIdentity struct{}

func (zbmathIdentity) parse(rec NormalizedRecord) (zbmathDocument, bool) {
	var d zbmathDocument
	if len(rec.Raw) == 0 {
		return d, false
	}
	if err := json.Unmarshal(rec.Raw, &d); err != nil {
		return d, false
	}
	return d, true
}

func (zi zbmathIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	d, ok := zi.parse(rec)
	if !ok {
		return nil
	}
	out := appendID(nil, identity.SchemeZbMATH, d.Identifier)
	for _, l := range d.Links {
		if strings.EqualFold(l.Type, "doi") && l.URL != "" {
			out = appendID(out, identity.SchemeDOI, l.URL)
		}
	}
	return out
}

// AssertedRelations returns nil: this fixture surface carries no
// cross-record relation zbMATH publishes in a form the identity model has
// a term for. references (when zbMATH resolves them) name related works
// by free-text citation rather than a typed relation, and are preserved
// as-is on the record rather than promoted to an identity assertion.
func (zi zbmathIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	return nil
}

func (zi zbmathIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	d, ok := zi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	desc := Descriptor{Title: d.Title.Title, Year: atoiSafe(d.Year)}
	for _, a := range d.Contributors.Authors {
		if a.Name != "" {
			desc.Authors = append(desc.Authors, a.Name)
		}
	}
	return desc
}

// MSCCode is one Mathematics Subject Classification code zbMATH
// published on this record, with the edition it was classified under.
// The same numeric code can name a different subject across editions, so
// Scheme is never dropped or normalized away.
type MSCCode struct {
	Code   string `json:"code"`
	Scheme string `json:"scheme"`
	Text   string `json:"text,omitempty"`
}

// MSCCodes reports every MSC code zbMATH published, each carrying its
// own edition.
func (zi zbmathIdentity) MSCCodes(rec NormalizedRecord) []MSCCode {
	d, ok := zi.parse(rec)
	if !ok {
		return nil
	}
	out := make([]MSCCode, 0, len(d.MSC))
	for _, m := range d.MSC {
		out = append(out, MSCCode{Code: m.Code, Scheme: m.Scheme, Text: m.Text})
	}
	return out
}

// Review is one editorial contribution zbMATH published on this record:
// a mathematician's review, an abstract, or another curated annotation.
// It is authored content a named or pseudonymous reviewer wrote, with its
// own rights posture distinct from the bibliographic metadata, so
// RecordRights never covers it and no redistribution is assumed for it.
type Review struct {
	Language         string `json:"language,omitempty"`
	ReviewerSign     string `json:"reviewer_sign,omitempty"`
	Text             string `json:"text"`
	ContributionType string `json:"contribution_type"`
}

// Reviews reports every editorial contribution zbMATH published on this
// record. A record with none returns nil, distinguishable from a record
// this adapter failed to parse.
func (zi zbmathIdentity) Reviews(rec NormalizedRecord) []Review {
	d, ok := zi.parse(rec)
	if !ok {
		return nil
	}
	out := make([]Review, 0, len(d.EditorialContributions))
	for _, e := range d.EditorialContributions {
		if e.Text == "" {
			continue
		}
		out = append(out, Review{
			Language: e.Language, ReviewerSign: e.Reviewer.Sign,
			Text: e.Text, ContributionType: e.ContributionType,
		})
	}
	return out
}

// RecordRights reports rights for the bibliographic record only, never
// for review text: a review is a separate authored work a named reviewer
// wrote, and this method says nothing about it. An empty license array,
// which every record queried during verification returned, reports
// unknown; zbMATH's landing page states the metadata is intended for
// reuse but this adapter does not act on an intention it cannot read off
// the record itself.
func (zbmathIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "zbmath (unparseable record)"}
	}
	return Rights{
		Redistribution: RedistributionUnknown,
		Source:         "zbmath:license (empty on every record observed during verification; applies to bibliographic metadata only, never to review text)",
	}
}

// Assets reports the external links zbMATH carries (DOI, publisher,
// arXiv, etc.), each under the record's own rights. Review text is
// reported separately through Reviews, never as an asset, since a review
// is prose with its own authorship and rights rather than a locator for
// the underlying document.
func (zi zbmathIdentity) Assets(rec NormalizedRecord) []Asset {
	d, ok := zi.parse(rec)
	if !ok {
		return nil
	}
	rights := zi.RecordRights(rec)
	out := make([]Asset, 0, len(d.Links))
	for i, l := range d.Links {
		if l.URL == "" {
			continue
		}
		out = append(out, Asset{
			AssetID:        "zbmath:" + d.Identifier + "#link-" + strconv.Itoa(i),
			Representation: "unspecified; role=" + firstNonEmpty(strings.ToLower(l.Type), "external-link"),
			CanonicalURL:   l.URL,
			Rights:         rights,
		})
	}
	return out
}

type zbmathSync struct{}

// SyncCapability reports incremental via the confirmed-live OAI-PMH
// endpoint at oai.zbmath.org. Bulk is false: this adapter does not fetch
// a full-corpus dump, and none was found published for unauthenticated
// use during verification.
func (zbmathSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: false, Incremental: true}
}

// ZbMATHSearchAdapter backs route ID "zbmath-search".
var ZbMATHSearchAdapter = &Adapter{
	ID:                 "zbmath-search",
	Description:        "zbMATH Open search over reviewed mathematics literature.",
	Searcher:           zbmathPagePagination{},
	Normalizer:         ZbMATHNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   zbmathIdentity{},
	DescriptorProvider: zbmathIdentity{},
	AssetProvider:      zbmathIdentity{},
	SyncProvider:       zbmathSync{},
}

// ZbMATHFetchAdapter backs route ID "zbmath-fetch".
var ZbMATHFetchAdapter = &Adapter{
	ID:                 "zbmath-fetch",
	Description:        "zbMATH Open single document by zbMATH document id.",
	Fetcher:            zbmathFetchByID{},
	Normalizer:         ZbMATHNormalizer{},
	CitationProvider:   GenericCitationProvider{},
	IdentityProvider:   zbmathIdentity{},
	DescriptorProvider: zbmathIdentity{},
	AssetProvider:      zbmathIdentity{},
	SyncProvider:       zbmathSync{},
}
