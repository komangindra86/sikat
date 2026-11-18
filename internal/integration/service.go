package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
)

// Service mengelola konfigurasi, impor CSV, sinkronisasi terjadwal, dan log.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	log   *slog.Logger
	// providers: provider SIMRS yang tersedia per jenis. Kosong sampai adapter
	// dibuat dari spesifikasi SIMRS (tidak ada integrasi palsu).
	providers map[string]Provider
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, clock: c, log: log, providers: map[string]Provider{}}
}

// RegisterProvider memasang provider SIMRS (dipakai adapter nyata atau test).
func (s *Service) RegisterProvider(p Provider) { s.providers[p.Kind()] = p }

// ProviderAvailable melaporkan apakah adapter jenis ini tersedia.
func (s *Service) ProviderAvailable(kind string) bool { _, ok := s.providers[kind]; return ok }

// ---------------------------------------------------------------------------
// Status & konfigurasi
// ---------------------------------------------------------------------------

// Status adalah ringkasan satu entitas untuk halaman integrasi.
type Status struct {
	Entity        string
	Provider      string
	Enabled       bool
	Schedule      sql.NullInt64
	Policy        string
	RetryCount    int
	NextRetryAt   sql.NullTime
	LastRunAt     sql.NullTime
	LastSuccessAt sql.NullTime
	Total         int
	FromSource    int // data yang berasal dari CSV/SIMRS
	LocalEdited   int // data dari sumber luar yang diubah manual setelah sinkron
	LastLog       *LogEntry
}

