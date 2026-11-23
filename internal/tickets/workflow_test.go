package tickets_test

import (
	"context"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/rbac"
	"sikat/internal/testutil"
	"sikat/internal/tickets"
)

type wf struct {
	fx
	clk         *clock.Fake
	tech, other *rbac.User
	coord       *rbac.User
	admin       *rbac.User
	reasonID    int64
	otherReason int64
}

func user(id int64, role string) *rbac.User {
	return &rbac.User{ID: id, RoleCode: role, FullName: "U"}
}

func setupWF(t *testing.T) wf {
	f := setup(t)
	clk := clock.NewFake(time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC))
	f.svc = tickets.NewService(f.db, clk, f.store)
	w := wf{fx: f, clk: clk}
	w.tech = user(testutil.CreateUser(t, f.db, "tek1", "TECHNICIAN", "password-tek1", false), rbac.RoleTechnician)
	w.other = user(testutil.CreateUser(t, f.db, "tek2", "TECHNICIAN", "password-tek2", false), rbac.RoleTechnician)
	w.coord = user(testutil.CreateUser(t, f.db, "koor", "COORDINATOR", "password-koor", false), rbac.RoleCoordinator)
	w.admin = user(testutil.CreateUser(t, f.db, "adm", "ADMIN", "password-adm1", false), rbac.RoleAdmin)
	_ = f.db.QueryRow(`SELECT id FROM pending_reasons WHERE label = 'Menunggu sparepart'`).Scan(&w.reasonID)
	_ = f.db.QueryRow(`SELECT id FROM pending_reasons WHERE label = 'Lainnya'`).Scan(&w.otherReason)
	return w
}

