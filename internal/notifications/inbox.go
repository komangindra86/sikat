package notifications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"sikat/internal/audit"
	"sikat/internal/database"
	"sikat/internal/rbac"
)

// ---------------------------------------------------------------------------
// Inbox
// ---------------------------------------------------------------------------

// Item adalah satu notifikasi di inbox.
type Item struct {
	ID        int64
	EventType string
	Title     string
	Body      string
	URL       string
	Channel   string
	ReadAt    sql.NullTime
	CreatedAt time.Time
}

// Unread menghitung notifikasi belum dibaca (badge). Error dianggap 0 agar
// badge tidak pernah menggagalkan halaman.
func (s *Service) Unread(ctx context.Context, userID int64) int {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n); err != nil {
		s.log.Warn("notifikasi: hitung belum dibaca", "err", err)
		return 0
	}
	return n
}

// PageSize adalah jumlah notifikasi per halaman inbox.
const PageSize = 30

// List mengembalikan notifikasi terbaru (sebelum ID tertentu bila before > 0).
func (s *Service) List(ctx context.Context, userID int64, unreadOnly bool, before int64) ([]Item, bool, error) {
	q := `SELECT id, event_type, title, body, url, channel, read_at, created_at FROM notifications WHERE user_id = ?`
	args := []any{userID}
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	if before > 0 {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, PageSize+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.EventType, &it.Title, &it.Body, &it.URL, &it.Channel, &it.ReadAt, &it.CreatedAt); err != nil {
			return nil, false, err
		}
		out = append(out, it)
	}
	more := len(out) > PageSize
	if more {
		out = out[:PageSize]
	}
	return out, more, rows.Err()
}

// ErrNotFound: notifikasi tidak ada atau milik orang lain.
var ErrNotFound = errors.New("notifikasi tidak ditemukan")

// Open menandai notifikasi dibaca dan mengembalikan tujuan tautannya.
func (s *Service) Open(ctx context.Context, userID, id int64) (string, error) {
	var target string
	err := s.db.QueryRowContext(ctx, `SELECT url FROM notifications WHERE id = ? AND user_id = ?`, id, userID).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE id = ? AND read_at IS NULL`, s.clock.Now(), id); err != nil {
		return "", err
	}
	if !strings.HasPrefix(target, "/app/") {
		target = "/app/notifications"
	}
	return target, nil
}

// ReadAll menandai semua notifikasi pengguna sudah dibaca.
func (s *Service) ReadAll(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, s.clock.Now(), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkTicketRead menandai notifikasi sebuah tiket dibaca saat detail tiket dibuka.
func (s *Service) MarkTicketRead(ctx context.Context, userID, ticketID int64) {
	if _, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND ticket_id = ? AND read_at IS NULL`,
		s.clock.Now(), userID, ticketID); err != nil {
		s.log.Warn("notifikasi: tandai dibaca", "err", err)
	}
}

// ---------------------------------------------------------------------------
// Langganan push (perangkat)
// ---------------------------------------------------------------------------

// SubscribeInput adalah PushSubscription dari browser.
type SubscribeInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	UserAgent string `json:"-"`
}

// ErrInvalidSubscription: data langganan dari browser tidak valid.
var ErrInvalidSubscription = errors.New("data langganan push tidak valid")

