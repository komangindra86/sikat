package notifications

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// decryptForTest adalah sisi browser (user agent) RFC 8291: memastikan payload
// yang kita kirim dapat dibuka dengan kunci langganan.
func decryptForTest(t *testing.T, body []byte, uaPriv *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatal("body terlalu pendek")
	}
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	idlen := int(body[20])
	asPub := body[21 : 21+idlen]
	ct := body[21+idlen:]
	if rs != 4096 || idlen != 65 {
		t.Fatalf("header aes128gcm tidak sesuai: rs=%d idlen=%d", rs, idlen)
	}
	asKey, err := ecdh.P256().NewPublicKey(asPub)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := uaPriv.ECDH(asKey)
	if err != nil {
		t.Fatal(err)
	}
	uaPub := uaPriv.PublicKey().Bytes()
	prkKey, _ := hkdf.Extract(sha256.New, shared, authSecret)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(uaPub)+string(asPub), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatalf("dekripsi gagal: %v", err)
	}
	if len(plain) == 0 || plain[len(plain)-1] != 0x02 {
		t.Fatal("delimiter record terakhir (0x02) tidak ada")
	}
	return plain[:len(plain)-1]
}

func TestWebPushEncryptsAndSignsWithVAPID(t *testing.T) {
	pub, priv, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseVAPID(pub, priv, "mailto:it@rs.example")
	if err != nil {
		t.Fatal(err)
	}
	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	authSecret := make([]byte, 16)
	_, _ = rand.Read(authSecret)

	msg := Message{Title: "SIKAT", Body: "Tiket baru prioritas Sedang · TKT-2026-000001", URL: "/app/notifications/1", Tag: "sikat-t1"}
	var got struct {
		headers http.Header
		body    []byte
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.headers = r.Header.Clone()
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	now := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	wp := &WebPush{VAPID: v, Client: srv.Client(), Now: func() time.Time { return now }}
	sub := Subscription{Endpoint: srv.URL + "/push/abc", P256dh: b64.EncodeToString(uaPriv.PublicKey().Bytes()), Auth: b64.EncodeToString(authSecret)}
	status, err := wp.Send(t.Context(), sub, msg, 30*time.Minute)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("kirim: %d %v", status, err)
	}

	if got.headers.Get("Content-Encoding") != "aes128gcm" || got.headers.Get("TTL") != "1800" {
		t.Fatalf("header salah: %v", got.headers)
	}
	if got.headers.Get("Topic") == "" || len(got.headers.Get("Topic")) > 32 {
		t.Fatalf("Topic harus diisi ≤ 32 karakter: %q", got.headers.Get("Topic"))
	}
	auth := got.headers.Get("Authorization")
	if !strings.HasPrefix(auth, "vapid t=") || !strings.HasSuffix(auth, ", k="+pub) {
		t.Fatalf("Authorization VAPID salah: %s", auth)
	}
	token := strings.TrimSuffix(strings.TrimPrefix(auth, "vapid t="), ", k="+pub)
	claims, err := verifyToken(pub, token)
	if err != nil {
		t.Fatalf("JWT VAPID tidak valid: %v", err)
	}
	if claims["aud"] != srv.URL || claims["sub"] != "mailto:it@rs.example" {
		t.Fatalf("klaim JWT: %v", claims)
	}
	if exp, _ := claims["exp"].(float64); int64(exp) != now.Add(12*time.Hour).Unix() {
		t.Fatalf("exp JWT: %v", claims["exp"])
	}

	plain := decryptForTest(t, got.body, uaPriv, authSecret)
	var back Message
	if err := json.Unmarshal(plain, &back); err != nil || back != msg {
		t.Fatalf("payload setelah dekripsi = %s (%v)", plain, err)
	}
	if bytes.Contains(got.body, []byte("TKT-2026")) {
		t.Fatal("payload tidak boleh terkirim sebagai teks biasa")
	}
}

