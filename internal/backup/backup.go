// Package backup membuat backup database harian (mysqldump → gzip), menyimpan
// riwayatnya di backup_logs, menghapus file lama sesuai retensi, dan menguji
// restore ke database sementara agar backup terbukti dapat dipulihkan.
package backup

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"sikat/internal/clock"
	"sikat/internal/database"
)

// Config mengatur backup.
type Config struct {
	Enabled   bool
	Dir       string // di luar folder web; mis. /var/backups/sikat
	Hour      int    // jam backup harian (WITA)
	KeepDays  int    // file lebih tua dihapus (minimal 3 file terbaru selalu disimpan)
	Mysqldump string // path biner mysqldump
	Mysql     string // path biner mysql (uji restore)
	DSN       string // DSN aplikasi (kredensial dibaca dari sini)
}

// Service menjalankan backup & uji restore.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	log   *slog.Logger
	cfg   Config
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock, log *slog.Logger, cfg Config) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if cfg.Hour < 0 || cfg.Hour > 23 {
		cfg.Hour = 2
	}
	if cfg.KeepDays <= 0 {
		cfg.KeepDays = 14
	}
	if cfg.Mysqldump == "" {
		cfg.Mysqldump = "mysqldump"
	}
	if cfg.Mysql == "" {
		cfg.Mysql = "mysql"
	}
	return &Service{db: db, clock: c, log: log, cfg: cfg}
}

// Enabled melaporkan apakah backup terjadwal aktif.
func (s *Service) Enabled() bool { return s.cfg.Enabled }

// Config mengembalikan konfigurasi (tanpa DSN) untuk tampilan.
func (s *Service) Config() Config { c := s.cfg; c.DSN = ""; return c }

// keyTables: tabel yang dihitung barisnya sebagai pembanding uji restore.
var keyTables = []string{"users", "departments", "rooms", "assets", "tickets", "ticket_histories",
	"internal_activities", "asset_maintenance_histories", "audit_logs"}

// Log adalah satu riwayat backup.
type Log struct {
	ID               int64
	Trigger, Status  string
	BackupDate       sql.NullTime
	StartedAt        time.Time
	FinishedAt       sql.NullTime
	FileName         sql.NullString
	SizeBytes        sql.NullInt64
	SHA256           sql.NullString
	MigrationVersion sql.NullInt64
	Error            sql.NullString
	RestoreTestedAt  sql.NullTime
	RestoreStatus    sql.NullString
	RestoreMessage   sql.NullString
}

// SizeText: ukuran file yang mudah dibaca.
func (l Log) SizeText() string {
	if !l.SizeBytes.Valid {
		return "–"
	}
	b := float64(l.SizeBytes.Int64)
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", b/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", b/(1<<10))
	}
}

