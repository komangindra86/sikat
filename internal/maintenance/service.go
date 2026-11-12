package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sikat/internal/activities"
	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
	"sikat/internal/master"
	"sikat/internal/rbac"
	"sikat/internal/settings"
	"sikat/internal/tickets"
)

// Error aturan bisnis.
var (
	ErrNotFound  = errors.New("data tidak ditemukan")
	ErrForbidden = errors.New("Anda tidak berhak melakukan tindakan ini.")
)

var assetStatuses = map[string]bool{"ACTIVE": true, "BROKEN": true, "IN_REPAIR": true, "UNUSED": true, "DISPOSED": true}
var decisions = map[string]bool{"NO_ACTION": true, "INSPECT": true, "REPAIR": true, "REPLACEMENT_PROPOSED": true, "OTHER": true}
var manualTypes = map[string]bool{"CORRECTIVE": true, "PREVENTIVE": true, "INSPECTION": true, "INSTALLATION": true, "RELOCATION": true}
var results = map[string]bool{"FIXED": true, "WORKAROUND": true, "REPLACED": true, "VENDOR": true, "NOT_REPRODUCIBLE": true, "CANNOT_FIX": true, "DONE": true}

// Service mengelola riwayat pemeliharaan.
type Service struct {
	db       *sql.DB
	clock    clock.Clock
	settings *settings.Service
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock, s *settings.Service) *Service {
	return &Service{db: db, clock: c, settings: s}
}

// Config membaca ambang dari pengaturan sistem.
func (s *Service) Config(ctx context.Context) Config {
	return Config{
		Threshold:        s.settings.Int(ctx, settings.KeyRepeatThreshold),
		WindowDays:       s.settings.Int(ctx, settings.KeyRepeatWindowDays),
		InspectThreshold: s.settings.Int(ctx, settings.KeyRepeatInspect),
		ReplaceAgeYears:  s.settings.Int(ctx, settings.KeyReplaceAgeYears),
		ReplaceCost:      int64(s.settings.Int(ctx, settings.KeyReplaceCost)),
	}
}

// ---------------------------------------------------------------------------
// Sparepart & biaya
// ---------------------------------------------------------------------------

type partLine struct {
	partID   int64
	qty      string
	unitCost sql.NullString
}

// parseParts memvalidasi baris sparepart. Harga kosong memakai harga acuan sparepart.
func parseParts(ctx context.Context, q database.DBTX, raw []tickets.PartLine, errs map[string]string) ([]partLine, error) {
	var out []partLine
	for _, r := range raw {
		pid := strings.TrimSpace(r.PartID)
		if pid == "" {
			continue // baris kosong diabaikan
		}
		id, err := strconv.ParseInt(pid, 10, 64)
		if err != nil || id <= 0 {
			errs["parts"] = "Pilihan sparepart tidak valid."
			continue
		}
		var name string
		var def sql.NullString
		err = q.QueryRowContext(ctx, `SELECT name, default_unit_cost FROM spare_parts WHERE id = ? AND is_active = TRUE`, id).Scan(&name, &def)
		if errors.Is(err, sql.ErrNoRows) {
			errs["parts"] = "Sparepart tidak ditemukan atau nonaktif."
			continue
		}
		if err != nil {
			return nil, err
		}
		qtyRaw := strings.TrimSpace(strings.ReplaceAll(r.Qty, ",", "."))
		if qtyRaw == "" {
			qtyRaw = "1"
		}
		qty, ok := new(big.Rat).SetString(qtyRaw)
		if !ok || qty.Sign() <= 0 || qty.Cmp(big.NewRat(9999, 1)) > 0 {
			errs["parts"] = "Jumlah " + name + " harus angka lebih dari 0."
			continue
		}
		line := partLine{partID: id, qty: qty.FloatString(2)}
		if c := strings.TrimSpace(r.UnitCost); c != "" {
			m, err := master.ParseMoney(c)
			if err != nil {
				errs["parts"] = "Harga " + name + ": " + err.Error() + "."
				continue
			}
			line.unitCost = sql.NullString{String: m, Valid: true}
		} else {
			line.unitCost = def
		}
		out = append(out, line)
	}
	if len(out) > 10 {
		errs["parts"] = "Maksimal 10 jenis sparepart per entri."
	}
	return out, nil
}

