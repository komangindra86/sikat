package server_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

func TestFieldCoordinatorMasterDataOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	koorID := testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)
	admin := newClient()
	admin.login("admin", "password-admin-123")

	// Dashboard admin memperingatkan bidang tanpa koordinator.
	if _, body := admin.get("/app"); !strings.Contains(body, "Belum ada koordinator aktif untuk") {
		t.Fatal("dashboard admin menampilkan bidang tanpa koordinator")
	}

	var fieldID int64
	if err := db.QueryRow(`SELECT id FROM it_categories WHERE name = 'Pelayanan & Support'`).Scan(&fieldID); err != nil {
		t.Fatal(err)
	}
	path := "/app/master/it-categories/" + itoa(fieldID)
	_, body := admin.get(path + "/edit")
	sel := regexp.MustCompile(`(?s)name="coordinator_user_id".*?</select>`).FindString(body)
	if sel == "" || !strings.Contains(sel, "User koor") || strings.Contains(sel, "User budi") || strings.Contains(sel, "User admin") {
		t.Fatalf("pilihan koordinator hanya akun ber-role Koordinator: %q", sel)
	}
	tok := admin.token(path + "/edit")
	resp, _ := admin.post(path, url.Values{"csrf_token": {tok}, "code": {"HW"}, "name": {"Pelayanan & Support"},
		"coordinator_user_id": {itoa(koorID)}, "sort_order": {"1"}, "is_active": {"1"}})
	if resp.StatusCode != 200 {
		t.Fatalf("simpan koordinator bidang: %d", resp.StatusCode)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE id = ? AND coordinator_user_id = ?`, fieldID, koorID) != 1 {
		t.Fatal("koordinator bidang tersimpan")
	}
	// Akun petugas tidak boleh dijadikan koordinator bidang.
	budiID := testutil.Count(t, db, `SELECT id FROM users WHERE username = 'budi'`)
	resp, _ = admin.post(path, url.Values{"csrf_token": {tok}, "code": {"HW"}, "name": {"Pelayanan & Support"},
		"coordinator_user_id": {itoa(int64(budiID))}, "sort_order": {"1"}, "is_active": {"1"}})
	if resp.StatusCode != 422 {
		t.Fatalf("petugas sebagai koordinator bidang harus ditolak (422), dapat %d", resp.StatusCode)
	}

	// Dashboard koordinator menyebut bidangnya; antrean default ke bidang tersebut.
	koor := newClient()
	koor.login("koor", "password-koor-123")
	if _, body := koor.get("/app"); !strings.Contains(body, "Bidang yang Anda koordinasi:") || !strings.Contains(body, "Pelayanan &amp; Support") {
		t.Fatal("dashboard koordinator menampilkan bidang yang dikoordinasi")
	}
	if _, body := koor.get("/app/tickets"); !strings.Contains(body, "Menampilkan tiket bidang yang Anda koordinasi: Pelayanan &amp; Support") {
		t.Fatal("antrean koordinator default ke bidangnya")
	}
	if _, body := koor.get("/app/tickets?semua=1"); strings.Contains(body, "Menampilkan tiket bidang yang Anda koordinasi") {
		t.Fatal("koordinator tetap dapat melihat semua bidang")
	}
}
