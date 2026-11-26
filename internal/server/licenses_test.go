package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/server"
	"sikat/internal/testutil"
)

func TestLicenseMasterList(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	admin := newClient()
	admin.login("admin", "password-admin-123")
	tech := newClient()
	tech.login("budi", "password-budi-123")
	if resp, _ := tech.get("/app/master/licenses"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas tidak mengelola lisensi, dapat %d", resp.StatusCode)
	}

	today := time.Now().In(clock.Location)
	soon := today.AddDate(0, 0, 10).Format("2006-01-02")
	tok := admin.token("/app/master/licenses/new")
	// Tanggal berakhir sebelum mulai berlaku → ditolak dengan pesan di field.
	resp, body := admin.post("/app/master/licenses", url.Values{"csrf_token": {tok}, "name": {"Office 365"},
		"started_at": {soon}, "expires_at": {today.AddDate(0, 0, -1).Format("2006-01-02")}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Tanggal berakhir tidak boleh sebelum tanggal mulai berlaku.") {
		t.Fatalf("tanggal terbalik harus ditolak: %d", resp.StatusCode)
	}
	if resp, _ := admin.post("/app/master/licenses", url.Values{"csrf_token": {tok}, "name": {"Office 365"}, "vendor": {"Microsoft"},
		"quantity": {"25"}, "expires_at": {soon}}); resp.Request.URL.Path != "/app/master/licenses" {
		t.Fatalf("simpan lisensi harus kembali ke daftar, berakhir di %s", resp.Request.URL.Path)
	}
	admin.post("/app/master/licenses", url.Values{"csrf_token": {tok}, "name": {"Domain"}})

	_, body = admin.get("/app/master/licenses")
	for _, want := range []string{"LIS-001", "Office 365", "Microsoft", `badge-warn whitespace-nowrap">10 hari lagi`, `badge-muted whitespace-nowrap">Tanpa batas`} {
		if !strings.Contains(body, want) {
			t.Errorf("daftar lisensi harus memuat %q", want)
		}
	}
	if i, j := strings.Index(body, "Office 365"), strings.Index(body, "Domain"); i < 0 || j < i {
		t.Error("lisensi yang paling dekat berakhir tampil lebih dulu; tanpa batas di akhir")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'licenses'`); n == 0 {
		t.Error("perubahan lisensi tercatat di jejak audit")
	}
	if _, body := admin.get("/app/settings/notifications"); !strings.Contains(body, "Lisensi segera habis") {
		t.Error("event lisensi dapat diatur di preferensi notifikasi")
	}
}