func parseCost(raw string, errs map[string]string, key string) sql.NullString {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sql.NullString{}
	}
	m, err := master.ParseMoney(raw)
	if err != nil {
		errs[key] = "Biaya: " + err.Error() + "."
		return sql.NullString{}
	}
	return sql.NullString{String: m, Valid: true}
}

func insertParts(ctx context.Context, tx *sql.Tx, entryID int64, parts []partLine, now time.Time) error {
	for _, p := range parts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO maintenance_parts (maintenance_history_id, spare_part_id, quantity, unit_cost, created_at) VALUES (?, ?, ?, ?, ?)`,
			entryID, p.partID, p.qty, p.unitCost, now); err != nil {
			return err
		}
	}
	return nil
}

// updateAssetStatus mengubah status perangkat bila berbeda, dengan audit.
func updateAssetStatus(ctx context.Context, tx *sql.Tx, assetID int64, status, reason string, now time.Time) error {
	if status == "" {
		return nil
	}
	var cur string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM assets WHERE id = ? FOR UPDATE`, assetID).Scan(&cur); err != nil {
		return err
	}
	if cur == status {
		return nil
	}
	disposed := "disposed_at = NULL"
	if status == "DISPOSED" {
		disposed = "disposed_at = COALESCE(disposed_at, ?)"
	}
	q := `UPDATE assets SET status = ?, ` + disposed + `, updated_at = ? WHERE id = ?`
	args := []any{status}
	if status == "DISPOSED" {
		args = append(args, now)
	}
	args = append(args, now, assetID)
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return err
	}
	return audit.Record(ctx, tx, now, audit.Entry{Action: "asset.status_change", EntityType: "assets", EntityID: assetID,
		Before: map[string]string{"status": cur}, After: map[string]string{"status": status, "reason": reason}})
}

// ---------------------------------------------------------------------------
// Kait tiket
// ---------------------------------------------------------------------------

// ValidateResolution memeriksa sparepart, biaya, dan kondisi perangkat pada form penyelesaian.
func (s *Service) ValidateResolution(ctx context.Context, tx *sql.Tx, ev tickets.ResolvedEvent) (map[string]string, error) {
	errs := map[string]string{}
	if _, err := parseParts(ctx, tx, ev.Parts, errs); err != nil {
		return nil, err
	}
	parseCost(ev.ServiceCost, errs, "service_cost")
	if ev.AssetStatusAfter != "" && !assetStatuses[ev.AssetStatusAfter] {
		errs["asset_status_after"] = "Kondisi perangkat tidak valid."
	}
	return errs, nil
}

