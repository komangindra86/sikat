package tickets

import (
	"context"
	"database/sql"
	"time"

	"sikat/internal/sla"
)

// Integrasi SLA (PHASE 5). Nilai kebijakan disalin ke tiket saat dibuat dan saat
// prioritas berubah; batas waktu dihitung ulang saat jeda berakhir dan saat
// tiket dibuka kembali (siklus baru).

// assignSLA memilih kebijakan untuk tiket baru dan menyalin nilainya.
// Prioritas kritis langsung dicatat sebagai eskalasi.
func (s *Service) assignSLA(ctx context.Context, tx *sql.Tx, id int64, m sla.Match, now time.Time) error {
	a, err := sla.Assign(ctx, tx, m, now, now, 0)
	if err != nil {
		return err
	}
	sets, args := a.Columns()
	args = append(args, now, id)
	if _, err := tx.ExecContext(ctx, `UPDATE tickets SET `+sets+`, sla_cycle_started_at = ? WHERE id = ?`, args...); err != nil {
		return err
	}
	if m.Priority == "CRITICAL" {
		_, err = sla.Record(ctx, tx, sla.Escalation{TicketID: id, Kind: sla.KindCritical, Cycle: 1, Level: 1,
			Priority: m.Priority, Notify: true}, now)
	}
	return err
}

// unpauseSLA mengakumulasi jeda dan menggeser batas waktu penyelesaian.
func (s *Service) unpauseSLA(ctx context.Context, tx *sql.Tx, t lockedTicket, now time.Time) (string, []any, error) {
	if !t.SLACalendarID.Valid || !t.SLAResolutionMinutes.Valid {
		return "sla_paused_since = NULL", nil, nil
	}
	total, due, err := sla.Unpause(ctx, tx, t.SLACalendarID.Int64, t.SLAPausedSince.Time, t.SLAPausedMinutes,
		t.cycleStart(), int(t.SLAResolutionMinutes.Int64), now)
	if err != nil {
		return "", nil, err
	}
	return "sla_paused_since = NULL, sla_paused_minutes = ?, resolution_due_at = ?", []any{total, due}, nil
}

func (t lockedTicket) cycleStart() time.Time {
	if t.SLACycleStartedAt.Valid {
		return t.SLACycleStartedAt.Time
	}
	return t.CreatedAt
}

// reopenSLA memulai siklus penyelesaian baru sejak tiket dibuka kembali.
func (s *Service) reopenSLA(ctx context.Context, tx *sql.Tx, t lockedTicket, now time.Time) (string, []any, error) {
	sets := "sla_cycle_started_at = ?, sla_paused_minutes = 0, sla_paused_since = NULL, resolution_breached_at = NULL, resolution_due_at = ?"
	if !t.SLACalendarID.Valid || !t.SLAResolutionMinutes.Valid {
		return sets, []any{now, nil}, nil
	}
	due, err := sla.DueFrom(ctx, tx, t.SLACalendarID.Int64, now, int(t.SLAResolutionMinutes.Int64))
	if err != nil {
		return "", nil, err
	}
	return sets, []any{now, due}, nil
}

// reassignSLA menerapkan kebijakan untuk prioritas baru. Batas waktu dihitung
// ulang dari awal (respons: sejak dibuat; penyelesaian: sejak awal siklus).
// Revisi naik agar peringatan untuk target baru tetap tercatat.
func (s *Service) reassignSLA(ctx context.Context, tx *sql.Tx, t lockedTicket, newPriority string, firstResponded bool, now time.Time) error {
	paused := t.SLAPausedMinutes
	if t.SLAPausedSince.Valid && t.SLACalendarID.Valid {
		total, _, err := sla.Unpause(ctx, tx, t.SLACalendarID.Int64, t.SLAPausedSince.Time, t.SLAPausedMinutes,
			t.cycleStart(), int(t.SLAResolutionMinutes.Int64), now)
		if err != nil {
			return err
		}
		paused = total
	}
	a, err := sla.Assign(ctx, tx, sla.Match{Priority: newPriority, TicketCategoryID: t.TicketCategoryID,
		ITCategoryID: t.ITCategoryID, DepartmentID: t.DepartmentID.Int64}, t.CreatedAt, t.cycleStart(), paused)
	if err != nil {
		return err
	}
	sets, args := a.Columns()
	var pausedSince any
	if t.SLAPausedSince.Valid && a.Found && a.Policy.AllowPause {
		pausedSince = now // tetap dijeda di bawah kebijakan baru
	}
	sets += ", sla_paused_minutes = ?, sla_paused_since = ?, sla_revision = sla_revision + 1"
	args = append(args, paused, pausedSince)
	if a.Found && !firstResponded && a.ResponseDue.After(now) {
		sets += ", response_breached_at = NULL"
	}
	if a.Found && a.ResolutionDue.After(now) {
		sets += ", resolution_breached_at = NULL"
	}
	args = append(args, t.ID)
	if _, err := tx.ExecContext(ctx, `UPDATE tickets SET `+sets+` WHERE id = ?`, args...); err != nil {
		return err
	}
	if newPriority == "CRITICAL" {
		_, err = sla.Record(ctx, tx, sla.Escalation{TicketID: t.ID, Kind: sla.KindCritical, Cycle: t.SLACycle,
			Level: t.SLARevision + 1, Priority: newPriority, Notify: true}, now)
	}
	return err
}

// recordLateBreach mencatat pelanggaran yang baru diketahui saat transisi
// (diambil/diselesaikan setelah batas waktu) untuk laporan, tanpa notifikasi.
func recordLateBreach(ctx context.Context, tx *sql.Tx, ticketID int64, kind string, due sql.NullTime,
	cycle, revision int, priority string, now time.Time) error {
	if !due.Valid || !now.After(due.Time) {
		return nil
	}
	_, err := sla.Record(ctx, tx, sla.Escalation{TicketID: ticketID, Kind: kind, Cycle: cycle, Level: revision,
		Priority: priority, DueAt: due.Time, Notify: false}, now)
	return err
}

// attachSLA mengisi evaluasi SLA pada daftar tiket untuk tampilan.
func (s *Service) attachSLA(ctx context.Context, list []Ticket) error {
	ids := make([]int64, 0, len(list))
	for _, t := range list {
		ids = append(ids, t.ID)
	}
	m, err := s.sla.TicketSLA(ctx, ids)
	if err != nil {
		return err
	}
	for i := range list {
		if row, ok := m[list[i].ID]; ok {
			r := row
			list[i].SLA = &r
		}
	}
	return nil
}
