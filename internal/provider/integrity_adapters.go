package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/integrity"
)

// IntegrityProvider implementations (x402-research-gateway#9).
//
// Each reads only NormalizedRecord.Raw, so integrity extraction adds no
// upstream call. Each carries the upstream's own term, its notice
// identifier where one exists, and whatever date the upstream published.
// Nothing here reconciles one provider against another.

// ---------- Crossref / Crossmark ----------

// crossrefUpdate is the Crossmark update block. `update-to` names works
// this record updates, so this record is the notice; `updated-by` names
// works that update this record, so this record is the affected work.
type crossrefUpdate struct {
	DOI     string `json:"DOI"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Updated struct {
		DateParts [][]int `json:"date-parts"`
		DateTime  string  `json:"date-time"`
	} `json:"updated"`
}

type crossrefUpdateBlock struct {
	DOI          string           `json:"DOI"`
	URL          string           `json:"URL"`
	UpdateTo     []crossrefUpdate `json:"update-to"`
	UpdatedBy    []crossrefUpdate `json:"updated-by"`
	UpdatePolicy string           `json:"update-policy"`
}

// IntegrityAssertions reports the Crossmark update relations for this
// record. Both directions are reported from the perspective of the work
// this record is about: `updated-by` says something happened to this work,
// and `update-to` says this record is the notice for another work, which is
// equally a fact about that work's integrity.
func (c crossrefIdentity) IntegrityAssertions(rec NormalizedRecord, at time.Time) []integrity.Assertion {
	var b crossrefUpdateBlock
	if len(rec.Raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(rec.Raw, &b); err != nil || b.DOI == "" {
		return nil
	}
	self := integrity.NewEndpoint(b.DOI)
	self.CanonicalURL = firstNonEmpty(b.URL, "https://doi.org/"+b.DOI)

	var out []integrity.Assertion
	for _, u := range b.UpdatedBy {
		if u.Type == "" {
			continue
		}
		a := integrity.New("crossref", "crossref:updated-by", self,
			crossrefUpdateTerm(u.Type), at).WithNotice(u.DOI)
		a.Date = crossrefUpdateDate(u)
		if u.Label != "" {
			a.Annotations = map[string]string{"label": u.Label}
		}
		out = append(out, a)
	}
	for _, u := range b.UpdateTo {
		if u.Type == "" || u.DOI == "" {
			continue
		}
		affected := integrity.NewEndpoint(u.DOI)
		a := integrity.New("crossref", "crossref:update-to", affected,
			crossrefUpdateTerm(u.Type), at).WithNotice(b.DOI)
		a.Date = crossrefUpdateDate(u)
		ann := map[string]string{"notice_is_queried_record": "true"}
		if u.Label != "" {
			ann["label"] = u.Label
		}
		a.Annotations = ann
		out = append(out, a)
	}
	if b.UpdatePolicy != "" {
		for i := range out {
			if out[i].Annotations == nil {
				out[i].Annotations = map[string]string{}
			}
			out[i].Annotations["update-policy"] = b.UpdatePolicy
		}
	}
	return out
}

// crossrefUpdateTerm normalizes Crossref's spelling to the underscored form
// the integrity vocabulary registers, without changing the term itself: the
// provider's string reaches the assertion through integrity.New, which
// records it verbatim.
func crossrefUpdateTerm(t string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(t)), " ", "_")
}

func crossrefUpdateDate(u crossrefUpdate) string {
	if u.Updated.DateTime != "" {
		return u.Updated.DateTime
	}
	if len(u.Updated.DateParts) > 0 && len(u.Updated.DateParts[0]) > 0 {
		parts := u.Updated.DateParts[0]
		out := itoaPad(parts[0], 4)
		if len(parts) > 1 {
			out += "-" + itoaPad(parts[1], 2)
		}
		if len(parts) > 2 {
			out += "-" + itoaPad(parts[2], 2)
		}
		return out
	}
	return ""
}

func itoaPad(n, width int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// ---------- Europe PMC ----------

// IntegrityAssertions reports Europe PMC's commentCorrectionList, which
// carries the PubMed correction, retraction, and expression-of-concern
// relations. Europe PMC's own type string is the record; a type this
// gateway has no status for is still emitted, unrecognized.
func (e epmcIdentity) IntegrityAssertions(rec NormalizedRecord, at time.Time) []integrity.Assertion {
	r, ok := e.parse(rec)
	if !ok {
		return nil
	}
	self := integrity.NewEndpoint(firstNonEmpty(r.DOI, r.PMID, r.PMCID, rec.ID))
	self.CanonicalURL = rec.CanonicalURL

	var out []integrity.Assertion
	for _, cc := range r.CommentCorrectionList.CommentCorrection {
		if cc.Type == "" {
			continue
		}
		a := integrity.New("europepmc", "europepmc:commentCorrectionList", self, cc.Type, at)
		if cc.ID != "" {
			a = a.WithNotice("pmid:" + cc.ID)
			a.NoticeID = cc.ID
		}
		out = append(out, a)
	}
	return out
}

func (epmcIdentity) Coverage() string {
	return "Europe PMC carries the PubMed correction, retraction, and expression-of-concern relations " +
		"for records in its index, which covers the biomedical literature rather than all disciplines."
}

// ---------- DataCite ----------

// IntegrityAssertions reports the repository-side version history: the
// version relations a depositor declared and any obsoletion. DataCite has
// no retraction vocabulary, so this contributes new_version and withdrawal
// only, which is what DataCite actually publishes.
func (dc dataciteIdentity) IntegrityAssertions(rec NormalizedRecord, at time.Time) []integrity.Assertion {
	d, ok := dc.parse(rec)
	if !ok {
		return nil
	}
	doi := firstNonEmpty(d.Attributes.DOI, d.ID)
	if doi == "" {
		return nil
	}
	self := integrity.NewEndpoint(doi)
	self.CanonicalURL = firstNonEmpty(d.Attributes.URL, "https://doi.org/"+doi)

	var out []integrity.Assertion
	for _, r := range d.Attributes.RelatedIdentifiers {
		term := strings.ToLower(strings.TrimSpace(r.RelationType))
		if _, known := integrity.StatusFor(term); !known {
			continue
		}
		a := integrity.New("datacite", "datacite:relatedIdentifiers", self,
			r.RelationType, at).WithNotice(r.RelatedIdentifier)
		if r.RelatedIdentifierType != "" {
			a.Annotations = map[string]string{"relatedIdentifierType": r.RelatedIdentifierType}
		}
		out = append(out, a)
	}
	return out
}

func (dataciteIdentity) Coverage() string {
	return "DataCite publishes depositor-declared version and obsoletion relations for research outputs. " +
		"It has no retraction vocabulary, so a DataCite record reporting nothing says nothing about retraction."
}

// ---------- arXiv ----------

// IntegrityAssertions reports arXiv's own version supersession
// (x402-research-gateway#19): each submission's Atom id carries the version
// number arXiv currently serves for that base identifier, and versions are
// sequential integers arXiv assigns itself. A record fetched at v(N) with
// N>1 is arXiv's own statement that the base submission has a newer version
// than whichever one an earlier caller may have cited; the notice is this
// record's own identifier, the same shape crossrefIdentity uses when a
// record is itself the notice.
//
// The affected Work is the base identifier with no version suffix, not
// v(N-1) specifically: identity.Identifier.Key() for an arXiv scheme already
// drops the version so that cross-version resolution works
// (RelVersionOf), and asserting per prior version here would produce
// assertions whose Work and Notice keys collide and collapse under
// integrity.Build's per-ID dedup, silently losing the fact for a submission
// three or more versions deep. One assertion against the unversioned work,
// naming the current version as the notice, says everything the identity
// model can distinguish: this work has a version beyond whatever was cited.
//
// arXiv has no retraction or correction vocabulary; an author who withdraws
// a submission does so in free-text prose inside the abstract or comment
// field, which this adapter does not parse into a status because turning
// prose into a typed integrity assertion risks misreading it, and a false
// retraction is worse than no signal.
func (a arxivIdentity) IntegrityAssertions(rec NormalizedRecord, at time.Time) []integrity.Assertion {
	r, ok := a.parse(rec)
	if !ok || r.Version == "" {
		return nil
	}
	if atoiSafe(r.Version) < 2 {
		return nil
	}
	baseID := r.ID
	if idx := strings.LastIndex(baseID, "v"); idx > 0 {
		baseID = baseID[:idx]
	}
	work := integrity.NewEndpoint(baseID)
	work.CanonicalURL = "https://arxiv.org/abs/" + baseID

	asrt := integrity.New("arxiv", "arxiv:version", work, "new_version", at).WithNotice(r.ID)
	asrt.Notice.CanonicalURL = firstNonEmpty(r.AbsURL, "https://arxiv.org/abs/"+r.ID)
	asrt.Annotations = map[string]string{"current_version": r.Version}
	return []integrity.Assertion{asrt}
}

func (arxivIdentity) Coverage() string {
	return "arXiv publishes its own sequential version numbers per submission: a record at v(N) names every " +
		"earlier version of the same submission as superseded. arXiv has no retraction or correction " +
		"vocabulary; an author withdrawal is free text in the abstract or comment field, which this adapter " +
		"does not parse into a status."
}
