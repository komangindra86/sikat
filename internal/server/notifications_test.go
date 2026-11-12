package server_test

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"sikat/internal/config"
	"sikat/internal/notifications"
	"sikat/internal/server"
	"sikat/internal/testutil"
)

type okSender struct{}

func (okSender) Send(context.Context, notifications.Subscription, notifications.Message, time.Duration) (int, error) {
	return 201, nil
}

// postJSON mengirim JSON seperti fetch() di pwa.js (CSRF lewat header).
func (c *client) postJSON(path, csrf string, body any) (*http.Response, string) {
	c.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", c.base+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, string(out)
}

func subscription() map[string]any {
	k, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/uji-" + base64.RawURLEncoding.EncodeToString(auth[:6]),
		"keys": map[string]string{"p256dh": base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), "auth": base64.RawURLEncoding.EncodeToString(auth)}}
}

func TestNotificationInboxAndPushSubscriptionOverHTTP(t *testing.T) {
	pub, priv, err := notifications.GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	_, newClient, db := newAppDB(t, server.Deps{
		Config:     config.Config{VAPIDPublicKey: pub, VAPIDPrivateKey: priv, VAPIDSubject: "mailto:it@rs.example"},
		PushSender: okSender{},
	})
	budiID := testutil.Count(t, db, `SELECT id FROM users WHERE username = 'budi'`)
	adminID := testutil.Count(t, db, `SELECT id FROM users WHERE username = 'admin'`)

	// Notifikasi langsung di DB (alur fan-out diuji di paket tickets).
	ev, err := notifications.Publish(context.Background(), db, notifications.Event{Type: notifications.Test, DedupeKey: "uji:1", TargetID: int64(budiID)}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO notifications (user_id, event_id, event_type, title, body, push_body, url, channel, created_at)
		VALUES (?, ?, 'TEST', 'Tiket baru TKT-2026-000009', 'Prioritas Tinggi · Printer · IGD', 'Tiket baru', '/app/tickets', 'PUSH', ?)`, budiID, ev, now)
	if err != nil {
		t.Fatal(err)
	}
	notifID, _ := res.LastInsertId()

	budi := newClient()
	budi.login("budi", "password-budi-123")
	_, body := budi.get("/app/notifications")
	if !strings.Contains(body, "Tiket baru TKT-2026-000009") || !strings.Contains(body, "1 belum dibaca") {
		t.Fatal("inbox menampilkan notifikasi belum dibaca")
	}
	if !strings.Contains(body, `class="count-badge" data-unread >1<`) && !strings.Contains(body, `data-unread >1</span>`) {
		t.Fatal("badge di menu menampilkan jumlah belum dibaca")
	}
	if _, body := budi.get("/app/notifications/badge"); strings.TrimSpace(body) != `{"unread":1}` {
		t.Fatalf("badge JSON = %s", body)
	}

	// Pengguna lain tidak bisa membuka notifikasi milik budi.
	admin := newClient()
	admin.login("admin", "password-admin-123")
	if resp, _ := admin.get("/app/notifications/" + itoa(notifID)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("notifikasi orang lain harus 404, dapat %d", resp.StatusCode)
	}
	// Membuka → ditandai dibaca → diarahkan ke tujuan.
	budi.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, _ := budi.get("/app/notifications/" + itoa(notifID))
	budi.http.CheckRedirect = nil
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/app/tickets" {
		t.Fatalf("buka notifikasi: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM notifications WHERE id = ? AND read_at IS NOT NULL`, notifID) != 1 {
		t.Fatal("notifikasi ditandai dibaca")
	}

	// Langganan push: tanpa CSRF ditolak; data rusak 400; valid tersimpan.
	tok := budi.token("/app/notifications/settings")
	if resp, _ := budi.postJSON("/app/notifications/push/subscribe", "salah", subscription()); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("tanpa CSRF harus 403, dapat %d", resp.StatusCode)
	}
	bad := subscription()
	bad["endpoint"] = "http://tidak-aman.example/push"
	if resp, _ := budi.postJSON("/app/notifications/push/subscribe", tok, bad); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("endpoint non-https harus 400, dapat %d", resp.StatusCode)
	}
	if resp, body := budi.postJSON("/app/notifications/push/subscribe", tok, subscription()); resp.StatusCode != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("langganan valid: %d %s", resp.StatusCode, body)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ? AND disabled_at IS NULL`, budiID) != 1 {
		t.Fatal("langganan tersimpan")
	}
	_, body = budi.get("/app/notifications/settings")
	if !strings.Contains(body, "Perangkat terdaftar") || !strings.Contains(body, `data-vapid-key="`+pub+`"`) || !strings.Contains(body, "Kirim notifikasi uji") {
		t.Fatal("halaman pengaturan menampilkan perangkat & kunci VAPID publik")
	}
	if strings.Contains(body, priv) {
		t.Fatal("kunci VAPID privat tidak boleh muncul di halaman")
	}
	if resp, body := budi.post("/app/notifications/push/test", url.Values{"csrf_token": {tok}}); resp.StatusCode != 200 || !strings.Contains(body, "Notifikasi uji dikirim ke 1 perangkat") {
		t.Fatalf("kirim uji: %d", resp.StatusCode)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'TEST' AND target_user_id = ?`, budiID) != 2 {
		t.Fatal("notifikasi uji masuk outbox untuk pengirimnya")
	}

	// Logout menghapus langganan perangkat ini (perangkat bersama).
	budi.post("/logout", url.Values{"csrf_token": {tok}})
	if testutil.Count(t, db, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ?`, budiID) != 0 {
		t.Fatal("logout harus menghapus langganan push perangkat tersebut")
	}

	// Preferensi per role: hanya config.manage; perubahan diaudit; nilai tidak valid 422.
	tech := newClient()
	tech.login("budi", "password-budi-123")
	if resp, _ := tech.get("/app/settings/notifications"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas tidak boleh mengubah preferensi, dapat %d", resp.StatusCode)
	}
	_, body = admin.get("/app/settings/notifications")
	if !strings.Contains(body, "Preferensi notifikasi") || !strings.Contains(body, `name="pref_TECHNICIAN_TICKET_CREATED"`) {
		t.Fatal("halaman preferensi menampilkan matriks role × kejadian")
	}
	atok := admin.token("/app/settings/notifications")
	if resp, _ := admin.post("/app/settings/notifications", url.Values{"csrf_token": {atok}, "pref_ADMIN_SLA_BREACH": {"SEMUA"}}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("nilai tidak valid harus 422, dapat %d", resp.StatusCode)
	}
	resp, body = admin.post("/app/settings/notifications", url.Values{"csrf_token": {atok}, "pref_ADMIN_SLA_BREACH": {"INBOX"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "Preferensi notifikasi disimpan") {
		t.Fatalf("simpan preferensi: %d", resp.StatusCode)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM notification_preferences p JOIN roles r ON r.id = p.role_id
		WHERE r.code = 'ADMIN' AND p.event_type = 'SLA_BREACH' AND p.channel = 'INBOX' AND p.updated_by = ?`, adminID) != 1 {
		t.Fatal("preferensi tersimpan")
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'notification.preferences'`) != 1 {
		t.Fatal("perubahan preferensi tercatat di audit")
	}
}

func TestPWAAssets(t *testing.T) {
	srv, newClient, _ := newAppDB(t, server.Deps{})
	anon := newClient()

	resp, body := anon.get("/sw.js")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("sw.js: %d %v", resp.StatusCode, resp.Header)
	}
	if strings.Contains(body, "__VERSION__") || strings.Contains(body, "__PRECACHE__") || !strings.Contains(body, `/static/css/app.css?v=`) {
		t.Fatal("sw.js harus berisi versi dan daftar precache berversi")
	}

	resp, body = anon.get("/manifest.webmanifest")
	var m struct {
		ShortName string `json:"short_name"`
		StartURL  string `json:"start_url"`
		Display   string `json:"display"`
		Icons     []struct{ Src, Sizes, Purpose string }
	}
	_ = json.Unmarshal([]byte(body), &m)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/manifest+json") ||
		m.ShortName != "SIKAT" || m.StartURL != "/app" || m.Display != "standalone" || len(m.Icons) < 3 {
		t.Fatalf("manifest tidak lengkap: %s", body)
	}
	sizes := map[string]bool{}
	for _, ic := range m.Icons {
		sizes[ic.Sizes+"/"+ic.Purpose] = true
		if r, _ := anon.get(strings.TrimPrefix(ic.Src, srv.URL)); r.StatusCode != 200 {
			t.Fatalf("ikon %s tidak tersedia", ic.Src)
		}
	}
	if !sizes["192x192/any"] || !sizes["512x512/any"] || !sizes["512x512/maskable"] {
		t.Fatalf("ikon wajib installable: %v", sizes)
	}

	// Halaman offline tanpa data session (aman disimpan service worker).
	resp, body = anon.get("/offline")
	if resp.StatusCode != 200 || !strings.Contains(body, "Tidak ada koneksi internet") || strings.Contains(body, "csrf-token") {
		t.Fatalf("halaman offline: %d", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatal("halaman offline tidak boleh membuat session")
	}
	if resp, _ := anon.get("/favicon.ico"); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("favicon: %d", resp.StatusCode)
	}

	// Manifest & pwa.js hanya di halaman setelah login (bukan halaman publik pelapor).
	if _, body := anon.get("/login"); strings.Contains(body, "manifest.webmanifest") {
		t.Fatal("halaman login/publik tidak memuat manifest")
	}
	u := newClient()
	u.login("budi", "password-budi-123")
	if _, body := u.get("/app"); !strings.Contains(body, `rel="manifest"`) || !strings.Contains(body, "js/pwa.js") {
		t.Fatal("halaman aplikasi memuat manifest & pwa.js")
	}
}
