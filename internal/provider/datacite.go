package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

// DataCite adapter (x402-research-gateway#24).
//
// Verified against api.datacite.org and support.datacite.org on 2026-08-17:
//
//	GET https://api.datacite.org/dois?query=…&page[size]=&page[cursor]=1
//	GET https://api.datacite.org/dois/{doi}
//
// JSON:API shape: {"data":[{"id":"10.x/y","attributes":{…}}]}. No
// authentication for public read. Pagination is `page[size]` with
// `page[number]`, and `page[cursor]` for deep paging past the offset limit,
// so the model reported here is "cursor".
//
// The failure this adapter exists to avoid: DataCite's *metadata* is CC0
// and the object it describes carries its own licence, which is routinely
// different, and it is absent on a large share of records. A CC0 DataCite record can describe a
// dataset under any licence or none. The two are read and reported apart,
// and an absent object licence reports unknown, which permits nothing.

type dataciteBody struct {
	Data  json.RawMessage `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
}

type dataciteDOI struct {
	ID         string `json:"id"`
	Attributes struct {
		DOI    string `json:"doi"`
		URL    string `json:"url"`
		Titles []struct {
			Title string `json:"title"`
			// TitleType is DataCite's own vocabulary: empty means the
			// record's primary title, "TranslatedTitle" is a provider-
			// published translation, "AlternativeTitle" and "Subtitle" are
			// the remaining values DataCite defines
			// (x402-research-gateway#21).
			TitleType string `json:"titleType"`
			// Lang is a BCP-47 tag DataCite lets a depositor attach per
			// title; empty means the depositor did not tag one.
			Lang string `json:"lang"`
		} `json:"titles"`
		Creators []struct {
			Name string `json:"name"`
		} `json:"creators"`
		PublicationYear int `json:"publicationYear"`
		// Language is the resource's own language, DataCite Metadata
		// Schema's top-level `language` property, the depositor's own
		// assertion and never a gateway default (x402-research-gateway
		// #21).
		Language string `json:"language"`
		Types    struct {
			ResourceType        string `json:"resourceType"`
			ResourceTypeGeneral string `json:"resourceTypeGeneral"`
		} `json:"types"`
		RightsList []struct {
			Rights                 string `json:"rights"`
			RightsURI              string `json:"rightsUri"`
			RightsIdentifier       string `json:"rightsIdentifier"`
			SchemeURI              string `json:"schemeUri"`
			RightsIdentifierScheme string `json:"rightsIdentifierScheme"`
		} `json:"rightsList"`
		RelatedIdentifiers []struct {
			RelatedIdentifier     string `json:"relatedIdentifier"`
			RelatedIdentifierType string `json:"relatedIdentifierType"`
			RelationType          string `json:"relationType"`
			ResourceTypeGeneral   string `json:"resourceTypeGeneral"`
		} `json:"relatedIdentifiers"`
		ContentURL []string `json:"contentUrl"`
	} `json:"attributes"`
}

// DataCiteNormalizer handles both the list and single-record JSON:API
// shapes: `data` is an array for /dois and an object for /dois/{doi}.
type DataCiteNormalizer struct{}

func (DataCiteNormalizer) Normalize(body []byte) []NormalizedRecord {
	var b dataciteBody
	if err := json.Unmarshal(body, &b); err != nil || len(b.Data) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(b.Data, &items); err != nil {
		// Single-record shape: data is one object.
		items = []json.RawMessage{b.Data}
	}
	recs := make([]NormalizedRecord, 0, len(items))
	for _, raw := range items {
		var d dataciteDOI
		if err := json.Unmarshal(raw, &d); err != nil {
			continue
		}
		doi := firstNonEmpty(d.Attributes.DOI, d.ID)
		if doi == "" {
			continue
		}
		recs = append(recs, NormalizedRecord{
			ID:           doi,
			CanonicalURL: "https://doi.org/" + doi,
			Raw:          raw,
		})
	}
	return recs
}

type dataciteCursorPagination struct{}

func (dataciteCursorPagination) PaginationModel() string { return "cursor" }

type dataciteFetchByDOI struct{}

func (dataciteFetchByDOI) IdentifierSchemes() []string { return []string{"doi"} }

type dataciteIdentity struct{}

func (dataciteIdentity) parse(rec NormalizedRecord) (dataciteDOI, bool) {
	var d dataciteDOI
	if len(rec.Raw) == 0 {
		return d, false
	}
	if err := json.Unmarshal(rec.Raw, &d); err != nil {
		return d, false
	}
	return d, true
}

func (dc dataciteIdentity) Identifiers(rec NormalizedRecord) []identity.Identifier {
	d, ok := dc.parse(rec)
	if !ok {
		return nil
	}
	return appendID(nil, identity.SchemeDOI, firstNonEmpty(d.Attributes.DOI, d.ID))
}

// dataciteRelationType maps DataCite's relationType vocabulary onto the
// identity relation types. The mapping is partial by design: DataCite's
// vocabulary describes dataset-to-publication relations the identity model
// has no term for (IsSupplementTo aside), and coercing IsCitedBy or
// IsDerivedFrom into an identity relation would assert something different
// from what DataCite said. Unmapped terms are preserved verbatim in
// ResourceRelations instead.
func dataciteRelationType(t string) (identity.RelationType, bool) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "issupplementto":
		return identity.RelSupplementTo, true
	case "isidenticalto":
		return identity.RelSameWork, true
	case "ispreviousversionof", "isnewversionof", "isversionof", "hasversion":
		return identity.RelVersionOf, true
	case "ispreprintof":
		return identity.RelPreprintOf, true
	case "isobsoletedby", "obsoletes":
		return identity.RelWithdraws, true
	default:
		return "", false
	}
}

// AssertedRelations surfaces the mapped subset of DataCite's related
// identifiers with provider-asserted evidence. DataCite's own relationType
// term survives on every record in Raw and in ResourceRelations, so nothing
// this mapping declines to translate is lost.
func (dc dataciteIdentity) AssertedRelations(nodeID string, rec NormalizedRecord, at time.Time) []identity.Relation {
	d, ok := dc.parse(rec)
	if !ok {
		return nil
	}
	ev := identity.ProviderAsserted("datacite", at)
	var out []identity.Relation
	for _, r := range d.Attributes.RelatedIdentifiers {
		rel, known := dataciteRelationType(r.RelationType)
		if !known || r.RelatedIdentifier == "" {
			continue
		}
		target, parsed := identity.Parse(r.RelatedIdentifier)
		if !parsed {
			continue
		}
		out = append(out, identity.Relation{
			From: nodeID, To: target.Key(), Type: rel, Evidence: ev,
		})
	}
	return out
}

// ResourceRelation is one DataCite related-identifier entry with its own
// vocabulary preserved beside any normalized term. DataCite's relationType
// carries meaning the identity model has no word for, so the provider's
// term is the record and the normalized term is the annotation.
type ResourceRelation struct {
	RelatedIdentifier     string `json:"related_identifier"`
	RelatedIdentifierType string `json:"related_identifier_type"`
	// ProviderRelationType is DataCite's own term, verbatim.
	ProviderRelationType string `json:"provider_relation_type"`
	// NormalizedRelationType is the identity vocabulary term, empty when
	// this gateway has none for the provider's term.
	NormalizedRelationType string `json:"normalized_relation_type,omitempty"`
	ResourceTypeGeneral    string `json:"resource_type_general,omitempty"`
}

// ResourceRelations reports every related identifier DataCite published,
// mapped or not.
func (dc dataciteIdentity) ResourceRelations(rec NormalizedRecord) []ResourceRelation {
	d, ok := dc.parse(rec)
	if !ok {
		return nil
	}
	var out []ResourceRelation
	for _, r := range d.Attributes.RelatedIdentifiers {
		entry := ResourceRelation{
			RelatedIdentifier:     r.RelatedIdentifier,
			RelatedIdentifierType: r.RelatedIdentifierType,
			ProviderRelationType:  r.RelationType,
			ResourceTypeGeneral:   r.ResourceTypeGeneral,
		}
		if rel, known := dataciteRelationType(r.RelationType); known {
			entry.NormalizedRelationType = string(rel)
		}
		out = append(out, entry)
	}
	return out
}

// ResourceType reports what kind of object this record describes. A
// dataset, a piece of software, and a text are different objects, and
// flattening them would lose the distinction that makes DataCite worth
// querying.
func (dc dataciteIdentity) ResourceType(rec NormalizedRecord) (general, specific string) {
	d, ok := dc.parse(rec)
	if !ok {
		return "", ""
	}
	return d.Attributes.Types.ResourceTypeGeneral, d.Attributes.Types.ResourceType
}

func (dc dataciteIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	d, ok := dc.parse(rec)
	if !ok {
		return Descriptor{}
	}
	desc := Descriptor{Year: d.Attributes.PublicationYear}
	if len(d.Attributes.Titles) > 0 {
		desc.Title = d.Attributes.Titles[0].Title
	}
	for _, c := range d.Attributes.Creators {
		if c.Name != "" {
			desc.Authors = append(desc.Authors, c.Name)
		}
	}
	return desc
}

// RecordRights reads the licence on the deposited object from rightsList.
// It is not the metadata licence: DataCite's metadata is CC0 and says
// nothing about the object. A record with an empty rightsList reports
// unknown, which permits nothing.
func (dc dataciteIdentity) RecordRights(rec NormalizedRecord) Rights {
	d, ok := dc.parse(rec)
	if !ok {
		return Rights{Redistribution: RedistributionUnknown, Source: "datacite (unparseable record)"}
	}
	if len(d.Attributes.RightsList) == 0 {
		return Rights{
			Redistribution: RedistributionUnknown,
			Source:         "datacite:rightsList (absent); the CC0 metadata licence says nothing about the object",
		}
	}
	first := d.Attributes.RightsList[0]
	rights := Rights{
		License:        firstNonEmpty(first.RightsIdentifier, first.Rights),
		LicenseURL:     first.RightsURI,
		Redistribution: RedistributionUnknown,
		Source:         "datacite:rightsList",
	}
	l := strings.ToLower(firstNonEmpty(first.RightsIdentifier, first.Rights, first.RightsURI))
	switch {
	case strings.Contains(l, "cc0"), strings.Contains(l, "publicdomain/zero"):
		rights.Redistribution = RedistributionAllowed
		rights.FreeToRead = true
	case strings.HasPrefix(l, "cc-by"), strings.HasPrefix(l, "cc by"), strings.Contains(l, "/licenses/by"):
		rights.Redistribution = RedistributionAllowed
		rights.FreeToRead = true
	}
	return rights
}

// Assets reports the deposited locations DataCite publishes. The landing
// page is always one; contentUrl entries are the object itself when the
// depositor supplied them. Every representation carries the object's own
// rights, never the CC0 that covers the metadata record.
func (dc dataciteIdentity) Assets(rec NormalizedRecord) []Asset {
	d, ok := dc.parse(rec)
	if !ok {
		return nil
	}
	rights := dc.RecordRights(rec)
	doi := firstNonEmpty(d.Attributes.DOI, d.ID)
	general := d.Attributes.Types.ResourceTypeGeneral
	if general == "" {
		general = "unspecified"
	}
	var out []Asset
	if d.Attributes.URL != "" {
		out = append(out, Asset{
			AssetID:        "datacite:" + doi + "#landing",
			Representation: "text/html; role=landing-page; resource-type=" + general,
			CanonicalURL:   d.Attributes.URL,
			Rights:         rights,
		})
	}
	for i, u := range d.Attributes.ContentURL {
		if u == "" {
			continue
		}
		out = append(out, Asset{
			AssetID:        "datacite:" + doi + "#content-" + itoaSmall(i),
			Representation: "unspecified; role=content; resource-type=" + general,
			CanonicalURL:   u,
			Rights:         rights,
		})
	}
	return out
}

// Multilingual reports the resource language and any titleType-tagged title
// forms DataCite's depositor published beyond the primary title
// (x402-research-gateway#21). DataCite's titleType vocabulary distinguishes
// a translation from an alternative title and a subtitle; only
// TranslatedTitle is carried as FormTranslated, since AlternativeTitle and
// Subtitle are not translations of anything and this gateway does not
// invent a relation DataCite did not assert.
func (dc dataciteIdentity) Multilingual(rec NormalizedRecord) Multilingual {
	d, ok := dc.parse(rec)
	if !ok {
		return Multilingual{}
	}
	m := Multilingual{Language: d.Attributes.Language}
	for _, t := range d.Attributes.Titles {
		if t.Title == "" {
			continue
		}
		switch t.TitleType {
		case "TranslatedTitle":
			m.Forms = append(m.Forms, LocalizedForm{
				Value: t.Title, Language: t.Lang, Kind: FormTranslated, Provider: "datacite",
			})
		case "AlternativeTitle":
			m.Forms = append(m.Forms, LocalizedForm{
				Value: t.Title, Language: t.Lang, Kind: FormSynonym, Provider: "datacite",
			})
		default:
			// The primary title (empty titleType) and Subtitle carry no
			// translation relation to preserve, so they are left out of
			// Forms unless the depositor tagged a language DataCite's own
			// primary-title field cannot otherwise carry.
			if t.TitleType == "" && t.Lang != "" {
				m.Forms = append(m.Forms, LocalizedForm{
					Value: t.Title, Language: t.Lang, Kind: FormOriginal, Provider: "datacite",
				})
			}
		}
	}
	return m
}

func itoaSmall(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

type dataciteSync struct{}

func (dataciteSync) SyncCapability() SyncCapability {
	return SyncCapability{Bulk: false, Incremental: true}
}

// DataCiteSearchAdapter backs route ID "datacite-search".
var DataCiteSearchAdapter = &Adapter{
	ID:                     "datacite-search",
	Description:            "DataCite /dois search over dataset, software, and other research-output DOIs.",
	Searcher:               dataciteCursorPagination{},
	Normalizer:             DataCiteNormalizer{},
	CitationProvider:       GenericCitationProvider{},
	IdentityProvider:       dataciteIdentity{},
	DescriptorProvider:     dataciteIdentity{},
	AssetProvider:          dataciteIdentity{},
	RecordRightsProvider:   dataciteIdentity{},
	ObjectRelationProvider: dataciteIdentity{},
	IntegrityProvider:      dataciteIdentity{},
	MultilingualProvider:   dataciteIdentity{},
	SyncProvider:           dataciteSync{},
}

// DataCiteFetchAdapter backs route ID "datacite-fetch".
var DataCiteFetchAdapter = &Adapter{
	ID:                     "datacite-fetch",
	Description:            "DataCite single DOI record.",
	Fetcher:                dataciteFetchByDOI{},
	Normalizer:             DataCiteNormalizer{},
	CitationProvider:       GenericCitationProvider{},
	IdentityProvider:       dataciteIdentity{},
	DescriptorProvider:     dataciteIdentity{},
	AssetProvider:          dataciteIdentity{},
	RecordRightsProvider:   dataciteIdentity{},
	ObjectRelationProvider: dataciteIdentity{},
	IntegrityProvider:      dataciteIdentity{},
	MultilingualProvider:   dataciteIdentity{},
	SyncProvider:           dataciteSync{},
}
