package users

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"sikat/internal/database"
)

// ErrNotFound dikembalikan bila pengguna tidak ada.
var ErrNotFound = errors.New("pengguna tidak ditemukan")

const selectUser = `
	SELECT u.id, u.username, u.full_name, u.nip, u.phone, u.email, u.role_id, r.code, r.name,
	       u.department_id, d.name, u.is_active, u.must_change_password, u.locked_until, u.last_login_at, u.created_at
	  FROM users u
	  JOIN roles r ON r.id = u.role_id
	  LEFT JOIN departments d ON d.id = u.department_id`

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var u User
	err := sc.Scan(&u.ID, &u.Username, &u.FullName, &u.NIP, &u.Phone, &u.Email, &u.RoleID, &u.RoleCode, &u.RoleName,
		&u.DepartmentID, &u.DepartmentName, &u.IsActive, &u.MustChangePassword, &u.LockedUntil, &u.LastLoginAt, &u.CreatedAt)
	return u, err
}

type repository struct{}

func (repository) list(ctx context.Context, db database.DBTX, f Filter) ([]User, int, error) {
	where := []string{"1=1"}
	var args []any
	if !f.IncludeInactive {
		where = append(where, "u.is_active = TRUE")
	}
	if f.RoleID != 0 {
		where = append(where, "u.role_id = ?")
		args = append(args, f.RoleID)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
		where = append(where, "(u.username LIKE ? OR u.full_name LIKE ? OR u.nip LIKE ?)")
		args = append(args, like, like, like)
	}
	w := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users u"+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx, selectUser+w+" ORDER BY u.full_name LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (repository) get(ctx context.Context, db database.DBTX, id int64, forUpdate bool) (User, error) {
	q := selectUser + " WHERE u.id = ?"
	if forUpdate {
		q += " FOR UPDATE"
	}
	u, err := scanUser(db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	rows, err := db.QueryContext(ctx, `SELECT it_category_id FROM user_competencies WHERE user_id = ? ORDER BY it_category_id`, id)
	if err != nil {
		return User{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var c int64
		if err := rows.Scan(&c); err != nil {
			return User{}, err
		}
		u.Competencies = append(u.Competencies, c)
	}
	return u, rows.Err()
}

func (repository) roles(ctx context.Context, db database.DBTX) ([]Role, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, code, name FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Code, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (repository) roleCode(ctx context.Context, db database.DBTX, id int64) (string, error) {
	var code string
	err := db.QueryRowContext(ctx, `SELECT code FROM roles WHERE id = ?`, id).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return code, err
}

func (repository) options(ctx context.Context, db database.DBTX, table string, include []int64) ([]Option, error) {
	// table hanya konstanta dari service.
	q := "SELECT id, name, is_active FROM " + table + " WHERE is_active = TRUE"
	args := []any{}
	if len(include) > 0 {
		q += " OR id IN (?" + strings.Repeat(",?", len(include)-1) + ")"
		for _, id := range include {
			args = append(args, id)
		}
	}
	order := " ORDER BY name"
	if table == "it_categories" {
		order = " ORDER BY sort_order, name"
	}
	rows, err := db.QueryContext(ctx, q+order, args...)
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

func (repository) countActive(ctx context.Context, db database.DBTX, table string, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE is_active = TRUE AND id IN (?"+
		strings.Repeat(",?", len(ids)-1)+")", args...).Scan(&n)
	return n, err
}

// countActiveAdmins menghitung admin aktif dengan mengunci baris-barisnya,
// sehingga dua admin tidak bisa saling menonaktifkan secara bersamaan.
func (repository) countActiveAdmins(ctx context.Context, tx *sql.Tx) (int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT u.id FROM users u JOIN roles r ON r.id = u.role_id
		 WHERE r.code = 'ADMIN' AND u.is_active = TRUE FOR UPDATE`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}

func (repository) insert(ctx context.Context, db database.DBTX, in Input, hash string, now time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, `
		INSERT INTO users (role_id, department_id, username, full_name, nip, phone, email, password_hash,
		                   must_change_password, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?)`,
		in.RoleID, database.NullInt64(in.DepartmentID), in.Username, in.FullName, database.NullString(in.NIP),
		database.NullString(in.Phone), database.NullString(in.Email), hash, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (repository) update(ctx context.Context, db database.DBTX, id int64, in Input, now time.Time) error {
	_, err := db.ExecContext(ctx, `
		UPDATE users SET role_id = ?, department_id = ?, full_name = ?, nip = ?, phone = ?, email = ?, updated_at = ?
		 WHERE id = ?`,
		in.RoleID, database.NullInt64(in.DepartmentID), in.FullName, database.NullString(in.NIP),
		database.NullString(in.Phone), database.NullString(in.Email), now, id)
	return err
}

func (repository) replaceCompetencies(ctx context.Context, db database.DBTX, id int64, cats []int64, now time.Time) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM user_competencies WHERE user_id = ?`, id); err != nil {
		return err
	}
	for _, c := range cats {
		if _, err := db.ExecContext(ctx, `INSERT INTO user_competencies (user_id, it_category_id, created_at) VALUES (?, ?, ?)`, id, c, now); err != nil {
			return err
		}
	}
	return nil
}
