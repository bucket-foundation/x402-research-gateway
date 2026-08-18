package provider

import "testing"

// pubchemPropertyFixture is trimmed from a live PubChem PUG-REST response
// verified 2026-08-18 (see pubchem.go's doc comment); the name-keyed and
// CID-keyed routes return byte-identical shapes for the same compound.
const pubchemPropertyFixture = `{"PropertyTable":{"Properties":[{"CID":2244,"MolecularFormula":"C9H8O4","MolecularWeight":"180.16","ConnectivitySMILES":"CC(=O)OC1=CC=CC=C1C(=O)O","IUPACName":"2-acetyloxybenzoic acid"}]}}`

func TestPubChemNormalizer(t *testing.T) {
	recs := PubChemNormalizer{}.Normalize([]byte(pubchemPropertyFixture))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].ID != "2244" {
		t.Errorf("id = %q, want 2244", recs[0].ID)
	}
	if recs[0].CanonicalURL != "https://pubchem.ncbi.nlm.nih.gov/compound/2244" {
		t.Errorf("canonical url = %q", recs[0].CanonicalURL)
	}
	if len(recs[0].Raw) == 0 {
		t.Error("raw record bytes must be preserved")
	}
}

func TestPubChemNormalizer_MalformedBody(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(``), []byte(`not json`), []byte(`{}`), []byte(`{"PropertyTable":{"Properties":[]}}`)} {
		if recs := (PubChemNormalizer{}).Normalize(body); len(recs) != 0 {
			t.Errorf("invented %d records from %q", len(recs), body)
		}
	}
}

func TestPubChemIdentity_Descriptor(t *testing.T) {
	recs := PubChemNormalizer{}.Normalize([]byte(pubchemPropertyFixture))
	d := pubchemIdentity{}.Descriptor(recs[0])
	if d.Title != "2-acetyloxybenzoic acid" {
		t.Errorf("title = %q", d.Title)
	}
}

func TestPubChemCompoundByCIDAdapter_HasSearchCapabilities(t *testing.T) {
	if PubChemCompoundByCIDAdapter.Normalizer == nil || PubChemCompoundByCIDAdapter.CitationProvider == nil {
		t.Error("pubchem-compound-cid should carry the new normalizer/citation capability")
	}
	if got := PubChemCompoundByCIDAdapter.Fetcher.IdentifierSchemes(); len(got) != 2 || got[0] != "cid" {
		t.Errorf("identifier_schemes: got %v want [cid pubchem-cid]", got)
	}
}

func TestPubChemIdentity_RecordRights(t *testing.T) {
	recs := PubChemNormalizer{}.Normalize([]byte(pubchemPropertyFixture))
	rights := pubchemIdentity{}.RecordRights(recs[0])
	if !rights.Permits() {
		t.Errorf("PubChem is a public-domain US-gov work, should permit redistribution, got %+v", rights)
	}
}

func TestPubChemFetchers_IdentifierSchemes(t *testing.T) {
	if got := (pubChemFetchByName{}).IdentifierSchemes(); len(got) != 1 || got[0] != "name" {
		t.Errorf("name fetcher schemes = %v", got)
	}
	got := (pubChemFetchByCID{}).IdentifierSchemes()
	if len(got) != 2 || got[0] != "cid" {
		t.Errorf("cid fetcher schemes = %v", got)
	}
}

func TestPubChemAdapters_Registered(t *testing.T) {
	reg := DefaultRegistry()
	if reg["pubchem-compound"] != PubChemCompoundAdapter {
		t.Error("pubchem-compound not wired to PubChemCompoundAdapter")
	}
	if reg["pubchem-compound-cid"] != PubChemCompoundByCIDAdapter {
		t.Error("pubchem-compound-cid not wired to PubChemCompoundByCIDAdapter")
	}
}