func TestParseVAPIDRejectsBadKeys(t *testing.T) {
	pub, priv, _ := GenerateVAPID()
	pub2, _, _ := GenerateVAPID()
	cases := map[string][3]string{
		"publik tidak cocok": {pub2, priv, "mailto:a@b.c"},
		"privat rusak":       {pub, "bukan-kunci", "mailto:a@b.c"},
		"subject http":       {pub, priv, "http://rs.example"},
	}
	for name, c := range cases {
		if _, err := ParseVAPID(c[0], c[1], c[2]); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
	if _, err := ParseVAPID(pub, priv, "https://sikat.rs.example"); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyPushResponses(t *testing.T) {
	cases := []struct {
		status int
		err    error
		want   outcome
	}{
		{201, nil, outcomeSent},
		{410, nil, outcomeGone},
		{404, nil, outcomeGone},
		{403, nil, outcomeKey},
		{429, nil, outcomeRetry},
		{503, nil, outcomeRetry},
		{413, nil, outcomeReject},
		{0, errors.New("dial tcp: timeout"), outcomeRetry},
		{0, errInvalidSub, outcomeReject},
	}
	for _, c := range cases {
		if got := classify(c.status, c.err); got != c.want {
			t.Errorf("classify(%d, %v) = %v, mau %v", c.status, c.err, got, c.want)
		}
	}
}

// Isi lock screen tidak boleh memuat informasi sensitif rumah sakit.
func TestPushBodyIsGenericForLockScreen(t *testing.T) {
	sensitive := []string{"IGD", "Gawat Darurat", "Printer macet", "Komputer Kasir", "KSR-01", "Budi Santoso", "Tn. Ahmad", "kertas"}
	ti := &ticketInfo{No: "TKT-2026-000123", Priority: "CRITICAL", Category: "Printer macet",
		Department: sql.NullString{String: "Instalasi Gawat Darurat", Valid: true},
		Room:       sql.NullString{String: "IGD Lantai 1", Valid: true},
		Asset:      sql.NullString{String: "Komputer Kasir", Valid: true}}
	payload := map[string]any{"note": "kertas pasien Tn. Ahmad", "reason": "Menunggu sparepart", "kind": "RESOLUTION_BREACH",
		"used_pct": 80, "count": 5, "window_days": 90, "due_at": "2026-11-12T08:00:00Z"}
	for _, e := range append(Events, EventInfo{Type: Test}) {
		ev := outboxRow{ID: 9, Type: e.Type, Payload: payload,
			TicketID: sql.NullInt64{Int64: 77, Valid: e.Type != RepeatDamage && e.Type != LicenseExpiry && e.Type != ProposalSubmitted && e.Type != Test},
			AssetID:  sql.NullInt64{Int64: 5, Valid: e.Type == RepeatDamage}}
		var tip *ticketInfo
		if ev.TicketID.Valid {
			tip = ti
		}
		c := compose(ev, tip, "Budi Santoso", "Komputer Kasir (KSR-01)")
		if c.PushBody == "" || c.Title == "" || c.Body == "" {
			t.Errorf("%s: teks kosong %+v", e.Type, c)
		}
		for _, s := range sensitive {
			if strings.Contains(c.PushBody, s) {
				t.Errorf("%s: teks push memuat %q: %q", e.Type, s, c.PushBody)
			}
		}
		if ev.TicketID.Valid && !strings.Contains(c.PushBody, ti.No) {
			t.Errorf("%s: teks push sebaiknya memuat nomor tiket: %q", e.Type, c.PushBody)
		}
		msg := PushMessage(e.Type, c.PushBody, 9, ev.TicketID.Int64)
		if msg.Title != "SIKAT" || !strings.HasPrefix(msg.URL, "/app/notifications/") {
			t.Errorf("%s: pesan push %+v", e.Type, msg)
		}
	}
	// Inbox (setelah login) boleh rinci.
	c := compose(outboxRow{Type: RepeatDamage, Payload: payload, AssetID: sql.NullInt64{Int64: 5, Valid: true}}, nil, "", "Komputer IGD-01 (IGD-01)")
	if c.Body != "Perhatian: Komputer IGD-01 (IGD-01) telah mengalami 5 gangguan dalam 90 hari terakhir." {
		t.Fatalf("isi inbox kerusakan berulang: %q", c.Body)
	}
}

func TestDeviceLabel(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Linux; Android 14; SM-A546E) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36":                      "Chrome di Android",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1": "Safari di iPhone",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 Edg/130.0":                   "Edge di Windows",
		"": "Browser",
	}
	for ua, want := range cases {
		if got := DeviceLabel(ua); got != want {
			t.Errorf("DeviceLabel(%q) = %q, mau %q", ua, got, want)
		}
	}
}

func TestAllowedPushHost(t *testing.T) {
	for host, want := range map[string]bool{
		"fcm.googleapis.com": true, "updates.push.services.mozilla.com": true, "wns2-sg2p.notify.windows.com": true,
		"web.push.apple.com": true, "127.0.0.1": false, "localhost": false, "intranet.rs.local": false,
		"evilfcm.googleapis.com.attacker.example": false, "notify.windows.com.evil.example": false,
	} {
		if got := AllowedPushHost(host); got != want {
			t.Errorf("AllowedPushHost(%q) = %v, mau %v", host, got, want)
		}
	}
	in := SubscribeInput{Endpoint: "https://10.0.0.5/admin"}
	if in.validate() == nil {
		t.Fatal("endpoint ke jaringan internal harus ditolak")
	}
}
