package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/middleware"
	"sikat/internal/qrcodes"
	"sikat/internal/server"
	"sikat/internal/testutil"
)

// seedLocation membuat unit, ruangan, dan QR ruangan; mengembalikan token QR.
func seedLocation(t *testing.T, db *sql.DB) string {
	t.Helper()
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('LAB','Laboratorium',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	dept, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'LAB1','Ruang Sampel',?,?)`, dept, now, now)
	if err != nil {
		t.Fatal(err)
	}
	room, _ := res.LastInsertId()
	qs := qrcodes.NewService(db, clock.Real{}, "http://localhost")
	id, err := qs.Generate(t.Context(), 0, qrcodes.TypeRoom, room)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := qs.Get(t.Context(), id)
	return q.Token
}

func categoryID(t *testing.T, db *sql.DB) string {
	var id string
	if err := db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Tidak bisa mencetak'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// postMultipart mengirim form multipart dengan satu foto PNG opsional.
func (c *client) postMultipart(path string, fields map[string]string, photo bool) (*http.Response, string) {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if photo {
		fw, _ := mw.CreateFormFile("photos", "printer.png")
		_ = png.Encode(fw, image.NewRGBA(image.Rect(0, 0, 64, 48)))
	}
	mw.Close()
	req, _ := http.NewRequest("POST", c.base+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b := new(bytes.Buffer)
	_, _ = b.ReadFrom(resp.Body)
	return resp, b.String()
}

func TestPublicReportFlow(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	token := seedLocation(t, db)
	cat := categoryID(t, db)

	pelapor := newClient()
	resp, body := pelapor.get("/q/" + token)
	if resp.StatusCode != 200 || !strings.Contains(body, "Laboratorium · Ruang Sampel") {
		t.Fatalf("form QR harus menampilkan lokasi: %d", resp.StatusCode)
	}
	if strings.Contains(body, "/app/") {
		t.Fatal("halaman publik tidak boleh menautkan halaman internal")
	}
	tok := pelapor.token("/q/" + token)

	// Validasi: field wajib kosong → 422, tidak ada tiket.
	resp, body = pelapor.postMultipart("/q/"+token, map[string]string{"csrf_token": tok}, false)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Nama pelapor wajib diisi") {
		t.Fatalf("validasi harus 422: %d", resp.StatusCode)
	}

	fields := map[string]string{
		"csrf_token": tok, "reporter_name": "Ani Lab", "ticket_category_id": cat,
		"description": "Printer tidak mau mencetak hasil lab", "impact_level": "2", "affected_scope": "3",
		"replacement_available": "NO", "incident_started": "now",
	}
	resp, body = pelapor.postMultipart("/q/"+token, fields, true)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Request.URL.Path, "/t/") || resp.Request.URL.Query().Get("baru") != "1" {
		t.Fatalf("kirim harus dialihkan ke halaman pemantauan: %d %s", resp.StatusCode, resp.Request.URL)
	}
	for _, want := range []string{"Laporan terkirim", "TKT-", "Tinggi", "Laboratorium · Ruang Sampel", "Menunggu diambil"} {
		if !strings.Contains(body, want) {
			t.Errorf("halaman konfirmasi harus memuat %q", want)
		}
	}
	trackPath := resp.Request.URL.Path
	foto := regexp.MustCompile(`/t/[^"]+/foto/\d+`).FindString(body)
	if foto == "" {
		t.Fatal("foto pelapor harus tampil di halaman pemantauan")
	}
	if resp, _ := pelapor.get(foto); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("foto harus tersaji sebagai JPEG: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// Pelapor lain dengan token salah tidak bisa melihat.
	stranger := newClient()
	if resp, _ := stranger.get("/t/" + strings.Repeat("A", 43)); resp.StatusCode != 404 {
		t.Fatalf("token salah harus 404, dapat %d", resp.StatusCode)
	}
	if resp, _ := stranger.get("/q/AAAAAAAAAAAAAAAAAAAAAA"); resp.StatusCode != 404 {
		t.Fatalf("QR tidak dikenal harus 404, dapat %d", resp.StatusCode)
	}

	// Petugas melihat tiket di antrean lalu mengambilnya (HTMX).
	tech := newClient()
	tech.login("budi", "password-budi-123")
	_, body = tech.get("/app/tickets")
	m := regexp.MustCompile(`/app/tickets/(\d+)/claim`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("tombol ambil tiket harus ada di antrean")
	}
	ctok := tech.token("/app/tickets")
	resp, body = tech.post("/app/tickets/"+m[1]+"/claim", url.Values{"csrf_token": {ctok}}, "HX-Request", "true")
	if resp.StatusCode != 200 || !strings.Contains(body, "Tiket berhasil diambil") {
		t.Fatalf("klaim HTMX harus berhasil: %d", resp.StatusCode)
	}

	// Petugas kedua mendapat pemberitahuan sudah diambil.
	testutilCreate(t, db, "sari")
	other := newClient()
	other.login("sari", "password-sari-123")
	otok := other.token("/app/tickets")
	_, body = other.post("/app/tickets/"+m[1]+"/claim", url.Values{"csrf_token": {otok}}, "HX-Request", "true")
	if !strings.Contains(body, "sudah diambil oleh User budi") {
		t.Fatal("petugas kedua harus diberi tahu tiket sudah diambil")
	}

	// Admin melihat antrean tapi tidak boleh mengambil tiket.
	admin := newClient()
	admin.login("admin", "password-admin-123")
	atok := admin.token("/app/tickets")
	if resp, _ := admin.post("/app/tickets/"+m[1]+"/claim", url.Values{"csrf_token": {atok}}); resp.StatusCode != 403 {
		t.Fatalf("admin tidak boleh klaim, dapat %d", resp.StatusCode)
	}

	// Pelapor melihat perkembangan.
	_, body = pelapor.get(trackPath)
	if !strings.Contains(body, "Diterima petugas User") || !strings.Contains(body, "Diterima") {
		t.Fatal("halaman pelapor harus menampilkan tiket sudah diterima")
	}
}

func testutilCreate(t *testing.T, db *sql.DB, username string) {
	t.Helper()
	// password mengikuti pola "password-<username>-123"
	testutil.CreateUser(t, db, username, "TECHNICIAN", "password-"+username+"-123", false)
}

func TestPublicReportGlobalQR(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	seedLocation(t, db)
	var dept, room string
	_ = db.QueryRow(`SELECT department_id, id FROM rooms WHERE code = 'LAB1'`).Scan(&dept, &room)

	// Admin membuat QR global dari halaman QR; QR global kedua ditolak.
	admin := newClient()
	admin.login("admin", "password-admin-123")
	_, body := admin.get("/app/qr")
	if !strings.Contains(body, "Buat QR global") {
		t.Fatal("halaman QR harus menawarkan pembuatan QR global")
	}
	atok := admin.token("/app/qr")
	_, body = admin.post("/app/qr/generate", url.Values{"csrf_token": {atok}, "type": {"GLOBAL"}, "target_id": {"0"}})
	if !strings.Contains(body, "QR berhasil dibuat") || !strings.Contains(body, "Semua lokasi") {
		t.Fatal("QR global harus berhasil dibuat")
	}
	_, body = admin.post("/app/qr/generate", url.Values{"csrf_token": {atok}, "type": {"GLOBAL"}, "target_id": {"0"}})
	if !strings.Contains(body, "QR global aktif sudah ada") {
		t.Fatal("QR global aktif kedua harus ditolak dengan pesan")
	}
	var qrID, token string
	if err := db.QueryRow(`SELECT id, token FROM qr_codes WHERE type = 'GLOBAL' AND is_active`).Scan(&qrID, &token); err != nil {
		t.Fatal(err)
	}
	if resp, body := admin.get("/app/qr/" + qrID + "/poster"); resp.StatusCode != 200 || !strings.Contains(body, "Ada gangguan IT?") {
		t.Fatalf("poster QR global harus tersedia: %d", resp.StatusCode)
	}

	// Pelapor memilih lokasi sendiri dan memilih masalah per bidang.
	pelapor := newClient()
	resp, body := pelapor.get("/q/" + token)
	for _, want := range []string{"Di mana gangguannya?", `name="department_id"`, "Pelayanan &amp; Support", "Komputer, laptop, monitor", "Lainnya / tidak yakin"} {
		if !strings.Contains(body, want) {
			t.Errorf("form QR global harus memuat %q (%d)", want, resp.StatusCode)
		}
	}
	if strings.Contains(body, "Lokasi terdeteksi") {
		t.Fatal("QR global tidak punya lokasi terdeteksi")
	}
	if _, body := pelapor.get("/q/" + token + "/ruangan?department_id=" + dept); !strings.Contains(body, `value="`+room+`"`) || !strings.Contains(body, "Ruang Sampel") {
		t.Fatal("pilihan ruangan harus dimuat untuk unit terpilih")
	}
	if resp, _ := pelapor.get("/q/" + seedOtherRoomQR(t, db) + "/ruangan?department_id=" + dept); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("daftar ruangan hanya untuk QR global, dapat %d", resp.StatusCode)
	}
	tok := pelapor.token("/q/" + token)
	fields := map[string]string{
		"csrf_token": tok, "reporter_name": "Ani", "ticket_category_id": categoryID(t, db),
		"description": "Printer tidak mau mencetak", "impact_level": "1", "affected_scope": "1",
	}
	resp, body = pelapor.postMultipart("/q/"+token, fields, false)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Pilih unit tempat gangguan terjadi") {
		t.Fatalf("unit wajib dipilih di QR global: %d", resp.StatusCode)
	}
	fields["department_id"], fields["room_id"] = dept, room
	resp, body = pelapor.postMultipart("/q/"+token, fields, false)
	if !strings.HasPrefix(resp.Request.URL.Path, "/t/") || !strings.Contains(body, "Laboratorium · Ruang Sampel") {
		t.Fatalf("laporan QR global harus tercatat dengan lokasi pilihan pelapor: %d %s", resp.StatusCode, resp.Request.URL)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM tickets WHERE qr_code_id = ? AND channel = 'QR' AND room_id = ?`, qrID, room); n != 1 {
		t.Fatalf("tiket harus terhubung ke QR global, dapat %d", n)
	}
}

// seedOtherRoomQR membuat ruangan lain beserta QR ruangannya; mengembalikan token QR.
func seedOtherRoomQR(t *testing.T, db *sql.DB) string {
	t.Helper()
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at)
		SELECT department_id, 'LAB2', 'Ruang Analis', ?, ? FROM rooms WHERE code = 'LAB1'`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	room, _ := res.LastInsertId()
	qs := qrcodes.NewService(db, clock.Real{}, "http://localhost")
	id, err := qs.Generate(t.Context(), 0, qrcodes.TypeRoom, room)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := qs.Get(t.Context(), id)
	return q.Token
}

func TestPublicRateLimitPerQR(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{QRRate: 2})
	token := seedLocation(t, db)
	cat := categoryID(t, db)
	c := newClient()
	tok := c.token("/q/" + token)
	fields := map[string]string{"csrf_token": tok, "reporter_name": "Ani", "ticket_category_id": cat,
		"description": "Printer macet lagi", "impact_level": "1", "affected_scope": "1"}
	codes := []int{}
	for i := 0; i < 3; i++ {
		resp, _ := c.postMultipart("/q/"+token, fields, false)
		codes = append(codes, resp.StatusCode)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("kiriman ke-3 dari QR yang sama harus dibatasi: %v", codes)
	}
	// Honeypot terisi → ditolak.
	c2 := newClient()
	tok2 := c2.token("/q/" + token)
	fields["csrf_token"], fields["website"] = tok2, "http://spam"
	if resp, _ := c2.postMultipart("/q/"+token, fields, false); resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("honeypot harus ditolak, dapat %d", resp.StatusCode)
	}
}

