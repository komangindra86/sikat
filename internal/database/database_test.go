package database_test

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"sikat"
	"sikat/internal/database"
	"sikat/internal/testutil"
)

func TestPrepareDSN(t *testing.T) {
	dsn, err := database.PrepareDSN("u:p@tcp(127.0.0.1:3306)/sikat")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"parseTime=true", "time_zone=%27%2B00%3A00%27", "sql_mode="} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN harus memuat %s: %s", want, dsn)
		}
	}
	if _, err := database.PrepareDSN("::bukan dsn::"); err == nil {
		t.Fatal("DSN rusak harus error")
	}
}

func TestMigrationsDownAndUp(t *testing.T) {
	db := testutil.DB(t) // sudah Up
	mfs, _ := fs.Sub(sikat.MigrationsFS, "migrations")
	m, err := database.NewMigrator(db, mfs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Reset(ctx); err != nil {
		t.Fatalf("down semua migrasi gagal: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'users'`); n != 0 {
		t.Fatal("tabel users harus terhapus setelah reset")
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up ulang gagal: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM roles`); n != 3 {
		t.Fatalf("seed role harus 3, dapat %d", n)
	}
}

func TestSessionTimeZoneIsUTC(t *testing.T) {
	db := testutil.DB(t)
	var tz string
	if err := db.QueryRow(`SELECT @@session.time_zone`).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "+00:00" {
		t.Fatalf("time_zone sesi harus +00:00, dapat %s", tz)
	}
	var now time.Time
	if err := db.QueryRow(`SELECT UTC_TIMESTAMP(3)`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	if now.Location() != time.UTC {
		t.Fatalf("waktu harus diparse sebagai UTC, dapat %v", now.Location())
	}
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	db := testutil.DB(t)
	if _, err := db.Exec(`INSERT INTO audit_logs (actor_type, action, created_at) VALUES ('SYSTEM','test', UTC_TIMESTAMP(3))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE audit_logs SET action = 'ubah'`); err == nil {
		t.Fatal("UPDATE audit_logs harus ditolak trigger")
	}
	if _, err := db.Exec(`DELETE FROM audit_logs`); err == nil {
		t.Fatal("DELETE audit_logs harus ditolak trigger")
	}
}

func TestErrorHelpers(t *testing.T) {
	db := testutil.DB(t)
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('A','A',?,?), ('A','B',?,?)`, now, now, now, now)
	if key, ok := database.DuplicateKey(err); !ok || key != "uq_departments_code" {
		t.Fatalf("DuplicateKey = %q,%v (%v)", key, ok, err)
	}
	_, err = db.Exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (9999,'R','R',?,?)`, now, now)
	if !database.IsForeignKeyMissing(err) {
		t.Fatalf("harus terdeteksi FK tidak ada: %v", err)
	}
	_, err = db.Exec(`INSERT INTO spare_parts (code, name, default_unit_cost, created_at, updated_at) VALUES ('X','X',-1,?,?)`, now, now)
	if name, ok := database.CheckConstraint(err); !ok || name != "chk_spare_parts_cost" {
		t.Fatalf("CheckConstraint = %q,%v (%v)", name, ok, err)
	}
}

func TestUniformCodesMigration(t *testing.T) {
	db := testutil.DB(t) // sudah Up, termasuk 00013
	ctx := context.Background()
	code := func(q string, args ...any) string {
		t.Helper()
		var s string
		if err := db.QueryRow(q, args...).Scan(&s); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return s
	}

	// Instalasi baru: kode seed ikut seragam, urut sesuai urutan tampil.
	if c := code(`SELECT code FROM it_categories WHERE name = 'Hardware'`); c != "BDG-01" {
		t.Fatalf("Hardware = %q, harus BDG-01", c)
	}
	if c := code(`SELECT code FROM it_categories WHERE is_triage`); c != "BDG-07" {
		t.Fatalf("Triase = %q, harus BDG-07", c)
	}
	if c := code(`SELECT code FROM pending_reasons WHERE label = 'Lainnya'`); c != "ALS-05" {
		t.Fatalf("alasan Lainnya = %q, harus ALS-05", c)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM pending_reasons WHERE requires_note`); n != 1 ||
		code(`SELECT label FROM pending_reasons WHERE requires_note`) != "Lainnya" {
		t.Fatal(`hanya alasan "Lainnya" (dulu kode OTHER) yang wajib keterangan`)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE NOT REGEXP_LIKE(code, '^BDG-[0-9]{2}$')`); n != 0 {
		t.Fatalf("%d bidang belum berformat BDG-NN", n)
	}

	// Down → kode lama kembali.
	mfs, _ := fs.Sub(sikat.MigrationsFS, "migrations")
	m, err := database.NewMigrator(db, mfs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Down(ctx); err != nil {
		t.Fatalf("down 00013: %v", err)
	}
	if c := code(`SELECT code FROM it_categories WHERE name = 'Hardware'`); c != "HW" {
		t.Fatalf("setelah down Hardware = %q, harus HW", c)
	}

	// Data lama: kode bebas, kode yang sudah berformat baru, data SIMRS, nomor teralokasi.
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	now := time.Now().UTC()
	igd := exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('IGD','IGD',?,?)`, now, now)
	exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('UNT-002','Farmasi',?,?)`, now, now)
	exec(`INSERT INTO departments (code, name, source, created_at, updated_at) VALUES ('SIM-01','Radiologi','SIMRS',?,?)`, now, now)
	exec(`INSERT INTO number_sequences (name, year, current_value) VALUES ('UNT', 0, 5)`)
	// Pindahan dari aplikasi lama (tercatat CSV) berkode LEGACY-: ikut diseragamkan.
	legacyDept := exec(`INSERT INTO departments (code, name, source, created_at, updated_at) VALUES ('LEGACY-U3','Penunjang Medis','CSV',?,?)`, now, now)
	legacyRoom := exec(`INSERT INTO rooms (department_id, code, name, source, created_at, updated_at) VALUES (?, 'LEGACY-R16','BEDAH SENTRAL IBS','CSV',?,?)`, igd, now, now)
	exec(`INSERT INTO rooms (department_id, code, name, source, created_at, updated_at) VALUES (?, 'SIM-R1','Ruang SIMRS','SIMRS',?,?)`, igd, now, now)
	cat := exec(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('PC','Komputer',?,?)`, now, now)
	// 31/12/2025 17.00 UTC = 01/01/2026 WITA
	a2026 := exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('IGD-PC-01','PC',?,'2025-12-31 17:00:00','2025-12-31 17:00:00')`, cat)
	a2025 := exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('KSR-01','PC',?,'2025-06-01 02:00:00','2025-06-01 02:00:00')`, cat)
	exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('PRK-2026-0001','PC',?,?,?)`, cat, now, now)

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00013 dengan data lama: %v", err)
	}
	checks := []struct{ q, want string }{
		{`SELECT code FROM departments WHERE id = ` + itoa(igd), "UNT-006"},        // lanjut setelah nomor teralokasi (5)
		{`SELECT code FROM departments WHERE name = 'Farmasi'`, "UNT-002"},         // sudah berformat baru: tetap
		{`SELECT code FROM departments WHERE name = 'Radiologi'`, "SIM-01"},        // data SIMRS: tetap
		{`SELECT code FROM departments WHERE id = ` + itoa(legacyDept), "UNT-007"}, // LEGACY- dari CSV: ikut
		{`SELECT code FROM rooms WHERE id = ` + itoa(legacyRoom), "RNG-0001"},
		{`SELECT code FROM rooms WHERE name = 'Ruang SIMRS'`, "SIM-R1"},
		{`SELECT code FROM asset_categories WHERE id = ` + itoa(cat), "KAT-01"},
		{`SELECT asset_code FROM assets WHERE id = ` + itoa(a2026), "PRK-2026-0002"}, // tahun WITA
		{`SELECT asset_code FROM assets WHERE id = ` + itoa(a2025), "PRK-2025-0001"},
		{`SELECT old_code FROM legacy_codes WHERE table_name = 'departments' AND row_id = ` + itoa(igd), "IGD"},
		{`SELECT current_value FROM number_sequences WHERE name = 'PRK' AND year = 2026`, "2"},
	}
	for _, c := range checks {
		if got := code(c.q); got != c.want {
			t.Errorf("%s = %q, harus %q", c.q, got, c.want)
		}
	}
	// Kode baru melanjutkan nomor setelah migrasi.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if c, err := database.NextCode(ctx, tx, "departments", "code", "UNT", 0, 3); err != nil || c != "UNT-008" {
		t.Fatalf("kode berikutnya = %q (%v), harus UNT-008", c, err)
	}
	if c, err := database.NextCode(ctx, tx, "it_categories", "code", "BDG", 0, 2); err != nil || c != "BDG-08" {
		t.Fatalf("kode bidang berikutnya = %q (%v), harus BDG-08", c, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
