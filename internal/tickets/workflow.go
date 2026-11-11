package tickets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"sikat/internal/clock"
	"sikat/internal/database"
	"sikat/internal/priority"
	"sikat/internal/rbac"
	"sikat/internal/workflows"
)

// Error alur kerja yang ditampilkan ke pengguna.
var (
	ErrForbidden  = errors.New("Anda tidak berhak melakukan tindakan ini pada tiket ini.")
	ErrTransition = errors.New("Tindakan ini tidak dapat dilakukan pada status tiket saat ini. Muat ulang halaman untuk melihat status terbaru.")
)

// Jenis kejadian histori tambahan (PHASE 3).
const (
	EventStarted         = "STARTED"
	EventPending         = "PENDING"
	EventResumed         = "RESUMED"
	EventResolved        = "RESOLVED"
	EventReleased        = "RELEASED"
	EventCancelled       = "CANCELLED"
	EventClosed          = "CLOSED"
	EventAutoClosed      = "AUTO_CLOSED"
	EventReopened        = "REOPENED"
	EventConfirmed       = "REPORTER_CONFIRMED"
	EventTakenOver       = "TAKEN_OVER"
	EventSupportAdded    = "SUPPORT_ADDED"
	EventSupportRemoved  = "SUPPORT_REMOVED"
	EventPriorityChanged = "PRIORITY_CHANGED"
	EventPhotoAdded      = "PHOTO_ADDED"
)

// Hasil penyelesaian.
var resolutionResults = map[string]bool{
	"FIXED": true, "WORKAROUND": true, "REPLACED": true, "VENDOR": true, "NOT_REPRODUCIBLE": true, "CANNOT_FIX": true,
}

type lockedTicket struct {
	ID         int64
	Status     string
	AssigneeID sql.NullInt64
	Priority   string
	SLACycle   int
	CreatedAt  time.Time
}

