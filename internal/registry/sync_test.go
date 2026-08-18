package registry

import "testing"

func TestSyncMode_ClosedSet(t *testing.T) {
	for _, m := range SyncModes {
		if !m.Valid() {
			t.Fatalf("%q is in SyncModes and reports invalid", m)
		}
	}
	if SyncMode("torrent").Valid() {
		t.Fatal("an unreviewed mode string validated")
	}
}

func baseProvider() Provider {
	return Provider{
		ProviderID: "example", Name: "Example", Type: TypeScholarlyMetadata,
		Status: StatusRegistered,
	}
}

func TestValidate_UnknownSyncModeRejected(t *testing.T) {
	p := baseProvider()
	p.Sync = Sync{Modes: []SyncMode{"torrent"}, Verified: true}
	if len(p.Validate()) == 0 {
		t.Fatal("an unknown sync mode passed validation")
	}
}

func TestValidate_ServeDirectNeedsRationale(t *testing.T) {
	p := baseProvider()
	p.Sync = Sync{Modes: []SyncMode{SyncChangeFeed}, ServeDirect: true, Verified: true}
	p.IncrementalUpdates = true
	if len(p.Validate()) == 0 {
		t.Fatal("serve_direct passed validation with no rationale")
	}
	p.Sync.ServeDirectRationale = "a few hundred records a day under CC0"
	if errs := p.Validate(); len(errs) != 0 {
		t.Fatalf("valid entry rejected: %v", errs)
	}
}

func TestValidate_UnverifiedSyncMustSaySoWhy(t *testing.T) {
	p := baseProvider()
	p.BulkAccess = true
	p.Sync = Sync{Modes: []SyncMode{SyncBulkSnapshot}}
	if len(p.Validate()) == 0 {
		t.Fatal("unverified sync metadata passed with no reason")
	}
	p.Sync.UnverifiedReason = "read from the provider's documentation"
	if errs := p.Validate(); len(errs) != 0 {
		t.Fatalf("valid entry rejected: %v", errs)
	}
}

func TestValidate_SyncAndCoarseBooleansMustAgree(t *testing.T) {
	bulk := baseProvider()
	bulk.Sync = Sync{Modes: []SyncMode{SyncBulkSnapshot}, Verified: true}
	if len(bulk.Validate()) == 0 {
		t.Fatal("a bulk mode with bulk_access false passed validation")
	}
	incremental := baseProvider()
	incremental.Sync = Sync{Modes: []SyncMode{SyncOAIPMH}, Verified: true}
	if len(incremental.Validate()) == 0 {
		t.Fatal("an incremental mode with incremental_updates false passed validation")
	}
}

func TestSnapshotRights_UnknownIsNotPermission(t *testing.T) {
	s := Snapshot{URL: "https://example.org/dump.tar.gz"}
	if s.Rights.RedistributionAllowed() {
		t.Fatal("a snapshot with no rights block permitted redistribution")
	}
	s.Rights.Redistribution = "unknown"
	if s.Rights.RedistributionAllowed() {
		t.Fatal("an explicit unknown permitted redistribution")
	}
}

// The registry as shipped must describe the bulk channels the gateway's own
// providers publish, and describe them honestly.
func TestRegistryFile_SyncPopulated(t *testing.T) {
	r, err := Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"arxiv", "europe-pmc", "openalex", "crossref", "datacite",
		"dblp", "ror", "pubmed", "core", "unpaywall", "semantic-scholar-api",
		"zbmath-open-rest", "clinicaltrials-gov-v2"}
	for _, id := range want {
		p, ok := r.Get(id)
		if !ok {
			t.Fatalf("%s is not in the registry", id)
		}
		if !p.Sync.Declared() {
			t.Fatalf("%s declares no sync capability", id)
		}
		if !p.Sync.Verified && p.Sync.UnverifiedReason == "" {
			t.Fatalf("%s is unverified and says nothing about why", id)
		}
		for _, snap := range p.Sync.Snapshots {
			if snap.URL == "" {
				t.Fatalf("%s: a snapshot with no URL describes nothing", id)
			}
			if snap.Rights.Redistribution == "" {
				t.Fatalf("%s: snapshot %q states no redistribution posture", id, snap.URL)
			}
		}
	}
}

// OAI-PMH endpoints are registered even though no harvester exists yet: an
// agent asking whether one exists deserves the answer.
func TestRegistryFile_OAIPMHRegistered(t *testing.T) {
	r, err := Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for i := range r.Providers {
		p := &r.Providers[i]
		if p.Sync.HasMode(SyncOAIPMH) {
			if p.Sync.OAIPMHEndpoint == "" {
				t.Fatalf("%s declares oai_pmh with no endpoint", p.ProviderID)
			}
			found++
		}
	}
	if found < 4 {
		t.Fatalf("only %d OAI-PMH endpoints registered", found)
	}
}

// A paid or gated snapshot must never read as redistributable, and the
// registry must not claim the gateway can exercise a channel it cannot.
func TestRegistryFile_GatedSnapshotsGrantNothing(t *testing.T) {
	r, err := Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := range r.Providers {
		p := &r.Providers[i]
		for _, snap := range p.Sync.Snapshots {
			if snap.Auth == "" || snap.Auth == "none" {
				continue
			}
			if snap.Rights.RedistributionAllowed() {
				t.Fatalf("%s: a %s snapshot is marked redistributable", p.ProviderID, snap.Auth)
			}
		}
	}
}
