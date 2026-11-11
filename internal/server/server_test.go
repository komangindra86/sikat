package server_test

import (
	"context"
	"database/sql"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sikat"
	"sikat/internal/clock"
	"sikat/internal/config"
	"sikat/internal/server"
	"sikat/internal/testutil"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func newApp(t *testing.T, loginRate int) (*httptest.Server, func() *client) {
	srv, nc, _ := newAppDB(t, server.Deps{LoginRate: loginRate})
	return srv, nc
}

// newAppDB merakit aplikasi lengkap di atas database uji baru. Field Deps yang
// diisi pemanggil (mis. LoginRate, QRRate) dipertahankan.
func newAppDB(t *testing.T, deps server.Deps) (*httptest.Server, func() *client, *sql.DB) {
	db := testutil.DB(t)
	testutil.CreateUser(t, db, "admin", "ADMIN", "password-admin-123", false)
	testutil.CreateUser(t, db, "budi", "TECHNICIAN", "password-budi-123", false)
	testutil.CreateUser(t, db, "baru", "TECHNICIAN", "sementara-123", true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	deps.Config = config.Config{Env: "development", BaseURL: "http://localhost", SessionLifetime: time.Hour,
		SessionIdleTimeout: time.Hour, StorageDir: t.TempDir()}
	deps.DB, deps.Log, deps.Clock = db, slog.New(slog.NewTextHandler(io.Discard, nil)), clock.Real{}
	deps.Templates, deps.Static = sikat.TemplatesFS, sikat.StaticFS
	app, err := server.New(ctx, deps)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler)
	t.Cleanup(srv.Close)
	return srv, func() *client {
		jar, _ := cookiejar.New(nil)
		return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
	}, db
}

func (c *client) get(path string) (*http.Response, string) {
	c.t.Helper()
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (c *client) token(path string) string {
	c.t.Helper()
	_, body := c.get(path)
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		c.t.Fatalf("token CSRF tidak ditemukan di %s", path)
	}
	return html.UnescapeString(m[1])
}

func (c *client) post(path string, form url.Values, headers ...string) (*http.Response, string) {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.base+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (c *client) login(user, pass string) *http.Response {
	c.t.Helper()
	tok := c.token("/login")
	resp, _ := c.post("/login", url.Values{"csrf_token": {tok}, "username": {user}, "password": {pass}})
	return resp
}

func TestLoginFlowAndRBAC(t *testing.T) {
	_, newClient := newApp(t, 0)

	anon := newClient()
	if resp, _ := anon.get("/app/master/departments"); resp.Request.URL.Path != "/login" {
		t.Fatalf("anonim harus dialihkan ke /login, berakhir di %s", resp.Request.URL.Path)
	}
	if resp := anon.login("budi", "salah"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login salah harus 401, dapat %d", resp.StatusCode)
	}

	tech := newClient()
	if resp := tech.login("budi", "password-budi-123"); resp.Request.URL.Path != "/app" {
		t.Fatalf("login petugas harus ke /app, berakhir di %s", resp.Request.URL.Path)
	}
	for path, want := range map[string]int{
		"/app": 200, "/app/assets": 200, "/app/master/departments": 403, "/app/users": 403,
		"/app/settings": 403, "/app/audit": 403, "/app/assets/new": 403,
	} {
		if resp, _ := tech.get(path); resp.StatusCode != want {
			t.Errorf("petugas GET %s = %d, ingin %d", path, resp.StatusCode, want)
		}
	}

	admin := newClient()
	admin.login("admin", "password-admin-123")
	for _, path := range []string{"/app/master/departments", "/app/master/spare-parts", "/app/users", "/app/settings", "/app/audit", "/app/assets/new",
		"/app/master/priority-rules", "/app/master/priority-rules/new", "/app/qr", "/app/qr/print", "/app/tickets",
		"/app/priority/test", "/app/priority/test?uji=1&ticket_category_id=1&impact=2&scope=3&replacement=NO"} {
		if resp, _ := admin.get(path); resp.StatusCode != 200 {
			t.Errorf("admin GET %s = %d", path, resp.StatusCode)
		}
	}
	if _, body := admin.get("/app/priority/test?uji=1&ticket_category_id=1&impact=2&scope=3&replacement=NO"); !strings.Contains(body, "Satu unit atau lebih berhenti") || !strings.Contains(body, "← cocok") {
		t.Error("uji aturan harus menunjukkan aturan yang cocok")
	}
	if resp, _ := admin.get("/app/qr/zip"); resp.StatusCode != 200 {
		t.Errorf("zip tanpa QR harus kembali ke daftar dengan pesan, dapat %d", resp.StatusCode)
	}
	if resp, _ := tech.get("/app/qr"); resp.StatusCode != 403 {
		t.Errorf("petugas tidak mengelola QR, dapat %d", resp.StatusCode)
	}
	if resp, _ := admin.get("/app/master/tidak-ada"); resp.StatusCode != 404 {
		t.Errorf("master tidak dikenal harus 404, dapat %d", resp.StatusCode)
	}

	// Logout menghapus session.
	tok := admin.token("/app")
	admin.post("/logout", url.Values{"csrf_token": {tok}})
	if resp, _ := admin.get("/app"); resp.Request.URL.Path != "/login" {
		t.Fatal("setelah logout harus kembali ke /login")
	}
}

func TestSessionFixationPrevented(t *testing.T) {
	_, newClient := newApp(t, 0)
	c := newClient()
	c.token("/login")
	u, _ := url.Parse(c.base)
	before := cookieValue(c.http.Jar.Cookies(u), "sikat_session")
	c.login("budi", "password-budi-123")
	after := cookieValue(c.http.Jar.Cookies(u), "sikat_session")
	if before == "" || after == "" || before == after {
		t.Fatalf("token session harus diganti saat login (before=%q after=%q)", before, after)
	}
}

func cookieValue(cs []*http.Cookie, name string) string {
	for _, c := range cs {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestCSRFProtection(t *testing.T) {
	_, newClient := newApp(t, 0)
	admin := newClient()
	admin.login("admin", "password-admin-123")

	form := url.Values{"code": {"IGD"}, "name": {"IGD"}}
	if resp, _ := admin.post("/app/master/departments", form); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST tanpa token harus 403, dapat %d", resp.StatusCode)
	}
	form.Set("csrf_token", "token-palsu")
	if resp, _ := admin.post("/app/master/departments", form); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("token salah harus 403, dapat %d", resp.StatusCode)
	}
	form.Set("csrf_token", admin.token("/app/master/departments/new"))
	if resp, _ := admin.post("/app/master/departments", form, "Sec-Fetch-Site", "cross-site"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("request lintas situs harus 403 walau token benar, dapat %d", resp.StatusCode)
	}
	resp, body := admin.post("/app/master/departments", form)
	if resp.StatusCode != 200 || !strings.Contains(body, "Unit berhasil ditambahkan") {
		t.Fatalf("POST dengan token benar harus sukses, dapat %d", resp.StatusCode)
	}
	// Token via header (HTMX).
	tok := admin.token("/app/master/departments/new")
	resp, _ = admin.post("/app/master/departments", url.Values{"code": {"ICU"}, "name": {"ICU"}}, "X-CSRF-Token", tok)
	if resp.StatusCode != 200 {
		t.Fatalf("token via header harus diterima, dapat %d", resp.StatusCode)
	}
}

func TestForcedPasswordChange(t *testing.T) {
	_, newClient := newApp(t, 0)
	c := newClient()
	if resp := c.login("baru", "sementara-123"); resp.Request.URL.Path != "/app/password" {
		t.Fatalf("user dengan password sementara harus ke /app/password, berakhir di %s", resp.Request.URL.Path)
	}
	if resp, _ := c.get("/app/assets"); resp.Request.URL.Path != "/app/password" {
		t.Fatal("halaman lain harus dialihkan ke ganti password")
	}
	tok := c.token("/app/password")
	resp, _ := c.post("/app/password", url.Values{"csrf_token": {tok}, "current": {"sementara-123"}, "new": {"kopi-pagi-di-igd"}, "confirm": {"kopi-pagi-di-igd"}})
	if resp.Request.URL.Path != "/app" {
		t.Fatalf("setelah ganti password harus ke /app, berakhir di %s", resp.Request.URL.Path)
	}
	if resp, _ := c.get("/app/assets"); resp.StatusCode != 200 {
		t.Fatal("setelah ganti password, halaman lain harus bisa dibuka")
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, newClient := newApp(t, 3)
	c := newClient()
	tok := c.token("/login")
	codes := []int{}
	for i := 0; i < 4; i++ {
		resp, _ := c.post("/login", url.Values{"csrf_token": {tok}, "username": {"budi"}, "password": {"salah"}})
		codes = append(codes, resp.StatusCode)
	}
	if codes[3] != http.StatusTooManyRequests {
		t.Fatalf("percobaan ke-4 harus 429, dapat %v", codes)
	}
}

func TestStaticAndHealth(t *testing.T) {
	srv, newClient := newApp(t, 0)
	c := newClient()
	resp, _ := c.get("/static/css/app.css?v=abc")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("aset statis: %d %s", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	if resp, _ := c.get("/static/css/"); resp.StatusCode != 404 {
		t.Fatalf("daftar isi folder statis harus ditolak, dapat %d", resp.StatusCode)
	}
	resp, body := c.get("/healthz")
	if resp.StatusCode != 200 || body != "ok" {
		t.Fatalf("healthz: %d %q", resp.StatusCode, body)
	}
	resp, _ = c.get("/login")
	if resp.Header.Get("Content-Security-Policy") == "" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("header keamanan wajib ada")
	}
	_ = srv
}

func TestDeactivatedUserLosesSession(t *testing.T) {
	_, newClient := newApp(t, 0)
	admin := newClient()
	admin.login("admin", "password-admin-123")
	tech := newClient()
	tech.login("budi", "password-budi-123")

	_, body := admin.get("/app/users?q=budi")
	m := regexp.MustCompile(`/app/users/(\d+)/active`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("form nonaktifkan budi tidak ditemukan")
	}
	tok := admin.token("/app/users")
	admin.post("/app/users/"+m[1]+"/active", url.Values{"csrf_token": {tok}, "active": {"0"}})

	if resp, _ := tech.get("/app"); resp.Request.URL.Path != "/login" {
		t.Fatal("user yang dinonaktifkan harus langsung keluar")
	}
}
