// Package settings mengelola pengaturan sistem (tabel system_settings).
// Daftar kunci yang dikenal didefinisikan di kode beserta tipe dan batasnya;
// nilainya disimpan di database dan dapat diubah Admin IT.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
)

// Kunci pengaturan yang dipakai aplikasi.
const (
	KeyHospitalName        = "app.hospital_name"
	KeyITContact           = "app.it_contact"
	KeyLoginMaxAttempts    = "security.login_max_attempts"
	KeyLoginLockoutMinutes = "security.login_lockout_minutes"
	KeyUploadMaxMB         = "upload.max_mb"
	KeyUploadMaxFiles      = "upload.max_files"
)

// Def mendefinisikan satu pengaturan.
type Def struct {
	Key     string
	Label   string
	Help    string
	Type    string // STRING | INT
	Default string
	MaxLen  int
	Min     int64
	Max     int64
	Group   string
}

// Defs adalah daftar pengaturan yang dapat diubah, sesuai urutan tampil.
var Defs = []Def{
	{Key: KeyHospitalName, Label: "Nama rumah sakit", Help: "Tampil di kepala halaman dan halaman login.", Type: "STRING", Default: "Rumah Sakit", MaxLen: 150, Group: "Umum"},
	{Key: KeyITContact, Label: "Kontak IT", Help: "Contoh: Ekstensi 123 / 0812-xxxx. Tampil di halaman login sebagai bantuan.", Type: "STRING", Default: "", MaxLen: 150, Group: "Umum"},
	{Key: KeyLoginMaxAttempts, Label: "Batas gagal login", Help: "Jumlah percobaan gagal berturut-turut sebelum akun dikunci sementara.", Type: "INT", Default: "5", Min: 3, Max: 20, Group: "Keamanan"},
	{Key: KeyLoginLockoutMinutes, Label: "Lama kunci akun (menit)", Help: "Lama akun dikunci setelah melewati batas gagal login.", Type: "INT", Default: "15", Min: 1, Max: 1440, Group: "Keamanan"},
	{Key: KeyUploadMaxMB, Label: "Ukuran maksimal foto (MB)", Help: "Batas ukuran satu foto yang diunggah. Foto dari HP dikompres otomatis sebelum dikirim.", Type: "INT", Default: "8", Min: 1, Max: 20, Group: "Lampiran"},
	{Key: KeyUploadMaxFiles, Label: "Jumlah foto per pengajuan", Help: "Jumlah foto maksimal dalam satu laporan.", Type: "INT", Default: "3", Min: 1, Max: 5, Group: "Lampiran"},
}

// DefByKey mencari definisi pengaturan.
func DefByKey(key string) (Def, bool) {
	for _, d := range Defs {
		if d.Key == key {
			return d, true
		}
	}
	return Def{}, false
}

// ErrUnknownKey dikembalikan untuk kunci yang tidak terdaftar.
var ErrUnknownKey = errors.New("pengaturan tidak dikenal")

// Service membaca dan mengubah pengaturan dengan cache di memori.
type Service struct {
	db    *sql.DB
	clock clock.Clock

	mu    sync.RWMutex
	cache map[string]string
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock) *Service {
	return &Service{db: db, clock: c}
}

func (s *Service) load(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	if s.cache != nil {
		c := s.cache
		s.mu.RUnlock()
		return c, nil
	}
	s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `SELECT setting_key, value FROM system_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache = m
	s.mu.Unlock()
	return m, nil
}

// Get mengembalikan nilai pengaturan; nilai default bila belum ada di DB atau DB gagal dibaca.
func (s *Service) Get(ctx context.Context, key string) string {
	def, _ := DefByKey(key)
	m, err := s.load(ctx)
	if err != nil {
		return def.Default
	}
	if v, ok := m[key]; ok {
		return v
	}
	return def.Default
}

// Int mengembalikan nilai pengaturan bertipe INT (default bila tidak valid).
func (s *Service) Int(ctx context.Context, key string) int {
	def, _ := DefByKey(key)
	n, err := strconv.Atoi(s.Get(ctx, key))
	if err != nil || int64(n) < def.Min || (def.Max > 0 && int64(n) > def.Max) {
		n, _ = strconv.Atoi(def.Default)
	}
	return n
}

// HospitalName untuk tampilan.
func (s *Service) HospitalName(ctx context.Context) string { return s.Get(ctx, KeyHospitalName) }

// ITContact untuk tampilan.
func (s *Service) ITContact(ctx context.Context) string { return s.Get(ctx, KeyITContact) }

// Item adalah pengaturan beserta nilainya untuk ditampilkan.
type Item struct {
	Def
	Value string
}

// List mengembalikan semua pengaturan yang dapat diubah.
func (s *Service) List(ctx context.Context) ([]Item, error) {
	if _, err := s.load(ctx); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(Defs))
	for _, d := range Defs {
		out = append(out, Item{Def: d, Value: s.Get(ctx, d.Key)})
	}
	return out, nil
}

// Validate memeriksa nilai sesuai definisi; mengembalikan nilai yang sudah dirapikan.
func Validate(d Def, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	switch d.Type {
	case "INT":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "", fmt.Errorf("Harus berupa angka bulat.")
		}
		if n < d.Min || n > d.Max {
			return "", fmt.Errorf("Harus antara %d dan %d.", d.Min, d.Max)
		}
		return strconv.FormatInt(n, 10), nil
	default:
		if d.MaxLen > 0 && utf8.RuneCountInString(v) > d.MaxLen {
			return "", fmt.Errorf("Maksimal %d karakter.", d.MaxLen)
		}
		if d.Key == KeyHospitalName && v == "" {
			return "", fmt.Errorf("Wajib diisi.")
		}
		return v, nil
	}
}

// Update menyimpan beberapa pengaturan sekaligus dalam satu transaksi.
// Mengembalikan error per kunci bila validasi gagal (tidak ada yang disimpan).
func (s *Service) Update(ctx context.Context, actorID int64, values map[string]string) (map[string]string, error) {
	fieldErrs := map[string]string{}
	clean := map[string]string{}
	for key, raw := range values {
		d, ok := DefByKey(key)
		if !ok {
			return nil, ErrUnknownKey
		}
		v, err := Validate(d, raw)
		if err != nil {
			fieldErrs[key] = err.Error()
			continue
		}
		clean[key] = v
	}
	if len(fieldErrs) > 0 {
		return fieldErrs, nil
	}

	current, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	err = database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, d := range Defs {
			v, ok := clean[d.Key]
			if !ok {
				continue
			}
			old, existed := current[d.Key]
			if existed && old == v {
				continue
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO system_settings (setting_key, value, value_type, description, updated_by, updated_at)
				VALUES (?, ?, ?, ?, ?, ?)
				ON DUPLICATE KEY UPDATE value = VALUES(value), updated_by = VALUES(updated_by), updated_at = VALUES(updated_at)`,
				d.Key, v, d.Type, d.Label, database.NullInt64(actorID), now)
			if err != nil {
				return err
			}
			if err := audit.Record(ctx, tx, now, audit.Entry{
				Action: "settings.update", EntityType: "system_setting",
				Before: map[string]string{d.Key: old}, After: map[string]string{d.Key: v},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Invalidate()
	return nil, nil
}

// Invalidate mengosongkan cache.
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}
