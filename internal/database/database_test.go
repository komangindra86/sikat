package database_test

import (
	"context"
	"io/fs"
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
