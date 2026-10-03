package model

import "testing"

func TestRolePublishingPermissions(t *testing.T) {
	for _, tc := range []struct {
		role       Role
		canPublish bool
		isAdmin    bool
	}{
		{RoleAdmin, true, true},
		{RoleEditor, true, false},
		{RoleUser, false, false},
		{"", false, false},
		{"administrator", false, false},
		{"Admin", false, false},
		{"editor ", false, false},
	} {
		if got := tc.role.CanPublish(); got != tc.canPublish {
			t.Errorf("role %q CanPublish() = %t, want %t", tc.role, got, tc.canPublish)
		}
		if got := tc.role.IsAdmin(); got != tc.isAdmin {
			t.Errorf("role %q IsAdmin() = %t, want %t", tc.role, got, tc.isAdmin)
		}
	}
}
