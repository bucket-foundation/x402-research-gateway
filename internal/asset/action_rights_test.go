package asset

import "testing"

func TestNormalizeActionRightsDefaultsToUnknown(t *testing.T) {
	got := (Rights{}).Normalize()

	if got.TDM != PermissionUnknown {
		t.Fatalf("tdm = %q, want unknown", got.TDM)
	}
	if got.Retention != PermissionUnknown {
		t.Fatalf("retention = %q, want unknown", got.Retention)
	}
}