func (in SubscribeInput) validate() error {
	u, err := url.Parse(in.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(in.Endpoint) > 2000 {
		return ErrInvalidSubscription
	}
	if pub, err := decodeB64(in.Keys.P256dh); err != nil || len(pub) != 65 || pub[0] != 4 {
		return ErrInvalidSubscription
	}
	if a, err := decodeB64(in.Keys.Auth); err != nil || len(a) != 16 {
		return ErrInvalidSubscription
	}
	return nil
}

// Subscribe menyimpan/memperbarui langganan perangkat untuk pengguna. Endpoint
// yang sama (browser yang sama) dipindahkan ke pengguna terakhir yang mengaktifkan.
func (s *Service) Subscribe(ctx context.Context, userID int64, in SubscribeInput) (int64, error) {
	if err := in.validate(); err != nil {
		return 0, err
	}
	now := s.clock.Now()
	sum := sha256.Sum256([]byte(in.Endpoint))
	ua := clip(in.UserAgent, 255)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO push_subscriptions (user_id, endpoint, endpoint_hash, p256dh, auth_secret, device_label, user_agent, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), user_id = VALUES(user_id), p256dh = VALUES(p256dh),
		  auth_secret = VALUES(auth_secret), device_label = VALUES(device_label), user_agent = VALUES(user_agent),
		  last_seen_at = VALUES(last_seen_at), disabled_at = NULL, disabled_reason = NULL, failure_count = 0, last_error = NULL`,
		userID, in.Endpoint, sum[:], in.Keys.P256dh, in.Keys.Auth, DeviceLabel(in.UserAgent), database.NullString(ua), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Unsubscribe menghapus langganan endpoint milik pengguna (dinonaktifkan dari browser).
func (s *Service) Unsubscribe(ctx context.Context, userID int64, endpoint string) error {
	sum := sha256.Sum256([]byte(endpoint))
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint_hash = ? AND user_id = ?`, sum[:], userID)
	return err
}

// RemoveDevice menghapus satu langganan milik pengguna.
func (s *Service) RemoveDevice(ctx context.Context, userID, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// Device adalah satu perangkat penerima push.
type Device struct {
	ID            int64
	Label         string
	CreatedAt     time.Time
	LastSeenAt    time.Time
	LastSuccessAt sql.NullTime
	LastError     sql.NullString
	DisabledAt    sql.NullTime
	DisabledWhy   sql.NullString
	EndpointHash  string // untuk mengenali "perangkat ini" di browser
}

// Devices mengembalikan perangkat milik pengguna (aktif lebih dulu).
func (s *Service) Devices(ctx context.Context, userID int64) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_label, created_at, last_seen_at, last_success_at, last_error, disabled_at, disabled_reason, HEX(endpoint_hash)
		  FROM push_subscriptions WHERE user_id = ?
		 ORDER BY disabled_at IS NOT NULL, last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Label, &d.CreatedAt, &d.LastSeenAt, &d.LastSuccessAt, &d.LastError, &d.DisabledAt, &d.DisabledWhy, &d.EndpointHash); err != nil {
			return nil, err
		}
		d.EndpointHash = strings.ToLower(d.EndpointHash)
		out = append(out, d)
	}
	return out, rows.Err()
}

