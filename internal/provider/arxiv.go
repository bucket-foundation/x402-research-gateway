package provider

import (
	"encoding/xml"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// arXiv adapter (x402-research-gateway#25).
//
// This is the provider that motivates the adapter layer: arXiv answers in
// Atom XML, and the declarative config-driven proxy path can express no
// parser for it. The Normalizer below is the `atom-style` parser the
// research index called for.
//
// Verified against info.arxiv.org/help/api on 2026-08-17:
//
//	GET http://export.arxiv.org/api/query?search_query=…&start=&max_results=
//
// No auth. Pagination is offset-based on `start` with `max_results` capped
// at 2000 per call and 30,000 results total; queries over 1,000 results are
// asked to be refined. The published rate guidance is one request per three
// seconds. arXiv is a nonprofit on donated infrastructure, so that guidance
// is a requirement. Route config carries the timeout and the operator is
// expected to cache; this adapter never issues a burst of its own.
//
// Rights: per-submission licences vary from CC0 through the CC-BY variants
// to the arXiv non-exclusive licence, so the licence is read per record and
// never assumed from a provider default. A record whose licence arXiv did
// not publish reports unknown, which permits nothing.

// arXiv Atom namespaces.
const (
	atomNS  = "http://www.w3.org/2005/Atom"
	arxivNS = "http://arxiv.org/schemas/atom"
)

type arxivFeed struct {
	XMLName      xml.Name     `xml:"feed"`
	TotalResults int          `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
	StartIndex   int          `xml:"http://a9.com/-/spec/opensearch/1.1/ startIndex"`
	ItemsPerPage int          `xml:"http://a9.com/-/spec/opensearch/1.1/ itemsPerPage"`
	Entries      []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID        string `xml:"id"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Title     string `xml:"title"`
	Summary   string `xml:"summary"`
	Authors   []struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Links []struct {
		Href  string `xml:"href,attr"`
		Rel   string `xml:"rel,attr"`
		Type  string `xml:"type,attr"`
		Title string `xml:"title,attr"`
	} `xml:"link"`
	DOI             string `xml:"http://arxiv.org/schemas/atom doi"`
	JournalRef      string `xml:"http://arxiv.org/schemas/atom journal_ref"`
	Comment         string `xml:"http://arxiv.org/schemas/atom comment"`
	License         string `xml:"http://arxiv.org/schemas/atom license"`
	PrimaryCategory struct {
		Term   string `xml:"term,attr"`
		Scheme string `xml:"scheme,attr"`
	} `xml:"http://arxiv.org/schemas/atom primary_category"`
	Categories []struct {
		Term   string `xml:"term,attr"`
		Scheme string `xml:"scheme,attr"`
	} `xml:"category"`
}

// arxivEntryRecord is the JSON projection of an Atom entry, stored as
// NormalizedRecord.Raw. Atom bytes cannot go into a json.RawMessage, so the
// entry is re-encoded rather than dropped: the record keeps every field
// arXiv published, including the ones this adapter does not read.
type arxivEntryRecord struct {
	ID              string              `json:"id"`
	Version         string              `json:"version,omitempty"`
	AbsURL          string              `json:"abs_url,omitempty"`
	Updated         string              `json:"updated,omitempty"`
	Published       string              `json:"published,omitempty"`
	Title           string              `json:"title,omitempty"`
	Summary         string              `json:"summary,omitempty"`
	Authors         []string            `json:"authors,omitempty"`
	DOI             string              `json:"doi,omitempty"`
	JournalRef      string              `json:"journal_ref,omitempty"`
	Comment         string              `json:"comment,omitempty"`
	License         string              `json:"license,omitempty"`
	PrimaryCategory arxivCategoryRecord `json:"primary_category,omitempty"`
	// Categories keep their scheme, because a bare term is ambiguous across
	// classification systems.
	Categories []arxivCategoryRecord `json:"categories,omitempty"`
	Links      []arxivLinkRecord     `json:"links,omitempty"`
}

type arxivCategoryRecord struct {
	Term   string `json:"term"`
	Scheme string `json:"scheme,omitempty"`
}

type arxivLinkRecord struct {
	Href  string `json:"href"`
	Rel   string `json:"rel,omitempty"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// ArXivNormalizer parses the Atom feed. A body that is not the expected
// feed produces no records, matching every other Normalizer in this
// package.
type ArXivNormalizer struct{}

func (ArXivNormalizer) Normalize(body []byte) []NormalizedRecord {
	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		// The Atom id is the abs URL carrying the versioned identifier,
		// e.g. http://arxiv.org/abs/2101.00001v3.
		id, ok := identity.New(identity.SchemeArXiv, e.ID)
		if !ok {
			continue
		}
		// The record id keeps the version, because v1 and v3 are distinct
		// objects and collapsing them would erase the earlier submission.
		recordID := id.Value
		if id.Version != "" {
			recordID += "v" + id.Version
		}
		rec := arxivEntryRecord{
			ID:         recordID,
			Version:    id.Version,
			AbsURL:     e.ID,
			Updated:    e.Updated,
			Published:  e.Published,
			Title:      collapseSpace(e.Title),
			Summary:    collapseSpace(e.Summary),
			DOI:        e.DOI,
			JournalRef: e.JournalRef,
			Comment:    e.Comment,
			License:    e.License,
			PrimaryCategory: arxivCategoryRecord{
				Term: e.PrimaryCategory.Term, Scheme: e.PrimaryCategory.Scheme,
			},
		}
		for _, a := range e.Authors {
			if n := strings.TrimSpace(a.Name); n != "" {
				rec.Authors = append(rec.Authors, n)
			}
		}
		for _, c := range e.Categories {
			if c.Term != "" {
				rec.Categories = append(rec.Categories, arxivCategoryRecord{Term: c.Term, Scheme: c.Scheme})
			}
		}
		for _, l := range e.Links {
			if l.Href != "" {
				rec.Links = append(rec.Links, arxivLinkRecord{Href: l.Href, Rel: l.Rel, Type: l.Type, Title: l.Title})
			}
		}
		raw, err := marshalRecord(rec)
		if err != nil {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           recordID,
			CanonicalURL: "https://arxiv.org/abs/" + recordID,
			Raw:          raw,
		})
	}
	return recs
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

type arxivOffsetPagination struct{}

func (arxivOffsetPagination) PaginationModel() string { return "offset" }

type arxivFetchByID struct{}

func (arxivFetchByID) IdentifierSchemes() []string { return []string{"arxiv"} }

type arxivIdentity struct{}

func (arxivIdentity) parse(rec NormalizedRecord) (arxivEntryRecord, bool) {
	var r arxivEntryRecord
	if len(rec.Raw) == 0 {
		return r, false
	}
	if err := unmarshalRecord(rec.Raw, &r); err != nil {
		return r, false
	}
	return r, true
}

func (a arxivIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	r, ok := a.parse(rec)
	if !ok {
		return nil
	}
	var out []identity.Identifier
	out = appendID(out, identity.SchemeArXiv, r.ID)
	// A submission that declares a published DOI carries it, which is what
	// lets identity resolution relate the preprint to the published article
	// rather than guessing from titles.
	out = appendID(out, identity.SchemeDOI, r.DOI)
	return out
}

// AssertedRelations surfaces the preprint-to-published relation arXiv
// itself declares through arxiv:doi. arXiv asserts that this submission has
// that DOI, so the relation is preprint_of with provider-asserted evidence.
// It is deliberately not same_work: a preprint and its published article
// are related without being the same object.
func (a arxivIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	r, ok := a.parse(rec)
	if !ok || r.DOI == "" {
		return nil
	}
	doi, ok := identity.New(identity.SchemeDOI, r.DOI)
	if !ok {
		return nil
	}
	return []identity.Relation{{
		From:     nodeID,
		To:       doi.Key(),
		Type:     identity.RelPreprintOf,
		Evidence: identity.ProviderAsserted("arxiv", at),
	}}
}

func (a arxivIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	r, ok := a.parse(rec)
	if !ok {
		return Descriptor{}
	}
	d := Descriptor{Title: r.Title, Authors: r.Authors}
	if len(r.Published) >= 4 {
		d.Year = atoiSafe(r.Published[:4])
	}
	return d
}

// RecordRights reads the licence arXiv published on this submission. arXiv
// licences vary per submission, so a record with no declared licence
// reports unknown, which permits nothing. The abstract metadata is openly
// available; that is not a statement about the paper.
func (a arxivIdentity) RecordRights(rec NormalizedRecord) Rights {
	r, ok := a.parse(rec)
	if !ok || r.License == "" {
		return Rights{Redistribution: RedistributionUnknown, Source: "arxiv:license (absent)"}
	}
	rights := Rights{
		License:        r.License,
		LicenseURL:     r.License,
		Redistribution: RedistributionUnknown,
		Source:         "arxiv:license",
		FreeToRead:     true,
	}
	// Only the licences that grant redistribution on their face are marked
	// allowed. The arXiv non-exclusive licence grants arXiv distribution
	// rights and grants the reader none, so it stays unknown here.
	l := strings.ToLower(r.License)
	switch {
	case strings.Contains(l, "publicdomain/zero"), strings.Contains(l, "cc0"):
		rights.Redistribution = RedistributionAllowed
	case strings.Contains(l, "licenses/by/"), strings.Contains(l, "licenses/by-sa/"),
		strings.Contains(l, "licenses/by-nc-sa/"), strings.Contains(l, "licenses/by-nc/"):
		rights.Redistribution = RedistributionAllowed
	}
	return rights
}

// Assets reports the abstract page, the PDF, and the TeX source as three
// distinct representations. Each inherits the submission's own licence,
// because a link is a locator and the terms come from the record.
func (a arxivIdentity) Assets(rec NormalizedRecord) []Asset {
	r, ok := a.parse(rec)
	if !ok {
		return nil
	}
	rights := a.RecordRights(rec)
	absURL := r.AbsURL
	if absURL == "" {
		absURL = "https://arxiv.org/abs/" + r.ID
	}
	assets := []Asset{{
		AssetID:        "arxiv:" + r.ID + "#abs",
		Representation: "text/html; role=abstract",
		CanonicalURL:   absURL,
		Rights:         rights,
	}}
	for _, l := range r.Links {
		if l.Title == "pdf" || l.Type == "application/pdf" {
			assets = append(assets, Asset{
				AssetID:        "arxiv:" + r.ID + "#pdf",
				Representation: "application/pdf",
				CanonicalURL:   l.Href,
				Rights:         rights,
			})
		}
	}
	// arXiv serves the submission source at a stable path that the Atom
	// feed does not enumerate. It is listed as a representation because it
	// is one, under the same per-submission terms.
	assets = append(assets, Asset{
		AssetID:        "arxiv:" + r.ID + "#source",
		Representation: "application/x-eprint-tar; role=source",
		CanonicalURL:   "https://arxiv.org/src/" + r.ID,
		Rights:         rights,
	})
	return assets
}

// arxivSync reports OAI-PMH incremental harvest, which arXiv operates
// openly, and no bulk: the full-text bulk sets are requester-pays object
// storage this gateway does not access.
type arxivSync struct{}

func (arxivSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: false, Incremental: true}
}

// ArXivSearchAdapter backs route ID "arxiv-search".
var ArXivSearchAdapter = &Adapter{
	ID:                   "arxiv-search",
	Description:          "arXiv query API over mathematics, physics, and computer science preprints.",
	Searcher:             arxivOffsetPagination{},
	Normalizer:           ArXivNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     arxivIdentity{},
	DescriptorProvider:   arxivIdentity{},
	AssetProvider:        arxivIdentity{},
	RecordRightsProvider: arxivIdentity{},
	SyncProvider:         arxivSync{},
	IntegrityProvider:    arxivIdentity{},
}

// ArXivFetchAdapter backs route ID "arxiv-fetch": one arXiv identifier in,
// one entry out, through the same Atom parser.
var ArXivFetchAdapter = &Adapter{
	ID:                   "arxiv-fetch",
	Description:          "arXiv single submission by identifier, version preserved.",
	Fetcher:              arxivFetchByID{},
	Normalizer:           ArXivNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	IdentityProvider:     arxivIdentity{},
	DescriptorProvider:   arxivIdentity{},
	AssetProvider:        arxivIdentity{},
	RecordRightsProvider: arxivIdentity{},
	SyncProvider:         arxivSync{},
	IntegrityProvider:    arxivIdentity{},
}