// TicketResolved mencatat entri perbaikan (CORRECTIVE) untuk siklus tiket ini.
// Satu entri per (tiket, perangkat, siklus) — dijaga UNIQUE index.
func (s *Service) TicketResolved(ctx context.Context, tx *sql.Tx, ev tickets.ResolvedEvent) error {
	errs := map[string]string{}
	parts, err := parseParts(ctx, tx, ev.Parts, errs)
	if err != nil {
		return err
	}
	cost := parseCost(ev.ServiceCost, errs, "service_cost")
	if len(errs) > 0 {
		return errors.New("isian penyelesaian tidak valid") // sudah divalidasi sebelumnya
	}
	status := ev.AssetStatusAfter
	if status == "" {
		status = "ACTIVE"
	}
	var started any
	if ev.StartedAt != nil {
		started = *ev.StartedAt
	}
	now := s.clock.Now()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO asset_maintenance_histories (asset_id, maintenance_type, source, ticket_id, ticket_cycle, ticket_category_id,
		       performed_by_user_id, complaint, action_taken, result, problem_started_at, started_at, finished_at, service_cost,
		       asset_status_after, notes, created_by, created_at, updated_at)
		VALUES (?, 'CORRECTIVE', 'TICKET', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.AssetID, ev.TicketID, ev.Cycle, ev.TicketCategoryID, ev.ActorID, database.NullString(ev.Complaint), ev.Action, ev.Result,
		ev.ProblemStartedAt, started, ev.FinishedAt, cost, status, database.NullString(ev.Note), ev.ActorID, now, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	if err := insertParts(ctx, tx, id, parts, now); err != nil {
		return err
	}
	return updateAssetStatus(ctx, tx, ev.AssetID, status, "penyelesaian tiket", now)
}

// TicketCreated menjalankan deteksi kerusakan berulang untuk perangkat tiket.
func (s *Service) TicketCreated(ctx context.Context, tx *sql.Tx, ev tickets.CreatedEvent) error {
	_, err := s.detect(ctx, tx, ev.AssetID, ev.TicketID)
	return err
}

// facts menghitung fakta perangkat. Gangguan dihitung dalam jendela waktu dan
// hanya setelah keputusan koordinator terakhir (keputusan "mereset" penghitungan).
func (s *Service) facts(ctx context.Context, q database.DBTX, assetID int64, c Config) (Facts, error) {
	now := s.clock.Now()
	since := now.Add(-time.Duration(c.WindowDays) * 24 * time.Hour)
	var lastDecision sql.NullTime
	if err := q.QueryRowContext(ctx, `SELECT MAX(decided_at) FROM asset_alerts WHERE asset_id = ? AND status = 'CLOSED'`, assetID).Scan(&lastDecision); err != nil {
		return Facts{}, err
	}
	if lastDecision.Valid && lastDecision.Time.After(since) {
		since = lastDecision.Time
	}
	var f Facts
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets WHERE asset_id = ? AND status <> 'CANCELLED' AND created_at > ?`, assetID, since).
		Scan(&f.CountInWindow); err != nil {
		return f, err
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(n), 0) FROM (SELECT COUNT(*) n FROM tickets WHERE asset_id = ? AND status <> 'CANCELLED'
		AND created_at > ? GROUP BY ticket_category_id) x`, assetID, since).Scan(&f.MaxSameCategory); err != nil {
		return f, err
	}
	var procured sql.NullTime
	if err := q.QueryRowContext(ctx, `SELECT procured_at FROM assets WHERE id = ?`, assetID).Scan(&procured); err != nil {
		return f, err
	}
	if procured.Valid {
		f.AgeYears = now.Sub(procured.Time).Hours() / 24 / 365.25
	}
	var cost sql.NullString
	if err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(h.service_cost), 0) + COALESCE((SELECT SUM(p.line_cost) FROM maintenance_parts p
		        JOIN asset_maintenance_histories h2 ON h2.id = p.maintenance_history_id WHERE h2.asset_id = ? AND h2.voided_at IS NULL), 0)
		  FROM asset_maintenance_histories h WHERE h.asset_id = ? AND h.voided_at IS NULL`, assetID, assetID).Scan(&cost); err != nil {
		return f, err
	}
	f.TotalCost, _ = strconv.ParseFloat(cost.String, 64)
	return f, nil
}

// detect membuat/memperbarui peringatan terbuka bila ambang tercapai.
// Mengembalikan ID peringatan (0 bila tidak ada).
func (s *Service) detect(ctx context.Context, tx *sql.Tx, assetID, ticketID int64) (int64, error) {
	c := s.Config(ctx)
	f, err := s.facts(ctx, tx, assetID, c)
	if err != nil {
		return 0, err
	}
	rec, reasons := Recommend(c, f)
	if rec == "" {
		return 0, nil
	}
	now := s.clock.Now()
	reasonText := strings.Join(reasons, "; ")
	var id int64
	var curRec string
	err = tx.QueryRowContext(ctx, `SELECT id, recommendation FROM asset_alerts WHERE asset_id = ? AND status <> 'CLOSED' FOR UPDATE`, assetID).Scan(&id, &curRec)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `
			INSERT INTO asset_alerts (asset_id, ticket_count, window_days, threshold, last_ticket_id, recommendation, reasons, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, assetID, f.CountInWindow, c.WindowDays, c.Threshold, database.NullInt64(ticketID), rec, reasonText, now, now)
		if err != nil {
			return 0, err
		}
		id, _ = res.LastInsertId()
		return id, audit.Record(ctx, tx, now, audit.Entry{Action: "asset.repeat_damage_alert", EntityType: "assets", EntityID: assetID, System: true,
			After: map[string]any{"alert_id": id, "count": f.CountInWindow, "recommendation": rec}})
	case err != nil:
		return 0, err
	}
	// Peringatan terbuka diperbarui; rekomendasi tidak diturunkan.
	if RecRank(curRec) > RecRank(rec) {
		rec = curRec
	}
	_, err = tx.ExecContext(ctx, `UPDATE asset_alerts SET ticket_count = ?, last_ticket_id = ?, recommendation = ?, reasons = ?, updated_at = ? WHERE id = ?`,
		f.CountInWindow, database.NullInt64(ticketID), rec, reasonText, now, id)
	return id, err
}

