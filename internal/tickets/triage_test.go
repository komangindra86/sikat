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

func catID(t *testing.T, w wf, name string) int64 {
	t.Helper()
	var id int64
	if err := w.db.QueryRow(`SELECT id FROM ticket_categories WHERE name = ?`, name).Scan(&id); err != nil {
		t.Fatalf("jenis %q: %v", name, err)
	}
	return id
}

func fieldID(t *testing.T, w wf, name string) int64 {
	t.Helper()
	var id int64
	if err := w.db.QueryRow(`SELECT id FROM it_categories WHERE name = ?`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTriageTicketIsSortedIntoTheRightField(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	n := notifSvc(w, nil)
	triage, network, hw := fieldID(t, w, "Triase / Belum terklasifikasi"), fieldID(t, w, "Pelayanan & Support"), fieldID(t, w, "ERM")
	now := w.clk.Now()
	// tek1: kompetensi ERM; tek2: kompetensi Pelayanan & Support (jaringan).
	if _, err := w.db.Exec(`INSERT INTO user_competencies (user_id, it_category_id, created_at) VALUES (?, ?, ?), (?, ?, ?)`,
		w.tech.ID, hw, now, w.other.ID, network, now); err != nil {
		t.Fatal(err)
	}
	koorNet := user(testutil.CreateUser(t, w.db, "koornet", "COORDINATOR", "password-koornet", false), rbac.RoleCoordinator)
	if _, err := w.db.Exec(`UPDATE it_categories SET coordinator_user_id = CASE id WHEN ? THEN ? WHEN ? THEN ? END WHERE id IN (?, ?)`,
		triage, w.coord.ID, network, koorNet.ID, triage, network); err != nil {
		t.Fatal(err)
	}

	// Pelapor memilih "Lainnya" → bidang Triase.
	lainnya := catID(t, w, "Lainnya")
	c, errs, err := w.svc.Create(ctx, w.draft(func(d *tickets.Draft) {
		nonCritical(d)
		d.TicketCategoryID = lainnya
		d.Description = "Mesin antrian tidak cetak nomor"
	}), nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	tk, _, _, err := w.svc.Get(ctx, c.ID)
	if err != nil || !tk.IsTriage || tk.ITCategoryID != triage {
		t.Fatalf("tiket Lainnya harus masuk bidang Triase: %+v %v", tk.ITCategoryName, err)
	}
	// Tiket triase terlihat semua petugas & dinotifikasi ke semua petugas.
	list, _, err := w.svc.Queue(ctx, tickets.QueueFilter{Mode: "available", UserID: w.tech.ID, OnlyCompetency: true, Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("petugas ERM melihat tiket triase di antrean tersedia: %d %v", len(list), err)
	}
	process(t, n)
	if inbox(t, w, w.tech.ID, notifications.TicketCreated) != 1 || inbox(t, w, w.other.ID, notifications.TicketCreated) != 1 {
		t.Fatal("tiket triase dikirim ke semua petugas")
	}
	var title string
	_ = w.db.QueryRow(`SELECT title FROM notifications WHERE user_id = ? AND ticket_id = ?`, w.coord.ID, c.ID).Scan(&title)
	if !strings.Contains(title, "perlu dipilah") {
		t.Fatalf("judul notifikasi koordinator triase: %q", title)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND ticket_id = ?`, koorNet.ID, c.ID) != 0 {
		t.Fatal("koordinator bidang lain tidak menerima tiket triase (triase punya koordinator)")
	}

	// Hanya koordinator triase (atau PJ) yang boleh memilah.
	network1 := catID(t, w, "Internet atau jaringan mati")
	if _, err := w.svc.ChangeCategory(ctx, c.ID, koorNet, network1, ""); !errors.Is(err, tickets.ErrForbidden) {
		t.Fatalf("koordinator bidang lain tidak boleh memilah tiket triase, dapat %v", err)
	}
	if _, err := w.svc.ChangeCategory(ctx, c.ID, w.tech, network1, ""); !errors.Is(err, tickets.ErrForbidden) {
		t.Fatalf("petugas yang bukan PJ tidak boleh memilah, dapat %v", err)
	}
	if errs, err := w.svc.ChangeCategory(ctx, c.ID, w.coord, network1, ""); err != nil || errs != nil {
		t.Fatalf("koordinator triase memilah tanpa alasan (dari Triase): %v %v", errs, err)
	}
	tk, hist, _, _ := w.svc.Get(ctx, c.ID)
	if tk.ITCategoryID != network || tk.IsTriage || !tk.ReportedCategoryName.Valid || tk.ReportedCategoryName.String != "Lainnya" {
		t.Fatalf("setelah dipilah: bidang %s, dipilih pelapor %v", tk.ITCategoryName, tk.ReportedCategoryName)
	}
	found := false
	for _, h := range hist {
		if h.EventType == tickets.EventCategoryChanged && strings.Contains(h.Note.String, "Lainnya → Internet atau jaringan mati") {
			found = true
		}
	}
	if !found {
		t.Fatal("koreksi tercatat di histori tiket")
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM tickets WHERE id = ? AND sla_revision = 2`, c.ID) != 1 {
		t.Fatal("kebijakan SLA dipilih ulang setelah koreksi")
	}
	// Tiket belum diambil → petugas & koordinator Pelayanan & Support diberi tahu.
	process(t, n)
	if inbox(t, w, w.other.ID, notifications.TicketTransferred) != 1 || inbox(t, w, koorNet.ID, notifications.TicketTransferred) != 1 {
		t.Fatal("dialihkan: petugas & koordinator bidang baru menerima notifikasi")
	}
	if inbox(t, w, w.tech.ID, notifications.TicketTransferred) != 0 || inbox(t, w, w.coord.ID, notifications.TicketTransferred) != 0 {
		t.Fatal("pelaku & petugas bidang lain tidak menerima notifikasi dialihkan")
	}

	// Koreksi berikutnya (bukan dari Triase) wajib alasan; kini kewenangan koordinator Pelayanan & Support.
	hwCat := catID(t, w, "SIMRS error atau tidak bisa dibuka") // bidang lain (ERM)
	if _, err := w.svc.ChangeCategory(ctx, c.ID, w.coord, hwCat, "Salah"); !errors.Is(err, tickets.ErrForbidden) {
		t.Fatalf("koordinator triase tidak lagi berwenang setelah pindah bidang, dapat %v", err)
	}
	if errs, _ := w.svc.ChangeCategory(ctx, c.ID, koorNet, hwCat, ""); errs["category_reason"] == "" {
		t.Fatal("koreksi di luar Triase wajib alasan")
	}
	if errs, _ := w.svc.ChangeCategory(ctx, c.ID, koorNet, network1, "x"); errs["ticket_category_id"] == "" {
		t.Fatal("jenis yang sama ditolak")
	}

	// PJ tiket boleh mengoreksi tiketnya sendiri; tiket selesai tidak bisa dikoreksi.
	if r, err := w.svc.Claim(ctx, c.ID, w.other.ID); err != nil || !r.OK {
		t.Fatal(err)
	}
	if errs, err := w.svc.ChangeCategory(ctx, c.ID, w.other, hwCat, "Ternyata aplikasi antrian SIMRS error"); err != nil || errs != nil {
		t.Fatalf("PJ boleh mengoreksi tiketnya: %v %v", errs, err)
	}
	if err := w.svc.Start(ctx, c.ID, w.other); err != nil {
		t.Fatal(err)
	}
	if errs, err := w.svc.Resolve(ctx, c.ID, w.other, tickets.ResolveInput{Action: "Ganti PSU", Result: "FIXED"}); err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if _, err := w.svc.ChangeCategory(ctx, c.ID, w.other, network1, "x"); !errors.Is(err, tickets.ErrTransition) {
		t.Fatalf("tiket selesai tidak dapat dikoreksi, dapat %v", err)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM tickets WHERE id = ? AND reported_ticket_category_id = ?`, c.ID, lainnya) != 1 {
		t.Fatal("jenis pilihan pelapor tetap yang pertama (Lainnya)")
	}
}
