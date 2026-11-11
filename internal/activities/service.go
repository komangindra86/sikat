package activities

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"sikat/internal/attachments"
	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
	"sikat/internal/rbac"
)

// Error aturan bisnis.
var (
	ErrNotFound  = errors.New("kegiatan tidak ditemukan")
	ErrForbidden = errors.New("Anda tidak berhak mengubah kegiatan ini.")
)

// Service berisi aturan bisnis kegiatan internal.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	store attachments.Store
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock, store attachments.Store) *Service {
	return &Service{db: db, clock: c, store: store}
}

const selectActivity = `
	SELECT a.id, a.activity_no, a.activity_date, a.activity_type_id, t.name, a.it_category_id, ic.name,
	       a.department_id, d.name, a.room_id, r.name, a.asset_id, s.asset_code, s.name,
	       a.related_ticket_id, tk.ticket_no, a.title, a.description, a.result, a.status,
	       a.started_at, a.ended_at, a.duration_minutes, a.created_by, cu.full_name, a.created_at, a.updated_at,
	       lm.user_id, lu.full_name
	  FROM internal_activities a
	  JOIN activity_types t ON t.id = a.activity_type_id
	  JOIN it_categories ic ON ic.id = a.it_category_id
	  JOIN users cu ON cu.id = a.created_by
	  LEFT JOIN departments d ON d.id = a.department_id
	  LEFT JOIN rooms r ON r.id = a.room_id
	  LEFT JOIN assets s ON s.id = a.asset_id
	  LEFT JOIN tickets tk ON tk.id = a.related_ticket_id
	  LEFT JOIN activity_members lm ON lm.activity_id = a.id AND lm.role = 'LEAD'
	  LEFT JOIN users lu ON lu.id = lm.user_id`

func scan(sc interface{ Scan(...any) error }) (Activity, error) {
	var a Activity
	err := sc.Scan(&a.ID, &a.No, &a.Date, &a.TypeID, &a.TypeName, &a.ITCategoryID, &a.ITCategoryName,
		&a.DepartmentID, &a.DepartmentName, &a.RoomID, &a.RoomName, &a.AssetID, &a.AssetCode, &a.AssetName,
		&a.RelatedTicketID, &a.RelatedTicketNo, &a.Title, &a.Description, &a.Result, &a.Status,
		&a.StartedAt, &a.EndedAt, &a.DurationMinutes, &a.CreatedBy, &a.CreatedByName, &a.CreatedAt, &a.UpdatedAt,
		&a.LeadID, &a.LeadName)
	return a, err
}