// Statuses mengembalikan status semua entitas.
func (s *Service) Statuses(ctx context.Context) ([]Status, error) {
	var out []Status
	for _, e := range Entities {
		st := Status{Entity: e}
		err := s.db.QueryRowContext(ctx, `SELECT provider, is_enabled, schedule_minutes, conflict_policy, retry_count, next_retry_at, last_run_at, last_success_at
			FROM integration_configs WHERE entity_type = ?`, e).
			Scan(&st.Provider, &st.Enabled, &st.Schedule, &st.Policy, &st.RetryCount, &st.NextRetryAt, &st.LastRunAt, &st.LastSuccessAt)
		if err != nil {
			return nil, err
		}
		t := tables[e].Table
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(source <> 'LOCAL'), 0),
			COALESCE(SUM(source <> 'LOCAL' AND local_modified_at IS NOT NULL AND (last_synced_at IS NULL OR local_modified_at > last_synced_at)), 0)
			FROM `+t).Scan(&st.Total, &st.FromSource, &st.LocalEdited); err != nil {
			return nil, err
		}
		logs, err := s.Logs(ctx, e, 1)
		if err != nil {
			return nil, err
		}
		if len(logs) > 0 {
			st.LastLog = &logs[0]
		}
		out = append(out, st)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Log
// ---------------------------------------------------------------------------

// LogEntry adalah satu baris log sinkronisasi/impor.
type LogEntry struct {
	ID                                                                  int64
	Entity, Provider, Trigger, Status                                   string
	Attempt                                                             int
	StartedAt                                                           time.Time
	FinishedAt                                                          sql.NullTime
	Total, Inserted, Updated, Unchanged, Conflicts, Failed, Deactivated int
	Error                                                               sql.NullString
	FileName                                                            sql.NullString
	By                                                                  sql.NullString
	Rows                                                                []RowResult
}

const logCols = `l.id, l.entity_type, l.provider, l.trigger_type, l.status, l.attempt, l.started_at, l.finished_at,
	l.total_count, l.inserted, l.updated, l.unchanged, l.conflicts, l.failed, l.deactivated, l.error_message, l.file_name, u.full_name`

func scanLog(sc interface{ Scan(...any) error }, l *LogEntry) error {
	return sc.Scan(&l.ID, &l.Entity, &l.Provider, &l.Trigger, &l.Status, &l.Attempt, &l.StartedAt, &l.FinishedAt,
		&l.Total, &l.Inserted, &l.Updated, &l.Unchanged, &l.Conflicts, &l.Failed, &l.Deactivated, &l.Error, &l.FileName, &l.By)
}

// Logs mengembalikan log terbaru (entity kosong = semua).
func (s *Service) Logs(ctx context.Context, entity string, limit int) ([]LogEntry, error) {
	q := `SELECT ` + logCols + ` FROM integration_sync_logs l LEFT JOIN users u ON u.id = l.triggered_by`
	var args []any
	if entity != "" {
		q += ` WHERE l.entity_type = ?`
		args = append(args, entity)
	}
	q += ` ORDER BY l.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := scanLog(rows, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ErrNotFound: data tidak ditemukan.
var ErrNotFound = errors.New("data tidak ditemukan")

// Log mengembalikan satu log beserta rincian baris.
func (s *Service) Log(ctx context.Context, id int64) (LogEntry, error) {
	var l LogEntry
	var details sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT `+logCols+`, l.details FROM integration_sync_logs l LEFT JOIN users u ON u.id = l.triggered_by WHERE l.id = ?`, id).
		Scan(&l.ID, &l.Entity, &l.Provider, &l.Trigger, &l.Status, &l.Attempt, &l.StartedAt, &l.FinishedAt,
			&l.Total, &l.Inserted, &l.Updated, &l.Unchanged, &l.Conflicts, &l.Failed, &l.Deactivated, &l.Error, &l.FileName, &l.By, &details)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	if err == nil && details.Valid {
		_ = json.Unmarshal([]byte(details.String), &l.Rows)
	}
	return l, err
}

// maxDetailRows: rincian yang disimpan di log (konflik & error lebih dulu).
const maxDetailRows = 500

func detailsJSON(res Result) string {
	var keep []RowResult
	for _, pass := range []func(string) bool{
		func(a string) bool { return a == ActConflict || a == ActError },
		func(a string) bool { return a != ActConflict && a != ActError && a != ActUnchanged },
	} {
		for _, r := range res.Rows {
			if pass(r.Action) && len(keep) < maxDetailRows {
				keep = append(keep, r)
			}
		}
	}
	b, _ := json.Marshal(keep)
	return string(b)
}

func statusOf(res Result) string {
	if res.Conflicts > 0 || res.Failed > 0 {
		return "PARTIAL"
	}
	return "SUCCESS"
}

func (s *Service) startLog(ctx context.Context, entity, provider, trigger string, attempt int, file string, by int64, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO integration_sync_logs (config_id, entity_type, provider, trigger_type, attempt, status, started_at, file_name, triggered_by)
		VALUES ((SELECT id FROM integration_configs WHERE entity_type = ?), ?, ?, ?, ?, 'RUNNING', ?, ?, ?)`,
		entity, entity, provider, trigger, attempt, now, database.NullString(file), database.NullInt64(by))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) finishLog(ctx context.Context, id int64, status string, res Result, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE integration_sync_logs SET status = ?, finished_at = ?, total_count = ?, inserted = ?, updated = ?, unchanged = ?,
		       conflicts = ?, failed = ?, deactivated = ?, error_message = ?, details = ?
		 WHERE id = ?`, status, s.clock.Now(), res.Total, res.Inserted, res.Updated, res.Unchanged,
		res.Conflicts, res.Failed, res.Deactivated, database.NullString(errMsg), detailsJSON(res), id)
	return err
}

// ---------------------------------------------------------------------------
// Impor CSV: unggah → pratinjau → terapkan
// ---------------------------------------------------------------------------

// Import adalah file CSV yang menunggu diterapkan.
type Import struct {
	ID          int64
	Entity      string
	FileName    string
	MarkMissing bool
	Policy      string
	Status      string
	LogID       sql.NullInt64
	CreatedAt   time.Time
	content     []byte
}

// CreateImport menyimpan CSV yang diunggah setelah memastikan formatnya terbaca.
func (s *Service) CreateImport(ctx context.Context, by int64, entity, fileName string, data []byte, markMissing bool, policy string) (int64, error) {
	if !ValidEntity(entity) {
		return 0, ErrUnsupported
	}
	if policy != PreferSource {
		policy = KeepLocal
	}
	if _, err := ParseCSV(entity, data); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO integration_imports (entity_type, file_name, content, mark_missing, conflict_policy, uploaded_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, entity, clip(fileName, 200), data, markMissing, policy, by, s.clock.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// GetImport membaca impor.
func (s *Service) GetImport(ctx context.Context, id int64) (Import, error) {
	var im Import
	err := s.db.QueryRowContext(ctx, `SELECT id, entity_type, file_name, content, mark_missing, conflict_policy, status, log_id, created_at
		FROM integration_imports WHERE id = ?`, id).
		Scan(&im.ID, &im.Entity, &im.FileName, &im.content, &im.MarkMissing, &im.Policy, &im.Status, &im.LogID, &im.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return im, ErrNotFound
	}
	return im, err
}

// Preview menghitung hasil impor tanpa menyimpan.
func (s *Service) Preview(ctx context.Context, im Import) (Result, error) {
	b, err := ParseCSV(im.Entity, im.content)
	if err != nil {
		return Result{}, err
	}
	return Apply(ctx, s.db, b, Options{Source: "CSV", Policy: im.Policy, MarkMissing: im.MarkMissing, DryRun: true}, s.clock.Now())
}

// ErrImportClosed: impor sudah diterapkan atau dibatalkan.
var ErrImportClosed = errors.New("impor ini sudah diterapkan atau dibatalkan")

// ApplyImport menerapkan impor, mencatat log & audit, lalu membuang isi file.
func (s *Service) ApplyImport(ctx context.Context, id, by int64) (int64, error) {
	im, err := s.GetImport(ctx, id)
	if err != nil {
		return 0, err
	}
	if im.Status != "PREVIEW" {
		return 0, ErrImportClosed
	}
	b, err := ParseCSV(im.Entity, im.content)
	if err != nil {
		return 0, err
	}
	now := s.clock.Now()
	logID, err := s.startLog(ctx, im.Entity, ProviderCSV, "MANUAL", 1, im.FileName, by, now)
	if err != nil {
		return 0, err
	}
	res, err := Apply(ctx, s.db, b, Options{Source: "CSV", Policy: im.Policy, MarkMissing: im.MarkMissing}, now)
	if err != nil {
		_ = s.finishLog(ctx, logID, "FAILED", res, err.Error())
		return logID, err
	}
	if err := s.finishLog(ctx, logID, statusOf(res), res, ""); err != nil {
		return logID, err
	}
	return logID, database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE integration_imports SET status = 'APPLIED', applied_at = ?, log_id = ?, content = NULL WHERE id = ?`,
			now, logID, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE integration_configs SET last_run_at = ?, last_success_at = ? WHERE entity_type = ?`, now, now, im.Entity); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "integration.csv_import", EntityType: "integration_sync_logs", EntityID: logID,
			ActorUserID: by, After: map[string]any{"entity": im.Entity, "file": im.FileName, "inserted": res.Inserted, "updated": res.Updated,
				"conflicts": res.Conflicts, "failed": res.Failed, "deactivated": res.Deactivated, "policy": im.Policy, "mark_missing": im.MarkMissing}})
	})
}

// CancelImport membatalkan impor dan membuang isi file.
func (s *Service) CancelImport(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE integration_imports SET status = 'CANCELLED', content = NULL WHERE id = ? AND status = 'PREVIEW'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrImportClosed
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sinkronisasi terjadwal (provider SIMRS) dengan retry
// ---------------------------------------------------------------------------

// retryDelay: jeda percobaan ulang ke-n (5, 10, 20, ... menit).
func retryDelay(n int) time.Duration { return time.Duration(5<<max(n-1, 0)) * time.Minute }

// Sync mengambil data dari provider entitas lalu menerapkannya. Kegagalan
// mengambil data dicatat dan dijadwalkan ulang sampai max_retries.
func (s *Service) Sync(ctx context.Context, entity, trigger string, by int64) (int64, error) {
	var provider, policy string
	var retryCount, maxRetries int
	if err := s.db.QueryRowContext(ctx, `SELECT provider, conflict_policy, retry_count, max_retries FROM integration_configs WHERE entity_type = ?`, entity).
		Scan(&provider, &policy, &retryCount, &maxRetries); err != nil {
		return 0, err
	}
	if provider == ProviderLocal || provider == ProviderCSV {
		return 0, ErrLocalSource
	}
	p, ok := s.providers[provider]
	if !ok {
		return 0, fmt.Errorf("adapter %s belum tersedia (menunggu spesifikasi SIMRS)", provider)
	}
	now := s.clock.Now()
	attempt := 1
	if trigger == "RETRY" {
		attempt = retryCount + 1
	}
	logID, err := s.startLog(ctx, entity, provider, trigger, attempt, "", by, now)
	if err != nil {
		return 0, err
	}
	b, ferr := fetch(ctx, p, entity)
	if ferr != nil {
		next := retryCount + 1
		var nextAt any
		if next <= maxRetries {
			nextAt = now.Add(retryDelay(next))
		}
		msg := "Gagal mengambil data dari " + provider + ": " + ferr.Error()
		if nextAt == nil {
			msg += fmt.Sprintf(" — batas %d percobaan ulang tercapai; menunggu jadwal berikutnya atau sinkron manual.", maxRetries)
		}
		if err := s.finishLog(ctx, logID, "FAILED", Result{}, msg); err != nil {
			return logID, err
		}
		_, err := s.db.ExecContext(ctx, `UPDATE integration_configs SET retry_count = ?, next_retry_at = ?, last_run_at = ? WHERE entity_type = ?`,
			min(next, 255), nextAt, now, entity)
		s.log.Warn("sinkronisasi gagal", "entitas", entity, "provider", provider, "percobaan", attempt, "err", ferr)
		if err != nil {
			return logID, err
		}
		return logID, ferr
	}
	res, err := Apply(ctx, s.db, b, Options{Source: p.Source(), Policy: policy, MarkMissing: true}, now)
	if err != nil {
		_ = s.finishLog(ctx, logID, "FAILED", res, err.Error())
		return logID, err
	}
	if err := s.finishLog(ctx, logID, statusOf(res), res, ""); err != nil {
		return logID, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE integration_configs SET retry_count = 0, next_retry_at = NULL, last_run_at = ?, last_success_at = ? WHERE entity_type = ?`,
		now, now, entity)
	return logID, err
}

