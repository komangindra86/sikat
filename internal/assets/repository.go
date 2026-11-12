package assets

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"sikat/internal/database"
)

// ErrNotFound dikembalikan bila perangkat tidak ada.
var ErrNotFound = errors.New("perangkat tidak ditemukan")

const selectAsset = `
	SELECT a.id, a.asset_code, a.name, a.asset_category_id, c.name, a.brand, a.model, a.serial_number,
	       a.department_id, d.name, a.room_id, r.name, a.procured_at, a.status, a.disposed_at, a.notes,
	       a.source, a.created_at, a.updated_at,
	       EXISTS (SELECT 1 FROM asset_alerts al WHERE al.asset_id = a.id AND al.status <> 'CLOSED')
	  FROM assets a
	  JOIN asset_categories c ON c.id = a.asset_category_id
	  LEFT JOIN departments d ON d.id = a.department_id
	  LEFT JOIN rooms r ON r.id = a.room_id`

func scanAsset(sc interface{ Scan(...any) error }) (Asset, error) {
	var a Asset
	err := sc.Scan(&a.ID, &a.AssetCode, &a.Name, &a.CategoryID, &a.CategoryName, &a.Brand, &a.Model, &a.SerialNumber,
		&a.DepartmentID, &a.DepartmentName, &a.RoomID, &a.RoomName, &a.ProcuredAt, &a.Status, &a.DisposedAt, &a.Notes,
		&a.Source, &a.CreatedAt, &a.UpdatedAt, &a.HasOpenAlert)
	return a, err
}

type repository struct{}

func (repository) list(ctx context.Context, db database.DBTX, f Filter) ([]Asset, int, error) {
	where := []string{"1=1"}
	var args []any
	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + escapeLike(s) + "%"
		where = append(where, "(a.asset_code LIKE ? OR a.name LIKE ? OR a.serial_number LIKE ? OR a.brand LIKE ? OR a.model LIKE ?)")
		args = append(args, like, like, like, like, like)
	}
	if f.DepartmentID != 0 {
		where = append(where, "a.department_id = ?")
		args = append(args, f.DepartmentID)
	}
	if f.CategoryID != 0 {
		where = append(where, "a.asset_category_id = ?")
		args = append(args, f.CategoryID)
	}
	switch f.Status {
	case "":
		where = append(where, "a.status <> 'DISPOSED'")
	case "ALL":
	default:
		where = append(where, "a.status = ?")
		args = append(args, f.Status)
	}
	w := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets a"+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx, selectAsset+w+" ORDER BY a.asset_code LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (repository) get(ctx context.Context, db database.DBTX, id int64, forUpdate bool) (Asset, error) {
	q := selectAsset + " WHERE a.id = ?"
	if forUpdate {
		q += " FOR UPDATE"
	}
	a, err := scanAsset(db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}

func args(in Input) []any {
	var procured sql.NullTime
	if in.ProcuredAt != nil {
		procured = sql.NullTime{Time: *in.ProcuredAt, Valid: true}
	}
	return []any{
		in.AssetCode, in.Name, in.CategoryID, database.NullString(in.Brand), database.NullString(in.Model),
		database.NullString(in.SerialNumber), database.NullInt64(in.DepartmentID), database.NullInt64(in.RoomID),
		dateArg(procured), in.Status, database.NullString(in.Notes),
	}
}

// dateArg mengirim DATE sebagai string agar tidak terkena konversi zona waktu.
func dateArg(t sql.NullTime) any {
	if !t.Valid {
		return nil
	}
	return t.Time.Format("2006-01-02")
}

func (repository) insert(ctx context.Context, db database.DBTX, in Input, now time.Time) (int64, error) {
	var disposed sql.NullTime
	if in.Status == StatusDisposed {
		disposed = sql.NullTime{Time: now, Valid: true}
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO assets (asset_code, name, asset_category_id, brand, model, serial_number,
		                    department_id, room_id, procured_at, status, notes, disposed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append(args(in), disposed, now, now)...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (repository) update(ctx context.Context, db database.DBTX, id int64, in Input, disposed sql.NullTime, now time.Time, markLocal bool) error {
	q := `UPDATE assets SET asset_code = ?, name = ?, asset_category_id = ?, brand = ?, model = ?, serial_number = ?,
	             department_id = ?, room_id = ?, procured_at = ?, status = ?, notes = ?, disposed_at = ?, updated_at = ?`
	a := append(args(in), disposed, now)
	if markLocal {
		q += ", local_modified_at = ?"
		a = append(a, now)
	}
	_, err := db.ExecContext(ctx, q+" WHERE id = ?", append(a, id)...)
	return err
}

func (repository) categoryActive(ctx context.Context, db database.DBTX, id int64) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_categories WHERE id = ? AND is_active = TRUE`, id).Scan(&n)
	return n > 0, err
}

func (repository) departmentActive(ctx context.Context, db database.DBTX, id int64) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM departments WHERE id = ? AND is_active = TRUE`, id).Scan(&n)
	return n > 0, err
}

// roomDepartment mengembalikan unit pemilik ruangan aktif (0 bila tidak ada/nonaktif).
func (repository) roomDepartment(ctx context.Context, db database.DBTX, roomID int64) (int64, error) {
	var dept int64
	err := db.QueryRowContext(ctx, `SELECT department_id FROM rooms WHERE id = ? AND is_active = TRUE`, roomID).Scan(&dept)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return dept, err
}

func (repository) options(ctx context.Context, db database.DBTX, table string, include int64) ([]Option, error) {
	// table hanya diisi konstanta dari service ("asset_categories" / "departments").
	rows, err := db.QueryContext(ctx, "SELECT id, name, is_active FROM "+table+" WHERE is_active = TRUE OR id = ? ORDER BY name", include)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Option
	for rows.Next() {
		var o Option
		var active bool
		if err := rows.Scan(&o.ID, &o.Label, &active); err != nil {
			return nil, err
		}
		if !active {
			o.Label += " (nonaktif)"
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (repository) rooms(ctx context.Context, db database.DBTX, departmentID, include int64) ([]RoomOption, error) {
	q := `SELECT r.id, r.name, r.department_id, r.is_active FROM rooms r
	       WHERE (r.is_active = TRUE OR r.id = ?)`
	a := []any{include}
	if departmentID != 0 {
		q += " AND r.department_id = ?"
		a = append(a, departmentID)
	}
	rows, err := db.QueryContext(ctx, q+" ORDER BY r.name", a...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoomOption
	for rows.Next() {
		var o RoomOption
		var active bool
		if err := rows.Scan(&o.ID, &o.Label, &o.DepartmentID, &active); err != nil {
			return nil, err
		}
		if !active {
			o.Label += " (nonaktif)"
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (repository) statusCounts(ctx context.Context, db database.DBTX) ([]StatusCount, error) {
	rows, err := db.QueryContext(ctx, `SELECT status, COUNT(*) FROM assets GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatusCount
	for rows.Next() {
		var s StatusCount
		if err := rows.Scan(&s.Status, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