func (s *Service) members(ctx context.Context, q database.DBTX, ids []int64) (map[int64][]Member, error) {
	out := map[int64][]Member{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, `
		SELECT m.activity_id, m.user_id, u.full_name, m.role FROM activity_members m JOIN users u ON u.id = m.user_id
		 WHERE m.activity_id IN (?`+strings.Repeat(",?", len(ids)-1)+`) ORDER BY m.role, u.full_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var aid int64
		var m Member
		if err := rows.Scan(&aid, &m.UserID, &m.Name, &m.Role); err != nil {
			return nil, err
		}
		out[aid] = append(out[aid], m)
	}
	return out, rows.Err()
}

// List mengembalikan daftar kegiatan dan total.
func (s *Service) List(ctx context.Context, f Filter) ([]Activity, int, error) {
	where := []string{"1=1"}
	var args []any
	if !f.From.IsZero() {
		where = append(where, "a.activity_date >= ?")
		args = append(args, f.From.Format("2006-01-02"))
	}
	if !f.To.IsZero() {
		where = append(where, "a.activity_date <= ?")
		args = append(args, f.To.Format("2006-01-02"))
	}
	if f.TypeID != 0 {
		where = append(where, "a.activity_type_id = ?")
		args = append(args, f.TypeID)
	}
	if f.ITCategoryID != 0 {
		where = append(where, "a.it_category_id = ?")
		args = append(args, f.ITCategoryID)
	}
	if f.MemberID != 0 {
		where = append(where, "EXISTS (SELECT 1 FROM activity_members x WHERE x.activity_id = a.id AND x.user_id = ?)")
		args = append(args, f.MemberID)
	}
	if validStatus[f.Status] {
		where = append(where, "a.status = ?")
		args = append(args, f.Status)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		where = append(where, "(a.activity_no LIKE ? OR a.title LIKE ? OR a.result LIKE ?)")
		args = append(args, like, like, like)
	}
	w := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM internal_activities a"+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 30
	}
	rows, err := s.db.QueryContext(ctx, selectActivity+w+" ORDER BY a.activity_date DESC, a.id DESC LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	var list []Activity
	var ids []int64
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		list = append(list, a)
		ids = append(ids, a.ID)
	}
	rows.Close()
	ms, err := s.members(ctx, s.db, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range list {
		list[i].Members = ms[list[i].ID]
	}
	return list, total, nil
}

// Get mengambil satu kegiatan beserta anggotanya.
func (s *Service) Get(ctx context.Context, id int64) (Activity, error) {
	a, err := scan(s.db.QueryRowContext(ctx, selectActivity+" WHERE a.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Activity{}, ErrNotFound
	}
	if err != nil {
		return Activity{}, err
	}
	ms, err := s.members(ctx, s.db, []int64{id})
	a.Members = ms[id]
	return a, err
}

// CanView: koordinator melihat semua; petugas hanya kegiatan yang ia ikuti/buat.
func CanView(a Activity, u *rbac.User) bool {
	if u.Has(rbac.PermActivityAll) {
		return true
	}
	if a.CreatedBy == u.ID {
		return true
	}
	for _, m := range a.Members {
		if m.UserID == u.ID {
			return true
		}
	}
	return false
}

// CanEdit: koordinator, pembuat, atau pelaksana utama.
func CanEdit(a Activity, u *rbac.User) bool {
	return u.Has(rbac.PermActivityAll) || a.CreatedBy == u.ID || (a.LeadID.Valid && a.LeadID.Int64 == u.ID)
}

func exists(ctx context.Context, tx *sql.Tx, q string, args ...any) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, q, args...).Scan(&n)
	return n > 0, err
}

// checkRefs memvalidasi referensi, melengkapi unit dari ruangan/perangkat, dan
// mencari tiket terkait dari nomornya. Referensi lama boleh sudah nonaktif.
func (s *Service) checkRefs(ctx context.Context, tx *sql.Tx, in *Input, errs map[string]string, before *Activity) (int64, error) {
	unchanged := func(old sql.NullInt64, v int64) bool { return before != nil && old.Valid && old.Int64 == v }
	if in.TypeID != 0 && (before == nil || before.TypeID != in.TypeID) {
		if ok, err := exists(ctx, tx, `SELECT COUNT(*) FROM activity_types WHERE id = ? AND is_active = TRUE`, in.TypeID); err != nil {
			return 0, err
		} else if !ok {
			errs["activity_type_id"] = "Jenis kegiatan tidak ditemukan atau nonaktif."
		}
	}
	if in.ITCategoryID != 0 && (before == nil || before.ITCategoryID != in.ITCategoryID) {
		if ok, err := exists(ctx, tx, `SELECT COUNT(*) FROM it_categories WHERE id = ? AND is_active = TRUE`, in.ITCategoryID); err != nil {
			return 0, err
		} else if !ok {
			errs["it_category_id"] = "Bidang tidak ditemukan atau nonaktif."
		}
	}
	if in.AssetID != 0 && !unchanged(before.assetID(), in.AssetID) {
		var room, dept sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT room_id, department_id FROM assets WHERE id = ? AND status <> 'DISPOSED'`, in.AssetID).Scan(&room, &dept)
		if errors.Is(err, sql.ErrNoRows) {
			errs["asset_id"] = "Perangkat tidak ditemukan atau sudah dihapus dari inventaris."
		} else if err != nil {
			return 0, err
		} else {
			if in.RoomID == 0 && room.Valid {
				in.RoomID = room.Int64
			}
			if in.DepartmentID == 0 && dept.Valid {
				in.DepartmentID = dept.Int64
			}
		}
	}
	if in.RoomID != 0 {
		var dept int64
		err := tx.QueryRowContext(ctx, `SELECT department_id FROM rooms WHERE id = ? AND (is_active = TRUE OR ? )`,
			in.RoomID, unchanged(before.roomID(), in.RoomID)).Scan(&dept)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			errs["room_id"] = "Ruangan tidak ditemukan atau nonaktif."
		case err != nil:
			return 0, err
		case in.DepartmentID == 0:
			in.DepartmentID = dept
		case in.DepartmentID != dept:
			errs["room_id"] = "Ruangan ini bukan milik unit yang dipilih."
		}
	}
	if in.DepartmentID != 0 && !unchanged(before.deptID(), in.DepartmentID) {
		if ok, err := exists(ctx, tx, `SELECT COUNT(*) FROM departments WHERE id = ? AND is_active = TRUE`, in.DepartmentID); err != nil {
			return 0, err
		} else if !ok {
			errs["department_id"] = "Unit tidak ditemukan atau nonaktif."
		}
	}
	var ticketID int64
	if in.RelatedTicketNo != "" {
		err := tx.QueryRowContext(ctx, `SELECT id FROM tickets WHERE ticket_no = ?`, in.RelatedTicketNo).Scan(&ticketID)
		if errors.Is(err, sql.ErrNoRows) {
			errs["related_ticket_no"] = "Nomor tiket tidak ditemukan."
		} else if err != nil {
			return 0, err
		}
	}
	people := append([]int64{in.LeadID}, in.SupportIDs...)
	for _, uid := range people {
		ok, err := exists(ctx, tx, `SELECT COUNT(*) FROM users u JOIN roles r ON r.id = u.role_id
			WHERE u.id = ? AND u.is_active = TRUE AND r.code IN ('TECHNICIAN','COORDINATOR')`, uid)
		if err != nil {
			return 0, err
		}
		if !ok {
			key := "support_ids"
			if uid == in.LeadID {
				key = "lead_id"
			}
			errs[key] = "Petugas tidak ditemukan atau nonaktif."
		}
	}
	return ticketID, nil
}

