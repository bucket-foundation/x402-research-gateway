package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/relation"
)

// ObjectRelationProvider implementations (x402-research-gateway#7).
//
// Each reads only NormalizedRecord.Raw, so relation extraction adds no
// upstream call. Each carries the provider's own relation term verbatim.
// The three vocabularies here disagree with each other on purpose:
// DataCite's `relationType` is camel-cased and object-centric, Crossref's
// update types are integrity-centric, and ClinicalTrials.gov's reference
// types describe how a publication relates to a study. None is translated
// into another.

// ---------- DataCite ----------

// ObjectRelations reports every DataCite related identifier as a relation
// between the deposited object and the related one. DataCite's own term is
// the record; a term the gateway has no word for is still emitted, with
// predicate.recognized false.
func (dc dataciteIdentity) ObjectRelations(rec NormalizedRecord, at time.Time) []relation.Relation {
	d, ok := dc.parse(rec)
	if !ok {
		return nil
	}
	doi := firstNonEmpty(d.Attributes.DOI, d.ID)
	if doi == "" {
		return nil
	}
	subject := relation.NewObject(d.Attributes.Types.ResourceTypeGeneral, doi)
	subject.CanonicalURL = firstNonEmpty(d.Attributes.URL, "https://doi.org/"+doi)

	var out []relation.Relation
	for _, r := range d.Attributes.RelatedIdentifiers {
		if r.RelatedIdentifier == "" || r.RelationType == "" {
			continue
		}
		object := relation.NewObject(r.ResourceTypeGeneral, r.RelatedIdentifier)
		rel := relation.New("datacite", "datacite:relatedIdentifiers",
			subject, r.RelationType, object, at)
		if r.RelatedIdentifierType != "" {
			rel.Annotations = map[string]string{
				"relatedIdentifierType": r.RelatedIdentifierType,
			}
		}
		out = append(out, rel)
	}
	return out
}

// ---------- Crossref ----------

// crossrefRelationBlock is Crossref's `relation` map: a relation name keyed
// to a list of endpoints. It sits beside `update-to` / `updated-by`, which
// carry the Crossmark integrity relations, and it is where dataset,
// software, preprint, and supplement links appear.
type crossrefRelationBlock map[string][]struct {
	ID     string `json:"id"`
	IDType string `json:"id-type"`
	// AssertedBy is "subject" or "object": which end of the relation
	// deposited it. Kept as an annotation, since it qualifies the assertion
	// without changing it.
	AssertedBy string `json:"asserted-by"`
}

// ObjectRelations reports Crossref's `relation` block plus the Crossmark
// update relations. `update-to` names works this record updates, so this
// record is the subject; `updated-by` names works that update this record,
// so this record is the object and the relation is emitted with the
// updating work as subject.
func (c crossrefIdentity) ObjectRelations(rec NormalizedRecord, at time.Time) []relation.Relation {
	w, ok := c.parse(rec)
	if !ok || w.DOI == "" {
		return nil
	}
	var block struct {
		Type     string                `json:"type"`
		Relation crossrefRelationBlock `json:"relation"`
	}
	_ = json.Unmarshal(rec.Raw, &block)

	subject := relation.NewObject(block.Type, w.DOI)
	subject.CanonicalURL = firstNonEmpty(w.URL, "https://doi.org/"+w.DOI)

	var out []relation.Relation
	for name, endpoints := range block.Relation {
		for _, e := range endpoints {
			if e.ID == "" {
				continue
			}
			object := relation.NewObject("", e.ID)
			rel := relation.New("crossref", "crossref:relation", subject, name, object, at)
			ann := map[string]string{}
			if e.IDType != "" {
				ann["id-type"] = e.IDType
			}
			if e.AssertedBy != "" {
				ann["asserted-by"] = e.AssertedBy
			}
			if len(ann) > 0 {
				rel.Annotations = ann
			}
			out = append(out, rel)
		}
	}
	for _, u := range w.UpdateTo {
		if u.DOI == "" || u.Type == "" {
			continue
		}
		out = append(out, relation.New("crossref", "crossref:update-to",
			subject, u.Type, relation.NewObject("", u.DOI).WithType(relation.TypeWork), at))
	}
	for _, u := range w.UpdatedBy {
		if u.DOI == "" || u.Type == "" {
			continue
		}
		updater := relation.NewObject(u.Type, u.DOI)
		out = append(out, relation.New("crossref", "crossref:updated-by",
			updater, u.Type, subject, at))
	}
	return out
}

// ---------- ClinicalTrials.gov ----------

// clinicalTrialsStudy is the slice of a v2 study record the relation
// extraction reads. `referencesModule` links a study to publications, and
// ClinicalTrials.gov's own `type` values (RESULT, DERIVED, BACKGROUND) say
// how, which is a third vocabulary again.
type clinicalTrialsStudy struct {
	ProtocolSection struct {
		IdentificationModule struct {
			NCTID string `json:"nctId"`
		} `json:"identificationModule"`
		DesignModule struct {
			StudyType string `json:"studyType"`
		} `json:"designModule"`
		ReferencesModule struct {
			References []struct {
				PMID     string `json:"pmid"`
				Type     string `json:"type"`
				Citation string `json:"citation"`
			} `json:"references"`
		} `json:"referencesModule"`
	} `json:"protocolSection"`
}

type clinicalTrialsRelations struct{}

func (clinicalTrialsRelations) parse(rec NormalizedRecord) (clinicalTrialsStudy, bool) {
	var s clinicalTrialsStudy
	if len(rec.Raw) == 0 {
		return s, false
	}
	if err := json.Unmarshal(rec.Raw, &s); err != nil {
		return s, false
	}
	return s, true
}

// ObjectRelations reports the publications a study references, as
// work-to-trial relations with the study as the object. ClinicalTrials.gov
// asserts the link from the study side; the direction here reads "this
// publication results from this trial," which is what the RESULT and
// DERIVED types mean.
func (ct clinicalTrialsRelations) ObjectRelations(rec NormalizedRecord, at time.Time) []relation.Relation {
	s, ok := ct.parse(rec)
	if !ok {
		return nil
	}
	nct := s.ProtocolSection.IdentificationModule.NCTID
	if nct == "" {
		return nil
	}
	trial := relation.NewObject(s.ProtocolSection.DesignModule.StudyType, nct).
		WithType(relation.TypeTrial)
	trial.CanonicalURL = "https://clinicaltrials.gov/study/" + nct
	// The NCT id is not an identity scheme this gateway registers, so it
	// stays in RawID and the object key is the raw form. That is accurate:
	// nothing else in the gateway can match on it yet.

	var out []relation.Relation
	for _, ref := range s.ProtocolSection.ReferencesModule.References {
		if ref.PMID == "" {
			continue
		}
		work := relation.NewObject("", ref.PMID).WithType(relation.TypeWork)
		term := strings.ToLower(strings.TrimSpace(ref.Type))
		if term == "" {
			term = "unspecified"
		}
		rel := relation.New("clinicaltrials", "clinicaltrials:referencesModule",
			work, term, trial, at)
		if ref.Citation != "" {
			rel.Annotations = map[string]string{"citation": ref.Citation}
		}
		out = append(out, rel)
	}
	return out
}
