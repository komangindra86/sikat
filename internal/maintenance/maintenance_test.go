package maintenance_test

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"sikat/internal/activities"
	"sikat/internal/attachments"
	"sikat/internal/clock"
	"sikat/internal/maintenance"
	"sikat/internal/priority"
	"sikat/internal/rbac"
	"sikat/internal/settings"
	"sikat/internal/testutil"
	"sikat/internal/tickets"
)

type env struct {
	db     *sql.DB
	clk    *clock.Fake
	tk     *tickets.Service
	act    *activities.Service
	ms     *maintenance.Service
	tech   *rbac.User
	coord  *rbac.User
	asset  int64
	room   int64
	cat    int64
	cat2   int64
	partID int64
}

func setup(t *testing.T) env {
	db := testutil.DB(t)
	clk := clock.NewFake(time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC))
	store := attachments.Store{Dir: t.TempDir()}
	st := settings.NewService(db, clk)
	e := env{db: db, clk: clk, tk: tickets.NewService(db, clk, store), act: activities.NewService(db, clk, store), ms: maintenance.NewService(db, clk, st)}
	e.tk.SetHooks(e.ms)
	e.act.SetHooks(e.ms)
	ins := func(q string, args ...any) int64 {
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	now := time.Now().UTC()
	dept := ins(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('IGD','IGD',?,?)`, now, now)
	e.room = ins(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'T1','Triase',?,?)`, dept, now, now)
	acat := ins(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('PC','PC',?,?)`, now, now)
	e.asset = ins(`INSERT INTO assets (asset_code, name, asset_category_id, room_id, department_id, procured_at, created_at, updated_at)
		VALUES ('IGD-01','Komputer IGD-01',?,?,?,'2019-01-01',?,?)`, acat, e.room, dept, now, now)
	e.partID = ins(`INSERT INTO spare_parts (code, name, default_unit_cost, created_at, updated_at) VALUES ('PSU','PSU 450W',175000,?,?)`, now, now)
	_ = db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Komputer tidak menyala'`).Scan(&e.cat)
	_ = db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Komputer lambat atau hang'`).Scan(&e.cat2)
	e.tech = &rbac.User{ID: testutil.CreateUser(t, db, "tek", "TECHNICIAN", "password-tek1", false), RoleCode: rbac.RoleTechnician}
	e.coord = &rbac.User{ID: testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor", false), RoleCode: rbac.RoleCoordinator}
	return e
}

func (e env) ticket(t *testing.T, cat int64) tickets.Created {
	c, errs, err := e.tk.Create(context.Background(), tickets.Draft{
		Channel: tickets.ChannelQR, RoomID: e.room, AssetID: e.asset, ReporterName: "Ani", TicketCategoryID: cat,
		Description: "Komputer bermasalah", Impact: 1, Scope: 1, Replacement: priority.ReplacementYes,
	}, nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	return c
}

func (e env) work(t *testing.T, id int64, in tickets.ResolveInput) map[string]string {
	ctx := context.Background()
	if res, err := e.tk.Claim(ctx, id, e.tech.ID); err != nil || !res.OK {
		t.Fatalf("klaim: %v", err)
	}
	if err := e.tk.Start(ctx, id, e.tech); err != nil {
		t.Fatal(err)
	}
	errs, err := e.tk.Resolve(ctx, id, e.tech, in)
	if err != nil {
		t.Fatal(err)
	}
	return errs
}

func TestTicketResolutionCreatesHistory(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	c := e.ticket(t, e.cat)
	e.clk.Advance(2 * time.Hour)

	// Sparepart tidak valid → tiket tidak berubah status, tidak ada riwayat.
	errs := e.work(t, c.ID, tickets.ResolveInput{Action: "x", Result: "REPLACED",
		Parts: []tickets.PartLine{{PartID: "99999", Qty: "1"}}})
	if errs["parts"] == "" {
		t.Fatal("sparepart tidak ada harus ditolak")
	}
	if n := testutil.Count(t, e.db, `SELECT COUNT(*) FROM asset_maintenance_histories`); n != 0 {
		t.Fatal("riwayat tidak boleh tercatat saat validasi gagal")
	}
	errs, err := e.tk.Resolve(ctx, c.ID, e.tech, tickets.ResolveInput{Action: "Ganti PSU", Result: "REPLACED", ServiceCost: "50.000",
		AssetStatusAfter: "ACTIVE", Parts: []tickets.PartLine{{PartID: strconv.FormatInt(e.partID, 10), Qty: "2"}, {PartID: ""}}})
	if err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	hist, _ := e.ms.History(ctx, e.asset)
	if len(hist) != 1 || hist[0].Type != "CORRECTIVE" || hist[0].Source != "TICKET" || !hist[0].TicketID.Valid {
		t.Fatalf("satu entri perbaikan dari tiket: %+v", hist)
	}
	h := hist[0]
	if len(h.Parts) != 1 || h.Parts[0].Qty != "2" || h.Parts[0].LineCost.String != "350000.00" || h.TotalCost() != "400000.00" {
		t.Fatalf("sparepart & biaya: %+v total=%s", h.Parts, h.TotalCost())
	}
	if h.DowntimeMinutes.Int64 != 120 || h.Complaint.String != "Komputer bermasalah" || h.CategoryName.String != "Komputer tidak menyala" {
		t.Fatalf("downtime/keluhan/jenis: %+v", h)
	}

	// Buka kembali → selesaikan lagi: entri kedua (siklus 2), bukan duplikat.
	if _, err := e.tk.Reopen(ctx, c.ID, e.coord, "Masih mati"); err != nil {
		t.Fatal(err)
	}
	if errs, err := e.tk.Resolve(ctx, c.ID, e.tech, tickets.ResolveInput{Action: "Ganti kabel", Result: "FIXED", AssetStatusAfter: "BROKEN"}); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	hist, _ = e.ms.History(ctx, e.asset)
	if len(hist) != 2 || hist[0].TicketCycle.Int64 != 2 {
		t.Fatalf("entri siklus kedua: %+v", hist)
	}
	var status string
	_ = e.db.QueryRow(`SELECT status FROM assets WHERE id = ?`, e.asset).Scan(&status)
	if status != "BROKEN" {
		t.Fatalf("kondisi perangkat harus mengikuti form penyelesaian, dapat %s", status)
	}
	st, err := e.ms.Stats(ctx, e.asset)
	if err != nil {
		t.Fatal(err)
	}
	if st.DamagesAll != 1 || st.RepairsAll != 2 || st.CostAll != "400000.00" || st.DowntimeAll < 120 || !st.LastMaintenance.Valid {
		t.Fatalf("statistik: %+v", st)
	}
}

func TestRepeatDamageDetection(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		e.ticket(t, e.cat2)
		e.clk.Advance(24 * time.Hour)
	}
	if a, _ := e.ms.OpenAlert(ctx, e.asset); a != nil {
		t.Fatal("2 gangguan belum mencapai ambang 3")
	}
	e.ticket(t, e.cat)
	a, err := e.ms.OpenAlert(ctx, e.asset)
	if err != nil || a == nil || a.TicketCount != 3 || a.Recommendation != maintenance.RecEvaluate {
		t.Fatalf("gangguan ke-3 harus memicu peringatan Evaluasi: %+v %v", a, err)
	}
	// Gangguan ke-4 memperbarui peringatan yang sama (bukan membuat baru).
	e.ticket(t, e.cat2) // cat2 kini 3× → pemeriksaan lanjutan; perangkat > 5 tahun → pertimbangan penggantian
	a, _ = e.ms.OpenAlert(ctx, e.asset)
	if a.TicketCount != 4 || a.Recommendation != maintenance.RecReplace {
		t.Fatalf("peringatan diperbarui: %+v", a)
	}
	if n := testutil.Count(t, e.db, `SELECT COUNT(*) FROM asset_alerts`); n != 1 {
		t.Fatalf("hanya satu peringatan terbuka per perangkat, dapat %d", n)
	}

	// Keputusan koordinator: petugas tidak berhak; keputusan tercatat di riwayat.
	if _, _, err := e.ms.Decide(ctx, e.tech, a.ID, "INSPECT", ""); err != maintenance.ErrForbidden {
		t.Fatal("petugas tidak boleh memutuskan peringatan")
	}
	if _, errs, _ := e.ms.Decide(ctx, e.coord, a.ID, "OTHER", ""); errs["decision_note"] == "" {
		t.Fatal("keputusan Lainnya wajib catatan")
	}
	if _, errs, err := e.ms.Decide(ctx, e.coord, a.ID, "INSPECT", "Cek motherboard"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if n := testutil.Count(t, e.db, `SELECT COUNT(*) FROM asset_maintenance_histories WHERE maintenance_type = 'ALERT_DECISION'`); n != 1 {
		t.Fatal("keputusan harus masuk riwayat perangkat")
	}
	// Setelah keputusan, penghitungan dimulai ulang: 1 tiket baru tidak memicu peringatan.
	e.clk.Advance(time.Hour)
	e.ticket(t, e.cat)
	if a, _ := e.ms.OpenAlert(ctx, e.asset); a != nil {
		t.Fatal("keputusan koordinator mereset penghitungan")
	}
	e.ticket(t, e.cat)
	e.ticket(t, e.cat)
	if a, _ := e.ms.OpenAlert(ctx, e.asset); a == nil || a.TicketCount != 3 {
		t.Fatalf("setelah 3 gangguan baru, peringatan baru muncul: %+v", a)
	}
}

func TestActivityHistoryAndManualEntries(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var typeID, itID int64
	_ = e.db.QueryRow(`SELECT id, it_category_id FROM activity_types WHERE name = 'Maintenance'`).Scan(&typeID, &itID)
	form := map[string]string{
		"activity_type_id": strconv.FormatInt(typeID, 10), "it_category_id": strconv.FormatInt(itID, 10),
		"title": "Bersihkan debu & ganti pasta", "status": "DONE", "result": "Suhu CPU turun",
		"asset_id": strconv.FormatInt(e.asset, 10), "maintenance_type": "PREVENTIVE",
		"started_at": "2026-11-10T09:00", "ended_at": "2026-11-10T10:00",
	}
	none := func(string) []string { return nil }
	id, _, errs, err := e.act.Create(ctx, e.tech, testutil.Form(form), none)
	if err != nil || id == 0 {
		t.Fatalf("%v %v", errs, err)
	}
	hist, _ := e.ms.History(ctx, e.asset)
	if len(hist) != 1 || hist[0].Type != "PREVENTIVE" || hist[0].Source != "ACTIVITY" || hist[0].RepairMinutes.Int64 != 60 {
		t.Fatalf("kegiatan selesai harus tercatat di riwayat: %+v", hist)
	}
	// Dibatalkan → entri ditandai batal; selesai lagi → aktif kembali; tidak pernah dobel.
	form["status"], form["result"] = "CANCELLED", ""
	if _, errs, err := e.act.Update(ctx, e.tech, id, testutil.Form(form), none); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	hist, _ = e.ms.History(ctx, e.asset)
	if !hist[0].VoidedAt.Valid {
		t.Fatal("kegiatan batal → entri riwayat dibatalkan")
	}
	form["status"], form["result"] = "DONE", "Ulang"
	_, _, _ = e.act.Update(ctx, e.tech, id, testutil.Form(form), none)
	hist, _ = e.ms.History(ctx, e.asset)
	if len(hist) != 1 || hist[0].VoidedAt.Valid {
		t.Fatalf("entri aktif kembali tanpa duplikat: %+v", hist)
	}

	// Entri manual: validasi, sparepart, biaya.
	errs, err = e.ms.AddManual(ctx, e.tech, e.asset, maintenance.ManualInput{Type: "CORRECTIVE"})
	if err != nil || errs["m_action"] == "" || errs["m_finished_at"] == "" {
		t.Fatalf("entri manual wajib tindakan & waktu selesai: %v %v", errs, err)
	}
	errs, err = e.ms.AddManual(ctx, e.tech, e.asset, maintenance.ManualInput{
		Type: "CORRECTIVE", Action: "Servis vendor: ganti motherboard", Result: "FIXED", FinishedAt: "2026-11-09T15:00",
		ServiceCost: "1.250.000", Parts: []tickets.PartLine{{PartID: strconv.FormatInt(e.partID, 10), Qty: "1", UnitCost: "200000"}},
	})
	if err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	st, _ := e.ms.Stats(ctx, e.asset)
	if st.CostAll != "1450000.00" || st.RepairsAll != 1 {
		t.Fatalf("statistik setelah entri manual: %+v", st)
	}
	// Koreksi: hanya koordinator, alasan wajib; entri batal tidak dihitung.
	hist, _ = e.ms.History(ctx, e.asset)
	var manualID int64
	for _, h := range hist {
		if h.Source == "MANUAL" {
			manualID = h.ID
		}
	}
	if _, _, err := e.ms.Void(ctx, e.tech, manualID, "salah input"); err != maintenance.ErrForbidden {
		t.Fatal("petugas tidak boleh membatalkan entri")
	}
	if _, errs, _ := e.ms.Void(ctx, e.coord, manualID, " "); errs["void_reason"] == "" {
		t.Fatal("pembatalan wajib alasan")
	}
	if _, _, err := e.ms.Void(ctx, e.coord, manualID, "Salah perangkat"); err != nil {
		t.Fatal(err)
	}
	st, _ = e.ms.Stats(ctx, e.asset)
	if st.CostAll != "0.00" || st.RepairsAll != 0 {
		t.Fatalf("entri batal tidak dihitung: %+v", st)
	}
	if _, err := e.db.Exec(`DELETE FROM asset_maintenance_histories WHERE id = ?`, manualID); err == nil {
		t.Fatal("riwayat tidak boleh dihapus (trigger)")
	}
}
