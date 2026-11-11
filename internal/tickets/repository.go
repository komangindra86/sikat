package tickets

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"sikat/internal/database"
)

// ErrNotFound dikembalikan bila tiket tidak ada.
var ErrNotFound = errors.New("tiket tidak ditemukan")

type repository struct{}

const selectTicket = `
	SELECT t.id, t.ticket_no, t.channel, t.status, t.priority, t.priority_source, pr.name,
	       t.department_id, d.name, r.name, r.location_note, t.asset_id, a.asset_code, a.name,
	       t.reporter_name, t.reporter_phone, rd.name, tc.name, ic.name, t.description,
	       t.impact_level, t.affected_scope, t.replacement_available, t.critical_service, t.incident_started_at,
	       t.assignee_id, au.full_name, cu.full_name, t.first_response_at, t.created_at,
	       (SELECT COUNT(*) FROM attachments x WHERE x.ticket_id = t.id),
	       t.work_started_at, t.pending_since, prs.label, t.resolved_at, t.closed_at, t.service_restored_at,
	       t.resolution_action, t.resolution_result, t.resolution_note, t.reporter_feedback, t.cancel_reason, t.sla_cycle
	  FROM tickets t
	  JOIN ticket_categories tc ON tc.id = t.ticket_category_id
	  JOIN it_categories ic ON ic.id = t.it_category_id
	  LEFT JOIN priority_rules pr ON pr.id = t.priority_rule_id
	  LEFT JOIN departments d ON d.id = t.department_id
	  LEFT JOIN rooms r ON r.id = t.room_id
	  LEFT JOIN assets a ON a.id = t.asset_id
	  LEFT JOIN departments rd ON rd.id = t.reporter_department_id
	  LEFT JOIN users au ON au.id = t.assignee_id
	  LEFT JOIN users cu ON cu.id = t.created_by_user_id
	  LEFT JOIN pending_reasons prs ON prs.id = t.pending_reason_id`