func (a *Activity) assetID() sql.NullInt64 {
	if a == nil {
		return sql.NullInt64{}
	}
	return a.AssetID
}

func (a *Activity) roomID() sql.NullInt64 {
	if a == nil {
		return sql.NullInt64{}
	}
	return a.RoomID
}

func (a *Activity) deptID() sql.NullInt64 {
	if a == nil {
		return sql.NullInt64{}
	}
	return a.DepartmentID
}

func timeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func (s *Service) saveMembers(ctx context.Context, tx *sql.Tx, id int64, in Input, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM activity_members WHERE activity_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO activity_members (activity_id, user_id, role, created_at) VALUES (?, ?, 'LEAD', ?)`, id, in.LeadID, now); err != nil {
		return err
	}
	for _, uid := range in.SupportIDs {
		if uid == in.LeadID {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO activity_members (activity_id, user_id, role, created_at) VALUES (?, ?, 'SUPPORT', ?)`, id, uid, now); err != nil {
			return err
		}
	}
	return nil
}

func snapshot(in Input, ticketID int64) map[string]any {
	m := map[string]any{
		"date": in.Date.Format("2006-01-02"), "activity_type_id": in.TypeID, "it_category_id": in.ITCategoryID,
		"department_id": in.DepartmentID, "room_id": in.RoomID, "asset_id": in.AssetID, "related_ticket_id": ticketID,
		"title": in.Title, "status": in.Status, "lead_id": in.LeadID, "support_ids": in.SupportIDs,
	}
	if in.StartedAt != nil {
		m["started_at"] = in.StartedAt.Format(time.RFC3339)
	}
	if in.EndedAt != nil {
		m["ended_at"] = in.EndedAt.Format(time.RFC3339)
	}
	return m
}

func mapCheck(err error, errs map[string]string) bool {
	if name, ok := database.CheckConstraint(err); ok {
		switch name {
		case "chk_activities_time":
			errs["ended_at"] = "Waktu selesai tidak boleh sebelum waktu mulai."
		case "chk_activities_done":
			errs["result"] = "Hasil wajib diisi untuk kegiatan yang selesai."
		default:
			errs["_form"] = "Isian tidak memenuhi aturan data."
		}
		return true
	}
	return false
}

