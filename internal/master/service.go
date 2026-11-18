package master

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
)

// ErrInUse dikembalikan saat data tidak bisa dihapus karena sudah dipakai.
var ErrInUse = errors.New("Data ini sudah dipakai data lain sehingga tidak dapat dihapus. Nonaktifkan saja.")

// Service berisi aturan bisnis master data.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	repo  repository
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock) *Service {
	return &Service{db: db, clock: c}
}

// List mengembalikan daftar dan total.
func (s *Service) List(ctx context.Context, e *Entity, q ListQuery) ([]Record, int, error) {
	return s.repo.list(ctx, s.db, e, q)
}

// Get mengambil satu baris.
func (s *Service) Get(ctx context.Context, e *Entity, id int64) (Record, error) {
	return s.repo.get(ctx, s.db, e, id, false)
}

// Options mengembalikan pilihan untuk setiap field select (kunci = kolom).
func (s *Service) Options(ctx context.Context, e *Entity, current map[string]string) (map[string][]Choice, error) {
	out := map[string][]Choice{}
	for _, f := range e.Fields {
		if f.Kind != KindSelect {
			continue
		}
		opts, err := s.repo.options(ctx, s.db, f.Ref, current[f.Column])
		if err != nil {
			return nil, err
		}
		out[f.Column] = opts
	}
	return out, nil
}

// RefLabels mengembalikan label seluruh referensi untuk tampilan daftar.
func (s *Service) RefLabels(ctx context.Context, e *Entity) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	for _, f := range e.Fields {
		if f.Kind != KindSelect {
			continue
		}
		m, err := s.repo.labels(ctx, s.db, f.Ref)
		if err != nil {
			return nil, err
		}
		out[f.Column] = m
	}
	return out, nil
}

func (s *Service) checkRefs(ctx context.Context, tx *sql.Tx, e *Entity, v *Validated, current map[string]string) error {
	for _, f := range e.Fields {
		if f.Kind != KindSelect || v.Values[f.Column] == nil {
			continue
		}
		id, _ := strconv.ParseInt(*v.Values[f.Column], 10, 64)
		ok, err := s.repo.refExists(ctx, tx, f.Ref, id, current[f.Column])
		if err != nil {
			return err
		}
		if !ok {
			v.Errors[f.Column] = "Pilihan " + f.Label + " tidak ditemukan atau sudah nonaktif."
		}
	}
	return nil
}

// mapDBError menerjemahkan pelanggaran unik menjadi error field. Mengembalikan
// true bila error sudah ditangani.
func mapDBError(e *Entity, err error, v *Validated) bool {
	if key, ok := database.DuplicateKey(err); ok {
		if rule, ok := e.UniqueFields[key]; ok {
			v.Errors[rule.Column] = rule.Message
		} else {
			v.Errors["_form"] = "Data yang sama sudah ada."
		}
		return true
	}
	if database.IsForeignKeyMissing(err) {
		v.Errors["_form"] = "Salah satu pilihan tidak lagi tersedia. Muat ulang halaman."
		return true
	}
	if name, ok := database.CheckConstraint(err); ok {
		if rule, ok := e.CheckFields[name]; ok {
			v.Errors[rule.Column] = rule.Message
		} else {
			v.Errors["_form"] = "Nilai yang diisi tidak memenuhi aturan data."
		}
		return true
	}
	return false
}

func snapshot(vals map[string]*string) map[string]any {
	m := make(map[string]any, len(vals))
	for k, v := range vals {
		if v == nil {
			m[k] = nil
		} else {
			m[k] = *v
		}
	}
	return m
}

func recordSnapshot(r Record) map[string]any {
	m := make(map[string]any, len(r.Values)+1)
	for k, v := range r.Values {
		m[k] = v
	}
	m["is_active"] = r.IsActive
	return m
}

// Create menyimpan data baru. Bila validasi gagal, Validated berisi error (id = 0).
func (s *Service) Create(ctx context.Context, e *Entity, get func(string) string) (int64, Validated, error) {
	v := Validate(e, get)
	now := s.clock.Now()
	var id int64
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.checkRefs(ctx, tx, e, &v, nil); err != nil {
			return err
		}
		if !v.OK() {
			return nil
		}
		newID, err := s.repo.insert(ctx, tx, e, v.Values, now)
		if err != nil {
			return err
		}
		id = newID
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "master." + e.Slug + ".create", EntityType: e.Table, EntityID: id, After: snapshot(v.Values),
		})
	})
	if err != nil {
		if mapDBError(e, err, &v) {
			return 0, v, nil
		}
		return 0, v, err
	}
	if !v.OK() {
		return 0, v, nil
	}
	return id, v, nil
}

// Update mengubah data. Data hasil sinkronisasi yang diubah manual ditandai
// local_modified_at agar sinkronisasi berikutnya tidak menimpanya.
func (s *Service) Update(ctx context.Context, e *Entity, id int64, get func(string) string) (Validated, error) {
	v := Validate(e, get)
	now := s.clock.Now()
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.repo.get(ctx, tx, e, id, true)
		if err != nil {
			return err
		}
		if err := s.checkRefs(ctx, tx, e, &v, before.Values); err != nil {
			return err
		}
		if !v.OK() {
			return nil
		}
		markLocal := e.TracksSync && before.Source != "LOCAL"
		if err := s.repo.update(ctx, tx, e, id, v.Values, now, markLocal); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "master." + e.Slug + ".update", EntityType: e.Table, EntityID: id,
			Before: recordSnapshot(before), After: snapshot(v.Values),
		})
	})
	if err != nil {
		if mapDBError(e, err, &v) {
			return v, nil
		}
		return v, err
	}
	return v, nil
}

// SetActive mengaktifkan/menonaktifkan data.
func (s *Service) SetActive(ctx context.Context, e *Entity, id int64, active bool) error {
	if !e.HasActive {
		return errors.New("entitas tidak mendukung status aktif")
	}
	now := s.clock.Now()
	return database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.repo.get(ctx, tx, e, id, true)
		if err != nil {
			return err
		}
		if before.IsActive == active {
			return nil
		}
		if err := s.repo.setActive(ctx, tx, e, id, active, now, e.TracksSync && before.Source != "LOCAL"); err != nil {
			return err
		}
		action := "deactivate"
		if active {
			action = "activate"
		}
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "master." + e.Slug + "." + action, EntityType: e.Table, EntityID: id,
			Before: map[string]bool{"is_active": before.IsActive}, After: map[string]bool{"is_active": active},
		})
	})
}

// Delete menghapus data yang belum pernah dipakai. Data yang sudah dipakai
// ditolak database (FK RESTRICT) dan dikembalikan sebagai ErrInUse.
func (s *Service) Delete(ctx context.Context, e *Entity, id int64) error {
	now := s.clock.Now()
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.repo.get(ctx, tx, e, id, true)
		if err != nil {
			return err
		}
		if e.TracksSync && before.Source != "LOCAL" {
			return ErrInUse
		}
		if err := s.repo.delete(ctx, tx, e, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "master." + e.Slug + ".delete", EntityType: e.Table, EntityID: id, Before: recordSnapshot(before),
		})
	})
	if database.IsForeignKeyInUse(err) {
		return ErrInUse
	}
	return err
}
