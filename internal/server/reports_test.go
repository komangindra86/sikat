package server_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

func TestReportsAndDashboardsOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)

	koor := newClient()
	koor.login("koor", "password-koor-123")
	if _, body := koor.get("/app/reports"); !strings.Contains(body, "Laporan Koordinator IT") || !strings.Contains(body, "Downtime perangkat") {
		t.Fatal("daftar laporan menampilkan seluruh laporan")
	}
	resp, body := koor.get("/app/reports/koordinator?periode=bulan&bulan=2026-11")
	if resp.StatusCode != 200 || !strings.Contains(body, "Hasil/Keterangan") || !strings.Contains(body, "Periode November 2026") {
		t.Fatalf("laporan koordinator: %d", resp.StatusCode)
	}
	if !strings.Contains(body, `name="petugas"`) {
		t.Fatal("koordinator dapat memfilter per petugas")
	}
	if resp, _ := koor.get("/app/reports/tidak-ada"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("laporan tidak dikenal harus 404, dapat %d", resp.StatusCode)
	}
	if resp, body := koor.get("/app/reports/bulanan?periode=bulan&bulan=2026-13"); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Bulan tidak valid") {
		t.Fatalf("filter tidak valid harus 422, dapat %d", resp.StatusCode)
	}

	// Ekspor: tipe konten, nama file, dan jejak audit.
	download := func(c *client, path string) (*http.Response, []byte) {
		t.Helper()
		r, err := c.http.Get(c.base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, b
	}
	r, b := download(koor, "/app/reports/sla?periode=tahun&tahun=2026&format=xlsx")
	if r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Type"), "spreadsheetml") ||
		r.Header.Get("Content-Disposition") != `attachment; filename="sikat-sla-2026.xlsx"` || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("ekspor Excel: %d %v", r.StatusCode, r.Header)
	}
	r, b = download(koor, "/app/reports/koordinator?periode=tanggal&dari=2026-11-01&sampai=2026-11-13&format=pdf")
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatalf("ekspor PDF: %d %v", r.StatusCode, r.Header)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'report.export'`) != 2 {
		t.Fatal("setiap ekspor tercatat di audit")
	}

	// Petugas: hanya data sendiri, tanpa filter petugas.
	tech := newClient()
	tech.login("budi", "password-budi-123")
	_, body = tech.get("/app/reports/koordinator")
	if !strings.Contains(body, "Data pribadi: User budi") || strings.Contains(body, `name="petugas"`) {
		t.Fatal("petugas hanya melihat laporan pribadi tanpa pilihan petugas lain")
	}
	if _, body := tech.get("/app/reports"); !strings.Contains(body, "pekerjaan Anda sendiri") {
		t.Fatal("daftar laporan petugas menjelaskan cakupan data pribadi")
	}

	// Dashboard per role.
	if _, body := tech.get("/app"); !strings.Contains(body, "SLA mendekati batas") || !strings.Contains(body, "Tersedia untuk saya") || !strings.Contains(body, "Beban kerja tim") {
		t.Fatal("dashboard petugas")
	}
	if _, body := koor.get("/app"); !strings.Contains(body, "Kinerja petugas") || !strings.Contains(body, "Distribusi gangguan") || !strings.Contains(body, "Kegiatan internal") {
		t.Fatal("dashboard koordinator")
	}
	admin := newClient()
	admin.login("admin", "password-admin-123")
	_, body = admin.get("/app")
	if !strings.Contains(body, "Status sistem") || !strings.Contains(body, "Integrasi SIMRS") || !strings.Contains(body, "SIMRS belum terhubung") || !strings.Contains(body, "Backup database") {
		t.Fatal("dashboard admin menampilkan status sistem apa adanya")
	}
	// Isi beranda (semua role) diperbarui otomatis lewat HTMX, memuat ulang bagiannya sendiri.
	if !strings.Contains(body, `id="beranda" hx-get="/app" hx-trigger="every 30s" hx-select="#beranda"`) {
		t.Fatal("beranda harus diperbarui otomatis tiap 30 detik")
	}
	if resp, _ := admin.get("/app/reports/beban-kerja"); resp.StatusCode != 200 {
		t.Fatalf("admin dapat membuka laporan, dapat %d", resp.StatusCode)
	}
}