// Logs mengembalikan riwayat terbaru.
func (s *Service) Logs(ctx context.Context, limit int) ([]Log, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, trigger_type, status, backup_date, started_at, finished_at, file_name, size_bytes,
		sha256, migration_version, error_message, restore_tested_at, restore_status, restore_message
		FROM backup_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Log
	for rows.Next() {
		var l Log
		if err := rows.Scan(&l.ID, &l.Trigger, &l.Status, &l.BackupDate, &l.StartedAt, &l.FinishedAt, &l.FileName, &l.SizeBytes,
			&l.SHA256, &l.MigrationVersion, &l.Error, &l.RestoreTestedAt, &l.RestoreStatus, &l.RestoreMessage); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// creds menulis file opsi mysql sementara (izin 0600) agar password tidak
// terlihat di daftar proses. Pemanggil wajib menghapusnya.
func (s *Service) creds() (path string, dbName string, err error) {
	c, err := mysql.ParseDSN(s.cfg.DSN)
	if err != nil {
		return "", "", fmt.Errorf("DSN database tidak valid: %w", err)
	}
	host, port := c.Addr, ""
	if h, p, ok := strings.Cut(c.Addr, ":"); ok {
		host, port = h, p
	}
	var b strings.Builder
	b.WriteString("[client]\n")
	fmt.Fprintf(&b, "user=%s\n", c.User)
	if c.Passwd != "" {
		fmt.Fprintf(&b, "password=\"%s\"\n", strings.ReplaceAll(c.Passwd, `"`, `\"`))
	}
	if c.Net == "unix" {
		fmt.Fprintf(&b, "socket=%s\n", c.Addr)
	} else {
		fmt.Fprintf(&b, "host=%s\n", host)
		if port != "" {
			fmt.Fprintf(&b, "port=%s\n", port)
		}
	}
	f, err := os.CreateTemp(s.cfg.Dir, ".sikat-cred-*.cnf")
	if err != nil {
		return "", "", err
	}
	_ = f.Chmod(0o600)
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), c.DBName, f.Close()
}

// ErrAlreadyDone: backup terjadwal hari ini sudah berhasil.
var ErrAlreadyDone = errors.New("backup hari ini sudah ada")

// RunDue menjalankan backup terjadwal bila sudah lewat jam backup (WITA) dan
// belum ada backup berhasil untuk tanggal tersebut. Dipanggil worker berkala.
func (s *Service) RunDue(ctx context.Context) (bool, error) {
	if !s.cfg.Enabled {
		return false, nil
	}
	now := s.clock.Now().In(clock.Location)
	if now.Hour() < s.cfg.Hour {
		return false, nil
	}
	day := now.Format("2006-01-02")
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_logs WHERE trigger_type = 'SCHEDULE' AND backup_date = ?
		AND (status IN ('SUCCESS','RUNNING') OR (status = 'FAILED' AND started_at > ?))`, day, s.clock.Now().Add(-time.Hour)).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil // sudah berhasil, sedang berjalan, atau gagal < 1 jam lalu (dicoba lagi tiap jam)
	}
	_, err := s.Run(ctx, "SCHEDULE", 0)
	return true, err
}

// Run membuat satu backup. Mengembalikan ID log.
func (s *Service) Run(ctx context.Context, trigger string, by int64) (int64, error) {
	start := s.clock.Now()
	var day any
	if trigger == "SCHEDULE" {
		day = start.In(clock.Location).Format("2006-01-02")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO backup_logs (trigger_type, status, backup_date, started_at, triggered_by)
		VALUES (?, 'RUNNING', ?, ?, ?)`, trigger, day, start, database.NullInt64(by))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	name, size, sum, ver, counts, runErr := s.dump(ctx, start)
	finish := s.clock.Now()
	if runErr != nil {
		s.log.Error("backup gagal", "err", runErr)
		_, err := s.db.ExecContext(ctx, `UPDATE backup_logs SET status = 'FAILED', finished_at = ?, error_message = ? WHERE id = ?`,
			finish, runErr.Error(), id)
		if err != nil {
			return id, err
		}
		return id, runErr
	}
	cj, _ := json.Marshal(counts)
	if _, err := s.db.ExecContext(ctx, `UPDATE backup_logs SET status = 'SUCCESS', finished_at = ?, file_name = ?, size_bytes = ?, sha256 = ?,
		migration_version = ?, row_counts = ? WHERE id = ?`, finish, name, size, sum, ver, string(cj), id); err != nil {
		return id, err
	}
	s.log.Info("backup selesai", "file", name, "ukuran", size, "detik", finish.Sub(start).Seconds())
	if err := s.prune(); err != nil {
		s.log.Warn("backup: gagal menghapus file lama", "err", err)
	}
	return id, nil
}

