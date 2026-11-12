package sla

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"sikat/internal/clock"
	"sikat/internal/database"
	"sikat/internal/notifications"
)

// Jenis eskalasi (kolom sla_escalations.kind).
const (
	KindCritical          = "CRITICAL_IMMEDIATE"
	KindResponseWarning   = "RESPONSE_WARNING"
	KindResponseBreach    = "RESPONSE_BREACH"
	KindResolutionWarning = "RESOLUTION_WARNING"
	KindResolutionBreach  = "RESOLUTION_BREACH"
)

// Jenis kejadian histori tiket yang ditulis modul SLA.
const (
	EventSLACritical = "SLA_CRITICAL"
	EventSLAWarning  = "SLA_WARNING"
	EventSLABreach   = "SLA_BREACH"
)

// Escalation adalah satu kejadian SLA pada tiket.
type Escalation struct {
	TicketID int64
	Kind     string
	Cycle    int
	Level    int // tickets.sla_revision
	Priority string
	DueAt    time.Time
	UsedPct  int
	// Notify=false: pelanggaran baru diketahui saat tiket diambil/diselesaikan,
	// dicatat untuk laporan tanpa memicu notifikasi.
	Notify bool
}

func (e Escalation) event() (string, string) {
	due := ""
	if !e.DueAt.IsZero() {
		due = " (batas " + e.DueAt.In(clock.Location).Format("02/01 15:04") + " WITA)"
	}
	switch e.Kind {
	case KindCritical:
		return EventSLACritical, "Tiket kritis: eskalasi segera ke koordinator."
	case KindResponseWarning:
		return EventSLAWarning, fmt.Sprintf("Target respons hampir habis: %d%% terpakai%s.", e.UsedPct, due)
	case KindResponseBreach:
		return EventSLABreach, "Target respons terlewati" + due + "."
	case KindResolutionWarning:
		return EventSLAWarning, fmt.Sprintf("Target penyelesaian hampir habis: %d%% terpakai%s.", e.UsedPct, due)
	default:
		return EventSLABreach, "Target penyelesaian terlewati" + due + "."
	}
}

