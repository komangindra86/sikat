// Package database mengelola koneksi MySQL, transaksi, migrasi, dan
// penerjemahan error MySQL menjadi kondisi yang dapat ditangani service.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

// DBTX adalah antarmuka bersama *sql.DB dan *sql.Tx, dipakai repository.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// PrepareDSN menambahkan parameter wajib: UTC, parseTime, charset, sql_mode ketat.
func PrepareDSN(dsn string) (string, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("DSN MySQL tidak valid: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Collation = "utf8mb4_0900_ai_ci"
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Params["sql_mode"] = "'TRADITIONAL,ONLY_FULL_GROUP_BY'"
	cfg.Params["transaction_isolation"] = "'READ-COMMITTED'"
	return cfg.FormatDSN(), nil
}

// Open membuka pool koneksi dan memastikan database dapat dijangkau.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	full, err := PrepareDSN(dsn)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", full)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("tidak dapat terhubung ke MySQL: %w", err)
	}
	return db, nil
}

// WithTx menjalankan fn dalam transaksi. Commit bila fn sukses, rollback bila
// fn mengembalikan error atau panic.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memulai transaksi: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaksi: %w", err)
	}
	return nil
}

// Migrator membungkus goose untuk migrasi ter-embed.
type Migrator struct {
	p *goose.Provider
}

// NewMigrator membuat migrator dari filesystem yang berisi file *.sql di root-nya.
func NewMigrator(db *sql.DB, migrations fs.FS) (*Migrator, error) {
	p, err := goose.NewProvider(goose.DialectMySQL, db, migrations)
	if err != nil {
		return nil, fmt.Errorf("menyiapkan migrasi: %w", err)
	}
	return &Migrator{p: p}, nil
}

// Up menjalankan semua migrasi yang belum diterapkan.
func (m *Migrator) Up(ctx context.Context) ([]string, error) {
	res, err := m.p.Up(ctx)
	var applied []string
	for _, r := range res {
		applied = append(applied, r.Source.Path)
	}
	return applied, err
}

// Down membatalkan satu migrasi terakhir.
func (m *Migrator) Down(ctx context.Context) (string, error) {
	r, err := m.p.Down(ctx)
	if r == nil {
		return "", err
	}
	return r.Source.Path, err
}

// Reset membatalkan semua migrasi (dipakai pengujian).
func (m *Migrator) Reset(ctx context.Context) error {
	_, err := m.p.DownTo(ctx, 0)
	return err
}

// Status mengembalikan status setiap migrasi dalam bentuk teks.
func (m *Migrator) Status(ctx context.Context) ([]string, error) {
	st, err := m.p.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(st))
	for _, s := range st {
		state := "belum"
		if s.State == goose.StateApplied {
			state = "diterapkan " + s.AppliedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, fmt.Sprintf("%-40s %s", s.Source.Path, state))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Penerjemahan error MySQL
// ---------------------------------------------------------------------------

var dupKeyRe = regexp.MustCompile(`for key '(?:[^.']+\.)?([^']+)'`)

// DuplicateKey mengembalikan nama index unik yang dilanggar (error 1062).
func DuplicateKey(err error) (string, bool) {
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		if m := dupKeyRe.FindStringSubmatch(me.Message); m != nil {
			return m[1], true
		}
		return "", true
	}
	return "", false
}

// IsForeignKeyInUse bernilai true bila baris tidak dapat dihapus/diubah karena
// masih direferensikan tabel lain (error 1451).
func IsForeignKeyInUse(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1451
}

// IsForeignKeyMissing bernilai true bila referensi yang diisi tidak ada (error 1452).
func IsForeignKeyMissing(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1452
}

var checkRe = regexp.MustCompile(`Check constraint '([^']+)'`)

// CheckConstraint mengembalikan nama CHECK constraint yang dilanggar (error 3819).
func CheckConstraint(err error) (string, bool) {
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 3819 {
		if m := checkRe.FindStringSubmatch(me.Message); m != nil {
			return m[1], true
		}
		return "", true
	}
	return "", false
}

// NextSequence mengalokasikan nomor urut tahunan (mis. TKT/KEG) secara atomik
// dalam transaksi tx. Baris sequence terkunci sampai commit; bila transaksi
// rollback, nomor ikut batal sehingga tidak ada lubang dari kegagalan.
func NextSequence(ctx context.Context, tx *sql.Tx, name string, year int) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO number_sequences (name, year, current_value) VALUES (?, ?, LAST_INSERT_ID(1))
		ON DUPLICATE KEY UPDATE current_value = LAST_INSERT_ID(current_value + 1)`, name, year); err != nil {
		return 0, err
	}
	var n int64
	err := tx.QueryRowContext(ctx, `SELECT LAST_INSERT_ID()`).Scan(&n)
	return n, err
}

// IsRetryable bernilai true untuk deadlock (1213) dan lock wait timeout (1205):
// transaksi aman diulang dari awal.
func IsRetryable(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1213 || me.Number == 1205)
}

// WithTxRetry seperti WithTx, tetapi mengulang transaksi (maks attempts kali)
// bila gagal karena deadlock/lock timeout. fn harus idempoten terhadap
// pengulangan (semua efeknya ada di dalam transaksi).
func WithTxRetry(ctx context.Context, db *sql.DB, attempts int, fn func(tx *sql.Tx) error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = WithTx(ctx, db, fn); err == nil || !IsRetryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(20*(i+1)) * time.Millisecond):
		}
	}
	return err
}

// NullString mengubah string kosong menjadi NULL.
func NullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// NullInt64 mengubah nilai 0 menjadi NULL (ID AUTO_INCREMENT tidak pernah 0).
func NullInt64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: v != 0}
}
