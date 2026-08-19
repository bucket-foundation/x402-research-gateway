package handler

import (
	"testing"

	"github.com/gianyrox/x402-research-gateway/internal/asset"
	"github.com/gianyrox/x402-research-gateway/internal/provider"
)

func TestMapRightsResearchActionsFromExplicitLicense(t *testing.T) {
	tests := []struct {
		name       string
		license    string
		licenseURL string
		want       asset.Permission
	}{
		{"cc-by", "cc-by", "", asset.PermissionAllowed},
		{"cc-by-sa", "CC-BY-SA-4.0", "", asset.PermissionAllowed},
		{"cc0", "cc0", "", asset.PermissionAllowed},
		{
			"cc-by-url",
			"CC-BY family",
			"https://creativecommons.org/licenses/by/4.0/",
			asset.PermissionAllowed,
		},
		{"cc-by-nc", "cc-by-nc", "", asset.PermissionUnknown},
		{"cc-by-nd", "cc-by-nd", "", asset.PermissionUnknown},
		{"absent", "", "", asset.PermissionUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapRights(provider.Rights{
				License:    tc.license,
				LicenseURL: tc.licenseURL,
			}, nil)

			if got.TDM != tc.want {
				t.Fatalf("tdm = %q, want %q", got.TDM, tc.want)
			}
			if got.Retention != tc.want {
				t.Fatalf("retention = %q, want %q", got.Retention, tc.want)
			}
		})
	}
}