// Record mencatat kejadian SLA secara idempoten. Hanya kejadian baru (belum
// pernah tercatat untuk tiket, jenis, siklus, dan revisi yang sama) yang
// menghasilkan histori tiket; mengembalikan true bila kejadian baru.
func Record(ctx context.Context, tx database.DBTX, e Escalation, now time.Time) (bool, error) {
	if e.Level < 1 {
		e.Level = 1
	}
	var due any
	if !e.DueAt.IsZero() {
		due = e.DueAt
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO sla_escalations (ticket_id, kind, cycle, level, priority, due_at, used_pct, notify, triggered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = id`,
		e.TicketID, e.Kind, e.Cycle, e.Level, e.Priority, due, e.UsedPct, e.Notify, now)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false, nil
	}
	escID, err := res.LastInsertId()
	if err != nil {
		return false, err
	}
	ev, note := e.event()
	meta, _ := json.Marshal(map[string]any{"kind": e.Kind, "cycle": e.Cycle, "used_pct": e.UsedPct})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ticket_histories (ticket_id, event_type, actor_type, note, meta, created_at)
		VALUES (?, ?, 'SYSTEM', ?, ?, ?)`, e.TicketID, ev, note, string(meta), now); err != nil {
		return false, err
	}
	var col string
	switch e.Kind {
	case KindResponseBreach:
		col = "response_breached_at"
	case KindResolutionBreach:
		col = "resolution_breached_at"
	}
	if e.Notify {
		if err := publish(ctx, tx, escID, e, now); err != nil {
			return false, err
		}
	}
	if col != "" {
		at := now
		if !e.DueAt.IsZero() && e.DueAt.Before(now) {
			at = e.DueAt
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tickets SET `+col+` = COALESCE(`+col+`, ?) WHERE id = ?`, at, e.TicketID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// TicketRow adalah tiket beserta hasil evaluasi SLA-nya.
type TicketRow struct {
	ID              int64
	TicketNo        string
	Status          string
	Priority        string
	Category        string
	Department      sql.NullString
	Room            sql.NullString
	AssigneeName    sql.NullString
	CalendarID      sql.NullInt64
	CalendarName    string
	PolicyName      sql.NullString
	Cycle           int
	Revision        int
	ResponseDueAt   sql.NullTime
	ResolutionDueAt sql.NullTime
	Snap            Snapshot
	Resp, Reso      Measure
}

// Location menggabungkan unit dan ruangan.
func (t TicketRow) Location() string {
	switch {
	case t.Department.Valid && t.Room.Valid:
		return t.Department.String + " · " + t.Room.String
	case t.Department.Valid:
		return t.Department.String
	case t.Room.Valid:
		return t.Room.String
	}
	return "–"
}

// Current adalah target yang sedang relevan (respons bila belum diambil).
func (t TicketRow) Current() Measure { m, _ := Current(t.Resp, t.Reso); return m }

// CurrentKind: "RESPONSE" atau "RESOLUTION".
func (t TicketRow) CurrentKind() string { _, k := Current(t.Resp, t.Reso); return k }

const ticketRowSelect = `
	SELECT t.id, t.ticket_no, t.status, t.priority, tc.name, d.name, r.name, au.full_name,
	       t.created_at, t.sla_cycle_started_at, t.first_response_at, t.resolved_at,
	       t.sla_response_minutes, t.sla_resolution_minutes, t.sla_warn_pct, t.sla_paused_since, t.sla_paused_minutes,
	       t.sla_calendar_id, sp.name, t.sla_cycle, t.sla_revision, t.response_due_at, t.resolution_due_at
	  FROM tickets t
	  JOIN ticket_categories tc ON tc.id = t.ticket_category_id
	  LEFT JOIN departments d ON d.id = t.department_id
	  LEFT JOIN rooms r ON r.id = t.room_id
	  LEFT JOIN users au ON au.id = t.assignee_id
	  LEFT JOIN sla_policies sp ON sp.id = t.sla_policy_id `

func loadTicketRows(ctx context.Context, q database.DBTX, cals map[int64]*Calendar, now time.Time, where string, args ...any) ([]TicketRow, error) {
	rows, err := q.QueryContext(ctx, ticketRowSelect+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketRow
	for rows.Next() {
		var t TicketRow
		var warn sql.NullInt64
		s := &t.Snap
		if err := rows.Scan(&t.ID, &t.TicketNo, &t.Status, &t.Priority, &t.Category, &t.Department, &t.Room, &t.AssigneeName,
			&s.CreatedAt, &s.CycleStartedAt, &s.FirstResponseAt, &s.ResolvedAt,
			&s.ResponseMinutes, &s.ResolutionMinutes, &warn, &s.PausedSince, &s.PausedMinutes,
			&t.CalendarID, &t.PolicyName, &t.Cycle, &t.Revision, &t.ResponseDueAt, &t.ResolutionDueAt); err != nil {
			return nil, err
		}
		s.Status = t.Status
		s.WarnPct = int(warn.Int64)
		cal := cals[t.CalendarID.Int64]
		if cal != nil {
			t.CalendarName = cal.Name
		}
		t.Resp, t.Reso = Evaluate(*s, cal, now)
		out = append(out, t)
	}
	return out, rows.Err()
}

const activeWhere = `WHERE t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING') AND t.sla_policy_id IS NOT NULL `

// ScanResult merangkum satu putaran pemeriksaan SLA.
type ScanResult struct {
	Checked   int
	Escalated int
}

type escKey struct {
	ticket       int64
	kind         string
	cycle, level int
}

// Scan memeriksa seluruh tiket aktif: melengkapi batas waktu yang kosong dan
// mencatat peringatan/pelanggaran baru. Aman dijalankan berulang (idempoten).
func (s *Service) Scan(ctx context.Context) (ScanResult, error) {
	var res ScanResult
	now := s.clock.Now()
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return res, err
	}
	list, err := loadTicketRows(ctx, s.db, cals, now, activeWhere)
	if err != nil {
		return res, err
	}
	res.Checked = len(list)
	if len(list) == 0 {
		return res, nil
	}

	seen := map[escKey]bool{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.ticket_id, e.kind, e.cycle, e.level FROM sla_escalations e
		  JOIN tickets t ON t.id = e.ticket_id `+activeWhere)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var k escKey
		if err := rows.Scan(&k.ticket, &k.kind, &k.cycle, &k.level); err != nil {
			rows.Close()
			return res, err
		}
		seen[k] = true
	}
	rows.Close()

	for _, t := range list {
		// Tiket lama (sebelum PHASE 5) atau hasil migrasi: lengkapi batas waktu.
		if (!t.ResponseDueAt.Valid && t.Resp.Defined) || (!t.ResolutionDueAt.Valid && !t.Reso.DueAt.IsZero()) {
			var rd, sd any
			if t.Resp.Defined {
				rd = t.Resp.DueAt
			}
			if !t.Reso.DueAt.IsZero() {
				sd = t.Reso.DueAt
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE tickets SET response_due_at = COALESCE(response_due_at, ?),
				resolution_due_at = COALESCE(resolution_due_at, ?), sla_cycle_started_at = COALESCE(sla_cycle_started_at, created_at)
				WHERE id = ?`, rd, sd, t.ID); err != nil {
				return res, err
			}
		}

		for _, e := range candidates(t) {
			if seen[escKey{e.TicketID, e.Kind, e.Cycle, e.Level}] {
				continue
			}
			var created bool
			err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
				var err error
				created, err = Record(ctx, tx, e, now)
				return err
			})
			if err != nil {
				return res, err
			}
			if created {
				res.Escalated++
			}
		}
	}
	return res, nil
}

