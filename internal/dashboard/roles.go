package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"sort"
	"strconv"
	"strings"
	"time"

	"sikat/internal/clock"
	"sikat/internal/rbac"
	"sikat/internal/reports"
	"sikat/internal/sla"
	"sikat/internal/tickets"
)

// Dashboard per role (master prompt §17). Semua angka dihitung langsung dari
// tabel transaksi saat halaman dibuka.

// TechView: dashboard petugas.
type TechView struct {
	Available, Mine, InProgress, Pending, DoneMonth, CriticalActive, NewCount int
	SLAAlerts                                                                 []sla.TicketRow
	Workload                                                                  []tickets.Workload
}

// DistRow adalah satu baris distribusi gangguan.
type DistRow struct {
	Name  string
	Count int
	Pct   int
}

// CoordView: dashboard koordinator.
type CoordView struct {
	MonthLabel string
	MonthQuery template.URL
	// Triage: tiket aktif di bidang Triase yang menunggu dipilah (ditampilkan untuk
	// koordinator triase, atau semua koordinator bila triase belum punya koordinator).
	TriageCount   int
	TriageFieldID int64
	// Fields: bidang yang dikoordinasi (kosong = belum ditetapkan → semua bidang).
	Fields                                             []string
	FieldQuery                                         template.URL // "&bidang_ids=…"
	MonthTotal, New, Working, Pending, DoneMonth       int
	CriticalActive, Breached, Activities, ActivityDone int
	Performance                                        reports.Table
	Distribution                                       []DistRow
	Workload                                           []tickets.Workload
}

// AdminView: dashboard admin (status sistem).
type AdminView struct {
	DBVersion        string
	DBOK             bool
	Migration        int64
	WorkerLeader     bool
	PushOn           bool
	OutboxPending    int
	OutboxFailed     int
	PushDevices      int
	MasterUpdatedAt  sql.NullTime
	Integration      string
	IntegrationOK    bool
	Backup           string
	BackupOK         bool
	LastAuditFailure sql.NullTime
	// FieldsWithoutCoordinator: bidang aktif tanpa koordinator aktif (notifikasi ke semua koordinator).
	FieldsWithoutCoordinator []string
}

func monthStart(now time.Time) time.Time {
	l := now.In(clock.Location)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, clock.Location).UTC()
}

const activeStatuses = `('CLAIMED','IN_PROGRESS','PENDING')`

func (h *Handler) techView(ctx context.Context, u *rbac.User, now time.Time) (TechView, error) {
	var v TechView
	ms := monthStart(now)
	err := h.DB.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM tickets WHERE status = 'NEW'
		          AND (NOT EXISTS (SELECT 1 FROM user_competencies WHERE user_id = ?)
		               OR it_category_id IN (SELECT it_category_id FROM user_competencies WHERE user_id = ?)
		               OR it_category_id IN (SELECT id FROM it_categories WHERE is_triage))),
		       (SELECT COUNT(*) FROM tickets WHERE assignee_id = ? AND status IN `+activeStatuses+`),
		       (SELECT COUNT(*) FROM tickets WHERE assignee_id = ? AND status = 'IN_PROGRESS'),
		       (SELECT COUNT(*) FROM tickets WHERE assignee_id = ? AND status = 'PENDING'),
		       (SELECT COUNT(DISTINCT ticket_id) FROM ticket_histories WHERE actor_user_id = ? AND event_type = 'RESOLVED' AND created_at >= ?),
		       (SELECT COUNT(*) FROM tickets WHERE priority = 'CRITICAL' AND status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING')),
		       (SELECT COUNT(*) FROM tickets WHERE status = 'NEW')`,
		u.ID, u.ID, u.ID, u.ID, u.ID, u.ID, ms).
		Scan(&v.Available, &v.Mine, &v.InProgress, &v.Pending, &v.DoneMonth, &v.CriticalActive, &v.NewCount)
	if err != nil {
		return v, err
	}
	// SLA mendekati batas: tiket saya + tiket baru yang tersedia untuk saya.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT id FROM tickets
		 WHERE (assignee_id = ? AND status IN `+activeStatuses+`)
		    OR (status = 'NEW' AND (NOT EXISTS (SELECT 1 FROM user_competencies WHERE user_id = ?)
		        OR it_category_id IN (SELECT it_category_id FROM user_competencies WHERE user_id = ?)
		        OR it_category_id IN (SELECT id FROM it_categories WHERE is_triage)))
		 ORDER BY id DESC LIMIT 300`, u.ID, u.ID, u.ID)
	if err != nil {
		return v, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return v, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return v, err
	}
	m, err := h.Tickets.SLA().TicketSLA(ctx, ids)
	if err != nil {
		return v, err
	}
	for _, r := range m {
		if st := r.Current().State; st == sla.StateWarning || st == sla.StateLate {
			v.SLAAlerts = append(v.SLAAlerts, r)
		}
	}
	sort.Slice(v.SLAAlerts, func(i, j int) bool {
		return v.SLAAlerts[i].Current().Remaining < v.SLAAlerts[j].Current().Remaining
	})
	if len(v.SLAAlerts) > 8 {
		v.SLAAlerts = v.SLAAlerts[:8]
	}
	v.Workload, err = h.Tickets.Workload(ctx)
	return v, err
}

