package tickets_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/sla"
	"sikat/internal/testutil"
	"sikat/internal/tickets"
)

// slaPolicies menyetel semua kebijakan standar ke nilai uji yang mudah dihitung.
func slaPolicies(t *testing.T, db *sql.DB, calendar string, resp, reso int, allowPause bool) {
	t.Helper()
	if _, err := db.Exec(`UPDATE sla_policies p JOIN sla_calendars c ON c.name = ?
		SET p.calendar_id = c.id, p.response_minutes = ?, p.resolution_minutes = ?, p.warn_pct = 75, p.allow_pause = ?`,
		calendar, resp, reso, allowPause); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, w wf) int {
	t.Helper()
	res, err := w.svc.SLA().Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res.Escalated
}

func escCount(t *testing.T, w wf, id int64, kind string) int {
	return testutil.Count(t, w.db, `SELECT COUNT(*) FROM sla_escalations WHERE ticket_id = ? AND kind = ?`, id, kind)
}

// nonCritical: dampak "tidak mengganggu" agar prioritas bukan kritis.
func nonCritical(d *tickets.Draft) { d.Impact = 0 }

func TestSLAEscalationIdempotentAcrossCycles(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	slaPolicies(t, w.db, "24 jam", 60, 240, false)
	c, errs, err := w.svc.Create(ctx, w.draft(nonCritical), nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if c.Priority == "CRITICAL" {
		t.Fatal("uji membutuhkan prioritas non-kritis")
	}
	var due time.Time
	if err := w.db.QueryRow(`SELECT response_due_at FROM tickets WHERE id = ?`, c.ID).Scan(&due); err != nil {
		t.Fatal(err)
	}
	if want := w.clk.Now().Add(time.Hour); !due.Equal(want) {
		t.Fatalf("batas respons = %s, mau %s", due, want)
	}
	if n := scan(t, w); n != 0 {
		t.Fatalf("tiket baru belum perlu eskalasi, dapat %d", n)
	}

	// 46 menit (77%) → pengingat respons, sekali saja.
	w.clk.Advance(46 * time.Minute)
	if n := scan(t, w); n != 1 || escCount(t, w, c.ID, sla.KindResponseWarning) != 1 {
		t.Fatalf("harus 1 pengingat respons, dapat %d", n)
	}
	for i := 0; i < 3; i++ {
		if n := scan(t, w); n != 0 {
			t.Fatalf("worker diulang tidak boleh membuat eskalasi baru, dapat %d", n)
		}
	}
	// Lewat batas → pelanggaran respons, sekali saja; kolom breached terisi.
	w.clk.Advance(20 * time.Minute)
	if n := scan(t, w); n != 1 || scan(t, w) != 0 {
		t.Fatal("pelanggaran respons harus tercatat tepat sekali")
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM tickets WHERE id = ? AND response_breached_at IS NOT NULL`, c.ID) != 1 {
		t.Fatal("response_breached_at harus terisi")
	}
	// Diambil setelah terlambat: tidak menduplikasi catatan worker.
	if res, err := w.svc.Claim(ctx, c.ID, w.tech.ID); err != nil || !res.OK {
		t.Fatal(err)
	}
	if escCount(t, w, c.ID, sla.KindResponseBreach) != 1 {
		t.Fatal("klaim terlambat tidak boleh menduplikasi pelanggaran")
	}
	if err := w.svc.Start(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}

	// Penyelesaian: 190 menit (79%) → pengingat; 250 menit → pelanggaran.
	w.clk.Advance(124 * time.Minute)
	if n := scan(t, w); n != 1 || escCount(t, w, c.ID, sla.KindResolutionWarning) != 1 {
		t.Fatalf("harus 1 pengingat penyelesaian, dapat %d", n)
	}
	w.clk.Advance(60 * time.Minute)
	if n := scan(t, w); n != 1 || scan(t, w) != 0 {
		t.Fatal("pelanggaran penyelesaian harus tercatat tepat sekali")
	}
	if errs, err := w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{Action: "Ganti PSU", Result: "FIXED"}); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if escCount(t, w, c.ID, sla.KindResolutionBreach) != 1 {
		t.Fatal("selesai terlambat tidak boleh menduplikasi pelanggaran")
	}

	// Dibuka kembali → siklus 2 dengan batas baru; pelanggaran siklus 2 tercatat terpisah.
	if errs, err := w.svc.Reopen(ctx, c.ID, w.coord, "Masih mati"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	var cycle int
	var resoDue time.Time
	var breached sql.NullTime
	if err := w.db.QueryRow(`SELECT sla_cycle, resolution_due_at, resolution_breached_at FROM tickets WHERE id = ?`, c.ID).
		Scan(&cycle, &resoDue, &breached); err != nil {
		t.Fatal(err)
	}
	if cycle != 2 || !resoDue.Equal(w.clk.Now().Add(240*time.Minute)) || breached.Valid {
		t.Fatalf("siklus baru: cycle=%d due=%s breached=%v", cycle, resoDue, breached)
	}
	if n := scan(t, w); n != 0 {
		t.Fatalf("siklus baru belum perlu eskalasi, dapat %d", n)
	}
	w.clk.Advance(241 * time.Minute)
	if n := scan(t, w); n != 1 || escCount(t, w, c.ID, sla.KindResolutionBreach) != 2 {
		t.Fatalf("pelanggaran siklus 2 harus tercatat terpisah, dapat %d", n)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM ticket_histories WHERE ticket_id = ? AND event_type IN ('SLA_WARNING','SLA_BREACH')`, c.ID) != 5 {
		t.Fatal("setiap eskalasi baru tercatat sekali di histori tiket")
	}

	// Laporan bulanan menghitung tiket ini sebagai melanggar respons & penyelesaian.
	rep, err := w.svc.SLA().MonthlyReport(ctx, w.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 1 || rep.Response.Missed != 1 || rep.Resolution.Missed != 1 || len(rep.Breaches) != 1 {
		t.Fatalf("laporan: %+v", rep)
	}
}

func TestSLACriticalAndPriorityChange(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	slaPolicies(t, w.db, "24 jam", 60, 240, false)
	if _, err := w.db.Exec(`UPDATE sla_policies SET response_minutes = 15, resolution_minutes = 120 WHERE priority = 'CRITICAL'`); err != nil {
		t.Fatal(err)
	}
	c, _, err := w.svc.Create(ctx, w.draft(nonCritical), nil)
	if err != nil {
		t.Fatal(err)
	}
	if errs, err := w.svc.ChangePriority(ctx, c.ID, w.coord, "CRITICAL", "Ruang operasi terdampak"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	var resp, rev int
	var due time.Time
	if err := w.db.QueryRow(`SELECT sla_response_minutes, sla_revision, response_due_at FROM tickets WHERE id = ?`, c.ID).
		Scan(&resp, &rev, &due); err != nil {
		t.Fatal(err)
	}
	if resp != 15 || rev != 2 || !due.Equal(w.clk.Now().Add(15*time.Minute)) {
		t.Fatalf("kebijakan kritis harus diterapkan: resp=%d rev=%d due=%s", resp, rev, due)
	}
	if escCount(t, w, c.ID, sla.KindCritical) != 1 {
		t.Fatal("prioritas kritis harus langsung dieskalasi")
	}
	if n := scan(t, w); n != 0 {
		t.Fatalf("worker tidak boleh mengulang eskalasi kritis, dapat %d", n)
	}

	// Tiket yang langsung kritis saat dibuat juga dieskalasi sekali.
	c2, _, err := w.svc.Create(ctx, w.draft(func(d *tickets.Draft) { d.RoomID = w.critRoom }), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Priority == "CRITICAL" && (escCount(t, w, c2.ID, sla.KindCritical) != 1 || scan(t, w) != 0) {
		t.Fatal("tiket kritis baru harus dieskalasi tepat sekali")
	}

	// Diambil terlambat tanpa worker berjalan: pelanggaran tercatat tanpa notifikasi.
	c3, _, err := w.svc.Create(ctx, w.draft(nonCritical), nil)
	if err != nil {
		t.Fatal(err)
	}
	w.clk.Advance(2 * time.Hour)
	if res, err := w.svc.Claim(ctx, c3.ID, w.tech.ID); err != nil || !res.OK {
		t.Fatal(err)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM sla_escalations WHERE ticket_id = ? AND kind = 'RESPONSE_BREACH' AND notify = FALSE`, c3.ID) != 1 {
		t.Fatal("klaim terlambat harus mencatat pelanggaran respons (tanpa notifikasi)")
	}
}

func TestSLAPauseOnlyWhenConfigured(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	slaPolicies(t, w.db, "24 jam", 60, 240, true)
	var vendor int64
	if err := w.db.QueryRow(`SELECT id FROM pending_reasons WHERE code = 'WAIT_VENDOR'`).Scan(&vendor); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.Exec(`UPDATE pending_reasons SET pauses_sla = TRUE WHERE id = ?`, vendor); err != nil {
		t.Fatal(err)
	}
	created := w.clk.Now()
	c, _ := w.newClaimed(t)
	if err := w.svc.Start(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}

	// Alasan yang tidak menghentikan SLA: waktu tetap berjalan.
	if errs, err := w.svc.Pend(ctx, c.ID, w.tech, w.reasonID, ""); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM tickets WHERE id = ? AND sla_paused_since IS NULL`, c.ID) != 1 {
		t.Fatal("alasan tanpa tanda 'Menghentikan SLA' tidak boleh menjeda")
	}
	if err := w.svc.Resume(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}

	// Alasan yang menghentikan SLA + kebijakan mengizinkan → dijeda.
	w.clk.Advance(60 * time.Minute)
	if errs, err := w.svc.Pend(ctx, c.ID, w.tech, vendor, ""); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	w.clk.Advance(150 * time.Minute) // 210 menit kalender, tapi hanya 60 menit terpakai
	if n := scan(t, w); n != 0 {
		t.Fatalf("tiket dijeda tidak boleh mendapat pengingat, dapat %d", n)
	}
	if err := w.svc.Resume(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}
	var paused int
	var due time.Time
	if err := w.db.QueryRow(`SELECT sla_paused_minutes, resolution_due_at FROM tickets WHERE id = ?`, c.ID).Scan(&paused, &due); err != nil {
		t.Fatal(err)
	}
	if paused != 150 || !due.Equal(created.Add((240+150)*time.Minute)) {
		t.Fatalf("jeda 150 menit menggeser batas: paused=%d due=%s", paused, due)
	}

	// Kebijakan tidak mengizinkan jeda → alasan apa pun tidak menjeda.
	slaPolicies(t, w.db, "24 jam", 60, 240, false)
	c2, _ := w.newClaimed(t)
	if err := w.svc.Start(ctx, c2.ID, w.tech); err != nil {
		t.Fatal(err)
	}
	if errs, err := w.svc.Pend(ctx, c2.ID, w.tech, vendor, ""); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM tickets WHERE id = ? AND sla_paused_since IS NULL`, c2.ID) != 1 {
		t.Fatal("kebijakan tanpa izin jeda tidak boleh menjeda")
	}
}

func TestSLAOfficeHoursAndHolidays(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	slaPolicies(t, w.db, "Jam kerja", 120, 480, false)
	// Jumat 13 Nov 2026 15.00 WITA; Senin 16 Nov dijadikan hari libur.
	w.clk = clock.NewFake(time.Date(2026, 11, 13, 7, 0, 0, 0, time.UTC))
	w.svc = tickets.NewService(w.db, w.clk, w.store)
	if _, err := w.db.Exec(`INSERT INTO sla_holidays (calendar_id, holiday_date, name, created_at)
		SELECT id, '2026-11-16', 'Libur uji', UTC_TIMESTAMP(3) FROM sla_calendars WHERE name = 'Jam kerja'`); err != nil {
		t.Fatal(err)
	}
	c, _, err := w.svc.Create(ctx, w.draft(nonCritical), nil)
	if err != nil {
		t.Fatal(err)
	}
	var resp, reso time.Time
	if err := w.db.QueryRow(`SELECT response_due_at, resolution_due_at FROM tickets WHERE id = ?`, c.ID).Scan(&resp, &reso); err != nil {
		t.Fatal(err)
	}
	// Respons 2 jam: Jumat 15–16 (1 jam), lewati Sabtu–Minggu & Senin libur → Selasa 09.00 WITA.
	if want := time.Date(2026, 11, 17, 1, 0, 0, 0, time.UTC); !resp.Equal(want) {
		t.Errorf("batas respons = %s, mau %s", resp, want)
	}
	// Penyelesaian 8 jam: 1 jam Jumat + 7 jam Selasa → Selasa 15.00 WITA.
	if want := time.Date(2026, 11, 17, 7, 0, 0, 0, time.UTC); !reso.Equal(want) {
		t.Errorf("batas penyelesaian = %s, mau %s", reso, want)
	}

	// Menghapus hari libur menghitung ulang batas tiket aktif.
	svc := w.svc.SLA()
	var calID, hid int64
	if err := w.db.QueryRow(`SELECT calendar_id, id FROM sla_holidays WHERE holiday_date = '2026-11-16'`).Scan(&calID, &hid); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteHoliday(ctx, calID, hid); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT response_due_at FROM tickets WHERE id = ?`, c.ID).Scan(&resp); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC); !resp.Equal(want) {
		t.Errorf("setelah libur dihapus, batas respons = %s, mau Senin 09.00 WITA", resp)
	}
}
