package assets

import (
	"context"
	"database/sql"
	"time"

	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
)

// Service berisi aturan bisnis perangkat.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	repo  repository
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock) *Service {
	return &Service{db: db, clock: c}
}

// List mengembalikan daftar perangkat dan total.
func (s *Service) List(ctx context.Context, f Filter) ([]Asset, int, error) {
	return s.repo.list(ctx, s.db, f)
}

// Get mengambil satu perangkat.
func (s *Service) Get(ctx context.Context, id int64) (Asset, error) {
	return s.repo.get(ctx, s.db, id, false)
}

// Categories mengembalikan pilihan kategori.
func (s *Service) Categories(ctx context.Context, include int64) ([]Option, error) {
	return s.repo.options(ctx, s.db, "asset_categories", include)
}

// Departments mengembalikan pilihan unit.
func (s *Service) Departments(ctx context.Context, include int64) ([]Option, error) {
	return s.repo.options(ctx, s.db, "departments", include)
}

// Rooms mengembalikan pilihan ruangan, dibatasi unit bila departmentID != 0.
func (s *Service) Rooms(ctx context.Context, departmentID, include int64) ([]RoomOption, error) {
	return s.repo.rooms(ctx, s.db, departmentID, include)
}

// StatusCounts untuk ringkasan dashboard.
func (s *Service) StatusCounts(ctx context.Context) ([]StatusCount, error) {
	return s.repo.statusCounts(ctx, s.db)
}

// CodePrefix adalah awalan kode inventaris otomatis: PRK-<tahun>-NNNN.
const CodePrefix = "PRK"

func today(now time.Time) time.Time {
	y, m, d := now.In(clock.Location).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, clock.Location)
}

// checkRefs memvalidasi referensi dan melengkapi unit dari ruangan bila kosong.
// Referensi lama (before) tetap diterima walaupun sudah nonaktif.
func (s *Service) checkRefs(ctx context.Context, tx *sql.Tx, in *Input, errs map[string]string, before *Asset) error {
	if in.CategoryID != 0 && (before == nil || before.CategoryID != in.CategoryID) {
		ok, err := s.repo.categoryActive(ctx, tx, in.CategoryID)
		if err != nil {
			return err
		}
		if !ok {
			errs["asset_category_id"] = "Kategori tidak ditemukan atau nonaktif."
		}
	}
	if in.DepartmentID != 0 && (before == nil || before.DepartmentID.Int64 != in.DepartmentID) {
		ok, err := s.repo.departmentActive(ctx, tx, in.DepartmentID)
		if err != nil {
			return err
		}
		if !ok {
			errs["department_id"] = "Unit tidak ditemukan atau nonaktif."
		}
	}
	if in.RoomID != 0 {
		var roomDept int64
		if before != nil && before.RoomID.Int64 == in.RoomID {
			// ruangan lama: baca unitnya walau ruangan sudah nonaktif
			if err := tx.QueryRowContext(ctx, `SELECT department_id FROM rooms WHERE id = ?`, in.RoomID).Scan(&roomDept); err != nil {
				return err
			}
		} else {
			d, err := s.repo.roomDepartment(ctx, tx, in.RoomID)
			if err != nil {
				return err
			}
			roomDept = d
		}
		switch {
		case roomDept == 0:
			errs["room_id"] = "Ruangan tidak ditemukan atau nonaktif."
		case in.DepartmentID == 0:
			in.DepartmentID = roomDept // unit otomatis dari ruangan
		case in.DepartmentID != roomDept:
			errs["room_id"] = "Ruangan ini bukan milik unit yang dipilih."
		}
	}
	return nil
}

func mapDBError(err error, errs map[string]string) bool {
	if key, ok := database.DuplicateKey(err); ok {
		if key == "uq_assets_code" {
			errs["asset_code"] = "Kode inventaris sudah dipakai perangkat lain."
		} else {
			errs["_form"] = "Data yang sama sudah ada."
		}
		return true
	}
	if database.IsForeignKeyMissing(err) {
		errs["_form"] = "Salah satu pilihan tidak lagi tersedia. Muat ulang halaman."
		return true
	}
	return false
}

