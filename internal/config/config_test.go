package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestFromEnvDefaults(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{"SIKAT_DB_DSN": "u:p@tcp(localhost)/sikat"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != "development" || cfg.Addr != "127.0.0.1:8080" || cfg.SessionLifetime != 12*time.Hour || cfg.SecureCookies() ||
		cfg.SimrsPegawaiURL != "" || cfg.SimrsTimeout != 30*time.Second {
		t.Fatalf("default tidak sesuai: %+v", cfg)
	}
}

func TestFromEnvValidation(t *testing.T) {
	_, err := FromEnv(env(map[string]string{
		"SIKAT_ENV": "production", "SIKAT_BASE_URL": "http://sikat.example", "SIKAT_LOG_LEVEL": "verbose",
		"SIKAT_TRUST_PROXY_HEADERS": "mungkin", "SIKAT_SESSION_IDLE_MINUTES": "1",
		"SIKAT_SIMRS_PEGAWAI_URL": "app.rs/pegawai", "SIKAT_SIMRS_TIMEOUT_SECONDS": "0",
	}))
	if err == nil {
		t.Fatal("konfigurasi tidak valid harus error")
	}
	for _, want := range []string{"SIKAT_DB_DSN", "https", "SIKAT_LOG_LEVEL", "SIKAT_TRUST_PROXY_HEADERS", "SIKAT_SESSION_IDLE_MINUTES",
		"SIKAT_SIMRS_PEGAWAI_URL", "SIKAT_SIMRS_TIMEOUT_SECONDS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("pesan error harus menyebut %s: %v", want, err)
		}
	}
	cfg, err := FromEnv(env(map[string]string{"SIKAT_ENV": "production", "SIKAT_BASE_URL": "https://sikat.example/", "SIKAT_DB_DSN": "x"}))
	if err != nil || !cfg.SecureCookies() || cfg.BaseURL != "https://sikat.example" {
		t.Fatalf("production https harus valid: %+v %v", cfg, err)
	}
}

func TestLoadDotEnvDoesNotOverride(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte("# komentar\nSIKAT_TEST_A=\"dari file\"\nSIKAT_TEST_B=dari file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIKAT_TEST_B", "dari env")
	os.Unsetenv("SIKAT_TEST_A")
	t.Cleanup(func() { os.Unsetenv("SIKAT_TEST_A") })
	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SIKAT_TEST_A") != "dari file" || os.Getenv("SIKAT_TEST_B") != "dari env" {
		t.Fatalf("A=%q B=%q", os.Getenv("SIKAT_TEST_A"), os.Getenv("SIKAT_TEST_B"))
	}
}