func lockTicket(ctx context.Context, tx *sql.Tx, id int64) (lockedTicket, error) {
	var t lockedTicket
	err := tx.QueryRowContext(ctx, `SELECT id, status, assignee_id, priority, sla_cycle, created_at FROM tickets WHERE id = ? FOR UPDATE`, id).
		Scan(&t.ID, &t.Status, &t.AssigneeID, &t.Priority, &t.SLACycle, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func lockTicketByToken(ctx context.Context, tx *sql.Tx, token string) (lockedTicket, error) {
	var t lockedTicket
	err := tx.QueryRowContext(ctx, `SELECT id, status, assignee_id, priority, sla_cycle, created_at FROM tickets WHERE public_token_hash = ? FOR UPDATE`,
		hashToken(token)).Scan(&t.ID, &t.Status, &t.AssigneeID, &t.Priority, &t.SLACycle, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (t lockedTicket) isPrimary(u *rbac.User) bool {
	return u != nil && t.AssigneeID.Valid && t.AssigneeID.Int64 == u.ID
}

func isSupport(ctx context.Context, tx *sql.Tx, ticketID, userID int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticket_assignments WHERE ticket_id = ? AND user_id = ? AND role = 'SUPPORT' AND released_at IS NULL`,
		ticketID, userID).Scan(&n)
	return n > 0, err
}

func cleanText(s string, max int, label string, required bool, errs map[string]string, key string) string {
	s = strings.TrimSpace(s)
	switch n := utf8.RuneCountInString(s); {
	case n == 0 && required:
		errs[key] = label + " wajib diisi."
	case n > max:
		errs[key] = label + " maksimal " + itoa(max) + " karakter."
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// transitionOpts menjelaskan satu perubahan status.
type transitionOpts struct {
	action    workflows.Action
	event     string
	actorType string
	actorID   int64
	note      string
	meta      map[string]any
	sets      string // kolom tambahan "a = ?, b = ?"
	args      []any
}

// applyTransition memvalidasi transisi pada tiket terkunci, memperbarui status,
// dan mencatat histori dalam transaksi yang sama.
func (s *Service) applyTransition(ctx context.Context, tx *sql.Tx, t lockedTicket, o transitionOpts, now time.Time) error {
	to, ok := workflows.Next(o.action, t.Status)
	if !ok {
		return ErrTransition
	}
	q := `UPDATE tickets SET status = ?, version = version + 1, updated_at = ?`
	args := []any{to, now}
	if o.sets != "" {
		q += ", " + o.sets
		args = append(args, o.args...)
	}
	q += ` WHERE id = ? AND status = ?`
	args = append(args, t.ID, t.Status)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrTransition
	}
	var meta string
	if len(o.meta) > 0 {
		b, _ := json.Marshal(o.meta)
		meta = string(b)
	}
	return s.repo.addHistory(ctx, tx, historyEntry{
		TicketID: t.ID, Event: o.event, FromStatus: t.Status, ToStatus: to,
		ActorType: o.actorType, ActorID: o.actorID, Note: o.note, Meta: meta,
	}, now)
}

// Start: penanggung jawab mulai mengerjakan tiket.
func (s *Service) Start(ctx context.Context, id int64, u *rbac.User) error {
	now := s.clock.Now()
	return database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) || !u.Has(rbac.PermTicketWork) {
			return ErrForbidden
		}
		return s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Start, event: EventStarted, actorType: "USER", actorID: u.ID,
			sets: "work_started_at = COALESCE(work_started_at, ?)", args: []any{now},
		}, now)
	})
}

// Pend menunda tiket dengan alasan wajib dari daftar alasan tunda.
func (s *Service) Pend(ctx context.Context, id int64, u *rbac.User, reasonID int64, note string) (map[string]string, error) {
	errs := map[string]string{}
	note = cleanText(note, 1000, "Keterangan", false, errs, "note")
	now := s.clock.Now()
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) || !u.Has(rbac.PermTicketWork) {
			return ErrForbidden
		}
		var code, label string
		err = tx.QueryRowContext(ctx, `SELECT code, label FROM pending_reasons WHERE id = ? AND is_active = TRUE`, reasonID).Scan(&code, &label)
		if errors.Is(err, sql.ErrNoRows) {
			errs["pending_reason_id"] = "Pilih alasan tiket ditunda."
		} else if err != nil {
			return err
		}
		if code == "OTHER" && note == "" {
			errs["note"] = "Jelaskan alasan lainnya."
		}
		if len(errs) > 0 {
			return nil
		}
		return s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Pend, event: EventPending, actorType: "USER", actorID: u.ID, note: note,
			meta: map[string]any{"pending_reason": label},
			sets: "pending_reason_id = ?, pending_since = ?", args: []any{reasonID, now},
		}, now)
	})
	if err != nil || len(errs) > 0 {
		return errs, err
	}
	return nil, nil
}

// Resume melanjutkan tiket yang tertunda.
func (s *Service) Resume(ctx context.Context, id int64, u *rbac.User) error {
	now := s.clock.Now()
	return database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) || !u.Has(rbac.PermTicketWork) {
			return ErrForbidden
		}
		return s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Resume, event: EventResumed, actorType: "USER", actorID: u.ID,
			sets: "pending_reason_id = NULL, pending_since = NULL, work_started_at = COALESCE(work_started_at, ?)", args: []any{now},
		}, now)
	})
}

// ResolveInput adalah isian form penyelesaian.
type ResolveInput struct {
	Action     string
	Result     string
	Note       string
	RestoredAt string // datetime-local WITA "2006-01-02T15:04"; kosong = sekarang
}

// Resolve menyelesaikan tiket. Tindakan dan hasil wajib diisi.
func (s *Service) Resolve(ctx context.Context, id int64, u *rbac.User, in ResolveInput) (map[string]string, error) {
	errs := map[string]string{}
	action := cleanText(in.Action, 4000, "Tindakan yang dilakukan", true, errs, "resolution_action")
	note := cleanText(in.Note, 2000, "Catatan", false, errs, "resolution_note")
	if !resolutionResults[in.Result] {
		errs["resolution_result"] = "Pilih hasil penyelesaian."
	}
	now := s.clock.Now()
	restored := now
	if strings.TrimSpace(in.RestoredAt) != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", strings.TrimSpace(in.RestoredAt), clock.Location)
		switch {
		case err != nil:
			errs["service_restored_at"] = "Format waktu tidak valid."
		case t.After(now.Add(time.Minute)):
			errs["service_restored_at"] = "Waktu layanan pulih tidak boleh di masa depan."
		default:
			restored = t.UTC()
		}
	}
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) || !u.Has(rbac.PermTicketWork) {
			return ErrForbidden
		}
		if _, ok := errs["service_restored_at"]; !ok && restored.Before(t.CreatedAt.Add(-time.Minute)) {
			errs["service_restored_at"] = "Waktu layanan pulih tidak boleh sebelum tiket dibuat."
		}
		if len(errs) > 0 {
			return nil
		}
		return s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Resolve, event: EventResolved, actorType: "USER", actorID: u.ID, note: note,
			meta: map[string]any{"result": in.Result},
			sets: `resolution_action = ?, resolution_result = ?, resolution_note = ?, resolved_at = ?,
			       service_restored_at = ?, pending_reason_id = NULL, pending_since = NULL`,
			args: []any{action, in.Result, database.NullString(note), now, restored},
		}, now)
	})
	if err != nil || len(errs) > 0 {
		return errs, err
	}
	return nil, nil
}

// Release mengembalikan tiket ke antrean (alasan wajib).
func (s *Service) Release(ctx context.Context, id int64, u *rbac.User, reason string) (map[string]string, error) {
	errs := map[string]string{}
	reason = cleanText(reason, 255, "Alasan", true, errs, "reason")
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	return nil, database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) || !u.Has(rbac.PermTicketWork) {
			return ErrForbidden
		}
		if err := s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Release, event: EventReleased, actorType: "USER", actorID: u.ID, note: reason,
			sets: "assignee_id = NULL",
		}, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_assignments SET released_at = ?, released_by = ?, release_reason = ?
			WHERE ticket_id = ? AND released_at IS NULL`, now, u.ID, reason, id)
		return err
	})
}

// Cancel membatalkan tiket (Koordinator/Admin, alasan wajib).
func (s *Service) Cancel(ctx context.Context, id int64, u *rbac.User, reason string) (map[string]string, error) {
	errs := map[string]string{}
	reason = cleanText(reason, 255, "Alasan pembatalan", true, errs, "reason")
	if len(errs) > 0 {
		return errs, nil
	}
	if !u.Has(rbac.PermTicketCancel) {
		return nil, ErrForbidden
	}
	now := s.clock.Now()
	return nil, database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Cancel, event: EventCancelled, actorType: "USER", actorID: u.ID, note: reason,
			sets: "cancelled_at = ?, cancel_reason = ?, pending_reason_id = NULL, pending_since = NULL", args: []any{now, reason},
		}, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_assignments SET released_at = ?, released_by = ?, release_reason = 'Tiket dibatalkan'
			WHERE ticket_id = ? AND released_at IS NULL`, now, u.ID, id)
		return err
	})
}

// Close menutup tiket yang sudah selesai (Koordinator).
func (s *Service) Close(ctx context.Context, id int64, u *rbac.User, note string) (map[string]string, error) {
	errs := map[string]string{}
	note = cleanText(note, 1000, "Catatan", false, errs, "note")
	if len(errs) > 0 {
		return errs, nil
	}
	if !u.Has(rbac.PermTicketTakeover) {
		return nil, ErrForbidden
	}
	now := s.clock.Now()
	return nil, database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		return s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Close, event: EventClosed, actorType: "USER", actorID: u.ID, note: note,
			sets: "closed_at = ?", args: []any{now},
		}, now)
	})
}

// reopenSets mengosongkan waktu selesai dan memulai siklus SLA baru.
const reopenSets = "resolved_at = NULL, service_restored_at = NULL, sla_cycle = sla_cycle + 1"

// Reopen membuka kembali tiket yang sudah selesai (Koordinator, alasan wajib).
func (s *Service) Reopen(ctx context.Context, id int64, u *rbac.User, reason string) (map[string]string, error) {
	errs := map[string]string{}
	reason = cleanText(reason, 1000, "Alasan", true, errs, "reason")
	if len(errs) > 0 {
		return errs, nil
	}
	if !u.Has(rbac.PermTicketTakeover) {
		return nil, ErrForbidden
	}
	now := s.clock.Now()
	return nil, database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		return s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Reopen, event: EventReopened, actorType: "USER", actorID: u.ID, note: reason,
			meta: map[string]any{"cycle": t.SLACycle + 1}, sets: reopenSets,
		}, now)
	})
}

// ReporterFeedback: pelapor mengonfirmasi penyelesaian. ok=true menutup tiket;
// ok=false (keterangan wajib) membuka kembali tiket ke penanggung jawab yang sama.
func (s *Service) ReporterFeedback(ctx context.Context, token string, ok bool, note string) (map[string]string, error) {
	if !ValidTokenFormat(token) {
		return nil, ErrNotFound
	}
	errs := map[string]string{}
	note = cleanText(note, 1000, "Keterangan", !ok, errs, "feedback_note")
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	return nil, database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicketByToken(ctx, tx, token)
		if err != nil {
			return err
		}
		if ok {
			return s.applyTransition(ctx, tx, t, transitionOpts{
				action: workflows.Close, event: EventConfirmed, actorType: "REPORTER", note: note,
				meta: map[string]any{"feedback": "OK"},
				sets: "closed_at = ?, reporter_feedback = 'OK', reporter_feedback_at = ?", args: []any{now, now},
			}, now)
		}
		return s.applyTransition(ctx, tx, t, transitionOpts{
			action: workflows.Reopen, event: EventReopened, actorType: "REPORTER", note: note,
			meta: map[string]any{"feedback": "NOT_OK", "cycle": t.SLACycle + 1},
			sets: reopenSets + ", reporter_feedback = 'NOT_OK', reporter_feedback_at = ?", args: []any{now},
		}, now)
	})
}

// AutoClose menutup tiket Selesai yang tidak dikonfirmasi pelapor dalam `days` hari.
// Idempoten: tiket yang sudah ditutup tidak tersentuh lagi. Mengembalikan jumlah tiket ditutup.
func (s *Service) AutoClose(ctx context.Context, days int) (int, error) {
	now := s.clock.Now()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tickets WHERE status = 'RESOLVED' AND resolved_at <= ? ORDER BY resolved_at LIMIT 500`, cutoff)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	closed := 0
	for _, id := range ids {
		err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
			t, err := lockTicket(ctx, tx, id)
			if err != nil {
				return err
			}
			if t.Status != workflows.Resolved {
				return nil // sudah dikonfirmasi/dibuka ulang sejak dipilih
			}
			if err := s.applyTransition(ctx, tx, t, transitionOpts{
				action: workflows.Close, event: EventAutoClosed, actorType: "SYSTEM",
				note: "Ditutup otomatis: tidak ada konfirmasi pelapor dalam " + itoa(days) + " hari.",
				sets: "closed_at = ?", args: []any{now},
			}, now); err != nil {
				return err
			}
			closed++
			return nil
		})
		if err != nil {
			return closed, err
		}
	}
	return closed, nil
}