// candidates menentukan kejadian SLA yang berlaku untuk tiket saat ini.
func candidates(t TicketRow) []Escalation {
	base := Escalation{TicketID: t.ID, Cycle: t.Cycle, Level: t.Revision, Priority: t.Priority, Notify: true}
	var out []Escalation
	if t.Priority == "CRITICAL" {
		e := base
		e.Kind = KindCritical
		out = append(out, e)
	}
	add := func(m Measure, warn, breach string) {
		if !m.Defined || !m.Running {
			return
		}
		e := base
		e.DueAt, e.UsedPct = m.DueAt, m.Pct
		switch {
		case m.Late():
			e.Kind = breach
		case m.State == StateWarning:
			e.Kind = warn
		default:
			return
		}
		out = append(out, e)
	}
	add(t.Resp, KindResponseWarning, KindResponseBreach)
	add(t.Reso, KindResolutionWarning, KindResolutionBreach)
	return out
}

// publish menulis event notifikasi untuk eskalasi baru (dalam transaksi yang
// sama) dan menautkannya ke baris sla_escalations.
func publish(ctx context.Context, tx database.DBTX, escID int64, e Escalation, now time.Time) error {
	evType := notifications.SLABreach
	switch e.Kind {
	case KindCritical:
		evType = notifications.TicketCritical
	case KindResponseWarning, KindResolutionWarning:
		evType = notifications.SLAWarning
	}
	payload := map[string]any{"kind": e.Kind, "cycle": e.Cycle, "used_pct": e.UsedPct}
	if !e.DueAt.IsZero() {
		payload["due_at"] = e.DueAt.UTC().Format(time.RFC3339Nano)
	}
	id, err := notifications.Publish(ctx, tx, notifications.Event{
		Type: evType, DedupeKey: fmt.Sprintf("sla:%d", escID), TicketID: e.TicketID, Payload: payload,
	}, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE sla_escalations SET outbox_event_id = ? WHERE id = ?`, id, escID)
	return err
}
