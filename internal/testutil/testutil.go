// Package testutil menyediakan database MySQL nyata untuk pengujian integrasi.
//
// Set SIKAT_TEST_DSN ke server MySQL uji TANPA nama database, contoh:
//
//	SIKAT_TEST_DSN="root@tcp(127.0.0.1:3306)/"
//
// Setiap pemanggilan DB membuat database baru bernama sikat_test_<nama>, menjalankan
// seluruh migrasi, lalu menghapus database tersebut setelah test selesai.
package testutil

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"time"

	"os"

	"github.com/go-sql-driver/mysql"

	"sikat"
	"sikat/internal/auth"
	"sikat/internal/database"
)

var nonWord = regexp.MustCompile(`[^a-z0-9_]+`)

// DB membuat database uji baru yang sudah dimigrasi.
func DB(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("SIKAT_TEST_DSN")
	if base == "" {
		t.Skip("SIKAT_TEST_DSN belum diset; test integrasi database dilewati")
	}
	cfg, err := mysql.ParseDSN(base)
	if err != nil {
		t.Fatalf("SIKAT_TEST_DSN tidak valid: %v", err)
	}

	name := nonWord.ReplaceAllString(strings.ToLower(t.Name()), "_")
	sum := sha1.Sum([]byte(t.Name()))
	if len(name) > 40 {
		name = name[:40]
	}
	dbName := "sikat_test_" + name + "_" + hex.EncodeToString(sum[:])[:6]

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+dbName+"`"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+dbName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}

	cfg.DBName = dbName
	db, err := database.Open(ctx, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	mfs, err := fs.Sub(sikat.MigrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := database.NewMigrator(db, mfs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("migrasi: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		cfg.DBName = ""
		a, err := sql.Open("mysql", cfg.FormatDSN())
		if err == nil {
			_, _ = a.Exec("DROP DATABASE IF EXISTS `" + dbName + "`")
			a.Close()
		}
	})
	return db
}

// CreateUser menambahkan user langsung ke database untuk keperluan test.
func CreateUser(t *testing.T, db *sql.DB, username, role, password string, mustChange bool) int64 {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	res, err := db.Exec(`
		INSERT INTO users (role_id, username, full_name, password_hash, must_change_password, created_at, updated_at)
		SELECT id, ?, ?, ?, ?, ?, ? FROM roles WHERE code = ?`,
		username, "User "+username, hash, mustChange, now, now, role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		t.Fatalf("role %s tidak ditemukan", role)
	}
	return id
}

// RoleID mengembalikan ID role berdasarkan kode.
func RoleID(t *testing.T, db *sql.DB, code string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM roles WHERE code = ?`, code).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Count menjalankan SELECT COUNT(*).
func Count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Form mengubah map menjadi fungsi getter seperti r.PostFormValue.
func Form(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}
