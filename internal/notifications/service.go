package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sikat/internal/clock"
	"sikat/internal/database"
)

// Batas & jeda pemrosesan.
const (
	maxEventAttempts = 5
	maxPushAttempts  = 5
	// pushTTL: push yang belum terkirim setelah selama ini tidak dikirim lagi
	// (informasi sudah tersedia di inbox; notifikasi basi hanya mengganggu).
	pushTTL   = time.Hour
	batchSize = 50
)

// Service memproses outbox, inbox, langganan push, dan preferensi.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	log   *slog.Logger
	// sender nil = Web Push tidak dikonfigurasi (inbox tetap berjalan).
	sender   Sender
	vapidKey string
}

// NewService membuat Service. sender boleh nil.
func NewService(db *sql.DB, c clock.Clock, log *slog.Logger, sender Sender, vapidPublicKey string) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, clock: c, log: log, sender: sender, vapidKey: vapidPublicKey}
}

// PushEnabled bernilai true bila kunci VAPID dikonfigurasi.
func (s *Service) PushEnabled() bool { return s.sender != nil && s.vapidKey != "" }

// VAPIDPublicKey untuk pushManager.subscribe di browser.
func (s *Service) VAPIDPublicKey() string { return s.vapidKey }

// ---------------------------------------------------------------------------
// Fan-out outbox → inbox (+ antrean push)
// ---------------------------------------------------------------------------

// ProcessResult adalah ringkasan satu putaran pemrosesan.
type ProcessResult struct {
	Events, Notifications, Deliveries, Failed int
}