// RunDue menjalankan sinkron yang jatuh tempo (jadwal atau percobaan ulang).
// Dipanggil worker; tidak melakukan apa pun selama semua entitas bersumber lokal.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.clock.Now()
	rows, err := s.db.QueryContext(ctx, `
		SELECT entity_type, provider, next_retry_at IS NOT NULL AND next_retry_at <= ?
		  FROM integration_configs
		 WHERE is_enabled = TRUE AND provider IN ('SIMRS_API','SIMRS_DB')
		   AND ((next_retry_at IS NOT NULL AND next_retry_at <= ?)
		     OR (next_retry_at IS NULL AND schedule_minutes IS NOT NULL
		         AND (last_run_at IS NULL OR last_run_at <= DATE_SUB(?, INTERVAL schedule_minutes MINUTE))))`, now, now, now)
	if err != nil {
		return 0, err
	}
	type due struct {
		entity, provider string
		retry            bool
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.entity, &d.provider, &d.retry); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, d := range list {
		if !s.ProviderAvailable(d.provider) {
			continue
		}
		trigger := "SCHEDULE"
		if d.retry {
			trigger = "RETRY"
		}
		if _, err := s.Sync(ctx, d.entity, trigger, 0); err == nil {
			n++
		}
	}
	return n, nil
}