// Create menyimpan kegiatan baru. Pelaksana utama = pembuat, kecuali koordinator memilih orang lain.
func (s *Service) Create(ctx context.Context, u *rbac.User, get func(string) string, getAll func(string) []string) (int64, map[string]string, map[string]string, error) {
	now := s.clock.Now()
	in, form, errs := Parse(get, getAll, now)
	if in.LeadID == 0 || !u.Has(rbac.PermActivityAll) {
		in.LeadID = u.ID
	}
	var id int64
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		ticketID, err := s.checkRefs(ctx, tx, &in, errs, nil)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			return nil
		}
		year := in.Date.Year()
		seq, err := database.NextSequence(ctx, tx, "KEG", year)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO internal_activities (activity_no, activity_date, activity_type_id, it_category_id, department_id, room_id,
			       asset_id, related_ticket_id, title, description, result, status, started_at, ended_at, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("KEG-%04d-%06d", year, seq), in.Date.Format("2006-01-02"), in.TypeID, in.ITCategoryID,
			database.NullInt64(in.DepartmentID), database.NullInt64(in.RoomID), database.NullInt64(in.AssetID), database.NullInt64(ticketID),
			in.Title, database.NullString(in.Description), database.NullString(in.Result), in.Status,
			timeArg(in.StartedAt), timeArg(in.EndedAt), u.ID, now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		if err := s.saveMembers(ctx, tx, id, in, now); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "activity.create", EntityType: "internal_activities", EntityID: id, After: snapshot(in, ticketID)})
	})
	if err != nil {
		if mapCheck(err, errs) {
			return 0, form, errs, nil
		}
		return 0, form, errs, err
	}
	if len(errs) > 0 {
		return 0, form, errs, nil
	}
	return id, form, nil, nil
}

// Update mengubah kegiatan (koordinator, pembuat, atau pelaksana utama).
func (s *Service) Update(ctx context.Context, u *rbac.User, id int64, get func(string) string, getAll func(string) []string) (map[string]string, map[string]string, error) {
	now := s.clock.Now()
	in, form, errs := Parse(get, getAll, now)
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT id FROM internal_activities WHERE id = ? FOR UPDATE`, id); err != nil {
			return err
		}
		before, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		if !CanEdit(before, u) {
			return ErrForbidden
		}
		if !u.Has(rbac.PermActivityAll) || in.LeadID == 0 {
			in.LeadID = before.LeadID.Int64 // hanya koordinator yang boleh mengganti pelaksana utama
			if in.LeadID == 0 {
				in.LeadID = u.ID
			}
		}
		ticketID, err := s.checkRefs(ctx, tx, &in, errs, &before)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE internal_activities SET activity_date = ?, activity_type_id = ?, it_category_id = ?, department_id = ?, room_id = ?,
			       asset_id = ?, related_ticket_id = ?, title = ?, description = ?, result = ?, status = ?, started_at = ?, ended_at = ?, updated_at = ?
			 WHERE id = ?`,
			in.Date.Format("2006-01-02"), in.TypeID, in.ITCategoryID, database.NullInt64(in.DepartmentID), database.NullInt64(in.RoomID),
			database.NullInt64(in.AssetID), database.NullInt64(ticketID), in.Title, database.NullString(in.Description),
			database.NullString(in.Result), in.Status, timeArg(in.StartedAt), timeArg(in.EndedAt), now, id); err != nil {
			return err
		}
		if err := s.saveMembers(ctx, tx, id, in, now); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "activity.update", EntityType: "internal_activities", EntityID: id,
			Before: map[string]any{"title": before.Title, "status": before.Status, "result": before.Result.String}, After: snapshot(in, ticketID)})
	})
	if err != nil {
		if mapCheck(err, errs) {
			return form, errs, nil
		}
		return form, errs, err
	}
	if len(errs) > 0 {
		return form, errs, nil
	}
	return form, nil, nil
}