func scanTicket(sc interface{ Scan(...any) error }) (Ticket, error) {
	var t Ticket
	err := sc.Scan(&t.ID, &t.TicketNo, &t.Channel, &t.Status, &t.Priority, &t.PrioritySource, &t.PriorityRuleName,
		&t.DepartmentID, &t.DepartmentName, &t.RoomName, &t.RoomLocation, &t.AssetID, &t.AssetCode, &t.AssetName,
		&t.ReporterName, &t.ReporterPhone, &t.ReporterDeptName, &t.CategoryName, &t.ITCategoryName, &t.Description,
		&t.Impact, &t.Scope, &t.Replacement, &t.CriticalService, &t.IncidentStartedAt,
		&t.AssigneeID, &t.AssigneeName, &t.CreatedByName, &t.FirstResponseAt, &t.CreatedAt, &t.AttachmentCount,
		&t.WorkStartedAt, &t.PendingSince, &t.PendingReason, &t.ResolvedAt, &t.ClosedAt, &t.ServiceRestoredAt,
		&t.ResolutionAction, &t.ResolutionResult, &t.ResolutionNote, &t.ReporterFeedback, &t.CancelReason, &t.SLACycle)
	return t, err
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func (repository) get(ctx context.Context, db database.DBTX, id int64) (Ticket, error) {
	t, err := scanTicket(db.QueryRowContext(ctx, selectTicket+" WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	return t, err
}

func (repository) getByToken(ctx context.Context, db database.DBTX, token string) (Ticket, error) {
	t, err := scanTicket(db.QueryRowContext(ctx, selectTicket+" WHERE t.public_token_hash = ?", hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	return t, err
}

func (repository) histories(ctx context.Context, db database.DBTX, ticketID int64) ([]History, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT h.id, h.event_type, h.from_status, h.to_status, h.from_priority, h.to_priority, h.meta,
		       h.actor_type, u.full_name, h.note, h.created_at
		  FROM ticket_histories h LEFT JOIN users u ON u.id = h.actor_user_id
		 WHERE h.ticket_id = ? ORDER BY h.created_at DESC, h.id DESC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []History
	for rows.Next() {
		var h History
		if err := rows.Scan(&h.ID, &h.EventType, &h.FromStatus, &h.ToStatus, &h.FromPrio, &h.ToPrio, &h.Meta,
			&h.ActorType, &h.ActorName, &h.Note, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (repository) attachments(ctx context.Context, db database.DBTX, ticketID int64, reporterOnly bool) ([]Attachment, error) {
	q := `SELECT id, width, height, size_bytes, uploaded_by_type, created_at FROM attachments WHERE ticket_id = ?`
	if reporterOnly {
		q += " AND visible_to_reporter = TRUE"
	}
	rows, err := db.QueryContext(ctx, q+" ORDER BY id", ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.Width, &a.Height, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type categoryInfo struct {
	itCategoryID    int64
	defaultPriority string
}

func (repository) category(ctx context.Context, db database.DBTX, id int64) (categoryInfo, bool, error) {
	var c categoryInfo
	err := db.QueryRowContext(ctx, `SELECT it_category_id, default_priority FROM ticket_categories WHERE id = ? AND is_active = TRUE`, id).
		Scan(&c.itCategoryID, &c.defaultPriority)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

func (repository) departmentActive(ctx context.Context, db database.DBTX, id int64) (bool, bool, error) {
	var critical bool
	err := db.QueryRowContext(ctx, `SELECT is_critical_service FROM departments WHERE id = ? AND is_active = TRUE`, id).Scan(&critical)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return critical, err == nil, err
}

// room mengembalikan unit pemilik dan flag kritis efektif (ruangan → unit).
func (repository) room(ctx context.Context, db database.DBTX, id int64) (deptID int64, critical bool, ok bool, err error) {
	err = db.QueryRowContext(ctx, `
		SELECT r.department_id, COALESCE(r.is_critical_service, d.is_critical_service)
		  FROM rooms r JOIN departments d ON d.id = r.department_id
		 WHERE r.id = ? AND r.is_active = TRUE`, id).Scan(&deptID, &critical)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, false, nil
	}
	return deptID, critical, err == nil, err
}

func (repository) asset(ctx context.Context, db database.DBTX, id int64) (roomID, deptID sql.NullInt64, ok bool, err error) {
	err = db.QueryRowContext(ctx, `SELECT room_id, department_id FROM assets WHERE id = ? AND status <> 'DISPOSED'`, id).Scan(&roomID, &deptID)
	if errors.Is(err, sql.ErrNoRows) {
		return roomID, deptID, false, nil
	}
	return roomID, deptID, err == nil, err
}

type insertTicket struct {
	Draft
	Number         string
	TokenHash      []byte
	ITCategoryID   int64
	Critical       bool
	Priority       string
	PriorityRuleID int64
}

func (repository) insert(ctx context.Context, tx *sql.Tx, t insertTicket, now time.Time) (int64, error) {
	var incident any
	if t.IncidentStartedAt != nil {
		incident = *t.IncidentStartedAt
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO tickets (ticket_no, public_token_hash, channel, created_by_user_id, qr_code_id,
		                     department_id, room_id, asset_id, reporter_name, reporter_department_id, reporter_phone,
		                     ticket_category_id, it_category_id, description, impact_level, affected_scope,
		                     replacement_available, critical_service, incident_started_at,
		                     priority, priority_source, priority_rule_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'AUTO', ?, 'NEW', ?, ?)`,
		t.Number, t.TokenHash, t.Channel, database.NullInt64(t.CreatedBy), database.NullInt64(t.QRCodeID),
		database.NullInt64(t.DepartmentID), database.NullInt64(t.RoomID), database.NullInt64(t.AssetID),
		t.ReporterName, database.NullInt64(t.ReporterDepartmentID), database.NullString(t.ReporterPhone),
		t.TicketCategoryID, t.ITCategoryID, t.Description, t.Impact, t.Scope,
		t.Replacement, t.Critical, incident, t.Priority, database.NullInt64(t.PriorityRuleID), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

type historyEntry struct {
	TicketID   int64
	Event      string
	FromStatus string
	ToStatus   string
	ActorType  string
	ActorID    int64
	Note       string
	Meta       string
}

func (repository) addHistory(ctx context.Context, tx *sql.Tx, h historyEntry, now time.Time) error {
	var meta any
	if h.Meta != "" {
		meta = h.Meta
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ticket_histories (ticket_id, event_type, from_status, to_status, actor_type, actor_user_id, note, meta, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.TicketID, h.Event, database.NullString(h.FromStatus), database.NullString(h.ToStatus), h.ActorType,
		database.NullInt64(h.ActorID), database.NullString(h.Note), meta, now)
	return err
}

type attachmentRow struct {
	Key, Mime     string
	Size          int
	Width, Height int
	SHA           [32]byte
	ByUser        int64
	OriginalName  string
}

func (repository) addAttachment(ctx context.Context, tx *sql.Tx, ticketID int64, a attachmentRow, now time.Time) error {
	by := "REPORTER"
	if a.ByUser != 0 {
		by = "USER"
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO attachments (ticket_id, uploaded_by_type, uploaded_by_user_id, storage_key, original_name, mime_type,
		                         size_bytes, width, height, sha256, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticketID, by, database.NullInt64(a.ByUser), a.Key, database.NullString(truncate(a.OriginalName, 255)), a.Mime,
		a.Size, a.Width, a.Height, a.SHA[:], now)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (repository) queue(ctx context.Context, db database.DBTX, f QueueFilter) ([]Ticket, int, error) {
	where := []string{}
	var args []any
	switch f.Mode {
	case "mine":
		where = append(where, "t.assignee_id = ?", "t.status IN ('CLAIMED','IN_PROGRESS','PENDING')")
		args = append(args, f.UserID)
	case "active":
		where = append(where, "t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING')")
	default:
		where = append(where, "t.status = 'NEW'")
	}
	if f.OnlyCompetency {
		where = append(where, "t.it_category_id IN (SELECT it_category_id FROM user_competencies WHERE user_id = ?)")
		args = append(args, f.UserID)
	}
	if f.Priority != "" {
		where = append(where, "t.priority = ?")
		args = append(args, f.Priority)
	}
	if f.DepartmentID != 0 {
		where = append(where, "t.department_id = ?")
		args = append(args, f.DepartmentID)
	}
	w := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tickets t"+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx, selectTicket+w+
		" ORDER BY FIELD(t.priority,'CRITICAL','HIGH','MEDIUM','LOW'), t.created_at, t.id LIMIT ? OFFSET ?",
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func (repository) workload(ctx context.Context, db database.DBTX) ([]Workload, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT u.id, u.full_name, r.name,
		       COALESCE(SUM(a.role = 'PRIMARY'), 0), COALESCE(SUM(a.role = 'SUPPORT'), 0)
		  FROM users u
		  JOIN roles r ON r.id = u.role_id AND r.code IN ('TECHNICIAN','COORDINATOR')
		  LEFT JOIN ticket_assignments a ON a.user_id = u.id AND a.released_at IS NULL
		       AND a.ticket_id IN (SELECT id FROM tickets WHERE status IN ('CLAIMED','IN_PROGRESS','PENDING'))
		 WHERE u.is_active = TRUE
		 GROUP BY u.id, u.full_name, r.name
		 ORDER BY COALESCE(SUM(a.role = 'PRIMARY'), 0) DESC, u.full_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workload
	for rows.Next() {
		var w Workload
		if err := rows.Scan(&w.UserID, &w.Name, &w.Role, &w.Primary, &w.Support); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (repository) options(ctx context.Context, db database.DBTX, q string, args ...any) ([]Option, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Option
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Label); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (repository) categories(ctx context.Context, db database.DBTX) ([]Category, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, help_text, it_category_id FROM ticket_categories WHERE is_active = TRUE ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.HelpText, &c.ITCategoryID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (repository) attachmentKey(ctx context.Context, db database.DBTX, q string, args ...any) (string, error) {
	var key string
	err := db.QueryRowContext(ctx, q, args...).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return key, err
}

func formatNumber(year int, n int64) string { return fmt.Sprintf("TKT-%04d-%06d", year, n) }
