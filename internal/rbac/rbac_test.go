package rbac

import "testing"

func TestMatrix(t *testing.T) {
	cases := []struct {
		role string
		perm Permission
		want bool
	}{
		{RoleAdmin, PermMasterManage, true},
		{RoleAdmin, PermUserManage, true},
		{RoleAdmin, PermConfigManage, true},
		{RoleAdmin, PermTicketClaim, false}, // admin tidak menangani tiket (asumsi Q6)
		{RoleCoordinator, PermMasterManage, false},
		{RoleCoordinator, PermTicketTakeover, true},
		{RoleCoordinator, PermTicketPriority, true},
		{RoleCoordinator, PermUserManage, false},
		{RoleTechnician, PermTicketClaim, true},
		{RoleTechnician, PermTicketTakeover, false},
		{RoleTechnician, PermTicketPriority, false},
		{RoleTechnician, PermMasterManage, false},
		{RoleTechnician, PermAssetView, true},
		{"UNKNOWN", PermDashboard, false},
	}
	for _, c := range cases {
		if got := RoleHas(c.role, c.perm); got != c.want {
			t.Errorf("RoleHas(%s, %s) = %v, ingin %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestUserHelpers(t *testing.T) {
	var nilUser *User
	if nilUser.Can("dashboard.view") || nilUser.Has(PermDashboard) {
		t.Fatal("user nil tidak boleh punya permission")
	}
	u := &User{FullName: "Budi Santoso", RoleCode: RoleTechnician}
	if u.FirstName() != "Budi" {
		t.Fatalf("FirstName = %q", u.FirstName())
	}
	if !u.Can("asset.view") || u.Can("user.manage") {
		t.Fatal("Can tidak sesuai matriks")
	}
}