// ---------------------------------------------------------------------------
// Kait kegiatan
// ---------------------------------------------------------------------------

// ActivitySaved menjaga entri riwayat perangkat dari kegiatan: dibuat/diperbarui
// saat kegiatan selesai dan terkait perangkat; dibatalkan (void) bila tidak lagi.
func (s *Service) ActivitySaved(ctx context.Context, tx *sql.Tx, ev activities.SavedEvent) error {
	now := s.clock.Now()
	var id int64
	var voided sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id, voided_at FROM asset_maintenance_histories WHERE activity_id = ? FOR UPDATE`, ev.ActivityID).Scan(&id, &voided)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if ev.Status != activities.StatusDone || ev.AssetID == 0 {
		if exists && !voided.Valid {
			_, err := tx.ExecContext(ctx, `UPDATE asset_maintenance_histories SET voided_at = ?, voided_by = ?, void_reason = ?, updated_at = ? WHERE id = ?`,
				now, database.NullInt64(ev.ActorID), "Kegiatan tidak lagi selesai atau tidak terkait perangkat", now, id)
			return err
		}
		return nil
	}
	finished := noon(ev.Date)
	switch {
	case ev.EndedAt != nil:
		finished = *ev.EndedAt
	case ev.StartedAt != nil:
		finished = *ev.StartedAt
	}
	var started any
	if ev.StartedAt != nil && !ev.StartedAt.After(finished) {
		started = *ev.StartedAt
	}
	action := ev.Title
	if ev.Result != "" {
		action += ": " + ev.Result
	}
	mtype := ev.MaintenanceType
	if mtype == "" {
		mtype = "PREVENTIVE"
	}
	if exists {
		_, err := tx.ExecContext(ctx, `
			UPDATE asset_maintenance_histories SET asset_id = ?, maintenance_type = ?, performed_by_user_id = ?, action_taken = ?,
			       started_at = ?, finished_at = ?, notes = ?, voided_at = NULL, voided_by = NULL, void_reason = NULL, updated_at = ?
			 WHERE id = ?`, ev.AssetID, mtype, database.NullInt64(ev.LeadID), action, started, finished,
			database.NullString(ev.Description), now, id)
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO asset_maintenance_histories (asset_id, maintenance_type, source, activity_id, performed_by_user_id, action_taken,
		       result, started_at, finished_at, notes, created_by, created_at, updated_at)
		VALUES (?, ?, 'ACTIVITY', ?, ?, ?, 'DONE', ?, ?, ?, ?, ?, ?)`,
		ev.AssetID, mtype, ev.ActivityID, database.NullInt64(ev.LeadID), action, started, finished,
		database.NullString(ev.Description), database.NullInt64(ev.ActorID), now, now)
	return err
}

func noon(d time.Time) time.Time {
	y, m, day := d.Date()
	return time.Date(y, m, day, 12, 0, 0, 0, clock.Location).UTC()
}

// ---------------------------------------------------------------------------
// Tampilan perangkat
// ---------------------------------------------------------------------------