// TakeOver: koordinator menjadi penanggung jawab utama (alasan wajib bila tiket sudah ada PJ).
func (s *Service) TakeOver(ctx context.Context, id int64, u *rbac.User, reason string) (map[string]string, error) {
	if !u.Has(rbac.PermTicketTakeover) {
		return nil, ErrForbidden
	}
	errs := map[string]string{}
	reason = cleanText(reason, 255, "Alasan", false, errs, "reason")
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	var claimNew bool
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		switch {
		case t.Status == workflows.New:
			claimNew = true
			return nil
		case !workflows.IsActive(t.Status):
			return ErrTransition
		case t.isPrimary(u):
			return nil
		}
		if reason == "" {
			errs["reason"] = "Alasan pengambilalihan wajib diisi."
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE ticket_assignments SET released_at = ?, released_by = ?, release_reason = ?
			WHERE ticket_id = ? AND released_at IS NULL AND (role = 'PRIMARY' OR user_id = ?)`, now, u.ID, "Diambil alih: "+reason, id, u.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ticket_assignments (ticket_id, user_id, role, assigned_by, assigned_at) VALUES (?, ?, 'PRIMARY', ?, ?)`,
			id, u.ID, u.ID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tickets SET assignee_id = ?, version = version + 1, updated_at = ? WHERE id = ?`, u.ID, now, id); err != nil {
			return err
		}
		return s.repo.addHistory(ctx, tx, historyEntry{TicketID: id, Event: EventTakenOver, ActorType: "USER", ActorID: u.ID,
			Note: reason, Meta: jsonMeta(map[string]any{"previous_assignee_id": t.AssigneeID.Int64})}, now)
	})
	if err != nil || len(errs) > 0 {
		return errs, err
	}
	if claimNew {
		res, err := s.Claim(ctx, id, u.ID)
		if err != nil {
			return nil, err
		}
		if !res.OK {
			return nil, ErrTransition
		}
	}
	return nil, nil
}

func jsonMeta(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// AddSupport menambahkan anggota pendukung (oleh PJ utama atau koordinator).
func (s *Service) AddSupport(ctx context.Context, id int64, u *rbac.User, userID int64) error {
	now := s.clock.Now()
	return database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) && !u.Has(rbac.PermTicketTakeover) {
			return ErrForbidden
		}
		if t.Status != workflows.Claimed && t.Status != workflows.InProgress && t.Status != workflows.Pending {
			return ErrTransition
		}
		var name, role string
		err = tx.QueryRowContext(ctx, `SELECT u.full_name, r.code FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = ? AND u.is_active = TRUE`, userID).Scan(&name, &role)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && role != rbac.RoleTechnician && role != rbac.RoleCoordinator) {
			return ErrMemberInvalid
		}
		if err != nil {
			return err
		}
		if t.AssigneeID.Int64 == userID {
			return ErrMemberInvalid
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ticket_assignments (ticket_id, user_id, role, assigned_by, assigned_at) VALUES (?, ?, 'SUPPORT', ?, ?)`,
			id, userID, u.ID, now); err != nil {
			if _, dup := database.DuplicateKey(err); dup {
				return ErrMemberExists
			}
			return err
		}
		return s.repo.addHistory(ctx, tx, historyEntry{TicketID: id, Event: EventSupportAdded, ActorType: "USER", ActorID: u.ID,
			Note: name, Meta: jsonMeta(map[string]any{"user_id": userID})}, now)
	})
}

