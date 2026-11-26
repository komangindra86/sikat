package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"sikat/internal/database"
)

// Aksi per baris hasil penerapan.
const (
	ActInsert     = "INSERT"
	ActUpdate     = "UPDATE"
	ActUnchanged  = "UNCHANGED"
	ActLink       = "LINK" // ditautkan ke data lokal yang sudah ada (isi sama)
	ActConflict   = "CONFLICT"
	ActError      = "ERROR"
	ActDeactivate = "DEACTIVATE"
	ActMissing    = "MISSING" // tidak ada di sumber; perangkat tidak diubah
)

// RowResult adalah hasil satu baris.
type RowResult struct {
	Row        int    `json:"row,omitempty"`
	ExternalID string `json:"external_id"`
	Label      string `json:"label"`
	Action     string `json:"action"`
	Message    string `json:"message,omitempty"`
}

// Result adalah ringkasan penerapan satu batch.
type Result struct {
	Total, Inserted, Updated, Unchanged, Conflicts, Failed, Deactivated int
	Rows                                                                []RowResult
}

func (r *Result) add(rr RowResult) {
	switch rr.Action {
	case ActInsert:
		r.Inserted++
	case ActUpdate:
		r.Updated++
	case ActUnchanged, ActLink:
		r.Unchanged++
	case ActConflict:
		r.Conflicts++
	case ActError:
		r.Failed++
	case ActDeactivate, ActMissing:
		r.Deactivated++
	}
	r.Rows = append(r.Rows, rr)
}

// Options mengatur penerapan.
type Options struct {
	// Source: label sumber untuk pemetaan & kolom source (CSV/SIMRS).
	Source string
	// Policy: KeepLocal (default) atau PreferSource.
	Policy string
	// MarkMissing: data lengkap — data yang pernah datang dari sumber ini tetapi
	// tidak ada lagi dinonaktifkan (tidak dihapus).
	MarkMissing bool
	// DryRun: hitung hasil tanpa menyimpan (pratinjau).
	DryRun bool
}

// input adalah satu baris yang sudah dinormalisasi dan siap disimpan.
type input struct {
	Row        int
	ExternalID string
	Label      string
	NatCol     string // kolom kunci alami untuk menautkan data lokal yang sudah ada
	NatVal     any
	Cols       []string
	Vals       []any
	Err        string
}

func (in input) hash() []byte {
	h := sha256.New()
	for i, c := range in.Cols {
		fmt.Fprintf(h, "%s=%v\x00", c, in.Vals[i])
	}
	return h.Sum(nil)
}

// entityTable adalah metadata tabel lokal per entitas.
type entityTable struct {
	Table     string
	HasActive bool
}

var tables = map[string]entityTable{
	EntityDepartment: {"departments", true},
	EntityRoom:       {"rooms", true},
	EntityAsset:      {"assets", false},
}

// Apply menerapkan batch ke tabel lokal dalam satu transaksi (dibatalkan bila
// DryRun). Aturan (docs/ARCHITECTURE.md §9):
//   - dipetakan lewat external_id; tanpa pemetaan → ditautkan ke data lokal
//     berkode sama bila isinya sama, atau dibuat baru;
//   - data lokal yang diubah manual setelah sinkron terakhir tidak ditimpa
//     (konflik), kecuali kebijakan PreferSource;
//   - data yang hilang dari sumber tidak dihapus, hanya dinonaktifkan (MarkMissing).
func Apply(ctx context.Context, db *sql.DB, b Batch, o Options, now time.Time) (Result, error) {
	var res Result
	if o.Source == "" {
		o.Source = "CSV"
	}
	if o.Policy == "" {
		o.Policy = KeepLocal
	}
	tbl, ok := tables[b.Entity]
	if !ok {
		return res, ErrUnsupported
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }()

	ins, err := prepare(ctx, tx, b, o.Source)
	if err != nil {
		return res, err
	}
	for row, msg := range b.RowErrors {
		res.add(RowResult{Row: row, Action: ActError, Message: msg})
	}
	seen := map[string]bool{}
	for _, in := range ins {
		res.Total++
		rr := RowResult{Row: in.Row, ExternalID: in.ExternalID, Label: in.Label}
		switch {
		case in.Err != "":
			rr.Action, rr.Message = ActError, in.Err
		case seen[in.ExternalID]:
			rr.Action, rr.Message = ActError, "external_id ganda di data sumber; baris ini dilewati"
		default:
			seen[in.ExternalID] = true
			rr.Action, rr.Message, err = applyOne(ctx, tx, tbl, b.Entity, in, o, now)
			if err != nil {
				return res, err
			}
		}
		res.add(rr)
	}
	if o.MarkMissing {
		if err := markMissing(ctx, tx, tbl, b.Entity, seen, o, now, &res); err != nil {
			return res, err
		}
	}
	res.Total += len(b.RowErrors)
	sort.SliceStable(res.Rows, func(i, j int) bool {
		if res.Rows[i].Row == 0 || res.Rows[j].Row == 0 {
			return res.Rows[j].Row == 0 && res.Rows[i].Row != 0
		}
		return res.Rows[i].Row < res.Rows[j].Row
	})
	if o.DryRun {
		return res, nil
	}
	return res, tx.Commit()
}