// SetProvider mengubah sumber data entitas (LOCAL, atau SIMRS bila adapter tersedia).
func (s *Service) SetProvider(ctx context.Context, by int64, entity, provider string, schedule int, policy string) error {
	if !ValidEntity(entity) {
		return ErrUnsupported
	}
	switch provider {
	case ProviderLocal:
	case ProviderSimrsAPI, ProviderSimrsDB:
		if !s.ProviderAvailable(provider) {
			return fmt.Errorf("adapter %s belum tersedia (menunggu spesifikasi SIMRS)", provider)
		}
	default:
		return ErrUnsupported
	}
	if policy != PreferSource {
		policy = KeepLocal
	}
	now := s.clock.Now()
	var sched any
	if schedule > 0 {
		sched = schedule
	}
	return database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE integration_configs SET provider = ?, is_enabled = ?, schedule_minutes = ?, conflict_policy = ?,
			retry_count = 0, next_retry_at = NULL, updated_by = ?, updated_at = ? WHERE entity_type = ?`,
			provider, provider != ProviderLocal, sched, policy, by, now, entity); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "integration.config", EntityType: "integration_configs", ActorUserID: by,
			After: map[string]any{"entity": entity, "provider": provider, "schedule_minutes": schedule, "policy": policy}})
	})
}

// Cleanup membuang isi file impor lama yang tidak diterapkan (7 hari).
func (s *Service) Cleanup(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE integration_imports SET status = 'CANCELLED', content = NULL
		WHERE status = 'PREVIEW' AND created_at < ?`, s.clock.Now().Add(-7*24*time.Hour))
	return err
}