// Process memproses event PENDING yang sudah jatuh tempo. Setiap event diproses
// dalam transaksinya sendiri; event yang gagal dicoba lagi dengan jeda.
func (s *Service) Process(ctx context.Context) (ProcessResult, error) {
	var res ProcessResult
	now := s.clock.Now()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM outbox_events WHERE status = 'PENDING' AND available_at <= ? ORDER BY id LIMIT ?`, now, batchSize)
	if err != nil {
		return res, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return res, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		var n, d int
		err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
			var err error
			n, d, err = s.fanOut(ctx, tx, id, now)
			return err
		})
		if err != nil {
			res.Failed++
			if ferr := s.failEvent(ctx, id, err, now); ferr != nil {
				return res, ferr
			}
			continue
		}
		res.Events++
		res.Notifications += n
		res.Deliveries += d
	}
	return res, nil
}

func (s *Service) failEvent(ctx context.Context, id int64, cause error, now time.Time) error {
	s.log.Error("notifikasi: event gagal diproses", "event_id", id, "err", cause)
	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	// Jeda bertambah: 30 dtk, 1, 2, 4 menit; FAILED setelah batas percobaan.
	_, err := s.db.ExecContext(ctx, `
		UPDATE outbox_events SET attempts = attempts + 1, last_error = ?,
		       available_at = DATE_ADD(?, INTERVAL 30 * POW(2, attempts) SECOND),
		       status = IF(attempts >= ?, 'FAILED', 'PENDING')
		 WHERE id = ? AND status = 'PENDING'`, msg, now, maxEventAttempts, id)
	return err
}

type outboxRow struct {
	ID       int64
	Type     string
	TicketID sql.NullInt64
	AssetID  sql.NullInt64
	ActorID  sql.NullInt64
	TargetID sql.NullInt64
	Payload  map[string]any
}

type ticketInfo struct {
	No, Priority, Status, Category string
	ITCategoryID                   int64
	FieldName                      string
	IsTriage                       bool
	Department, Room, Asset        sql.NullString
}

func (t ticketInfo) location() string {
	var parts []string
	if t.Room.Valid {
		parts = append(parts, t.Room.String)
	}
	if t.Department.Valid && (!t.Room.Valid || !strings.Contains(t.Room.String, t.Department.String)) {
		parts = append(parts, t.Department.String)
	}
	return strings.Join(parts, ", ")
}

// recipient adalah satu penerima beserta kanal yang dipakai.
type recipient struct {
	UserID  int64
	Channel string
}

// fanOut menentukan penerima event, menulis inbox, dan menjadwalkan push.
func (s *Service) fanOut(ctx context.Context, tx *sql.Tx, id int64, now time.Time) (int, int, error) {
	var ev outboxRow
	var payload sql.NullString
	var status string
	err := tx.QueryRowContext(ctx, `
		SELECT id, event_type, ticket_id, asset_id, actor_user_id, target_user_id, payload, status
		  FROM outbox_events WHERE id = ? FOR UPDATE`, id).
		Scan(&ev.ID, &ev.Type, &ev.TicketID, &ev.AssetID, &ev.ActorID, &ev.TargetID, &payload, &status)
	if err != nil {
		return 0, 0, err
	}
	if status != "PENDING" {
		return 0, 0, nil // sudah diproses (mis. oleh putaran lain)
	}
	ev.Payload = map[string]any{}
	if payload.Valid {
		_ = json.Unmarshal([]byte(payload.String), &ev.Payload)
	}

	var ti *ticketInfo
	if ev.TicketID.Valid {
		ti = &ticketInfo{}
		err := tx.QueryRowContext(ctx, `
			SELECT t.ticket_no, t.priority, t.status, tc.name, t.it_category_id, ic.name, ic.is_triage, d.name, r.name, a.name
			  FROM tickets t
			  JOIN ticket_categories tc ON tc.id = t.ticket_category_id
			  JOIN it_categories ic ON ic.id = t.it_category_id
			  LEFT JOIN departments d ON d.id = t.department_id
			  LEFT JOIN rooms r ON r.id = t.room_id
			  LEFT JOIN assets a ON a.id = t.asset_id
			 WHERE t.id = ?`, ev.TicketID.Int64).
			Scan(&ti.No, &ti.Priority, &ti.Status, &ti.Category, &ti.ITCategoryID, &ti.FieldName, &ti.IsTriage, &ti.Department, &ti.Room, &ti.Asset)
		if err != nil {
			return 0, 0, fmt.Errorf("tiket event: %w", err)
		}
	}
	var actorName string
	if ev.ActorID.Valid {
		_ = tx.QueryRowContext(ctx, `SELECT full_name FROM users WHERE id = ?`, ev.ActorID.Int64).Scan(&actorName)
	}
	var assetName string
	if ev.AssetID.Valid {
		_ = tx.QueryRowContext(ctx, `SELECT CONCAT(name, ' (', asset_code, ')') FROM assets WHERE id = ?`, ev.AssetID.Int64).Scan(&assetName)
	}

	c := compose(ev, ti, actorName, assetName)
	recips, err := s.recipients(ctx, tx, ev, ti)
	if err != nil {
		return 0, 0, err
	}

	var nNotif, nDeliv int
	for _, r := range recips {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO notifications (user_id, event_id, event_type, title, body, push_body, url, ticket_id, channel, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE id = id`,
			r.UserID, ev.ID, ev.Type, clip(c.Title, 150), clip(c.Body, 500), clip(c.PushBody, 200), c.URL,
			ev.TicketID, r.Channel, now)
		if err != nil {
			return 0, 0, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			continue
		}
		nNotif++
		if r.Channel != ChannelPush || !s.PushEnabled() {
			continue
		}
		notifID, _ := res.LastInsertId()
		dres, err := tx.ExecContext(ctx, `
			INSERT INTO push_deliveries (notification_id, subscription_id, next_attempt_at, created_at)
			SELECT ?, id, ?, ? FROM push_subscriptions WHERE user_id = ? AND disabled_at IS NULL`,
			notifID, now, now, r.UserID)
		if err != nil {
			return 0, 0, err
		}
		n, _ := dres.RowsAffected()
		nDeliv += int(n)
	}
	_, err = tx.ExecContext(ctx, `UPDATE outbox_events SET status = 'DONE', processed_at = ?, recipients = ?, last_error = NULL WHERE id = ?`,
		now, nNotif, ev.ID)
	return nNotif, nDeliv, err
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// recipients: Koordinator & Admin menerima sesuai preferensi role; Petugas
// dibatasi relevansi (kompetensi atau keanggotaan tim tiket). Pelaku tidak
// menerima notifikasi atas tindakannya sendiri.
func (s *Service) recipients(ctx context.Context, tx *sql.Tx, ev outboxRow, ti *ticketInfo) ([]recipient, error) {
	if ev.Type == Test {
		if !ev.TargetID.Valid {
			return nil, nil
		}
		return []recipient{{UserID: ev.TargetID.Int64, Channel: ChannelPush}}, nil
	}
	// Kanal per role untuk event ini.
	prefs := map[string]string{}
	rows, err := tx.QueryContext(ctx, `
		SELECT r.code, p.channel FROM notification_preferences p JOIN roles r ON r.id = p.role_id
		 WHERE p.event_type = ? AND p.channel <> 'NONE'`, ev.Type)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var role, ch string
		if err := rows.Scan(&role, &ch); err != nil {
			rows.Close()
			return nil, err
		}
		prefs[role] = ch
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(prefs) == 0 {
		return nil, nil
	}

	audience := audienceFor(ev, ti)
	var techScope string
	var techArgs []any
	switch {
	case audience == audTarget:
		if !ev.TargetID.Valid {
			return nil, nil
		}
	case audience == audPool && ti != nil && ti.IsTriage:
		// Tiket triase (di luar daftar) dikirim ke semua petugas agar cepat direspons.
	case audience == audPool && ti != nil:
		techScope = ` AND (NOT EXISTS (SELECT 1 FROM user_competencies c WHERE c.user_id = u.id)
		                  OR EXISTS (SELECT 1 FROM user_competencies c WHERE c.user_id = u.id AND c.it_category_id = ?))`
		techArgs = []any{ti.ITCategoryID}
	case audience == audTeam && ev.TicketID.Valid:
		techScope = ` AND EXISTS (SELECT 1 FROM ticket_assignments ta WHERE ta.ticket_id = ? AND ta.user_id = u.id AND ta.released_at IS NULL)`
		techArgs = []any{ev.TicketID.Int64}
	case audience == audAll:
	default:
		return nil, nil
	}

	q := `SELECT u.id, r.code FROM users u JOIN roles r ON r.id = u.role_id WHERE u.is_active = TRUE`
	var args []any
	if audience == audTarget {
		q += ` AND u.id = ?`
		args = append(args, ev.TargetID.Int64)
	}
	if ev.ActorID.Valid {
		q += ` AND u.id <> ?`
		args = append(args, ev.ActorID.Int64)
	}
	// Koordinator per bidang: hanya koordinator bidang tiket (ditambah koordinator
	// yang menjadi anggota tim tiket). Bidang tanpa koordinator aktif → semua koordinator.
	coordScope := ""
	var coordArgs []any
	if ti != nil && audience != audTarget {
		var cid sql.NullInt64
		err := tx.QueryRowContext(ctx, `
			SELECT ic.coordinator_user_id FROM it_categories ic
			  JOIN users cu ON cu.id = ic.coordinator_user_id AND cu.is_active = TRUE
			  JOIN roles cr ON cr.id = cu.role_id AND cr.code = 'COORDINATOR'
			 WHERE ic.id = ?`, ti.ITCategoryID).Scan(&cid)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if cid.Valid {
			coordScope = ` AND (u.id = ? OR EXISTS (SELECT 1 FROM ticket_assignments ta2
				WHERE ta2.ticket_id = ? AND ta2.user_id = u.id AND ta2.released_at IS NULL))`
			coordArgs = []any{cid.Int64, ev.TicketID.Int64}
		}
	}
	// Petugas & koordinator dibatasi cakupan; role lain (Admin): semua anggota role.
	q += ` AND CASE r.code WHEN 'TECHNICIAN' THEN (TRUE` + techScope + `)
	                       WHEN 'COORDINATOR' THEN (TRUE` + coordScope + `)
	                       ELSE TRUE END`
	args = append(args, techArgs...)
	args = append(args, coordArgs...)
	q += ` ORDER BY u.id`

	rows, err = tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recipient
	for rows.Next() {
		var id int64
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, err
		}
		if ch, ok := prefs[role]; ok {
			out = append(out, recipient{UserID: id, Channel: ch})
		}
	}
	return out, rows.Err()
}

