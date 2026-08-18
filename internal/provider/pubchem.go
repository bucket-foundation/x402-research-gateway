package provider

import (
	"encoding/json"
	"strconv"
)

// PubChem adapter (x402-research-gateway#16).
//
// Verified live against pubchem.ncbi.nlm.nih.gov on 2026-08-18 (no key
// required):
//
//	GET https://pubchem.ncbi.nlm.nih.gov/rest/pug/compound/name/{name}/property/MolecularFormula,MolecularWeight,IUPACName,CanonicalSMILES/JSON
//	GET https://pubchem.ncbi.nlm.nih.gov/rest/pug/compound/cid/{cid}/property/MolecularFormula,MolecularWeight,IUPACName,CanonicalSMILES/JSON
//	  -> both return the identical shape:
//	     {"PropertyTable":{"Properties":[{"CID":2244,"MolecularFormula":"C9H8O4",
//	       "MolecularWeight":"180.16","ConnectivitySMILES":"CC(=O)...",
//	       "IUPACName":"2-acetyloxybenzoic acid"}]}}
//
// PUG-REST is a compound-by-identifier fetch, not a search: name and CID
// are two identifier schemes into the same lookup, which is why one
// Normalizer and one Fetcher-shaped adapter cover both, per this issue's
// "provider type matches source reality" rule (no forced search interface
// where none exists). PubChem has no keyword search over compound names,
// only per-compound property retrieval by an already-known identifier;
// `/compound/name/{name}/cids/JSON` resolves a name to CIDs but returns a
// bare id list, not compound properties, and is out of scope this pass.
//
// Rights: public-domain, US-government work, per PubChem's own site
// (pubchem.ncbi.nlm.nih.gov, "PubChem data ... is freely available in the
// public domain", read 2026-08-18), matching this gateway's prior
// pubchem-pug-rest registry entry.
type pubchemProperty struct {
	CID                int    `json:"CID"`
	MolecularFormula   string `json:"MolecularFormula"`
	MolecularWeight    string `json:"MolecularWeight"`
	IUPACName          string `json:"IUPACName"`
	ConnectivitySMILES string `json:"ConnectivitySMILES"`
	CanonicalSMILES    string `json:"CanonicalSMILES"`
}

type pubchemPropertyTable struct {
	PropertyTable struct {
		Properties []pubchemProperty `json:"Properties"`
	} `json:"PropertyTable"`
}

// PubChemNormalizer handles the PUG-REST PropertyTable envelope, the one
// shape both the name-keyed and CID-keyed routes return.
type PubChemNormalizer struct{}

func (PubChemNormalizer) Normalize(body []byte) []NormalizedRecord {
	var table pubchemPropertyTable
	if err := json.Unmarshal(body, &table); err != nil {
		return nil
	}
	recs := make([]NormalizedRecord, 0, len(table.PropertyTable.Properties))
	for _, p := range table.PropertyTable.Properties {
		if p.CID == 0 {
			continue
		}
		raw, err := marshalRecord(p)
		if err != nil {
			continue
		}
		id := strconv.Itoa(p.CID)
		recs = append(recs, NormalizedRecord{
			ID:           id,
			CanonicalURL: "https://pubchem.ncbi.nlm.nih.gov/compound/" + id,
			Raw:          raw,
		})
	}
	return recs
}

// pubChemFetchByName implements Fetcher: pubchem-compound looks up one
// compound record by name.
type pubChemFetchByName struct{}

func (pubChemFetchByName) IdentifierSchemes() []string { return []string{"name"} }

// pubChemFetchByCID implements Fetcher: pubchem-compound-cid looks up one
// compound record by its PubChem CID, the identifier the name lookup
// itself resolves to.
type pubChemFetchByCID struct{}

func (pubChemFetchByCID) IdentifierSchemes() []string { return []string{"cid", "pubchem-cid"} }

type pubchemIdentity struct{}

func (pubchemIdentity) parse(rec NormalizedRecord) (pubchemProperty, bool) {
	var p pubchemProperty
	if len(rec.Raw) == 0 {
		return p, false
	}
	if err := json.Unmarshal(rec.Raw, &p); err != nil {
		return p, false
	}
	return p, true
}

func (pi pubchemIdentity) Descriptor(rec NormalizedRecord) Descriptor {
	p, ok := pi.parse(rec)
	if !ok {
		return Descriptor{}
	}
	return Descriptor{Title: p.IUPACName}
}

// RecordRights reports PubChem's site-wide public-domain designation,
// verified against pubchem.ncbi.nlm.nih.gov on 2026-08-18. PubChem carries
// no per-record licence field to read instead, unlike CORE or DataCite: a
// US National Library of Medicine database, it inherits the public-domain
// status of a US federal government work.
func (pubchemIdentity) RecordRights(rec NormalizedRecord) Rights {
	if len(rec.Raw) == 0 {
		return Rights{Redistribution: RedistributionUnknown, Source: "pubchem (unparseable record)"}
	}
	return Rights{
		License:        "public-domain (US-gov)",
		Redistribution: RedistributionAllowed,
		Source:         "pubchem:site (\"freely available in the public domain\"), verified 2026-08-18",
		FreeToRead:     true,
	}
}

// PubChemCompoundAdapter backs route ID "pubchem-compound": one compound
// record by name. Deliberately kept Fetcher-only with no Normalizer or
// CitationProvider (pre-#16 behavior): pubchem-compound is a live
// production route, and adding a hits array here would be a wire-format
// change for existing traffic that this issue's scope does not call for.
// The new capabilities below (search, per-record rights, descriptor) live
// on PubChemCompoundByCIDAdapter instead, which carries no production
// route yet.
var PubChemCompoundAdapter = &Adapter{
	ID:          "pubchem-compound",
	Description: "PubChem PUG REST — a single compound record by name.",
	Fetcher:     pubChemFetchByName{},
}

// PubChemCompoundByCIDAdapter backs route ID "pubchem-compound-cid": one
// compound record by PubChem CID, the identifier scheme the name lookup
// itself resolves through (x402-research-gateway#16).
var PubChemCompoundByCIDAdapter = &Adapter{
	ID:                   "pubchem-compound-cid",
	Description:          "PubChem PUG REST — a single compound record by PubChem CID.",
	Fetcher:              pubChemFetchByCID{},
	Normalizer:           PubChemNormalizer{},
	CitationProvider:     GenericCitationProvider{},
	DescriptorProvider:   pubchemIdentity{},
	RecordRightsProvider: pubchemIdentity{},
}