func (s *Service) dump(ctx context.Context, start time.Time) (name string, size int64, sum string, ver int64, counts map[string]int64, err error) {
	if err := os.MkdirAll(s.cfg.Dir, 0o750); err != nil {
		return "", 0, "", 0, nil, fmt.Errorf("folder backup: %w", err)
	}
	// Jumlah baris & versi migrasi dicatat sebelum dump (pembanding uji restore).
	counts = map[string]int64{}
	for _, t := range keyTables {
		var n int64
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n); err != nil {
			return "", 0, "", 0, nil, err
		}
		counts[t] = n
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&ver); err != nil {
		return "", 0, "", 0, nil, err
	}
	cred, dbName, err := s.creds()
	if err != nil {
		return "", 0, "", 0, nil, err
	}
	defer os.Remove(cred)

	name = "sikat-" + start.In(clock.Location).Format("20060102-150405") + ".sql.gz"
	final := filepath.Join(s.cfg.Dir, name)
	partial := final + ".partial"
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return "", 0, "", 0, nil, err
	}
	h := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(f, h))
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, s.cfg.Mysqldump, "--defaults-extra-file="+cred,
		"--single-transaction", "--quick", "--no-tablespaces", "--routines", "--triggers",
		"--hex-blob", "--default-character-set=utf8mb4", "--set-gtid-purged=OFF", dbName)
	cmd.Stdout = gz
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := gz.Close()
	if err := f.Close(); err != nil && runErr == nil {
		runErr = err
	}
	if runErr == nil {
		runErr = closeErr
	}
	if runErr != nil {
		os.Remove(partial)
		msg := strings.TrimSpace(stderr.String())
		if errors.Is(runErr, exec.ErrNotFound) {
			msg = "mysqldump tidak ditemukan; atur SIKAT_MYSQLDUMP ke lokasi biner mysqldump"
		}
		if msg == "" {
			msg = runErr.Error()
		}
		return "", 0, "", 0, nil, errors.New("mysqldump gagal: " + clip(msg, 500))
	}
	if err := os.Rename(partial, final); err != nil {
		return "", 0, "", 0, nil, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return "", 0, "", 0, nil, err
	}
	return name, st.Size(), hex.EncodeToString(h.Sum(nil)), ver, counts, nil
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// prune menghapus file backup yang lebih tua dari KeepDays; 3 file terbaru selalu disimpan.
func (s *Service) prune() error {
	files, err := filepath.Glob(filepath.Join(s.cfg.Dir, "sikat-*.sql.gz"))
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files))) // nama berisi waktu → terbaru dulu
	cut := s.clock.Now().Add(-time.Duration(s.cfg.KeepDays) * 24 * time.Hour)
	for i, f := range files {
		if i < 3 {
			continue
		}
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		if st.ModTime().Before(cut) {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrNoBackup: belum ada backup berhasil untuk diuji.
var ErrNoBackup = errors.New("belum ada backup berhasil")

// RestoreTest memulihkan backup ke database sementara, membandingkan versi
// migrasi & jumlah baris tabel utama dengan catatan saat backup, lalu menghapus
// database sementara. logID 0 = backup berhasil terbaru.
func (s *Service) RestoreTest(ctx context.Context, logID int64) (string, error) {
	q := `SELECT id, file_name, migration_version, row_counts FROM backup_logs WHERE status = 'SUCCESS'`
	var args []any
	if logID > 0 {
		q += ` AND id = ?`
		args = append(args, logID)
	}
	q += ` ORDER BY id DESC LIMIT 1`
	var id int64
	var name string
	var ver int64
	var countsJSON sql.NullString
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&id, &name, &ver, &countsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoBackup
	}
	if err != nil {
		return "", err
	}
	msg, testErr := s.restoreInto(ctx, filepath.Join(s.cfg.Dir, name), ver, countsJSON.String)
	status := "SUCCESS"
	if testErr != nil {
		status, msg = "FAILED", testErr.Error()
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE backup_logs SET restore_tested_at = ?, restore_status = ?, restore_message = ? WHERE id = ?`,
		s.clock.Now(), status, clip(msg, 2000), id); err != nil {
		return msg, err
	}
	return msg, testErr
}

func (s *Service) restoreInto(ctx context.Context, path string, wantVer int64, countsJSON string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("file backup tidak dapat dibuka: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("file backup rusak: %w", err)
	}
	cred, _, err := s.creds()
	if err != nil {
		return "", err
	}
	defer os.Remove(cred)
	tmp := fmt.Sprintf("sikat_restoretest_%d", s.clock.Now().UnixNano())
	if _, err := s.db.ExecContext(ctx, "CREATE DATABASE `"+tmp+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		return "", fmt.Errorf("membuat database uji (butuh hak CREATE/DROP): %w", err)
	}
	defer func() { _, _ = s.db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+tmp+"`") }()

	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, s.cfg.Mysql, "--defaults-extra-file="+cred, "--default-character-set=utf8mb4", tmp)
	cmd.Stdin = gz
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if errors.Is(err, exec.ErrNotFound) {
			msg = "mysql client tidak ditemukan; atur SIKAT_MYSQL ke lokasi biner mysql"
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New("memulihkan backup gagal: " + clip(msg, 500))
	}
	var gotVer int64
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM `"+tmp+"`.goose_db_version WHERE is_applied").Scan(&gotVer); err != nil {
		return "", fmt.Errorf("memeriksa hasil restore: %w", err)
	}
	if gotVer != wantVer {
		return "", fmt.Errorf("versi migrasi hasil restore %d, seharusnya %d", gotVer, wantVer)
	}
	want := map[string]int64{}
	_ = json.Unmarshal([]byte(countsJSON), &want)
	var parts []string
	for _, t := range keyTables {
		var n int64
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+tmp+"`."+t).Scan(&n); err != nil {
			return "", fmt.Errorf("tabel %s tidak ada di hasil restore: %w", t, err)
		}
		if w, ok := want[t]; ok && n != w {
			return "", fmt.Errorf("jumlah baris %s hasil restore %d, saat backup %d", t, n, w)
		}
		parts = append(parts, fmt.Sprintf("%s %d", t, n))
	}
	return fmt.Sprintf("Restore ke database sementara berhasil: migrasi versi %d; %s.", gotVer, strings.Join(parts, ", ")), nil
}
