package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

// Audit keamanan PHASE 9: mengganti/me-reset password mengakhiri session lain
// milik pengguna tersebut (mis. HP yang hilang), session saat ini tetap aktif.
func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	hp := newClient()
	hp.login("budi", "password-budi-123")
	pc := newClient()
	pc.login("budi", "password-budi-123")
	noRedirect := func(c *client) {
		c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	noRedirect(hp)
	noRedirect(pc)
	if resp, _ := hp.get("/app"); resp.StatusCode != 200 {
		t.Fatalf("session HP aktif, dapat %d", resp.StatusCode)
	}

	tok := pc.token("/app/password")
	resp, _ := pc.post("/app/password", url.Values{"csrf_token": {tok}, "current": {"password-budi-123"},
		"new": {"password-baru-456"}, "confirm": {"password-baru-456"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("ganti password: %d", resp.StatusCode)
	}
	if resp, _ := pc.get("/app"); resp.StatusCode != 200 {
		t.Fatalf("session yang mengganti password tetap aktif, dapat %d", resp.StatusCode)
	}
	if resp, _ := hp.get("/app"); resp.StatusCode != http.StatusSeeOther || !strings.HasSuffix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("session lain harus berakhir setelah password diganti, dapat %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Reset oleh admin juga mengakhiri session pengguna tersebut.
	admin := newClient()
	admin.login("admin", "password-admin-123")
	budiID := itoa(int64(testutil.Count(t, db, `SELECT id FROM users WHERE username = 'budi'`)))
	atok := admin.token("/app/users/" + budiID + "/edit")
	admin.post("/app/users/"+budiID+"/reset-password", url.Values{"csrf_token": {atok}})
	if resp, _ := pc.get("/app"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset password oleh admin mengakhiri session pengguna, dapat %d", resp.StatusCode)
	}
}

func TestBackupPageAccess(t *testing.T) {
	_, newClient, _ := newAppDB(t, server.Deps{DisableBackup: true})
	tech := newClient()
	tech.login("budi", "password-budi-123")
	if resp, _ := tech.get("/app/backups"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas tidak boleh membuka backup, dapat %d", resp.StatusCode)
	}
	admin := newClient()
	admin.login("admin", "password-admin-123")
	resp, body := admin.get("/app/backups")
	if resp.StatusCode != 200 || !strings.Contains(body, "Backup database") || !strings.Contains(body, "Uji restore") {
		t.Fatalf("halaman backup admin: %d", resp.StatusCode)
	}
	if _, body := admin.get("/app"); !strings.Contains(body, "Belum ada backup") {
		t.Fatal("dashboard admin menampilkan status backup")
	}
}
