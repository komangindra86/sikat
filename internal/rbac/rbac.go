// Package rbac berisi role, permission, dan matriks hak akses.
// Permission didefinisikan di kode agar mudah diaudit dan diuji; tabel roles
// hanya menyimpan role sistem beserta nama tampilannya.
package rbac

import "unicode"

// Role sistem.
const (
	RoleAdmin       = "ADMIN"
	RoleCoordinator = "COORDINATOR"
	RoleTechnician  = "TECHNICIAN"
)

// Permission adalah hak akses atas satu kemampuan.
type Permission string

// Daftar permission. Yang belum dipakai di fase berjalan tetap didefinisikan
// agar matriks lengkap sesuai docs/ARCHITECTURE.md §5.
const (
	PermDashboard      Permission = "dashboard.view"
	PermAssetView      Permission = "asset.view"
	PermMasterManage   Permission = "master.manage"
	PermUserManage     Permission = "user.manage"
	PermConfigManage   Permission = "config.manage"
	PermAuditView      Permission = "audit.view"
	PermTicketQueue    Permission = "ticket.view_queue"
	PermTicketClaim    Permission = "ticket.claim"
	PermTicketCreate   Permission = "ticket.create"
	PermTicketTakeover Permission = "ticket.take_over"
	PermTicketPriority Permission = "ticket.change_priority"
	PermTicketCancel   Permission = "ticket.cancel"
	PermTicketWork     Permission = "ticket.update_own"
	PermActivityCreate Permission = "activity.create"
	PermActivityAll    Permission = "activity.manage_all"
	PermReportAll      Permission = "report.view_all"
	PermReportOwn      Permission = "report.view_own"
	PermIntegration    Permission = "integration.manage"
	// PermMaintenanceRecord: mencatat riwayat pemeliharaan manual pada perangkat.
	PermMaintenanceRecord Permission = "maintenance.record"
	// PermMaintenanceReview: membatalkan entri riwayat & memutuskan peringatan kerusakan berulang.
	PermMaintenanceReview Permission = "maintenance.review"
	// PermSLAView: melihat dashboard SLA, eskalasi, dan laporan pelanggaran bulanan.
	PermSLAView Permission = "sla.view"
)

var matrix = map[string]map[Permission]bool{
	RoleAdmin: set(
		PermDashboard, PermAssetView, PermMasterManage, PermUserManage, PermConfigManage,
		PermAuditView, PermTicketQueue, PermTicketCancel, PermReportAll, PermIntegration, PermSLAView,
	),
	RoleCoordinator: set(
		PermDashboard, PermAssetView, PermTicketQueue, PermTicketClaim, PermTicketCreate, PermTicketTakeover,
		PermTicketPriority, PermTicketCancel, PermTicketWork, PermActivityCreate, PermActivityAll,
		PermReportAll, PermMaintenanceRecord, PermMaintenanceReview, PermSLAView,
	),
	RoleTechnician: set(
		PermDashboard, PermAssetView, PermTicketQueue, PermTicketClaim, PermTicketCreate, PermTicketWork,
		PermActivityCreate, PermReportOwn, PermMaintenanceRecord,
	),
}

func set(perms ...Permission) map[Permission]bool {
	m := make(map[Permission]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// RoleHas memeriksa apakah role memiliki permission.
func RoleHas(role string, p Permission) bool {
	return matrix[role][p]
}

// ValidRole memeriksa apakah kode role dikenal.
func ValidRole(role string) bool {
	_, ok := matrix[role]
	return ok
}

// User adalah identitas pengguna yang sedang login.
type User struct {
	ID                 int64
	Username           string
	FullName           string
	RoleCode           string
	RoleName           string
	MustChangePassword bool
}

// Can memeriksa permission. Menerima string agar mudah dipanggil dari template.
func (u *User) Can(p string) bool {
	if u == nil {
		return false
	}
	return RoleHas(u.RoleCode, Permission(p))
}

// Has memeriksa permission bertipe.
func (u *User) Has(p Permission) bool {
	if u == nil {
		return false
	}
	return RoleHas(u.RoleCode, p)
}

// Initials mengembalikan inisial maksimal dua huruf (untuk avatar).
func (u *User) Initials() string {
	if u == nil {
		return ""
	}
	var out []rune
	prevSpace := true
	for _, r := range u.FullName {
		if r == ' ' {
			prevSpace = true
			continue
		}
		if prevSpace && len(out) < 2 {
			out = append(out, unicode.ToUpper(r))
		}
		prevSpace = false
	}
	return string(out)
}

// FirstName mengembalikan kata pertama nama lengkap (untuk sapaan).
func (u *User) FirstName() string {
	if u == nil {
		return ""
	}
	for i, r := range u.FullName {
		if r == ' ' {
			return u.FullName[:i]
		}
	}
	return u.FullName
}