// Stats menghitung statistik perangkat (semua & dalam jendela).
func (s *Service) Stats(ctx context.Context, assetID int64) (Stats, error) {
	c := s.Config(ctx)
	since := s.clock.Now().Add(-time.Duration(c.WindowDays) * 24 * time.Hour)
	st := Stats{WindowDays: c.WindowDays}
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM tickets WHERE asset_id = ? AND status <> 'CANCELLED'),
		       (SELECT COUNT(*) FROM tickets WHERE asset_id = ? AND status <> 'CANCELLED' AND created_at >= ?),
		       (SELECT COUNT(*) FROM asset_maintenance_histories WHERE asset_id = ? AND voided_at IS NULL AND maintenance_type = 'CORRECTIVE'),
		       (SELECT COUNT(*) FROM asset_maintenance_histories WHERE asset_id = ? AND voided_at IS NULL AND maintenance_type = 'CORRECTIVE' AND finished_at >= ?),
		       (SELECT COALESCE(SUM(downtime_minutes), 0) FROM asset_maintenance_histories WHERE asset_id = ? AND voided_at IS NULL),
		       (SELECT COALESCE(SUM(downtime_minutes), 0) FROM asset_maintenance_histories WHERE asset_id = ? AND voided_at IS NULL AND finished_at >= ?),
		       (SELECT MAX(finished_at) FROM asset_maintenance_histories WHERE asset_id = ? AND voided_at IS NULL AND maintenance_type <> 'ALERT_DECISION')`,
		assetID, assetID, since, assetID, assetID, since, assetID, assetID, since, assetID).
		Scan(&st.DamagesAll, &st.DamagesWindow, &st.RepairsAll, &st.RepairsWindow, &st.DowntimeAll, &st.DowntimeWindow, &st.LastMaintenance)
	if err != nil {
		return st, err
	}
	costQ := `SELECT COALESCE(SUM(h.service_cost), 0) + COALESCE(SUM((SELECT SUM(p.line_cost) FROM maintenance_parts p WHERE p.maintenance_history_id = h.id)), 0)
	            FROM asset_maintenance_histories h WHERE h.asset_id = ? AND h.voided_at IS NULL`
	if err := s.db.QueryRowContext(ctx, costQ, assetID).Scan(&st.CostAll); err != nil {
		return st, err
	}
	err = s.db.QueryRowContext(ctx, costQ+` AND h.finished_at >= ?`, assetID, since).Scan(&st.CostWindow)
	return st, err
}

// History mengembalikan riwayat pemeliharaan perangkat (terbaru dulu), termasuk entri yang dibatalkan.
func (s *Service) History(ctx context.Context, assetID int64) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT h.id, h.asset_id, h.maintenance_type, h.source, h.ticket_id, t.ticket_no, h.ticket_cycle, h.activity_id, a.activity_no,
		       tc.name, u.full_name, h.complaint, h.action_taken, h.result, h.started_at, h.finished_at, h.repair_minutes,
		       h.downtime_minutes, h.service_cost, (SELECT SUM(p.line_cost) FROM maintenance_parts p WHERE p.maintenance_history_id = h.id),
		       h.asset_status_after, h.notes, h.voided_at, h.void_reason, vu.full_name
		  FROM asset_maintenance_histories h
		  LEFT JOIN tickets t ON t.id = h.ticket_id
		  LEFT JOIN internal_activities a ON a.id = h.activity_id
		  LEFT JOIN ticket_categories tc ON tc.id = h.ticket_category_id
		  LEFT JOIN users u ON u.id = h.performed_by_user_id
		  LEFT JOIN users vu ON vu.id = h.voided_by
		 WHERE h.asset_id = ? ORDER BY h.finished_at DESC, h.id DESC LIMIT 200`, assetID)
	if err != nil {
		return nil, err
	}
	var out []Entry
	idx := map[int64]int{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.AssetID, &e.Type, &e.Source, &e.TicketID, &e.TicketNo, &e.TicketCycle, &e.ActivityID, &e.ActivityNo,
			&e.CategoryName, &e.PerformerName, &e.Complaint, &e.Action, &e.Result, &e.StartedAt, &e.FinishedAt, &e.RepairMinutes,
			&e.DowntimeMinutes, &e.ServiceCost, &e.PartsCost, &e.StatusAfter, &e.Notes, &e.VoidedAt, &e.VoidReason, &e.VoiderName); err != nil {
			rows.Close()
			return nil, err
		}
		idx[e.ID] = len(out)
		out = append(out, e)
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	prow, err := s.db.QueryContext(ctx, `
		SELECT p.maintenance_history_id, sp.name, sp.unit, p.quantity, p.unit_cost, p.line_cost
		  FROM maintenance_parts p JOIN spare_parts sp ON sp.id = p.spare_part_id
		  JOIN asset_maintenance_histories h ON h.id = p.maintenance_history_id
		 WHERE h.asset_id = ? ORDER BY p.id`, assetID)
	if err != nil {
		return nil, err
	}
	defer prow.Close()
	for prow.Next() {
		var hid int64
		var p PartUsed
		if err := prow.Scan(&hid, &p.Name, &p.Unit, &p.Qty, &p.UnitCost, &p.LineCost); err != nil {
			return nil, err
		}
		if i, ok := idx[hid]; ok {
			p.Qty = strings.TrimSuffix(strings.TrimSuffix(p.Qty, "0"), ".0")
			p.Qty = strings.TrimSuffix(p.Qty, ".")
			out[i].Parts = append(out[i].Parts, p)
		}
	}
	return out, prow.Err()
}