type audience int

const (
	audNone   audience = iota
	audAll             // semua anggota role
	audPool            // petugas sesuai kompetensi
	audTeam            // anggota tim tiket
	audTarget          // satu orang
)

func audienceFor(ev outboxRow, ti *ticketInfo) audience {
	switch ev.Type {
	case TicketTransferred:
		if ti != nil && ti.Status == "NEW" {
			return audPool
		}
		return audTeam
	case TicketCreated, TicketCritical, TicketClaimed:
		return audPool
	case TicketStarted, TicketPending, TicketResolved, TicketReopened:
		return audTeam
	case TicketAssigned:
		return audTarget
	case SLAWarning, SLABreach:
		if k, _ := ev.Payload["kind"].(string); strings.HasPrefix(k, "RESPONSE") {
			return audPool
		}
		return audTeam
	case RepeatDamage, LicenseExpiry, ProposalSubmitted:
		return audAll
	}
	return audNone
}

// ---------------------------------------------------------------------------
// Pengiriman push
// ---------------------------------------------------------------------------

// PushResult adalah ringkasan satu putaran pengiriman push.
type PushResult struct {
	Sent, Retry, Failed, Gone, Expired int
}

// SendPush mengirim push yang jatuh tempo. 404/410 → langganan dinonaktifkan;
// 429/5xx/jaringan → dicoba lagi dengan jeda; error lain → gagal permanen.
func (s *Service) SendPush(ctx context.Context) (PushResult, error) {
	var res PushResult
	if !s.PushEnabled() {
		return res, nil
	}
	now := s.clock.Now()
	// Push basi tidak dikirim lagi.
	r, err := s.db.ExecContext(ctx, `
		UPDATE push_deliveries d JOIN notifications n ON n.id = d.notification_id
		   SET d.status = 'EXPIRED'
		 WHERE d.status = 'PENDING' AND n.created_at < ?`, now.Add(-pushTTL))
	if err != nil {
		return res, err
	}
	if n, _ := r.RowsAffected(); n > 0 {
		res.Expired = int(n)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.attempts, s.id, s.endpoint, s.p256dh, s.auth_secret,
		       n.id, n.event_type, n.push_body, n.ticket_id, n.created_at
		  FROM push_deliveries d
		  JOIN push_subscriptions s ON s.id = d.subscription_id AND s.disabled_at IS NULL
		  JOIN notifications n ON n.id = d.notification_id
		 WHERE d.status = 'PENDING' AND d.next_attempt_at <= ?
		 ORDER BY d.id LIMIT ?`, now, batchSize)
	if err != nil {
		return res, err
	}
	type job struct {
		id, notifID int64
		attempts    int
		sub         Subscription
		event, body string
		ticketID    sql.NullInt64
		created     time.Time
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.attempts, &j.sub.ID, &j.sub.Endpoint, &j.sub.P256dh, &j.sub.Auth,
			&j.notifID, &j.event, &j.body, &j.ticketID, &j.created); err != nil {
			rows.Close()
			return res, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		msg := PushMessage(j.event, j.body, j.notifID, j.ticketID.Int64)
		ttl := pushTTL - now.Sub(j.created)
		status, sendErr := s.sender.Send(ctx, j.sub, msg, ttl)
		outcome := classify(status, sendErr)
		if err := s.recordDelivery(ctx, j.id, j.sub.ID, j.attempts, status, sendErr, outcome, now); err != nil {
			return res, err
		}
		switch outcome {
		case outcomeSent:
			res.Sent++
		case outcomeRetry:
			if j.attempts+1 >= maxPushAttempts {
				res.Failed++
			} else {
				res.Retry++
			}
		case outcomeGone:
			res.Gone++
		default:
			res.Failed++
		}
	}
	return res, nil
}

type outcome int

const (
	outcomeSent outcome = iota
	outcomeRetry
	outcomeGone   // langganan tidak berlaku lagi (404/410)
	outcomeReject // ditolak permanen (400, 413, dll.)
	outcomeKey    // kunci VAPID tidak cocok dengan langganan (401/403)
)

func classify(status int, err error) outcome {
	switch {
	case err != nil && status == 0:
		// Error jaringan → coba lagi. Error validasi lokal (endpoint/kunci rusak) → tolak.
		if errors.Is(err, errInvalidSub) {
			return outcomeReject
		}
		return outcomeRetry
	case status >= 200 && status < 300:
		return outcomeSent
	case status == 404 || status == 410:
		return outcomeGone
	case status == 401 || status == 403:
		return outcomeKey
	case status == 429 || status >= 500:
		return outcomeRetry
	default:
		return outcomeReject
	}
}

var errInvalidSub = errors.New("langganan push tidak valid")

func (s *Service) recordDelivery(ctx context.Context, id, subID int64, attempts, status int, sendErr error, o outcome, now time.Time) error {
	errText := ""
	if sendErr != nil {
		errText = clip(sendErr.Error(), 255)
	} else if o != outcomeSent {
		errText = fmt.Sprintf("HTTP %d", status)
	}
	var st any
	if status > 0 {
		st = status
	}
	switch o {
	case outcomeSent:
		if _, err := s.db.ExecContext(ctx, `UPDATE push_deliveries SET status = 'SENT', attempts = attempts + 1, last_status = ?, last_error = NULL, sent_at = ? WHERE id = ?`, st, now, id); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx, `UPDATE push_subscriptions SET last_success_at = ?, failure_count = 0, last_error = NULL WHERE id = ?`, now, subID)
		return err
	case outcomeRetry:
		next := now.Add(time.Duration(30<<attempts) * time.Second) // 30 dtk, 1, 2, 4 menit
		if _, err := s.db.ExecContext(ctx, `
			UPDATE push_deliveries SET attempts = attempts + 1, last_status = ?, last_error = ?, next_attempt_at = ?,
			       status = IF(attempts >= ?, 'FAILED', 'PENDING')
			 WHERE id = ?`, st, errText, next, maxPushAttempts, id); err != nil {
			return err
		}
	case outcomeGone, outcomeKey:
		dstatus, reason := "GONE", "Langganan kedaluwarsa atau dicabut"
		if o == outcomeKey {
			dstatus, reason = "FAILED", "Kunci server berubah; aktifkan ulang di perangkat"
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE push_deliveries SET status = ?, attempts = attempts + 1, last_status = ?, last_error = ? WHERE id = ?`, dstatus, st, errText, id); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE push_subscriptions SET disabled_at = ?, disabled_reason = ? WHERE id = ? AND disabled_at IS NULL`, now, reason, subID); err != nil {
			return err
		}
		// Push lain yang masih menunggu untuk perangkat ini tidak perlu dicoba.
		_, err := s.db.ExecContext(ctx, `UPDATE push_deliveries SET status = ? WHERE subscription_id = ? AND status = 'PENDING'`, dstatus, subID)
		return err
	default:
		if _, err := s.db.ExecContext(ctx, `UPDATE push_deliveries SET status = 'FAILED', attempts = attempts + 1, last_status = ?, last_error = ? WHERE id = ?`, st, errText, id); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE push_subscriptions SET failure_count = failure_count + 1, last_error = ? WHERE id = ?`, errText, subID)
	return err
}