func (h *Handler) coordView(ctx context.Context, u *rbac.User, now time.Time) (CoordView, error) {
	var v CoordView
	ms := monthStart(now)
	f, _ := reports.ParseFilter(nil, now)
	v.MonthLabel, v.MonthQuery = f.PeriodLabel(), f.QueryString()
	from := ms.In(clock.Location).Format("2006-01-02")

	// Koordinator per bidang: angka dibatasi ke bidang yang dikoordinasinya.
	rows, err := h.DB.QueryContext(ctx, `SELECT id, name FROM it_categories WHERE coordinator_user_id = ? ORDER BY sort_order, name`, u.ID)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return v, err
		}
		f.FieldIDs = append(f.FieldIDs, id)
		v.Fields = append(v.Fields, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return v, err
	}
	tc, ac, hc := "TRUE", "TRUE", "TRUE"
	var fa []any
	if len(f.FieldIDs) > 0 {
		in := "(SELECT id FROM it_categories WHERE coordinator_user_id = ?)"
		tc = "it_category_id IN " + in
		ac = "it_category_id IN " + in
		hc = "ticket_id IN (SELECT id FROM tickets WHERE it_category_id IN " + in + ")"
		fa = []any{u.ID}
		for _, id := range f.FieldIDs {
			v.FieldQuery += template.URL("&bidang_ids=" + strconv.FormatInt(id, 10))
		}
	}
	args := func(vals ...any) []any {
		var out []any
		for _, x := range vals {
			out = append(out, x)
			out = append(out, fa...)
		}
		return out
	}
	q := `
		SELECT (SELECT COUNT(*) FROM tickets WHERE created_at >= ? AND ` + tc + `),
		       (SELECT COUNT(*) FROM tickets WHERE status = 'NEW' AND ? AND ` + tc + `),
		       (SELECT COUNT(*) FROM tickets WHERE status IN ('CLAIMED','IN_PROGRESS') AND ? AND ` + tc + `),
		       (SELECT COUNT(*) FROM tickets WHERE status = 'PENDING' AND ? AND ` + tc + `),
		       (SELECT COUNT(DISTINCT ticket_id) FROM ticket_histories WHERE event_type = 'RESOLVED' AND created_at >= ? AND ` + hc + `),
		       (SELECT COUNT(*) FROM tickets WHERE priority = 'CRITICAL' AND status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING') AND ? AND ` + tc + `),
		       (SELECT COUNT(DISTINCT ticket_id) FROM sla_escalations WHERE kind IN ('RESPONSE_BREACH','RESOLUTION_BREACH') AND triggered_at >= ? AND ` + hc + `),
		       (SELECT COUNT(*) FROM internal_activities WHERE activity_date >= ? AND status <> 'CANCELLED' AND ` + ac + `),
		       (SELECT COUNT(*) FROM internal_activities WHERE activity_date >= ? AND status = 'DONE' AND ` + ac + `)`
	err = h.DB.QueryRowContext(ctx, q, args(ms, true, true, true, ms, true, ms, from, from)...).
		Scan(&v.MonthTotal, &v.New, &v.Working, &v.Pending, &v.DoneMonth, &v.CriticalActive, &v.Breached, &v.Activities, &v.ActivityDone)
	if err != nil {
		return v, err
	}
	var tf sql.NullInt64
	var tcoord sql.NullInt64
	var tcount int
	err = h.DB.QueryRowContext(ctx, `
		SELECT ic.id,
		       (SELECT u.id FROM users u JOIN roles r ON r.id = u.role_id
		         WHERE u.id = ic.coordinator_user_id AND u.is_active AND r.code = 'COORDINATOR'),
		       (SELECT COUNT(*) FROM tickets t WHERE t.it_category_id = ic.id AND t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING'))
		  FROM it_categories ic WHERE ic.is_triage AND ic.is_active`).Scan(&tf, &tcoord, &tcount)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	if tf.Valid && (!tcoord.Valid || tcoord.Int64 == u.ID) {
		v.TriageCount, v.TriageFieldID = tcount, tf.Int64
	}
	if d, ok := reports.Find("petugas"); ok {
		if v.Performance, err = h.Reports.Run(ctx, d, f); err != nil {
			return v, err
		}
	}
	dq := `SELECT ic.name, COUNT(*) FROM tickets t JOIN it_categories ic ON ic.id = t.it_category_id WHERE t.created_at >= ?`
	if len(f.FieldIDs) > 0 {
		dq += ` AND ic.coordinator_user_id = ?`
	}
	rows, err = h.DB.QueryContext(ctx, dq+` GROUP BY ic.id, ic.name ORDER BY COUNT(*) DESC, ic.name`, args(ms)...)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var d DistRow
		if err := rows.Scan(&d.Name, &d.Count); err != nil {
			return v, err
		}
		v.Distribution = append(v.Distribution, d)
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	for i := range v.Distribution {
		if v.MonthTotal > 0 {
			v.Distribution[i].Pct = v.Distribution[i].Count * 100 / v.MonthTotal
		}
	}
	v.Workload, err = h.Tickets.Workload(ctx)
	return v, err
}

