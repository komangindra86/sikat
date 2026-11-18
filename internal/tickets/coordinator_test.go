package tickets_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sikat/internal/notifications"
	"sikat/internal/rbac"
	"sikat/internal/testutil"
	"sikat/internal/tickets"
)

// Koordinator per bidang (keputusan 13/11/2026): tindakan koordinator hanya untuk
// bidangnya; bidang tanpa koordinator aktif → semua koordinator (cadangan).
func TestCoordinatorActionsLimitedToOwnField(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	other := user(testutil.CreateUser(t, w.db, "koor2", "COORDINATOR", "password-koor2", false), rbac.RoleCoordinator)
	var field int64
	if err := w.db.QueryRow(`SELECT it_category_id FROM ticket_categories WHERE id = ?`, w.cat).Scan(&field); err != nil {
		t.Fatal(err)
	}
	setCoord := func(id any) {
		t.Helper()
		if _, err := w.db.Exec(`UPDATE it_categories SET coordinator_user_id = ? WHERE id = ?`, id, field); err != nil {
			t.Fatal(err)
		}
	}
	setCoord(w.coord.ID)
	c, _ := w.newClaimed(t)

	// Koordinator bidang lain: ditolak dengan pesan yang menyebut koordinatornya.
	_, err := w.svc.ChangePriority(ctx, c.ID, other, "CRITICAL", "Ruang operasi")
	if !errors.Is(err, tickets.ErrForbidden) || !strings.Contains(err.Error(), "dikoordinasi oleh") {
		t.Fatalf("ubah prioritas oleh koordinator bidang lain harus ditolak, dapat %v", err)
	}
	if _, err := w.svc.TakeOver(ctx, c.ID, other, "Bantu"); !errors.Is(err, tickets.ErrForbidden) {
		t.Fatalf("ambil alih bidang lain harus ditolak, dapat %v", err)
	}
	if err := w.svc.AddSupport(ctx, c.ID, other, w.other.ID); !errors.Is(err, tickets.ErrForbidden) {
		t.Fatalf("atur anggota tim bidang lain harus ditolak, dapat %v", err)
	}
	if _, err := w.svc.Cancel(ctx, c.ID, other, "Duplikat"); !errors.Is(err, tickets.ErrForbidden) {
		t.Fatalf("batalkan tiket bidang lain harus ditolak, dapat %v", err)
	}
	// Tetap boleh melihat dan memberi catatan.
	if _, _, _, err := w.svc.Get(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if errs, err := w.svc.AddComment(ctx, c.ID, other, "Catatan dari koordinator lain", true); err != nil || errs != nil {
		t.Fatalf("catatan internal tetap boleh: %v %v", errs, err)
	}

	// Koordinator bidang: boleh.
	if errs, err := w.svc.ChangePriority(ctx, c.ID, w.coord, "HIGH", "Dampak lebih luas"); err != nil || errs != nil {
		t.Fatalf("koordinator bidang boleh mengubah prioritas: %v %v", errs, err)
	}
	// Admin (bukan koordinator) tidak terkena batasan bidang untuk pembatalan.
	if errs, err := w.svc.Cancel(ctx, c.ID, w.admin, "Tiket uji"); err != nil || errs != nil {
		t.Fatalf("admin tetap boleh membatalkan: %v %v", errs, err)
	}

	// Cadangan: bidang tanpa koordinator, atau koordinatornya nonaktif → semua koordinator boleh.
	c2, _ := w.newClaimed(t)
	if _, err := w.db.Exec(`UPDATE users SET is_active = FALSE WHERE id = ?`, w.coord.ID); err != nil {
		t.Fatal(err)
	}
	if errs, err := w.svc.ChangePriority(ctx, c2.ID, other, "HIGH", "Koordinator bidang cuti"); err != nil || errs != nil {
		t.Fatalf("koordinator bidang nonaktif → koordinator lain boleh: %v %v", errs, err)
	}
	setCoord(nil)
	if _, err := w.svc.TakeOver(ctx, c2.ID, other, "Bidang tanpa koordinator"); err != nil {
		t.Fatalf("bidang tanpa koordinator → semua koordinator boleh: %v", err)
	}
	fc, err := w.svc.TicketFieldCoordinator(ctx, c2.ID)
	if err != nil || fc.Active || !fc.CanCoordinate(other) {
		t.Fatalf("status koordinator bidang: %+v %v", fc, err)
	}
}

func TestCoordinatorNotificationsFollowField(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	n := notifSvc(w, nil)
	other := testutil.CreateUser(t, w.db, "koor2", "COORDINATOR", "password-koor2", false)
	var field int64
	_ = w.db.QueryRow(`SELECT it_category_id FROM ticket_categories WHERE id = ?`, w.cat).Scan(&field)

	// Bidang punya koordinator → hanya dia (di antara koordinator) yang menerima.
	if _, err := w.db.Exec(`UPDATE it_categories SET coordinator_user_id = ? WHERE id = ?`, w.coord.ID, field); err != nil {
		t.Fatal(err)
	}
	c1, _, _ := w.svc.Create(ctx, w.draft(nonCritical), nil)
	process(t, n)
	count := func(uid, ticket int64) int {
		return testutil.Count(t, w.db, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND ticket_id = ?`, uid, ticket)
	}
	if count(w.coord.ID, c1.ID) != 1 || count(other, c1.ID) != 0 {
		t.Fatal("tiket baru hanya untuk koordinator bidangnya")
	}
	// Koordinator lain yang ikut menangani (anggota tim) tetap menerima kabar tiket itu.
	if r, err := w.svc.Claim(ctx, c1.ID, other); err != nil || !r.OK {
		t.Fatalf("klaim oleh koordinator lain: %v", err)
	}
	if err := w.svc.AddSupport(ctx, c1.ID, w.coord, w.tech.ID); err != nil {
		t.Fatal(err)
	}
	if errs, err := w.svc.Pend(ctx, c1.ID, user(other, rbac.RoleCoordinator), w.reasonID, "Menunggu PSU"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	process(t, n)
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND ticket_id = ? AND event_type = 'TICKET_PENDING'`, w.coord.ID, c1.ID) != 1 {
		t.Fatal("koordinator bidang menerima kabar tiket tertunda")
	}

	// Bidang tanpa koordinator → semua koordinator (cadangan).
	if _, err := w.db.Exec(`UPDATE it_categories SET coordinator_user_id = NULL WHERE id = ?`, field); err != nil {
		t.Fatal(err)
	}
	c2, _, _ := w.svc.Create(ctx, w.draft(nonCritical), nil)
	process(t, n)
	if count(w.coord.ID, c2.ID) != 1 || count(other, c2.ID) != 1 {
		t.Fatal("bidang tanpa koordinator: semua koordinator menerima")
	}
	if inbox(t, w, w.coord.ID, notifications.TicketCreated) != 2 {
		t.Fatal("koordinator bidang menerima kedua tiket baru")
	}
}
