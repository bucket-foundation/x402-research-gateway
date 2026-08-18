package asset

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gianyrox/x402-research-gateway/internal/identity"
)

var at = time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)

func query(t *testing.T) identity.Identifier {
	t.Helper()
	id, ok := identity.Parse("10.7717/peerj.4375")
	if !ok {
		t.Fatal("fixture identifier did not parse")
	}
	return id
}

func TestRights_UnknownIsNeverPermitted(t *testing.T) {
	for name, r := range map[string]Rights{
		"zero value":  {},
		"explicit":    {Redistribution: RedistributionUnknown},
		"prohibited":  {Redistribution: RedistributionProhibited},
		"free toread": {FreeToRead: true},
		"licensed but unstated": {License: "some publisher licence",
			LicenseURL: "https://example.org/terms"},
	} {
		if r.Normalize().Permits() {
			t.Fatalf("%s: permitted redistribution", name)
		}
	}
	if !(Rights{Redistribution: RedistributionAllowed}).Normalize().Permits() {
		t.Fatal("an explicit allowance was not honored")
	}
}

func TestRights_NormalizeFillsZeroValueAndRejectsGarbage(t *testing.T) {
	if got := (Rights{}).Normalize().Redistribution; got != RedistributionUnknown {
		t.Fatalf("zero value normalized to %q", got)
	}
	got := (Rights{Redistribution: Redistribution("permitted-ish")}).Normalize()
	if got.Redistribution != RedistributionUnknown {
		t.Fatalf("unrecognized value normalized to %q", got.Redistribution)
	}
	if !strings.Contains(got.Source, "permitted-ish") {
		t.Fatalf("the rejected value was not recorded: %q", got.Source)
	}
}

func TestBuild_AbsentIsAnAnswer(t *testing.T) {
	set := Build(query(t), nil, []ProviderReport{
		{Provider: "unpaywall-fetch", Consulted: true, Outcome: OutcomeOK},
		{Provider: "europepmc-fetch", Consulted: true, Outcome: OutcomeOK},
	}, at)
	if set.Availability != AvailabilityAbsent {
		t.Fatalf("availability = %q", set.Availability)
	}
	if set.OpenAccessCopyFound {
		t.Fatal("no assets yet open_access_copy_found is true")
	}
	if len(set.ProvidersConsulted) != 2 {
		t.Fatalf("providers dropped: %+v", set.ProvidersConsulted)
	}
	if set.DiscoveryNotice == "" {
		t.Fatal("discovery notice missing")
	}
}

func TestBuild_NoProviderAnsweredIsUnknownNotAbsent(t *testing.T) {
	set := Build(query(t), nil, []ProviderReport{
		{Provider: "unpaywall-fetch", Consulted: true, Outcome: OutcomeTimeout},
		{Provider: "core-search", Outcome: OutcomeNotConfigured},
	}, at)
	if set.Availability != AvailabilityUnknown {
		t.Fatalf("availability = %q, want unknown; a failed fan-out is not a negative result", set.Availability)
	}
}

func TestBuild_RetrievableWinsAndRightsStayPerAsset(t *testing.T) {
	set := Build(query(t), []Asset{
		{Provider: "crossref-fetch", AssetID: "b", CanonicalURL: "https://publisher.example/pdf",
			Availability: AvailabilityRestricted},
		{Provider: "unpaywall-fetch", AssetID: "a", CanonicalURL: "https://repo.example/pdf",
			Availability: AvailabilityRetrievable,
			Rights:       Rights{License: "cc-by", Redistribution: RedistributionAllowed, FreeToRead: true}},
	}, []ProviderReport{
		{Provider: "unpaywall-fetch", Consulted: true, Outcome: OutcomeOK, AssetCount: 1},
		{Provider: "crossref-fetch", Consulted: true, Outcome: OutcomeOK, AssetCount: 1},
	}, at)

	if set.Availability != AvailabilityRetrievable {
		t.Fatalf("availability = %q", set.Availability)
	}
	if !set.OpenAccessCopyFound {
		t.Fatal("a free-to-read retrievable asset did not set open_access_copy_found")
	}
	if set.Assets[0].Provider != "crossref-fetch" {
		t.Fatalf("assets are not sorted by provider: %+v", set.Assets)
	}
	// The publisher asset carried no rights statement, so it must read
	// unknown rather than inheriting the permissive one beside it.
	if set.Assets[0].Rights.Permits() {
		t.Fatal("an asset with no rights statement was reported as permitted")
	}
	if !set.Assets[1].Rights.Permits() {
		t.Fatal("an explicit per-asset allowance was lost")
	}
	for _, a := range set.Assets {
		if a.RetrievedAt == "" {
			t.Fatalf("asset %s has no retrieval timestamp", a.AssetID)
		}
	}
}

func TestBuild_MetadataRightsSeparateFromContentRights(t *testing.T) {
	set := Build(query(t), []Asset{
		{Provider: "crossref-fetch", AssetID: "a", CanonicalURL: "https://publisher.example/pdf"},
	}, []ProviderReport{
		{Provider: "crossref-fetch", Consulted: true, Outcome: OutcomeOK, AssetCount: 1,
			MetadataRights: Rights{License: "CC0", Redistribution: RedistributionAllowed}},
	}, at)
	if !set.ProvidersConsulted[0].MetadataRights.Permits() {
		t.Fatal("metadata rights lost")
	}
	if set.Assets[0].Rights.Permits() {
		t.Fatal("a CC0 metadata licence leaked onto the content rights")
	}
}

func TestBuild_Deterministic(t *testing.T) {
	assets := []Asset{
		{Provider: "b", AssetID: "2", CanonicalURL: "https://x/2"},
		{Provider: "a", AssetID: "1", CanonicalURL: "https://x/1"},
	}
	reports := []ProviderReport{{Provider: "b", Outcome: OutcomeOK}, {Provider: "a", Outcome: OutcomeOK}}
	one, _ := json.Marshal(Build(query(t), assets, reports, at))
	two, _ := json.Marshal(Build(query(t), []Asset{assets[1], assets[0]},
		[]ProviderReport{reports[1], reports[0]}, at))
	if string(one) != string(two) {
		t.Fatal("Build is not order-independent")
	}
}