func (w wf) newClaimed(t *testing.T) (tickets.Created, string) {
	c, errs, err := w.svc.Create(context.Background(), w.draft(nil), nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if res, err := w.svc.Claim(context.Background(), c.ID, w.tech.ID); err != nil || !res.OK {
		t.Fatalf("klaim: %v %+v", err, res)
	}
	return c, c.Token
}

func status(t *testing.T, w wf, id int64) string {
	var s string
	if err := w.db.QueryRow(`SELECT status FROM tickets WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFullLifecycleWithReporterFeedback(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	c, token := w.newClaimed(t)

	if err := w.svc.Start(ctx, c.ID, w.other); err != tickets.ErrForbidden {
		t.Fatalf("bukan penanggung jawab tidak boleh memulai, dapat %v", err)
	}
	if err := w.svc.Start(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}
	// Tunda: alasan wajib; alasan "Lainnya" wajib keterangan.
	if errs, _ := w.svc.Pend(ctx, c.ID, w.tech, 0, ""); errs["pending_reason_id"] == "" {
		t.Fatal("tunda tanpa alasan harus ditolak")
	}
	if errs, _ := w.svc.Pend(ctx, c.ID, w.tech, w.otherReason, ""); errs["note"] == "" {
		t.Fatal("alasan Lainnya tanpa keterangan harus ditolak")
	}
	if errs, err := w.svc.Pend(ctx, c.ID, w.tech, w.reasonID, "PSU dipesan"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if status(t, w, c.ID) != "PENDING" {
		t.Fatal("status harus PENDING")
	}
	// Tidak bisa diselesaikan langsung dari PENDING.
	if _, err := w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{Action: "x", Result: "FIXED"}); err != tickets.ErrTransition {
		t.Fatalf("resolve dari PENDING harus ditolak, dapat %v", err)
	}
	w.clk.Advance(time.Hour)
	if err := w.svc.Resume(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}
	// Selesaikan: tindakan & hasil wajib.
	errs, err := w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{})
	if err != nil || errs["resolution_action"] == "" || errs["resolution_result"] == "" {
		t.Fatalf("resolve tanpa tindakan/hasil harus ditolak: %v %v", errs, err)
	}
	errs, _ = w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{Action: "x", Result: "FIXED", RestoredAt: "2030-01-01T00:00"})
	if errs["service_restored_at"] == "" {
		t.Fatal("waktu pulih di masa depan harus ditolak")
	}
	if errs, err := w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{Action: "Ganti PSU", Result: "REPLACED"}); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}

	// Pelapor: "belum beres" wajib keterangan → dibuka kembali, siklus 2.
	if errs, _ := w.svc.ReporterFeedback(ctx, token, false, ""); errs["feedback_note"] == "" {
		t.Fatal("belum beres tanpa keterangan harus ditolak")
	}
	if errs, err := w.svc.ReporterFeedback(ctx, token, false, "Masih mati"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	tk, _, _, _ := w.svc.Get(ctx, c.ID)
	if tk.Status != "IN_PROGRESS" || tk.SLACycle != 2 || tk.ResolvedAt.Valid || tk.ReporterFeedback.String != "NOT_OK" {
		t.Fatalf("buka kembali oleh pelapor: %+v", tk)
	}
	if errs, err := w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{Action: "Ganti kabel power", Result: "FIXED"}); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if errs, err := w.svc.ReporterFeedback(ctx, token, true, ""); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if status(t, w, c.ID) != "CLOSED" {
		t.Fatal("konfirmasi pelapor harus menutup tiket")
	}
	// Status akhir: tidak ada aksi lagi.
	if _, err := w.svc.ReporterFeedback(ctx, token, true, ""); err != tickets.ErrTransition {
		t.Fatalf("konfirmasi ganda harus ditolak, dapat %v", err)
	}
	if _, err := w.svc.AddReporterComment(ctx, token, "halo"); err != tickets.ErrTransition {
		t.Fatal("pelapor tidak bisa berkomentar di tiket yang ditutup")
	}

	events := map[string]int{}
	_, hist, _, _ := w.svc.Get(ctx, c.ID)
	for _, h := range hist {
		events[h.EventType]++
	}
	for ev, n := range map[string]int{"CREATED": 1, "CLAIMED": 1, "STARTED": 1, "PENDING": 1, "RESUMED": 1, "RESOLVED": 2, "REOPENED": 1, "REPORTER_CONFIRMED": 1} {
		if events[ev] != n {
			t.Errorf("histori %s = %d, ingin %d (%v)", ev, events[ev], n, events)
		}
	}
}

func TestReleaseCancelAndPermissions(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	c, _ := w.newClaimed(t)

	if errs, _ := w.svc.Release(ctx, c.ID, w.tech, " "); errs["reason"] == "" {
		t.Fatal("lepas tiket tanpa alasan harus ditolak")
	}
	if _, err := w.svc.Release(ctx, c.ID, w.tech, "Bukan bidang saya"); err != nil {
		t.Fatal(err)
	}
	tk, _, _, _ := w.svc.Get(ctx, c.ID)
	if tk.Status != "NEW" || tk.AssigneeID.Valid {
		t.Fatalf("tiket harus kembali ke antrean tanpa PJ: %+v", tk)
	}
	if n := testutil.Count(t, w.db, `SELECT COUNT(*) FROM ticket_assignments WHERE ticket_id = ? AND released_at IS NULL`, c.ID); n != 0 {
		t.Fatal("assignment harus dilepas")
	}
	// Bisa diambil lagi oleh petugas lain.
	if res, _ := w.svc.Claim(ctx, c.ID, w.other.ID); !res.OK {
		t.Fatal("tiket yang dilepas harus bisa diambil lagi")
	}

	if _, err := w.svc.Cancel(ctx, c.ID, w.tech, "duplikat"); err != tickets.ErrForbidden {
		t.Fatalf("petugas tidak boleh membatalkan, dapat %v", err)
	}
	if errs, _ := w.svc.Cancel(ctx, c.ID, w.coord, ""); errs["reason"] == "" {
		t.Fatal("batal tanpa alasan harus ditolak")
	}
	if _, err := w.svc.Cancel(ctx, c.ID, w.admin, "Laporan ganda dengan TKT lain"); err != nil {
		t.Fatalf("admin boleh membatalkan: %v", err)
	}
	if status(t, w, c.ID) != "CANCELLED" {
		t.Fatal("status harus CANCELLED")
	}
	if err := w.svc.Start(ctx, c.ID, w.other); err != tickets.ErrForbidden && err != tickets.ErrTransition {
		t.Fatalf("tiket batal tidak bisa dikerjakan, dapat %v", err)
	}
	if n := testutil.Count(t, w.db, `SELECT COUNT(*) FROM tickets WHERE id = ? AND assignee_id IS NOT NULL AND status = 'CANCELLED'`, c.ID); n != 1 {
		t.Fatal("penanggung jawab terakhir tetap tercatat pada tiket batal")
	}
}

func TestTakeOverSupportAndPriority(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	c, token := w.newClaimed(t)

	// Pendukung: PJ menambah, ganda ditolak, PJ sendiri ditolak.
	if err := w.svc.AddSupport(ctx, c.ID, w.other, w.coord.ID); err != tickets.ErrForbidden {
		t.Fatalf("bukan PJ/koordinator tidak boleh menambah anggota, dapat %v", err)
	}
	if err := w.svc.AddSupport(ctx, c.ID, w.tech, w.other.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.AddSupport(ctx, c.ID, w.tech, w.other.ID); err != tickets.ErrMemberExists {
		t.Fatalf("anggota ganda harus ditolak, dapat %v", err)
	}
	if err := w.svc.AddSupport(ctx, c.ID, w.tech, w.tech.ID); err != tickets.ErrMemberInvalid {
		t.Fatalf("PJ tidak bisa jadi pendukung, dapat %v", err)
	}
	if err := w.svc.AddSupport(ctx, c.ID, w.tech, w.admin.ID); err != tickets.ErrMemberInvalid {
		t.Fatalf("admin bukan petugas lapangan, dapat %v", err)
	}
	// Pendukung boleh unggah foto, petugas lain tidak.
	if err := w.svc.AddPhotos(ctx, c.ID, w.other, []tickets.Photo{pngPhoto(t)}, false); err != nil {
		t.Fatalf("pendukung boleh unggah foto: %v", err)
	}

	// Ambil alih: hanya koordinator, alasan wajib.
	if _, err := w.svc.TakeOver(ctx, c.ID, w.tech, "x"); err != tickets.ErrForbidden {
		t.Fatal("petugas tidak boleh ambil alih")
	}
	if errs, _ := w.svc.TakeOver(ctx, c.ID, w.coord, ""); errs["reason"] == "" {
		t.Fatal("ambil alih tanpa alasan harus ditolak")
	}
	if _, err := w.svc.TakeOver(ctx, c.ID, w.coord, "Petugas cuti"); err != nil {
		t.Fatal(err)
	}
	members, _ := w.svc.Members(ctx, c.ID)
	if len(members) != 2 || members[0].UserID != w.coord.ID || members[0].Role != "PRIMARY" {
		t.Fatalf("koordinator harus jadi PJ, pendukung tetap: %+v", members)
	}
	if err := w.svc.Start(ctx, c.ID, w.tech); err != tickets.ErrForbidden {
		t.Fatal("PJ lama tidak boleh lagi mengubah status")
	}

	// Ubah prioritas: koordinator dengan alasan; histori mencatat dari → ke.
	if _, err := w.svc.ChangePriority(ctx, c.ID, w.tech, "HIGH", "x"); err != tickets.ErrForbidden {
		t.Fatal("petugas tidak boleh ubah prioritas")
	}
	if errs, _ := w.svc.ChangePriority(ctx, c.ID, w.coord, "HIGH", ""); errs["reason"] == "" {
		t.Fatal("ubah prioritas tanpa alasan harus ditolak")
	}
	if errs, err := w.svc.ChangePriority(ctx, c.ID, w.coord, "HIGH", "Kasir utama, antrean pasien panjang"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	var src, from, to string
	_ = w.db.QueryRow(`SELECT priority_source FROM tickets WHERE id = ?`, c.ID).Scan(&src)
	_ = w.db.QueryRow(`SELECT from_priority, to_priority FROM ticket_histories WHERE ticket_id = ? AND event_type = 'PRIORITY_CHANGED'`, c.ID).Scan(&from, &to)
	if src != "MANUAL" || from != "MEDIUM" || to != "HIGH" {
		t.Fatalf("prioritas manual: src=%s %s→%s", src, from, to)
	}

	// Catatan internal tidak terlihat pelapor.
	if _, err := w.svc.AddComment(ctx, c.ID, w.coord, "Cek garansi dulu", true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.AddComment(ctx, c.ID, w.coord, "Teknisi menuju lokasi", false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.AddReporterComment(ctx, token, "Saya di ruang sebelah"); err != nil {
		t.Fatal(err)
	}
	all, _ := w.svc.Comments(ctx, c.ID, false)
	pub, _ := w.svc.Comments(ctx, c.ID, true)
	if len(all) != 3 || len(pub) != 2 {
		t.Fatalf("catatan: semua=%d publik=%d", len(all), len(pub))
	}
	for _, cm := range pub {
		if cm.IsInternal {
			t.Fatal("catatan internal bocor ke pelapor")
		}
	}
	_, _, pubAtts, _ := w.svc.GetPublic(ctx, token)
	if len(pubAtts) != 0 {
		t.Fatal("foto petugas yang tidak ditandai untuk pelapor tidak boleh terlihat")
	}
}

func TestAutoCloseIdempotent(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	resolve := func(c tickets.Created) {
		if err := w.svc.Start(ctx, c.ID, w.tech); err != nil {
			t.Fatal(err)
		}
		if errs, err := w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{Action: "ok", Result: "FIXED"}); err != nil || errs != nil {
			t.Fatalf("%v %v", errs, err)
		}
	}
	old, _ := w.newClaimed(t)
	resolve(old)
	w.clk.Advance(2 * 24 * time.Hour)
	fresh, _ := w.newClaimed(t)
	resolve(fresh)
	w.clk.Advance(24*time.Hour + time.Minute) // old: 3 hari+, fresh: 1 hari

	n, err := w.svc.AutoClose(ctx, 3)
	if err != nil || n != 1 {
		t.Fatalf("hanya tiket lama yang ditutup: n=%d err=%v", n, err)
	}
	if status(t, w, old.ID) != "CLOSED" || status(t, w, fresh.ID) != "RESOLVED" {
		t.Fatal("status setelah tutup otomatis salah")
	}
	n, _ = w.svc.AutoClose(ctx, 3)
	if n != 0 {
		t.Fatalf("menjalankan ulang tidak boleh menutup apa pun lagi, n=%d", n)
	}
	if c := testutil.Count(t, w.db, `SELECT COUNT(*) FROM ticket_histories WHERE event_type = 'AUTO_CLOSED'`); c != 1 {
		t.Fatalf("histori AUTO_CLOSED harus tepat 1, dapat %d", c)
	}
}
