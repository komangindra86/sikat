package backup_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"sikat/internal/backup"
	"sikat/internal/clock"
	"sikat/internal/testutil"
)

// tool mencari biner MySQL: env, PATH, lalu lokasi Laragon (development Windows).
func tool(t *testing.T, env, name string) string {
	t.Helper()
	if p := os.Getenv(env); p != "" {
		return p
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	matches, _ := filepath.Glob(`C:\laragon\bin\mysql\*\bin\` + name + ".exe")
	if len(matches) > 0 {
		return matches[0]
	}
	t.Skipf("%s tidak ditemukan (set %s)", name, env)
	return ""
}

func TestBackupAndRestoreTest(t *testing.T) {
	db := testutil.DB(t)
	dump := tool(t, "SIKAT_TEST_MYSQLDUMP", "mysqldump")
	client := tool(t, "SIKAT_TEST_MYSQL", "mysql")
	cfg, err := mysql.ParseDSN(os.Getenv("SIKAT_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT DATABASE()`).Scan(&cfg.DBName); err != nil {
		t.Fatal(err)
	}
	testutil.CreateUser(t, db, "admin", "ADMIN", "password-admin-123", false)
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 11, 18, 17, 30, 0, 0, time.UTC)) // 19 Nov 01:30 WITA
	svc := backup.NewService(db, clk, nil, backup.Config{Enabled: true, Dir: dir, Hour: 2, KeepDays: 14,
		Mysqldump: dump, Mysql: client, DSN: cfg.FormatDSN()})
	ctx := context.Background()

	// Sebelum jam backup (02.00 WITA): belum dijalankan.
	if ran, err := svc.RunDue(ctx); err != nil || ran {
		t.Fatalf("sebelum jam backup tidak dijalankan: %v %v", ran, err)
	}
	clk.Advance(time.Hour) // 02.30 WITA
	ran, err := svc.RunDue(ctx)
	if err != nil || !ran {
		t.Fatalf("backup terjadwal: %v %v", ran, err)
	}
	// Sekali per hari.
	if ran, _ := svc.RunDue(ctx); ran {
		t.Fatal("backup terjadwal hanya sekali per tanggal")
	}
	logs, err := svc.Logs(ctx, 5)
	if err != nil || len(logs) != 1 || logs[0].Status != "SUCCESS" || !logs[0].SHA256.Valid || logs[0].SizeBytes.Int64 == 0 {
		t.Fatalf("riwayat backup: %+v %v", logs, err)
	}
	if !strings.HasPrefix(logs[0].FileName.String, "sikat-20261119-0230") {
		t.Fatalf("nama file memakai waktu WITA: %s", logs[0].FileName.String)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("hanya file backup yang tertinggal (file kredensial sementara dihapus): %d berkas", len(entries))
	}

	msg, err := svc.RestoreTest(ctx, 0)
	if err != nil || !strings.Contains(msg, "berhasil") || !strings.Contains(msg, "users 1") {
		t.Fatalf("uji restore: %q %v", msg, err)
	}
	logs, _ = svc.Logs(ctx, 1)
	if logs[0].RestoreStatus.String != "SUCCESS" || !logs[0].RestoreTestedAt.Valid {
		t.Fatalf("hasil uji restore tercatat: %+v", logs[0])
	}
	var leftovers int
	_ = db.QueryRow(`SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name LIKE ?`, "sikat_restoretest_%").Scan(&leftovers)
	if leftovers != 0 {
		t.Fatal("database uji restore dihapus setelah pengujian")
	}

	// File rusak → uji restore gagal & tercatat.
	if err := os.WriteFile(filepath.Join(dir, logs[0].FileName.String), []byte("bukan gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RestoreTest(ctx, logs[0].ID); err == nil {
		t.Fatal("file backup rusak harus gagal diuji")
	}
	logs, _ = svc.Logs(ctx, 1)
	if logs[0].RestoreStatus.String != "FAILED" {
		t.Fatal("kegagalan uji restore tercatat")
	}

	// mysqldump tidak ada → backup gagal dengan pesan jelas.
	bad := backup.NewService(db, clk, nil, backup.Config{Enabled: true, Dir: dir, Mysqldump: filepath.Join(dir, "tidak-ada"), DSN: cfg.FormatDSN()})
	if _, err := bad.Run(ctx, "MANUAL", 0); err == nil || !strings.Contains(err.Error(), "mysqldump") {
		t.Fatalf("galat mysqldump: %v", err)
	}
}