// Error anggota pendukung.
var (
	ErrMemberInvalid = errors.New("Petugas tidak ditemukan, nonaktif, atau sudah menjadi penanggung jawab.")
	ErrMemberExists  = errors.New("Petugas ini sudah menjadi anggota tim tiket.")
)

// RemoveSupport melepas anggota pendukung.
func (s *Service) RemoveSupport(ctx context.Context, id int64, u *rbac.User, userID int64) error {
	now := s.clock.Now()
	return database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) && !u.Has(rbac.PermTicketTakeover) && u.ID != userID {
			return ErrForbidden
		}
		var name string
		_ = tx.QueryRowContext(ctx, `SELECT full_name FROM users WHERE id = ?`, userID).Scan(&name)
		res, err := tx.ExecContext(ctx, `UPDATE ticket_assignments SET released_at = ?, released_by = ?, release_reason = 'Dilepas dari tim'
			WHERE ticket_id = ? AND user_id = ? AND role = 'SUPPORT' AND released_at IS NULL`, now, u.ID, id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return s.repo.addHistory(ctx, tx, historyEntry{TicketID: id, Event: EventSupportRemoved, ActorType: "USER", ActorID: u.ID,
			Note: name, Meta: jsonMeta(map[string]any{"user_id": userID})}, now)
	})
}

