package domain

import (
	"slices"
	"testing"
	"time"
)

// The table from the "Roles and permissions" section of api/openapi.yaml.
func TestRolePermissionsTable(t *testing.T) {
	want := map[Role][]Permission{
		RoleOwner: {"apps:read", "apps:write", "audit:read", "connections:read", "connections:write", "deliveries:read",
			"deliveries:replay", "payments:read", "reconciliation:write", "users:manage"},
		RoleAdmin: {"apps:read", "apps:write", "audit:read", "connections:read", "connections:write", "deliveries:read",
			"deliveries:replay", "payments:read", "reconciliation:write"},
		RoleViewer: {"apps:read", "connections:read", "deliveries:read", "payments:read"},
	}
	if len(RolePermissions) != len(want) {
		t.Fatalf("roles = %v", RolePermissions)
	}
	for role, perms := range want {
		if got := RolePermissions[role]; !slices.Equal(got, perms) {
			t.Errorf("%s = %v, want %v", role, got, perms)
		}
		if !slices.IsSorted(role.Permissions()) {
			t.Errorf("%s permissions not sorted", role)
		}
	}
	if !slices.Equal(AllPermissions(), want[RoleOwner]) {
		t.Fatalf("AllPermissions = %v", AllPermissions())
	}
}

func TestRoleCan(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleOwner, PermUsersManage, true},
		{RoleAdmin, PermUsersManage, false},
		{RoleAdmin, PermAppsWrite, true},
		{RoleAdmin, PermAuditRead, true},
		{RoleViewer, PermAppsRead, true},
		{RoleViewer, PermAppsWrite, false},
		{RoleViewer, PermDeliveriesReplay, false},
		{RoleViewer, PermAuditRead, false},
		{RoleViewer, PermConnectionsRead, true},
		{RoleViewer, PermConnectionsWrite, false},
		{RoleAdmin, PermConnectionsWrite, true},
		{RoleOwner, PermReconciliationWrite, true},
		{RoleAdmin, PermReconciliationWrite, true},
		{RoleViewer, PermReconciliationWrite, false},
		{RoleViewer, PermPaymentsRead, true},
		{Role("root"), PermAppsRead, false},
		{"", PermAppsRead, false},
	}
	for _, tc := range cases {
		if got := tc.role.Can(tc.perm); got != tc.want {
			t.Errorf("%q.Can(%s) = %v, want %v", tc.role, tc.perm, got, tc.want)
		}
	}
}

func TestRoleValidAndPermissionsCopy(t *testing.T) {
	for _, r := range []Role{RoleOwner, RoleAdmin, RoleViewer} {
		if !r.Valid() {
			t.Errorf("%s invalid", r)
		}
	}
	for _, r := range []Role{"", "Owner", "root"} {
		if r.Valid() {
			t.Errorf("%q valid", r)
		}
	}
	perms := RoleViewer.Permissions()
	perms[0] = "hacked"
	if RolePermissions[RoleViewer][0] == "hacked" {
		t.Fatal("Permissions returned the shared slice")
	}
	if p := Role("root").Permissions(); p == nil || len(p) != 0 {
		t.Fatalf("unknown role permissions = %#v, want empty non-nil", p)
	}
}

func TestUserStatus(t *testing.T) {
	u := &User{Role: RoleOwner}
	if u.Status() != UserActive || !u.IsActiveOwner() {
		t.Fatal("active owner")
	}
	now := time.Now()
	u.DisabledAt = &now
	if u.Status() != UserDisabled || u.IsActiveOwner() {
		t.Fatal("disabled owner counted as active")
	}
}

func TestRateLimitedError(t *testing.T) {
	if e := RateLimited(0); e.Code != CodeRateLimited || e.Details["retry_after_seconds"] != 1 {
		t.Fatalf("RateLimited(0) = %+v", e)
	}
	if e := RateLimited(90); e.Details["retry_after_seconds"] != 90 {
		t.Fatalf("RateLimited(90) = %+v", e)
	}
	e := InvalidFields()
	if fields, ok := e.Details["fields"].([]FieldError); !ok || fields == nil {
		t.Fatalf("InvalidFields() details = %#v", e.Details)
	}
}