func snapshot(in Input) map[string]any {
	m := map[string]any{
		"asset_code": in.AssetCode, "name": in.Name, "asset_category_id": in.CategoryID,
		"brand": in.Brand, "model": in.Model, "serial_number": in.SerialNumber,
		"department_id": in.DepartmentID, "room_id": in.RoomID, "status": in.Status, "notes": in.Notes,
	}
	if in.ProcuredAt != nil {
		m["procured_at"] = in.ProcuredAt.Format("2006-01-02")
	}
	return m
}

func assetSnapshot(a Asset) map[string]any {
	m := map[string]any{
		"asset_code": a.AssetCode, "name": a.Name, "asset_category_id": a.CategoryID,
		"brand": a.Brand.String, "model": a.Model.String, "serial_number": a.SerialNumber.String,
		"department_id": a.DepartmentID.Int64, "room_id": a.RoomID.Int64, "status": a.Status, "notes": a.Notes.String,
	}
	if a.ProcuredAt.Valid {
		m["procured_at"] = a.ProcuredAt.Time.Format("2006-01-02")
	}
	return m
}

// Create menyimpan perangkat baru. id = 0 bila ada error validasi.
func (s *Service) Create(ctx context.Context, get func(string) string) (int64, map[string]string, map[string]string, error) {
	now := s.clock.Now()
	in, form, errs := Validate(get, today(now))
	var id int64
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.checkRefs(ctx, tx, &in, errs, nil); err != nil {
			return err
		}
		if len(errs) > 0 {
			return nil
		}
		code, err := database.NextCode(ctx, tx, "assets", "asset_code", CodePrefix, today(now).Year(), 4)
		if err != nil {
			return err
		}
		in.AssetCode = code
		form["asset_code"] = code
		newID, err := s.repo.insert(ctx, tx, in, now)
		if err != nil {
			return err
		}
		id = newID
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "asset.create", EntityType: "assets", EntityID: id, After: snapshot(in),
		})
	})
	if err != nil {
		if mapDBError(err, errs) {
			return 0, form, errs, nil
		}
		return 0, form, errs, err
	}
	if len(errs) > 0 {
		return 0, form, errs, nil
	}
	return id, form, nil, nil
}

// Update mengubah perangkat. Perangkat tidak pernah dihapus; gunakan status
// "Dihapus dari inventaris" (DISPOSED).
func (s *Service) Update(ctx context.Context, id int64, get func(string) string) (map[string]string, map[string]string, error) {
	now := s.clock.Now()
	in, form, errs := Validate(get, today(now))
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.repo.get(ctx, tx, id, true)
		if err != nil {
			return err
		}
		in.AssetCode = before.AssetCode // kode inventaris tidak dapat diubah
		form["asset_code"] = before.AssetCode
		if err := s.checkRefs(ctx, tx, &in, errs, &before); err != nil {
			return err
		}
		if len(errs) > 0 {
			return nil
		}
		disposed := before.DisposedAt
		switch {
		case in.Status == StatusDisposed && before.Status != StatusDisposed:
			disposed = sql.NullTime{Time: now, Valid: true}
		case in.Status != StatusDisposed:
			disposed = sql.NullTime{}
		}
		if err := s.repo.update(ctx, tx, id, in, disposed, now, before.Source != "LOCAL"); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "asset.update", EntityType: "assets", EntityID: id,
			Before: assetSnapshot(before), After: snapshot(in),
		})
	})
	if err != nil {
		if mapDBError(err, errs) {
			return form, errs, nil
		}
		return form, errs, err
	}
	if len(errs) > 0 {
		return form, errs, nil
	}
	return form, nil, nil
}

// History mengembalikan riwayat perubahan data perangkat dari audit log.
func (s *Service) History(ctx context.Context, id int64) ([]audit.Log, error) {
	logs, _, err := audit.List(ctx, s.db, audit.Filter{EntityType: "assets", EntityID: id, Limit: 50})
	return logs, err
}