// SendTest mengirim notifikasi uji ke semua perangkat aktif pengguna.
// Mengembalikan jumlah perangkat aktif.
func (s *Service) SendTest(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ? AND disabled_at IS NULL`, userID).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	now := s.clock.Now()
	_, err := Publish(ctx, s.db, Event{Type: Test, DedupeKey: fmt.Sprintf("test:%d:%d", userID, now.UnixNano()), TargetID: userID}, now)
	return n, err
}

// DeviceLabel membuat nama perangkat yang mudah dikenali dari User-Agent.
func DeviceLabel(ua string) string {
	l := strings.ToLower(ua)
	browser := "Browser"
	switch {
	case strings.Contains(l, "edg/"):
		browser = "Edge"
	case strings.Contains(l, "samsungbrowser"):
		browser = "Samsung Internet"
	case strings.Contains(l, "opr/") || strings.Contains(l, "opera"):
		browser = "Opera"
	case strings.Contains(l, "firefox/"):
		browser = "Firefox"
	case strings.Contains(l, "chrome/") || strings.Contains(l, "crios/"):
		browser = "Chrome"
	case strings.Contains(l, "safari/"):
		browser = "Safari"
	}
	osName := ""
	switch {
	case strings.Contains(l, "android"):
		osName = "Android"
	case strings.Contains(l, "iphone"):
		osName = "iPhone"
	case strings.Contains(l, "ipad"):
		osName = "iPad"
	case strings.Contains(l, "windows"):
		osName = "Windows"
	case strings.Contains(l, "mac os"):
		osName = "macOS"
	case strings.Contains(l, "linux"):
		osName = "Linux"
	}
	if osName == "" {
		return browser
	}
	return browser + " di " + osName
}

// ---------------------------------------------------------------------------
// Preferensi per role
// ---------------------------------------------------------------------------

// RolePrefs adalah kanal per event untuk satu role.
type RolePrefs struct {
	Code, Name string
	Channels   map[string]string
}

// Preferences mengembalikan matriks role × event.
func (s *Service) Preferences(ctx context.Context) ([]RolePrefs, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.code, r.name, p.event_type, p.channel
		  FROM roles r LEFT JOIN notification_preferences p ON p.role_id = r.id
		 ORDER BY FIELD(r.code, 'COORDINATOR', 'TECHNICIAN', 'ADMIN'), r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RolePrefs
	idx := map[string]int{}
	for rows.Next() {
		var code, name string
		var ev, ch sql.NullString
		if err := rows.Scan(&code, &name, &ev, &ch); err != nil {
			return nil, err
		}
		i, ok := idx[code]
		if !ok {
			i = len(out)
			idx[code] = i
			out = append(out, RolePrefs{Code: code, Name: name, Channels: map[string]string{}})
		}
		if ev.Valid {
			out[i].Channels[ev.String] = ch.String
		}
	}
	for i := range out {
		for _, e := range Events {
			if out[i].Channels[e.Type] == "" {
				out[i].Channels[e.Type] = ChannelNone
			}
		}
	}
	return out, rows.Err()
}

// RolePreferences mengembalikan kanal per event untuk satu role.
func (s *Service) RolePreferences(ctx context.Context, role string) (map[string]string, error) {
	all, err := s.Preferences(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if r.Code == role {
			return r.Channels, nil
		}
	}
	return map[string]string{}, nil
}

// SavePreferences menyimpan matriks preferensi (nilai dari form "pref_<ROLE>_<EVENT>").
func (s *Service) SavePreferences(ctx context.Context, actor *rbac.User, get func(string) string) (map[string]string, error) {
	current, err := s.Preferences(ctx)
	if err != nil {
		return nil, err
	}
	errs := map[string]string{}
	type change struct{ role, event, from, to string }
	var changes []change
	for _, r := range current {
		for _, e := range Events {
			v := get("pref_" + r.Code + "_" + e.Type)
			if v == "" {
				continue
			}
			if v != ChannelNone && v != ChannelInbox && v != ChannelPush {
				errs["pref_"+r.Code+"_"+e.Type] = "Pilihan tidak valid."
				continue
			}
			if v != r.Channels[e.Type] {
				changes = append(changes, change{r.Code, e.Type, r.Channels[e.Type], v})
			}
		}
	}
	if len(errs) > 0 || len(changes) == 0 {
		return errs, nil
	}
	now := s.clock.Now()
	return nil, database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, after := map[string]any{}, map[string]any{}
		for _, c := range changes {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO notification_preferences (role_id, event_type, channel, updated_by, updated_at)
				SELECT id, ?, ?, ?, ? FROM roles WHERE code = ?
				ON DUPLICATE KEY UPDATE channel = VALUES(channel), updated_by = VALUES(updated_by), updated_at = VALUES(updated_at)`,
				c.event, c.to, actor.ID, now, c.role); err != nil {
				return err
			}
			before[c.role+"."+c.event] = c.from
			after[c.role+"."+c.event] = c.to
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "notification.preferences", EntityType: "notification_preferences",
			ActorUserID: actor.ID, Before: before, After: after})
	})
}
