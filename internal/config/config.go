// Package config membaca konfigurasi dari environment (dan file .env bila ada)
// lalu memvalidasinya saat start agar aplikasi gagal cepat dengan pesan jelas.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config adalah konfigurasi runtime aplikasi.
type Config struct {
	Env                string
	Addr               string
	BaseURL            string
	DBDSN              string
	TrustProxyHeaders  bool
	LogLevel           slog.Level
	SessionLifetime    time.Duration
	SessionIdleTimeout time.Duration
	StorageDir         string
	// Backup database harian (mysqldump → gzip) & uji restore.
	BackupEnabled  bool
	BackupDir      string
	BackupHour     int
	BackupKeepDays int
	Mysqldump      string
	MysqlClient    string
	// Web Push (VAPID). Kosong = push nonaktif; inbox in-app tetap berjalan.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	// API SIMRS (sama dengan aplikasi CSSD): daftar pegawai untuk saran nama pelapor.
	// URL atau token kosong = saran nama nonaktif; nama pelapor tetap diisi bebas.
	SimrsPegawaiURL string
	SimrsToken      string
	SimrsCookie     string
	SimrsTimeout    time.Duration
}

// IsProduction bernilai true bila SIKAT_ENV=production.
func (c Config) IsProduction() bool { return c.Env == "production" }

// SecureCookies bernilai true bila aplikasi diakses lewat HTTPS.
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.BaseURL, "https://") }

// Load membaca file .env (bila ada, tanpa menimpa variabel yang sudah diset)
// lalu membangun Config dari environment.
func Load(dotenvPath string) (Config, error) {
	if dotenvPath != "" {
		if err := LoadDotEnv(dotenvPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("membaca %s: %w", dotenvPath, err)
		}
	}
	return FromEnv(os.Getenv)
}

// FromEnv membangun Config dari fungsi lookup (dipisah agar mudah diuji).
func FromEnv(get func(string) string) (Config, error) {
	var errs []string
	cfg := Config{
		Env:     strings.ToLower(defaultStr(get("SIKAT_ENV"), "development")),
		Addr:    defaultStr(get("SIKAT_ADDR"), "127.0.0.1:8080"),
		BaseURL: strings.TrimRight(defaultStr(get("SIKAT_BASE_URL"), "http://localhost:8080"), "/"),
		DBDSN:   get("SIKAT_DB_DSN"),
		// Folder data runtime (foto lampiran). Harus di luar folder yang disajikan web server.
		StorageDir: defaultStr(get("SIKAT_STORAGE_DIR"), "storage"),
	}

	if cfg.Env != "development" && cfg.Env != "production" {
		errs = append(errs, "SIKAT_ENV harus 'development' atau 'production'")
	}
	if cfg.DBDSN == "" {
		errs = append(errs, "SIKAT_DB_DSN wajib diisi")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, "SIKAT_BASE_URL harus URL lengkap, contoh https://sikat.rs.example")
	} else if cfg.IsProduction() && u.Scheme != "https" {
		errs = append(errs, "SIKAT_BASE_URL wajib https:// di production")
	}

	if v := get("SIKAT_TRUST_PROXY_HEADERS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, "SIKAT_TRUST_PROXY_HEADERS harus true/false")
		}
		cfg.TrustProxyHeaders = b
	}

	switch strings.ToLower(defaultStr(get("SIKAT_LOG_LEVEL"), "info")) {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		errs = append(errs, "SIKAT_LOG_LEVEL harus debug/info/warn/error")
	}

	hours, err := intEnv(get, "SIKAT_SESSION_LIFETIME_HOURS", 12, 1, 24*7)
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.SessionLifetime = time.Duration(hours) * time.Hour
	idle, err := intEnv(get, "SIKAT_SESSION_IDLE_MINUTES", 120, 5, 24*60)
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.SessionIdleTimeout = time.Duration(idle) * time.Minute

	cfg.BackupEnabled = true
	if v := get("SIKAT_BACKUP_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, "SIKAT_BACKUP_ENABLED harus true/false")
		}
		cfg.BackupEnabled = b
	}
	cfg.BackupDir = defaultStr(get("SIKAT_BACKUP_DIR"), strings.TrimRight(cfg.StorageDir, `/\`)+"/backups")
	if cfg.BackupHour, err = intEnv(get, "SIKAT_BACKUP_HOUR", 2, 0, 23); err != nil {
		errs = append(errs, err.Error())
	}
	if cfg.BackupKeepDays, err = intEnv(get, "SIKAT_BACKUP_KEEP_DAYS", 14, 1, 3650); err != nil {
		errs = append(errs, err.Error())
	}
	cfg.Mysqldump = defaultStr(get("SIKAT_MYSQLDUMP"), "mysqldump")
	cfg.MysqlClient = defaultStr(get("SIKAT_MYSQL"), "mysql")

	cfg.VAPIDPublicKey = strings.TrimSpace(get("SIKAT_VAPID_PUBLIC_KEY"))
	cfg.VAPIDPrivateKey = strings.TrimSpace(get("SIKAT_VAPID_PRIVATE_KEY"))
	cfg.VAPIDSubject = strings.TrimSpace(get("SIKAT_VAPID_SUBJECT"))
	if (cfg.VAPIDPublicKey == "") != (cfg.VAPIDPrivateKey == "") {
		errs = append(errs, "SIKAT_VAPID_PUBLIC_KEY dan SIKAT_VAPID_PRIVATE_KEY harus diisi keduanya (buat dengan: sikat vapid-keys) atau dikosongkan")
	}
	if cfg.VAPIDSubject == "" && u != nil {
		if u.Scheme == "https" {
			cfg.VAPIDSubject = cfg.BaseURL
		} else {
			cfg.VAPIDSubject = "mailto:admin@" + u.Hostname()
		}
	}

	cfg.SimrsPegawaiURL = strings.TrimSpace(get("SIKAT_SIMRS_PEGAWAI_URL"))
	cfg.SimrsToken = strings.TrimSpace(get("SIKAT_SIMRS_TOKEN"))
	cfg.SimrsCookie = strings.TrimSpace(get("SIKAT_SIMRS_COOKIE"))
	if cfg.SimrsPegawaiURL != "" {
		if su, err := url.Parse(cfg.SimrsPegawaiURL); err != nil || (su.Scheme != "http" && su.Scheme != "https") || su.Host == "" {
			errs = append(errs, "SIKAT_SIMRS_PEGAWAI_URL harus URL lengkap http(s)://")
		}
	}
	secs, err := intEnv(get, "SIKAT_SIMRS_TIMEOUT_SECONDS", 30, 1, 300)
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.SimrsTimeout = time.Duration(secs) * time.Second

	if len(errs) > 0 {
		return Config{}, errors.New("konfigurasi tidak valid:\n  - " + strings.Join(errs, "\n  - "))
	}
	return cfg, nil
}

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func intEnv(get func(string) string, key string, def, min, max int) (int, error) {
	v := strings.TrimSpace(get(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return def, fmt.Errorf("%s harus angka %d..%d", key, min, max)
	}
	return n, nil
}

// LoadDotEnv membaca file KEY=VALUE sederhana. Variabel yang sudah ada di
// environment tidak ditimpa. Baris kosong dan baris diawali '#' diabaikan.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, val, ok := strings.Cut(text, "=")
		if !ok {
			return fmt.Errorf("baris %d: format harus KEY=VALUE", line)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return sc.Err()
}