// AddPhotos menambahkan lampiran foto kegiatan.
func (s *Service) AddPhotos(ctx context.Context, u *rbac.User, id int64, photos []attachments.Processed) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !CanView(a, u) {
		return ErrForbidden
	}
	now := s.clock.Now()
	var keys []string
	for _, p := range photos {
		k, err := s.store.Save(p.Data, now)
		if err != nil {
			for _, k := range keys {
				s.store.Remove(k)
			}
			return err
		}
		keys = append(keys, k)
	}
	err = database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		for i, p := range photos {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO attachments (activity_id, uploaded_by_type, uploaded_by_user_id, storage_key, mime_type, size_bytes,
				                         width, height, sha256, visible_to_reporter, created_at)
				VALUES (?, 'USER', ?, ?, ?, ?, ?, ?, ?, FALSE, ?)`,
				id, u.ID, keys[i], p.Mime, len(p.Data), p.Width, p.Height, p.SHA256[:], now); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "activity.photo_add", EntityType: "internal_activities", EntityID: id,
			After: map[string]int{"count": len(photos)}})
	})
	if err != nil {
		for _, k := range keys {
			s.store.Remove(k)
		}
	}
	return err
}

// Attachments mengembalikan lampiran kegiatan.
func (s *Service) Attachments(ctx context.Context, id int64) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, created_at FROM attachments WHERE activity_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttachmentKey mengembalikan kunci file lampiran beserta ID kegiatannya.
func (s *Service) AttachmentKey(ctx context.Context, attID int64) (string, int64, error) {
	var key string
	var aid int64
	err := s.db.QueryRowContext(ctx, `SELECT storage_key, activity_id FROM attachments WHERE id = ? AND activity_id IS NOT NULL`, attID).Scan(&key, &aid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return key, aid, err
}

// Store mengembalikan penyimpanan file.
func (s *Service) Store() attachments.Store { return s.store }

func (s *Service) options(ctx context.Context, q string, args ...any) ([]Option, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Option
	for rows.Next() {
		var o Option
		var extra sql.NullInt64
		if err := rows.Scan(&o.ID, &o.Label, &extra); err != nil {
			return nil, err
		}
		o.Extra = extra.Int64
		out = append(out, o)
	}
	return out, rows.Err()
}

// Types mengembalikan jenis kegiatan aktif (Extra = bidang bawaan).
func (s *Service) Types(ctx context.Context, include int64) ([]Option, error) {
	return s.options(ctx, `SELECT id, name, it_category_id FROM activity_types WHERE is_active = TRUE OR id = ? ORDER BY sort_order, name`, include)
}

// ITCategories mengembalikan bidang aktif.
func (s *Service) ITCategories(ctx context.Context, include int64) ([]Option, error) {
	return s.options(ctx, `SELECT id, name, NULL FROM it_categories WHERE is_active = TRUE OR id = ? ORDER BY sort_order, name`, include)
}

// Departments mengembalikan unit aktif.
func (s *Service) Departments(ctx context.Context, include int64) ([]Option, error) {
	return s.options(ctx, `SELECT id, name, NULL FROM departments WHERE is_active = TRUE OR id = ? ORDER BY name`, include)
}

// Rooms mengembalikan ruangan aktif suatu unit.
func (s *Service) Rooms(ctx context.Context, dept, include int64) ([]Option, error) {
	if dept == 0 {
		return nil, nil
	}
	return s.options(ctx, `SELECT id, name, NULL FROM rooms WHERE department_id = ? AND (is_active = TRUE OR id = ?) ORDER BY name`, dept, include)
}

// Assets mengembalikan perangkat di ruangan/unit (atau perangkat terpilih).
func (s *Service) Assets(ctx context.Context, dept, room, include int64) ([]Option, error) {
	switch {
	case room != 0:
		return s.options(ctx, `SELECT id, CONCAT(name, ' (', asset_code, ')'), NULL FROM assets WHERE (room_id = ? AND status <> 'DISPOSED') OR id = ? ORDER BY name`, room, include)
	case dept != 0:
		return s.options(ctx, `SELECT id, CONCAT(name, ' (', asset_code, ')'), NULL FROM assets WHERE (department_id = ? AND status <> 'DISPOSED') OR id = ? ORDER BY name LIMIT 300`, dept, include)
	case include != 0:
		return s.options(ctx, `SELECT id, CONCAT(name, ' (', asset_code, ')'), NULL FROM assets WHERE id = ?`, include)
	}
	return nil, nil
}

// Staff mengembalikan petugas & koordinator aktif.
func (s *Service) Staff(ctx context.Context) ([]Option, error) {
	return s.options(ctx, `SELECT u.id, u.full_name, NULL FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.is_active = TRUE AND r.code IN ('TECHNICIAN','COORDINATOR') ORDER BY u.full_name`)
}

// MonthCount menghitung kegiatan selesai bulan berjalan (WITA) untuk user (0 = semua).
func (s *Service) MonthCount(ctx context.Context, userID int64) (int, error) {
	now := s.clock.Now().In(clock.Location)
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.Location).Format("2006-01-02")
	q := `SELECT COUNT(*) FROM internal_activities a WHERE a.status = 'DONE' AND a.activity_date >= ?`
	args := []any{from}
	if userID != 0 {
		q += ` AND EXISTS (SELECT 1 FROM activity_members m WHERE m.activity_id = a.id AND m.user_id = ?)`
		args = append(args, userID)
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}
