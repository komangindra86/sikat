package master

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"sikat/internal/database"
)

// ErrNotFound dikembalikan bila baris tidak ada.
var ErrNotFound = errors.New("data tidak ditemukan")

type repository struct{}

func (repository) columns(e *Entity) []string {
	cols := []string{"id"}
	for _, f := range e.Fields {
		cols = append(cols, f.Column)
	}
	if e.HasActive {
		cols = append(cols, "is_active")
	}
	if e.TracksSync {
		cols = append(cols, "source")
	}
	return append(cols, "updated_at")
}

func (r repository) scan(e *Entity, sc interface{ Scan(...any) error }) (Record, error) {
	rec := Record{Values: map[string]string{}, IsActive: true, Source: "LOCAL"}
	vals := make([]sql.NullString, len(e.Fields))
	dest := []any{&rec.ID}
	for i := range vals {
		dest = append(dest, &vals[i])
	}
	if e.HasActive {
		dest = append(dest, &rec.IsActive)
	}
	if e.TracksSync {
		dest = append(dest, &rec.Source)
	}
	dest = append(dest, &rec.UpdatedAt)
	if err := sc.Scan(dest...); err != nil {
		return Record{}, err
	}
	for i, f := range e.Fields {
		rec.Values[f.Column] = vals[i].String
	}
	return rec, nil
}

func (r repository) list(ctx context.Context, db database.DBTX, e *Entity, q ListQuery) ([]Record, int, error) {
	where := []string{"1=1"}
	var args []any
	if e.HasActive && !q.IncludeInactive {
		where = append(where, "is_active = TRUE")
	}
	if s := strings.TrimSpace(q.Search); s != "" && len(e.Search) > 0 {
		like := "%" + escapeLike(s) + "%"
		var ors []string
		for _, c := range e.Search {
			ors = append(ors, c+" LIKE ?")
			args = append(args, like)
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	w := strings.Join(where, " AND ")

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+e.Table+" WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT "+strings.Join(r.columns(e), ", ")+" FROM "+e.Table+" WHERE "+w+
			" ORDER BY "+e.OrderBy+", id LIMIT ? OFFSET ?", append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		rec, err := r.scan(e, rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	return out, total, rows.Err()
}

func (r repository) get(ctx context.Context, db database.DBTX, e *Entity, id int64, forUpdate bool) (Record, error) {
	q := "SELECT " + strings.Join(r.columns(e), ", ") + " FROM " + e.Table + " WHERE id = ?"
	if forUpdate {
		q += " FOR UPDATE"
	}
	rec, err := r.scan(e, db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return rec, err
}

func (r repository) insert(ctx context.Context, db database.DBTX, e *Entity, vals map[string]*string, now time.Time) (int64, error) {
	cols := []string{}
	ph := []string{}
	args := []any{}
	for _, f := range e.Fields {
		cols = append(cols, f.Column)
		ph = append(ph, "?")
		args = append(args, nullable(vals[f.Column]))
	}
	cols = append(cols, "created_at", "updated_at")
	ph = append(ph, "?", "?")
	args = append(args, now, now)
	res, err := db.ExecContext(ctx,
		"INSERT INTO "+e.Table+" ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(ph, ", ")+")", args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r repository) update(ctx context.Context, db database.DBTX, e *Entity, id int64, vals map[string]*string, now time.Time, markLocal bool) error {
	sets := []string{}
	args := []any{}
	for _, f := range e.Fields {
		if f.Auto != nil {
			continue // kode otomatis tidak pernah diubah
		}
		sets = append(sets, f.Column+" = ?")
		args = append(args, nullable(vals[f.Column]))
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, now)
	if markLocal {
		sets = append(sets, "local_modified_at = ?")
		args = append(args, now)
	}
	args = append(args, id)
	_, err := db.ExecContext(ctx, "UPDATE "+e.Table+" SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

// setActive mengubah status aktif; markLocal menandai perubahan manual pada data
// yang berasal dari sumber luar (agar sinkronisasi tidak menimpanya).
func (r repository) setActive(ctx context.Context, db database.DBTX, e *Entity, id int64, active bool, now time.Time, markLocal bool) error {
	q := "UPDATE " + e.Table + " SET is_active = ?, updated_at = ?"
	if markLocal {
		q += ", local_modified_at = ?"
		_, err := db.ExecContext(ctx, q+" WHERE id = ?", active, now, now, id)
		return err
	}
	_, err := db.ExecContext(ctx, q+" WHERE id = ?", active, now, id)
	return err
}

func (r repository) delete(ctx context.Context, db database.DBTX, e *Entity, id int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM "+e.Table+" WHERE id = ?", id)
	return err
}

// refExists memeriksa referensi ada dan aktif. Nilai lama (currentID) tetap
// diterima walaupun sudah nonaktif agar edit tidak gagal karena referensi lama.
func (r repository) refExists(ctx context.Context, db database.DBTX, ref *Ref, id int64, currentID string) (bool, error) {
	if currentID != "" && currentID == strconv.FormatInt(id, 10) {
		return true, nil
	}
	var n int
	q := "SELECT COUNT(*) FROM " + ref.Table + " WHERE id = ? AND is_active = TRUE"
	if ref.NoActive {
		q = "SELECT COUNT(*) FROM " + ref.Table + " WHERE id = ?"
	}
	if ref.Where != "" {
		q += " AND (" + ref.Where + ")"
	}
	err := db.QueryRowContext(ctx, q, id).Scan(&n)
	return n > 0, err
}

// options mengambil pilihan aktif untuk select; includeID tetap disertakan walau nonaktif.
func (r repository) options(ctx context.Context, db database.DBTX, ref *Ref, includeID string) ([]Choice, error) {
	where := "TRUE"
	if ref.Where != "" {
		where = ref.Where
	}
	q := fmt.Sprintf("SELECT id, %s, is_active AND (%s) FROM %s WHERE (is_active = TRUE AND (%s)) OR id = ? ORDER BY %s", ref.LabelCol, where, ref.Table, where, ref.OrderBy)
	if ref.NoActive {
		q = fmt.Sprintf("SELECT id, %s, TRUE FROM %s WHERE (%s) OR id = ? ORDER BY %s", ref.LabelCol, ref.Table, where, ref.OrderBy)
	}
	inc, _ := strconv.ParseInt(includeID, 10, 64)
	rows, err := db.QueryContext(ctx, q, inc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Choice
	for rows.Next() {
		var id int64
		var label string
		var active bool
		if err := rows.Scan(&id, &label, &active); err != nil {
			return nil, err
		}
		if !active {
			label += " (nonaktif)"
		}
		out = append(out, Choice{Value: strconv.FormatInt(id, 10), Label: label})
	}
	return out, rows.Err()
}

// labels mengambil seluruh label referensi (termasuk nonaktif) untuk tampilan daftar.
func (r repository) labels(ctx context.Context, db database.DBTX, ref *Ref) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT id, %s FROM %s", ref.LabelCol, ref.Table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id int64
		var label string
		if err := rows.Scan(&id, &label); err != nil {
			return nil, err
		}
		out[strconv.FormatInt(id, 10)] = label
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