// ChangePriority mengubah prioritas secara manual (Koordinator, alasan wajib).
func (s *Service) ChangePriority(ctx context.Context, id int64, u *rbac.User, p, reason string) (map[string]string, error) {
	if !u.Has(rbac.PermTicketPriority) {
		return nil, ErrForbidden
	}
	errs := map[string]string{}
	reason = cleanText(reason, 1000, "Alasan perubahan prioritas", true, errs, "reason")
	if !priority.ValidPriority(p) {
		errs["priority"] = "Pilih prioritas."
	}
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if workflows.IsTerminal(t.Status) {
			return ErrTransition
		}
		if t.Priority == p {
			errs["priority"] = "Prioritas baru sama dengan prioritas saat ini."
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tickets SET priority = ?, priority_source = 'MANUAL', version = version + 1, updated_at = ? WHERE id = ?`,
			p, now, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ticket_histories (ticket_id, event_type, from_priority, to_priority, actor_type, actor_user_id, note, created_at)
			VALUES (?, ?, ?, ?, 'USER', ?, ?, ?)`, id, EventPriorityChanged, t.Priority, p, u.ID, reason, now)
		return err
	})
	if err != nil || len(errs) > 0 {
		return errs, err
	}
	return nil, nil
}

// Comment adalah catatan pada tiket.
type Comment struct {
	ID         int64
	AuthorType string
	AuthorName sql.NullString
	Body       string
	IsInternal bool
	CreatedAt  time.Time
}