func applyOne(ctx context.Context, tx *sql.Tx, tbl entityTable, entity string, in input, o Options, now time.Time) (string, string, error) {
	var localID int64
	var oldHash []byte
	var missingSince sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT local_id, source_hash, missing_since FROM external_id_mappings WHERE source = ? AND entity_type = ? AND external_id = ?`,
		o.Source, entity, in.ExternalID).Scan(&localID, &oldHash, &missingSince)
	mapped := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	newHash := in.hash()

	var localMod, lastSync sql.NullTime
	if mapped {
		err := tx.QueryRowContext(ctx, `SELECT local_modified_at, last_synced_at FROM `+tbl.Table+` WHERE id = ?`, localID).Scan(&localMod, &lastSync)
		if errors.Is(err, sql.ErrNoRows) {
			// Data lokal sudah tidak ada: pemetaan lama dibuang, diperlakukan sebagai baru.
			if _, err := tx.ExecContext(ctx, `DELETE FROM external_id_mappings WHERE source = ? AND entity_type = ? AND external_id = ?`,
				o.Source, entity, in.ExternalID); err != nil {
				return "", "", err
			}
			mapped = false
		} else if err != nil {
			return "", "", err
		}
	}

	if !mapped {
		// Tautkan ke data lokal berkode sama, bila ada.
		var existing int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM `+tbl.Table+` WHERE `+in.NatCol+` = ?`, in.NatVal).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			id, msg, err := insertRow(ctx, tx, tbl, in, o.Source, now)
			if err != nil || msg != "" {
				return ActError, msg, err
			}
			return ActInsert, "", upsertMapping(ctx, tx, o.Source, entity, in.ExternalID, id, newHash, now)
		case err != nil:
			return "", "", err
		}
		same, diff, err := sameValues(ctx, tx, tbl, existing, in)
		if err != nil {
			return "", "", err
		}
		if same {
			if _, err := tx.ExecContext(ctx, `UPDATE `+tbl.Table+` SET last_synced_at = ? WHERE id = ?`, now, existing); err != nil {
				return "", "", err
			}
			return ActLink, "Ditautkan ke data yang sudah ada", upsertMapping(ctx, tx, o.Source, entity, in.ExternalID, existing, newHash, now)
		}
		if o.Policy != PreferSource {
			// Tidak ditautkan agar data lokal tetap terlindungi pada impor berikutnya.
			return ActConflict, "Sudah ada data lokal berkode sama dengan isi berbeda (" + diff + ")", nil
		}
		if msg, err := updateRow(ctx, tx, tbl, existing, in, o.Source, now); err != nil || msg != "" {
			return ActError, msg, err
		}
		return ActUpdate, "Ditautkan dan diperbarui dari sumber (" + diff + ")", upsertMapping(ctx, tx, o.Source, entity, in.ExternalID, existing, newHash, now)
	}

	locallyModified := localMod.Valid && (!lastSync.Valid || localMod.Time.After(lastSync.Time))
	if string(oldHash) == string(newHash) && !missingSince.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE external_id_mappings SET last_synced_at = ?, missing_since = NULL WHERE source = ? AND entity_type = ? AND external_id = ?`,
			now, o.Source, entity, in.ExternalID); err != nil {
			return "", "", err
		}
		return ActUnchanged, "", nil
	}
	if locallyModified && o.Policy != PreferSource {
		_, diff, err := sameValues(ctx, tx, tbl, localID, in)
		if err != nil {
			return "", "", err
		}
		return ActConflict, "Data lokal diubah manual setelah sinkron terakhir; data sumber berbeda (" + diff + ")", nil
	}
	if msg, err := updateRow(ctx, tx, tbl, localID, in, o.Source, now); err != nil || msg != "" {
		return ActError, msg, err
	}
	msg := ""
	if missingSince.Valid {
		msg = "Muncul kembali di sumber"
	}
	return ActUpdate, msg, upsertMapping(ctx, tx, o.Source, entity, in.ExternalID, localID, newHash, now)
}

func insertRow(ctx context.Context, tx *sql.Tx, tbl entityTable, in input, source string, now time.Time) (int64, string, error) {
	cols := append(append([]string{}, in.Cols...), "source", "last_synced_at", "created_at", "updated_at")
	vals := append(append([]any{}, in.Vals...), source, now, now, now)
	q := `INSERT INTO ` + tbl.Table + ` (` + strings.Join(cols, ", ") + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + `)`
	res, err := tx.ExecContext(ctx, q, vals...)
	if key, dup := database.DuplicateKey(err); dup {
		return 0, "Nilai bentrok dengan data lain (" + friendlyKey(key) + ")", nil
	}
	if err != nil {
		return 0, "", err
	}
	id, err := res.LastInsertId()
	return id, "", err
}

func updateRow(ctx context.Context, tx *sql.Tx, tbl entityTable, id int64, in input, source string, now time.Time) (string, error) {
	sets := make([]string, 0, len(in.Cols)+4)
	for _, c := range in.Cols {
		sets = append(sets, c+" = ?")
	}
	sets = append(sets, "source = ?", "last_synced_at = ?", "local_modified_at = NULL", "updated_at = ?")
	args := append(append([]any{}, in.Vals...), source, now, now, id)
	_, err := tx.ExecContext(ctx, `UPDATE `+tbl.Table+` SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if key, dup := database.DuplicateKey(err); dup {
		return "Nilai bentrok dengan data lain (" + friendlyKey(key) + ")", nil
	}
	return "", err
}