// PushMessage menyusun isi push untuk lock screen: judul tetap "SIKAT", isi
// generik (push_body), tanpa lokasi, deskripsi, atau nama. Detail hanya
// terlihat setelah membuka aplikasi (login).
func PushMessage(eventType, body string, notifID, ticketID int64) Message {
	tag := fmt.Sprintf("sikat-n%d", notifID)
	if ticketID != 0 {
		tag = fmt.Sprintf("sikat-t%d", ticketID) // kabar terbaru tiket menggantikan yang lama
	}
	return Message{
		Title: "SIKAT", Body: body, URL: fmt.Sprintf("/app/notifications/%d", notifID), Tag: tag,
		Urgent: eventType == TicketCritical || eventType == SLABreach,
	}
}

// ---------------------------------------------------------------------------
// Pemeliharaan data
// ---------------------------------------------------------------------------

// Retention: notifikasi & event lebih tua dari ini dihapus.
const Retention = 180 * 24 * time.Hour

// Cleanup menghapus notifikasi lama, event selesai yang tidak lagi dirujuk,
// dan langganan yang sudah lama nonaktif.
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	cut := s.clock.Now().Add(-Retention)
	var total int64
	for _, q := range []string{
		`DELETE FROM notifications WHERE created_at < ? LIMIT 5000`,
		`DELETE FROM outbox_events WHERE status <> 'PENDING' AND created_at < ?
		   AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.event_id = outbox_events.id) LIMIT 5000`,
		`DELETE FROM push_subscriptions WHERE disabled_at IS NOT NULL AND disabled_at < ?`,
	} {
		res, err := s.db.ExecContext(ctx, q, cut)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}