// AuthorFirstName untuk tampilan pelapor.
func (c Comment) AuthorFirstName() string {
	n := c.AuthorName.String
	if i := strings.IndexByte(n, ' '); i > 0 {
		return n[:i]
	}
	return n
}

// AddComment menambahkan catatan petugas. internal=true tidak terlihat pelapor.
func (s *Service) AddComment(ctx context.Context, id int64, u *rbac.User, body string, internal bool) (map[string]string, error) {
	errs := map[string]string{}
	body = cleanText(body, 2000, "Catatan", true, errs, "body")
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	return nil, database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		if workflows.IsTerminal(t.Status) && !u.Has(rbac.PermTicketTakeover) {
			return ErrTransition
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_comments (ticket_id, author_type, author_user_id, body, is_internal, created_at)
			VALUES (?, 'USER', ?, ?, ?, ?)`, id, u.ID, body, internal, now)
		return err
	})
}

// AddReporterComment menambahkan keterangan dari pelapor (selalu terlihat petugas & pelapor).
func (s *Service) AddReporterComment(ctx context.Context, token, body string) (map[string]string, error) {
	if !ValidTokenFormat(token) {
		return nil, ErrNotFound
	}
	errs := map[string]string{}
	body = cleanText(body, 1000, "Keterangan", true, errs, "comment")
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	return nil, database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		t, err := lockTicketByToken(ctx, tx, token)
		if err != nil {
			return err
		}
		if workflows.IsTerminal(t.Status) {
			return ErrTransition
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_comments (ticket_id, author_type, body, is_internal, created_at)
			VALUES (?, 'REPORTER', ?, FALSE, ?)`, t.ID, body, now)
		return err
	})
}