func (h *Handler) adminView(ctx context.Context) (AdminView, error) {
	v := AdminView{
		Integration: "SIMRS belum terhubung — integrasi direncanakan lewat API SIMRS (menunggu spesifikasi). Master data dikelola lokal di Master Data.",
		Backup:      "Belum ada backup. Buka menu Backup untuk membuat backup pertama dan menguji restore.",
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	v.DBOK = h.DB.PingContext(pctx) == nil
	if !v.DBOK {
		return v, nil
	}
	if err := h.DB.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&v.DBVersion); err != nil {
		return v, err
	}
	if err := h.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&v.Migration); err != nil {
		return v, err
	}
	var err error
	if err = h.DB.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM outbox_events WHERE status = 'PENDING'),
		       (SELECT COUNT(*) FROM outbox_events WHERE status = 'FAILED'),
		       (SELECT COUNT(*) FROM push_subscriptions WHERE disabled_at IS NULL)`).
		Scan(&v.OutboxPending, &v.OutboxFailed, &v.PushDevices); err != nil {
		return v, err
	}
	// Integrasi: sumber SIMRS aktif & hasil sinkron terakhirnya, atau impor CSV terakhir.
	var simrs int
	var lastStatus, lastProv sql.NullString
	var lastAt sql.NullTime
	if err = h.DB.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM integration_configs WHERE provider IN ('SIMRS_API','SIMRS_DB') AND is_enabled),
		       (SELECT status FROM integration_sync_logs ORDER BY id DESC LIMIT 1),
		       (SELECT provider FROM integration_sync_logs ORDER BY id DESC LIMIT 1),
		       (SELECT started_at FROM integration_sync_logs ORDER BY id DESC LIMIT 1)`).Scan(&simrs, &lastStatus, &lastProv, &lastAt); err != nil {
		return v, err
	}
	if simrs > 0 {
		v.IntegrationOK = lastStatus.String == "SUCCESS"
		v.Integration = fmt.Sprintf("%d entitas tersinkron dari SIMRS; sinkron terakhir: %s", simrs, strings.ToLower(lastStatus.String))
	} else if lastAt.Valid {
		v.Integration += " Impor terakhir " + lastAt.Time.In(clock.Location).Format("02/01/2006 15:04") + " WITA."
	}
	// Backup terakhir & uji restore terakhir.
	var bStatus, bFile, rStatus sql.NullString
	var bAt, rAt sql.NullTime
	if err = h.DB.QueryRowContext(ctx, `
		SELECT (SELECT status FROM backup_logs ORDER BY id DESC LIMIT 1),
		       (SELECT file_name FROM backup_logs WHERE status = 'SUCCESS' ORDER BY id DESC LIMIT 1),
		       (SELECT started_at FROM backup_logs WHERE status = 'SUCCESS' ORDER BY id DESC LIMIT 1),
		       (SELECT restore_status FROM backup_logs WHERE restore_tested_at IS NOT NULL ORDER BY restore_tested_at DESC LIMIT 1),
		       (SELECT MAX(restore_tested_at) FROM backup_logs)`).Scan(&bStatus, &bFile, &bAt, &rStatus, &rAt); err != nil {
		return v, err
	}
	if bAt.Valid {
		age := h.now().Sub(bAt.Time)
		v.BackupOK = age < 26*time.Hour && bStatus.String != "FAILED"
		v.Backup = "Backup berhasil terakhir " + bAt.Time.In(clock.Location).Format("02/01/2006 15:04") + " WITA"
		if bStatus.String == "FAILED" {
			v.Backup += "; backup terbaru GAGAL"
		} else if age >= 26*time.Hour {
			v.Backup += " (lebih dari sehari lalu)"
		}
		if rAt.Valid {
			v.Backup += "; uji restore " + strings.ToLower(map[string]string{"SUCCESS": "berhasil", "FAILED": "GAGAL"}[rStatus.String]) +
				" " + rAt.Time.In(clock.Location).Format("02/01/2006")
		} else {
			v.Backup += "; belum pernah diuji restore"
		}
	} else if bStatus.String == "FAILED" {
		v.Backup = "Backup gagal — periksa riwayat di menu Backup."
	}
	if v.FieldsWithoutCoordinator, err = FieldsWithoutCoordinator(ctx, h.DB); err != nil {
		return v, err
	}
	if h.WorkerLeader != nil {
		v.WorkerLeader = h.WorkerLeader()
	}
	if h.PushEnabled != nil {
		v.PushOn = h.PushEnabled()
	}
	return v, nil
}

// FieldsWithoutCoordinator mengembalikan nama bidang aktif yang belum memiliki
// koordinator aktif ber-role Koordinator.
func FieldsWithoutCoordinator(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ic.name FROM it_categories ic
		  LEFT JOIN users u ON u.id = ic.coordinator_user_id AND u.is_active = TRUE
		  LEFT JOIN roles r ON r.id = u.role_id AND r.code = 'COORDINATOR'
		 WHERE ic.is_active = TRUE AND r.id IS NULL
		 ORDER BY ic.sort_order, ic.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (h *Handler) now() time.Time {
	if h.Reports != nil {
		return h.Reports.Now()
	}
	return time.Now()
}