func friendlyKey(key string) string {
	switch {
	case strings.Contains(key, "name"):
		return "nama sudah dipakai"
	case strings.Contains(key, "code") || strings.Contains(key, "nip"):
		return "kode sudah dipakai"
	}
	return key
}

func upsertMapping(ctx context.Context, tx *sql.Tx, source, entity, ext string, localID int64, hash []byte, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO external_id_mappings (source, entity_type, external_id, local_id, source_hash, last_synced_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE local_id = VALUES(local_id), source_hash = VALUES(source_hash),
		  last_synced_at = VALUES(last_synced_at), missing_since = NULL`,
		source, entity, ext, localID, hash, now, now)
	return err
}

// sameValues membandingkan isi data lokal dengan data sumber; mengembalikan
// daftar kolom yang berbeda untuk pesan konflik.
func sameValues(ctx context.Context, tx *sql.Tx, tbl entityTable, id int64, in input) (bool, string, error) {
	sel := make([]string, len(in.Cols))
	for i, c := range in.Cols {
		sel[i] = "COALESCE(CAST(" + c + " AS CHAR), '')"
	}
	cur := make([]sql.NullString, len(in.Cols))
	ptr := make([]any, len(cur))
	for i := range cur {
		ptr[i] = &cur[i]
	}
	if err := tx.QueryRowContext(ctx, `SELECT `+strings.Join(sel, ", ")+` FROM `+tbl.Table+` WHERE id = ?`, id).Scan(ptr...); err != nil {
		return false, "", err
	}
	var diff []string
	for i, c := range in.Cols {
		if cur[i].String != valueString(in.Vals[i]) {
			diff = append(diff, columnLabel(c))
		}
	}
	return len(diff) == 0, strings.Join(diff, ", "), nil
}

func valueString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "1"
		}
		return "0"
	case sql.NullInt64:
		if !x.Valid {
			return ""
		}
		return fmt.Sprint(x.Int64)
	case sql.NullString:
		return x.String
	default:
		return fmt.Sprint(x)
	}
}

var colLabels = map[string]string{
	"code": "kode", "name": "nama", "full_name": "nama", "is_critical_service": "layanan kritis", "is_active": "aktif",
	"department_id": "unit", "location_note": "lokasi", "position": "jabatan", "phone": "telepon", "nip": "NIP",
	"asset_code": "kode aset", "asset_category_id": "kategori", "brand": "merek", "model": "model",
	"serial_number": "nomor seri", "room_id": "ruangan",
}

func columnLabel(c string) string {
	if l, ok := colLabels[c]; ok {
		return l
	}
	return c
}

// markMissing menonaktifkan data yang pernah datang dari sumber ini tetapi tidak
// ada lagi di data lengkap terbaru. Data yang diubah manual dilaporkan sebagai konflik.
func markMissing(ctx context.Context, tx *sql.Tx, tbl entityTable, entity string, seen map[string]bool, o Options, now time.Time, res *Result) error {
	rows, err := tx.QueryContext(ctx, `SELECT external_id, local_id FROM external_id_mappings
		WHERE source = ? AND entity_type = ? AND missing_since IS NULL`, o.Source, entity)
	if err != nil {
		return err
	}
	type miss struct {
		ext string
		id  int64
	}
	var list []miss
	for rows.Next() {
		var m miss
		if err := rows.Scan(&m.ext, &m.id); err != nil {
			rows.Close()
			return err
		}
		if !seen[m.ext] {
			list = append(list, m)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range list {
		rr := RowResult{ExternalID: m.ext, Label: labelOf(ctx, tx, tbl, m.id)}
		if !tbl.HasActive {
			rr.Action, rr.Message = ActMissing, "Tidak ada lagi di sumber; perangkat tidak diubah (status dikelola lewat pemeliharaan)"
		} else {
			var localMod, lastSync sql.NullTime
			_ = tx.QueryRowContext(ctx, `SELECT local_modified_at, last_synced_at FROM `+tbl.Table+` WHERE id = ?`, m.id).Scan(&localMod, &lastSync)
			if localMod.Valid && (!lastSync.Valid || localMod.Time.After(lastSync.Time)) && o.Policy != PreferSource {
				res.add(RowResult{ExternalID: m.ext, Label: rr.Label, Action: ActConflict,
					Message: "Tidak ada lagi di sumber, tetapi data lokal diubah manual; tidak dinonaktifkan"})
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE `+tbl.Table+` SET is_active = FALSE, last_synced_at = ?, updated_at = ? WHERE id = ?`, now, now, m.id); err != nil {
				return err
			}
			rr.Action, rr.Message = ActDeactivate, "Tidak ada lagi di sumber; dinonaktifkan (tidak dihapus)"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE external_id_mappings SET missing_since = ? WHERE source = ? AND entity_type = ? AND external_id = ?`,
			now, o.Source, entity, m.ext); err != nil {
			return err
		}
		res.add(rr)
	}
	return nil
}

func labelOf(ctx context.Context, tx *sql.Tx, tbl entityTable, id int64) string {
	var s string
	_ = tx.QueryRowContext(ctx, `SELECT name FROM `+tbl.Table+` WHERE id = ?`, id).Scan(&s)
	return s
}

// ---------------------------------------------------------------------------
// Normalisasi & resolusi referensi per entitas
// ---------------------------------------------------------------------------

func checkLen(errs *[]string, label, v string, max int) {
	if utf8.RuneCountInString(v) > max {
		*errs = append(*errs, fmt.Sprintf("%s maksimal %d karakter", label, max))
	}
}

// resolveRef mencari ID lokal dari referensi: external_id pada sumber yang sama,
// lalu kode lokal.
func resolveRef(ctx context.Context, tx *sql.Tx, source, entity, table, codeCol, ref string) (int64, bool, error) {
	if ref == "" {
		return 0, false, nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT local_id FROM external_id_mappings WHERE source = ? AND entity_type = ? AND external_id = ?`,
		source, entity, ref).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE `+codeCol+` = ?`, ref).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func prepare(ctx context.Context, tx *sql.Tx, b Batch, source string) ([]input, error) {
	var out []input
	switch b.Entity {
	case EntityDepartment:
		for _, d := range b.Departments {
			var errs []string
			checkLen(&errs, "external_id", d.ExternalID, 100)
			checkLen(&errs, "Kode", d.Code, 30)
			checkLen(&errs, "Nama", d.Name, 150)
			out = append(out, input{Row: d.Row, ExternalID: d.ExternalID, Label: d.Name, NatCol: "code", NatVal: strings.ToUpper(d.Code),
				Cols: []string{"code", "name", "is_critical_service", "is_active"},
				Vals: []any{strings.ToUpper(d.Code), d.Name, d.Critical, d.Active}, Err: strings.Join(errs, "; ")})
		}
	case EntityRoom:
		for _, r := range b.Rooms {
			var errs []string
			checkLen(&errs, "external_id", r.ExternalID, 100)
			checkLen(&errs, "Kode", r.Code, 30)
			checkLen(&errs, "Nama", r.Name, 150)
			checkLen(&errs, "Lokasi", r.LocationNote, 150)
			dept, ok, err := resolveRef(ctx, tx, source, EntityDepartment, "departments", "code", r.DepartmentRef)
			if err != nil {
				return nil, err
			}
			if !ok {
				errs = append(errs, "Unit "+r.DepartmentRef+" tidak ditemukan (impor unit lebih dulu atau pakai kode unit SIKAT)")
			}
			out = append(out, input{Row: r.Row, ExternalID: r.ExternalID, Label: r.Name, NatCol: "code", NatVal: strings.ToUpper(r.Code),
				Cols: []string{"department_id", "code", "name", "location_note", "is_active"},
				Vals: []any{dept, strings.ToUpper(r.Code), r.Name, nullStr(r.LocationNote), r.Active}, Err: strings.Join(errs, "; ")})
		}
	case EntityAsset:
		for _, a := range b.Assets {
			var errs []string
			checkLen(&errs, "external_id", a.ExternalID, 100)
			checkLen(&errs, "Kode aset", a.Code, 50)
			checkLen(&errs, "Nama", a.Name, 150)
			checkLen(&errs, "Merek", a.Brand, 80)
			checkLen(&errs, "Model", a.Model, 80)
			checkLen(&errs, "Nomor seri", a.Serial, 100)
			var cat int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM asset_categories WHERE code = ? OR name = ? ORDER BY code = ? DESC LIMIT 1`,
				a.CategoryRef, a.CategoryRef, a.CategoryRef).Scan(&cat)
			if errors.Is(err, sql.ErrNoRows) {
				errs = append(errs, "Kategori perangkat "+a.CategoryRef+" belum ada di SIKAT (tambahkan di Master Data)")
			} else if err != nil {
				return nil, err
			}
			var room, dept sql.NullInt64
			if a.RoomRef != "" {
				id, ok, err := resolveRef(ctx, tx, source, EntityRoom, "rooms", "code", a.RoomRef)
				if err != nil {
					return nil, err
				}
				if !ok {
					errs = append(errs, "Ruangan "+a.RoomRef+" tidak ditemukan")
				} else {
					room = sql.NullInt64{Int64: id, Valid: true}
					var d int64
					if err := tx.QueryRowContext(ctx, `SELECT department_id FROM rooms WHERE id = ?`, id).Scan(&d); err != nil {
						return nil, err
					}
					dept = sql.NullInt64{Int64: d, Valid: true}
				}
			}
			if a.DepartmentRef != "" {
				id, ok, err := resolveRef(ctx, tx, source, EntityDepartment, "departments", "code", a.DepartmentRef)
				if err != nil {
					return nil, err
				}
				switch {
				case !ok:
					errs = append(errs, "Unit "+a.DepartmentRef+" tidak ditemukan")
				case dept.Valid && dept.Int64 != id:
					errs = append(errs, "Ruangan tidak berada di unit "+a.DepartmentRef)
				default:
					dept = sql.NullInt64{Int64: id, Valid: true}
				}
			}
			out = append(out, input{Row: a.Row, ExternalID: a.ExternalID, Label: a.Name, NatCol: "asset_code", NatVal: a.Code,
				Cols: []string{"asset_code", "name", "asset_category_id", "brand", "model", "serial_number", "department_id", "room_id"},
				Vals: []any{a.Code, a.Name, cat, nullStr(a.Brand), nullStr(a.Model), nullStr(a.Serial), dept, room},
				Err:  strings.Join(errs, "; ")})
		}
	}
	return out, nil
}