// Comments mengembalikan catatan tiket; publicOnly menyembunyikan catatan internal.
func (s *Service) Comments(ctx context.Context, id int64, publicOnly bool) ([]Comment, error) {
	q := `SELECT c.id, c.author_type, u.full_name, c.body, c.is_internal, c.created_at
	        FROM ticket_comments c LEFT JOIN users u ON u.id = c.author_user_id WHERE c.ticket_id = ?`
	if publicOnly {
		q += " AND c.is_internal = FALSE"
	}
	rows, err := s.db.QueryContext(ctx, q+" ORDER BY c.created_at, c.id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.AuthorType, &c.AuthorName, &c.Body, &c.IsInternal, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddPhotos menambahkan foto oleh petugas (PJ, pendukung, atau koordinator).
func (s *Service) AddPhotos(ctx context.Context, id int64, u *rbac.User, photos []Photo, visibleToReporter bool) error {
	if len(photos) == 0 {
		return nil
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
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		sup, err := isSupport(ctx, tx, id, u.ID)
		if err != nil {
			return err
		}
		if !t.isPrimary(u) && !sup && !u.Has(rbac.PermTicketTakeover) {
			return ErrForbidden
		}
		if t.Status == workflows.Cancelled {
			return ErrTransition
		}
		for i, p := range photos {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO attachments (ticket_id, uploaded_by_type, uploaded_by_user_id, storage_key, original_name, mime_type,
				                         size_bytes, width, height, sha256, visible_to_reporter, created_at)
				VALUES (?, 'USER', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, u.ID, keys[i], database.NullString(truncate(p.OriginalName, 255)), p.Mime, len(p.Data), p.Width, p.Height,
				p.SHA256[:], visibleToReporter, now); err != nil {
				return err
			}
		}
		return s.repo.addHistory(ctx, tx, historyEntry{TicketID: id, Event: EventPhotoAdded, ActorType: "USER", ActorID: u.ID,
			Meta: jsonMeta(map[string]any{"count": len(photos), "visible_to_reporter": visibleToReporter})}, now)
	})
	if err != nil {
		for _, k := range keys {
			s.store.Remove(k)
		}
	}
	return err
}

// Member adalah anggota tim tiket yang aktif.
type Member struct {
	UserID int64
	Name   string
	Role   string
}

// Members mengembalikan anggota tim aktif (PJ utama lebih dulu).
func (s *Service) Members(ctx context.Context, id int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.user_id, u.full_name, a.role FROM ticket_assignments a JOIN users u ON u.id = a.user_id
		 WHERE a.ticket_id = ? AND a.released_at IS NULL ORDER BY a.role = 'SUPPORT', a.assigned_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Name, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// StaffOptions mengembalikan petugas & koordinator aktif (calon anggota pendukung).
func (s *Service) StaffOptions(ctx context.Context) ([]Option, error) {
	return s.repo.options(ctx, s.db, `
		SELECT u.id, u.full_name FROM users u JOIN roles r ON r.id = u.role_id
		 WHERE u.is_active = TRUE AND r.code IN ('TECHNICIAN','COORDINATOR') ORDER BY u.full_name`)
}

// PendingReasons mengembalikan alasan tunda aktif.
func (s *Service) PendingReasons(ctx context.Context) ([]Option, error) {
	return s.repo.options(ctx, s.db, `SELECT id, label FROM pending_reasons WHERE is_active = TRUE ORDER BY sort_order, label`)
}

// Template adalah template tindakan.
type Template struct {
	ID    int64
	Title string
	Body  string
}

// ActionTemplates mengembalikan template yang cocok dengan jenis gangguan/bidang tiket (atau umum).
func (s *Service) ActionTemplates(ctx context.Context, ticketID int64) ([]Template, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.title, a.body FROM action_templates a JOIN tickets t ON t.id = ?
		 WHERE a.is_active = TRUE
		   AND (a.ticket_category_id IS NULL OR a.ticket_category_id = t.ticket_category_id)
		   AND (a.it_category_id IS NULL OR a.it_category_id = t.it_category_id)
		 ORDER BY (a.ticket_category_id IS NULL), (a.it_category_id IS NULL), a.sort_order, a.title`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.ID, &t.Title, &t.Body); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