func TestStaffCreateOnBehalf(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	seedLocation(t, db)
	cat := categoryID(t, db)
	var dept string
	_ = db.QueryRow(`SELECT id FROM departments WHERE code = 'LAB'`).Scan(&dept)

	tech := newClient()
	tech.login("budi", "password-budi-123")
	tok := tech.token("/app/tickets/new")
	resp, body := tech.postMultipart("/app/tickets", map[string]string{
		"csrf_token": tok, "channel": "PHONE", "department_id": dept, "reporter_name": "Bu Lab",
		"ticket_category_id": cat, "description": "Telepon: printer lab mati", "impact_level": "1",
		"affected_scope": "1", "claim_now": "1",
	}, false)
	if resp.StatusCode != 200 || !strings.Contains(body, "Anda penanggung jawab tiket ini") {
		t.Fatalf("tiket telepon + langsung ditangani: %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Link pemantauan untuk pelapor") || !strings.Contains(body, "Telepon") {
		t.Fatal("halaman detail harus menampilkan link pelapor & sumber Telepon")
	}
	// Link hanya tampil sekali.
	_, body = tech.get(resp.Request.URL.Path)
	if strings.Contains(body, "Link pemantauan untuk pelapor") {
		t.Fatal("link pelapor hanya boleh tampil sekali")
	}
	admin := newClient()
	admin.login("admin", "password-admin-123")
	if resp, _ := admin.get("/app/tickets/new"); resp.StatusCode != 403 {
		t.Fatalf("admin tidak mencatat tiket (bukan penangan), dapat %d", resp.StatusCode)
	}
}

func TestRedactPath(t *testing.T) {
	cases := map[string]string{
		"/t/abcDEF123/foto/5": "/t/[token]/foto/5",
		"/q/XYZ":              "/q/[token]",
		"/app/tickets/3":      "/app/tickets/3",
	}
	for in, want := range cases {
		if got := middleware.RedactPath(in); got != want {
			t.Errorf("RedactPath(%q) = %q, ingin %q", in, got, want)
		}
	}
}

// fakeEmployees menggantikan API pegawai SIMRS.
type fakeEmployees []string

func (f fakeEmployees) Names(context.Context) []string { return f }

func TestStaffFormSuggestsSimrsEmployees(t *testing.T) {
	_, newClient, _ := newAppDB(t, server.Deps{Employees: fakeEmployees{"Ners Ani Lestari", "Wayan <Sari>"}})
	tech := newClient()
	tech.login("budi", "password-budi-123")
	_, body := tech.get("/app/tickets/new")
	if !strings.Contains(body, `list="pegawai"`) || !strings.Contains(body, `<option value="Ners Ani Lestari"></option>`) ||
		!strings.Contains(body, `<option value="Wayan &lt;Sari&gt;"></option>`) || !strings.Contains(body, "daftar pegawai SIMRS") {
		t.Fatal("form catat tiket menyarankan nama pegawai dari SIMRS")
	}

	// SIMRS tidak tersedia: form tetap bisa dipakai, nama diisi bebas.
	_, newClient2, _ := newAppDB(t, server.Deps{Employees: fakeEmployees(nil)})
	tech2 := newClient2()
	tech2.login("budi", "password-budi-123")
	if resp, body := tech2.get("/app/tickets/new"); resp.StatusCode != 200 || strings.Contains(body, `list="pegawai"`) || !strings.Contains(body, `name="reporter_name"`) {
		t.Fatalf("tanpa daftar pegawai form tetap tampil tanpa saran: %d", resp.StatusCode)
	}
}

func TestPublicFormSearchesSimrsEmployees(t *testing.T) {
	names := fakeEmployees{"Ani <Wayan>", "Ani Lestari"}
	for i := 0; i < 12; i++ {
		names = append(names, "Pegawai Anindya "+string(rune('A'+i)))
	}
	_, newClient, db := newAppDB(t, server.Deps{Employees: names})
	token := seedLocation(t, db)
	c := newClient()

	// Form publik tidak memuat daftar pegawai; nama dicari per ketikan.
	_, body := c.get("/q/" + token)
	if !strings.Contains(body, `list="saran-pegawai"`) || !strings.Contains(body, `hx-get="/q/`+token+`/pegawai"`) ||
		strings.Contains(body, "Ani Lestari") {
		t.Fatal("form pelapor mencari nama per ketikan tanpa memuat seluruh daftar")
	}
	search := func(q string) (int, string) {
		resp, body := c.get("/q/" + token + "/pegawai?reporter_name=" + url.QueryEscape(q))
		return resp.StatusCode, body
	}
	if code, body := search("an"); code != 200 || strings.Contains(body, "<option") {
		t.Fatalf("kurang dari 3 huruf: tanpa hasil (%d)", code)
	}
	code, body := search("ani")
	if code != 200 || !strings.Contains(body, `<option value="Ani Lestari"></option>`) || !strings.Contains(body, `<option value="Ani &lt;Wayan&gt;"></option>`) {
		t.Fatalf("pencarian nama: %d %s", code, body)
	}
	if n := strings.Count(body, "<option"); n != 8 {
		t.Fatalf("hasil dibatasi 8 nama, dapat %d", n)
	}
	if resp, _ := c.get("/q/TIDAKADA0000000000000/pegawai?reporter_name=ani"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("hanya lewat QR yang dikenal, dapat %d", resp.StatusCode)
	}
	// Batas per IP: setelah melewati batas, hasil kosong (bukan error).
	limited := false
	for i := 0; i < 80 && !limited; i++ {
		if _, body := search("ani"); !strings.Contains(body, "<option") {
			limited = true
		}
	}
	if !limited {
		t.Fatal("pencarian nama dibatasi per IP")
	}

	// Tanpa API pegawai: form tanpa saran, endpoint tidak ada.
	_, newClient2, db2 := newAppDB(t, server.Deps{})
	token2 := seedLocation(t, db2)
	c2 := newClient2()
	if _, body := c2.get("/q/" + token2); strings.Contains(body, "saran-pegawai") {
		t.Fatal("tanpa API pegawai form tidak memuat saran")
	}
	if resp, _ := c2.get("/q/" + token2 + "/pegawai?reporter_name=ani"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("tanpa API pegawai endpoint pencarian 404, dapat %d", resp.StatusCode)
	}
}