const selectAlert = `
	SELECT al.id, al.asset_id, a.asset_code, a.name, d.name, r.name, al.status, al.ticket_count, al.window_days, al.threshold,
	       al.recommendation, al.reasons, al.decision, al.decision_note, u.full_name, al.decided_at, al.created_at, al.updated_at
	  FROM asset_alerts al JOIN assets a ON a.id = al.asset_id
	  LEFT JOIN departments d ON d.id = a.department_id
	  LEFT JOIN rooms r ON r.id = a.room_id
	  LEFT JOIN users u ON u.id = al.decided_by`

func scanAlert(sc interface{ Scan(...any) error }) (Alert, error) {
	var a Alert
	err := sc.Scan(&a.ID, &a.AssetID, &a.AssetCode, &a.AssetName, &a.DepartmentName, &a.RoomName, &a.Status, &a.TicketCount, &a.WindowDays,
		&a.Threshold, &a.Recommendation, &a.Reasons, &a.Decision, &a.DecisionNote, &a.DeciderName, &a.DecidedAt, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

// OpenAlert mengembalikan peringatan terbuka perangkat (nil bila tidak ada).
func (s *Service) OpenAlert(ctx context.Context, assetID int64) (*Alert, error) {
	a, err := scanAlert(s.db.QueryRowContext(ctx, selectAlert+` WHERE al.asset_id = ? AND al.status <> 'CLOSED'`, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Alerts mengembalikan daftar peringatan (open=true: yang belum diputuskan).
func (s *Service) Alerts(ctx context.Context, open bool, limit int) ([]Alert, error) {
	q := selectAlert + ` WHERE al.status <> 'CLOSED' ORDER BY FIELD(al.recommendation,'CONSIDER_REPLACEMENT','INSPECT','EVALUATE'), al.ticket_count DESC, al.updated_at DESC`
	if !open {
		q = selectAlert + ` WHERE al.status = 'CLOSED' ORDER BY al.decided_at DESC`
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, q+` LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SpareParts mengembalikan sparepart aktif.
func (s *Service) SpareParts(ctx context.Context) ([]Option, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, CONCAT(name, ' (', unit, ')'), default_unit_cost FROM spare_parts WHERE is_active = TRUE ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Option
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Label, &o.Cost); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// View menyusun data pemeliharaan untuk halaman detail perangkat.
func (s *Service) View(ctx context.Context, assetID int64, u *rbac.User) (AssetView, error) {
	var v AssetView
	var err error
	if v.Stats, err = s.Stats(ctx, assetID); err != nil {
		return v, err
	}
	if v.History, err = s.History(ctx, assetID); err != nil {
		return v, err
	}
	if v.Alert, err = s.OpenAlert(ctx, assetID); err != nil {
		return v, err
	}
	v.CanRecord = u.Has(rbac.PermMaintenanceRecord)
	v.CanReview = u.Has(rbac.PermMaintenanceReview)
	if v.CanRecord {
		if v.Parts, err = s.SpareParts(ctx); err != nil {
			return v, err
		}
	}
	v.Form = map[string]string{}
	return v, nil
}

// ---------------------------------------------------------------------------
// Entri manual, pembatalan, keputusan peringatan
// ---------------------------------------------------------------------------

// ManualInput adalah isian form entri manual.
type ManualInput struct {
	Type        string
	Action      string
	Result      string
	StartedAt   string // datetime-local WITA
	FinishedAt  string // datetime-local WITA
	ServiceCost string
	StatusAfter string
	Notes       string
	Parts       []tickets.PartLine
}

func parseLocal(raw string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", raw, clock.Location)
	if err != nil {
		return nil, false
	}
	u := t.UTC()
	return &u, true
}

// AddManual mencatat pemeliharaan di luar tiket/kegiatan (mis. servis vendor).
func (s *Service) AddManual(ctx context.Context, u *rbac.User, assetID int64, in ManualInput) (map[string]string, error) {
	if !u.Has(rbac.PermMaintenanceRecord) {
		return nil, ErrForbidden
	}
	errs := map[string]string{}
	now := s.clock.Now()
	if !manualTypes[in.Type] {
		errs["m_type"] = "Pilih jenis pemeliharaan."
	}
	in.Action = strings.TrimSpace(in.Action)
	if in.Action == "" {
		errs["m_action"] = "Tindakan wajib diisi."
	} else if utf8.RuneCountInString(in.Action) > 4000 {
		errs["m_action"] = "Tindakan maksimal 4000 karakter."
	}
	if in.Result == "" {
		in.Result = "DONE"
	}
	if !results[in.Result] {
		errs["m_result"] = "Hasil tidak valid."
	}
	started, ok := parseLocal(in.StartedAt)
	if !ok {
		errs["m_started_at"] = "Format waktu tidak valid."
	}
	finished, ok := parseLocal(in.FinishedAt)
	switch {
	case !ok:
		errs["m_finished_at"] = "Format waktu tidak valid."
	case finished == nil:
		errs["m_finished_at"] = "Waktu selesai wajib diisi."
	case finished.After(now.Add(time.Minute)):
		errs["m_finished_at"] = "Waktu selesai tidak boleh di masa depan."
	case started != nil && started.After(*finished):
		errs["m_started_at"] = "Waktu mulai tidak boleh setelah waktu selesai."
	}
	if in.StatusAfter != "" && !assetStatuses[in.StatusAfter] {
		errs["m_status_after"] = "Kondisi perangkat tidak valid."
	}
	notes := strings.TrimSpace(in.Notes)
	var entryID int64
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE id = ?`, assetID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		parts, err := parseParts(ctx, tx, in.Parts, errs)
		if err != nil {
			return err
		}
		cost := parseCost(in.ServiceCost, errs, "m_service_cost")
		if len(errs) > 0 {
			return nil
		}
		var st any
		if started != nil {
			st = *started
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO asset_maintenance_histories (asset_id, maintenance_type, source, performed_by_user_id, action_taken, result,
			       started_at, finished_at, service_cost, asset_status_after, notes, created_by, created_at, updated_at)
			VALUES (?, ?, 'MANUAL', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			assetID, in.Type, u.ID, in.Action, in.Result, st, *finished, cost, database.NullString(in.StatusAfter),
			database.NullString(notes), u.ID, now, now)
		if err != nil {
			return err
		}
		entryID, _ = res.LastInsertId()
		if err := insertParts(ctx, tx, entryID, parts, now); err != nil {
			return err
		}
		if err := updateAssetStatus(ctx, tx, assetID, in.StatusAfter, "entri pemeliharaan manual", now); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "maintenance.manual_add", EntityType: "asset_maintenance_histories", EntityID: entryID,
			After: map[string]any{"asset_id": assetID, "type": in.Type, "action": in.Action, "service_cost": cost.String, "parts": len(parts)}})
	})
	if err != nil || len(errs) > 0 {
		return errs, err
	}
	return nil, nil
}

// Void menandai entri riwayat sebagai dibatalkan (koreksi), dengan alasan wajib.
func (s *Service) Void(ctx context.Context, u *rbac.User, entryID int64, reason string) (int64, map[string]string, error) {
	if !u.Has(rbac.PermMaintenanceReview) {
		return 0, nil, ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 255 {
		return 0, map[string]string{"void_reason": "Alasan pembatalan wajib diisi (maks. 255 karakter)."}, nil
	}
	now := s.clock.Now()
	var assetID int64
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var voided sql.NullTime
		err := tx.QueryRowContext(ctx, `SELECT asset_id, voided_at FROM asset_maintenance_histories WHERE id = ? FOR UPDATE`, entryID).Scan(&assetID, &voided)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil || voided.Valid {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE asset_maintenance_histories SET voided_at = ?, voided_by = ?, void_reason = ?, updated_at = ? WHERE id = ?`,
			now, u.ID, reason, now, entryID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "maintenance.void", EntityType: "asset_maintenance_histories", EntityID: entryID,
			After: map[string]string{"reason": reason}})
	})
	return assetID, nil, err
}

// Decide menutup peringatan dengan keputusan koordinator dan mencatatnya di riwayat perangkat.
func (s *Service) Decide(ctx context.Context, u *rbac.User, alertID int64, decision, note string) (int64, map[string]string, error) {
	if !u.Has(rbac.PermMaintenanceReview) {
		return 0, nil, ErrForbidden
	}
	errs := map[string]string{}
	if !decisions[decision] {
		errs["decision"] = "Pilih keputusan."
	}
	note = strings.TrimSpace(note)
	if decision == "OTHER" && note == "" {
		errs["decision_note"] = "Jelaskan keputusan lainnya."
	}
	if utf8.RuneCountInString(note) > 2000 {
		errs["decision_note"] = "Catatan maksimal 2000 karakter."
	}
	if len(errs) > 0 {
		return 0, errs, nil
	}
	now := s.clock.Now()
	var assetID int64
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT asset_id, status FROM asset_alerts WHERE id = ? FOR UPDATE`, alertID).Scan(&assetID, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status == "CLOSED" {
			errs["decision"] = "Peringatan ini sudah diputuskan."
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE asset_alerts SET status = 'CLOSED', decision = ?, decision_note = ?, decided_by = ?, decided_at = ?, updated_at = ?
			WHERE id = ?`, decision, database.NullString(note), u.ID, now, now, alertID); err != nil {
			return err
		}
		action := "Keputusan koordinator atas peringatan kerusakan berulang: " + decisionLabel(decision)
		if note != "" {
			action += ". " + note
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO asset_maintenance_histories (asset_id, maintenance_type, source, asset_alert_id, performed_by_user_id, action_taken,
			       result, finished_at, created_by, created_at, updated_at)
			VALUES (?, 'ALERT_DECISION', 'ALERT', ?, ?, ?, 'DONE', ?, ?, ?, ?)`, assetID, alertID, u.ID, action, now, u.ID, now, now); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "asset.alert_decision", EntityType: "asset_alerts", EntityID: alertID,
			After: map[string]string{"decision": decision, "note": note}})
	})
	if err != nil || len(errs) > 0 {
		return assetID, errs, err
	}
	return assetID, nil, nil
}

func decisionLabel(d string) string {
	switch d {
	case "NO_ACTION":
		return "tidak perlu tindakan"
	case "INSPECT":
		return "pemeriksaan lanjutan"
	case "REPAIR":
		return "perbaikan menyeluruh"
	case "REPLACEMENT_PROPOSED":
		return "diusulkan penggantian"
	}
	return "lainnya"
}

// addDecimal menjumlahkan dua string desimal ("" dianggap kosong).
func addDecimal(a, b string) string {
	if a == "" && b == "" {
		return ""
	}
	x, _ := new(big.Rat).SetString(defaultZero(a))
	y, _ := new(big.Rat).SetString(defaultZero(b))
	return new(big.Rat).Add(x, y).FloatString(2)
}

func defaultZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}
