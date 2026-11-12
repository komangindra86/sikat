package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

func TestAssetMaintenanceOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('PRN','Printer',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	cat, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('PRN-01','Printer Kasir',?,?,?)`, cat, now, now)
	asset, _ := res.LastInsertId()
	path := "/app/assets/" + itoa(asset)
	testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)

	tech := newClient()
	tech.login("budi", "password-budi-123")
	_, body := tech.get(path)
	for _, want := range []string{"Statistik", "Riwayat kerusakan", "Catat pemeliharaan manual"} {
		if !strings.Contains(body, want) {
			t.Errorf("halaman perangkat harus memuat %q", want)
		}
	}
	tok := tech.token(path)
	resp, body := tech.post("/app/maintenance/assets/"+itoa(asset), url.Values{"csrf_token": {tok}, "m_type": {"CORRECTIVE"}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Tindakan wajib diisi") {
		t.Fatalf("validasi entri manual harus 422: %d", resp.StatusCode)
	}
	resp, body = tech.post("/app/maintenance/assets/"+itoa(asset), url.Values{"csrf_token": {tok}, "m_type": {"CORRECTIVE"},
		"m_action": {"Ganti roller"}, "m_result": {"FIXED"}, "m_finished_at": {time.Now().Add(-time.Hour).In(time.FixedZone("WITA", 8*3600)).Format("2006-01-02T15:04")},
		"m_service_cost": {"75000"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "Riwayat pemeliharaan dicatat") || !strings.Contains(body, "Rp 75.000") {
		t.Fatalf("entri manual harus tersimpan & tampil: %d", resp.StatusCode)
	}
	if strings.Contains(body, "Batalkan entri (koreksi)") {
		t.Error("petugas tidak melihat tombol pembatalan entri")
	}
	if resp, _ := tech.post("/app/maintenance/entries/1/void", url.Values{"csrf_token": {tok}, "void_reason": {"x"}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas tidak boleh membatalkan entri, dapat %d", resp.StatusCode)
	}

	koor := newClient()
	koor.login("koor", "password-koor-123")
	if _, body := koor.get(path); !strings.Contains(body, "Batalkan entri (koreksi)") {
		t.Error("koordinator dapat membatalkan entri")
	}
	if resp, body := koor.get("/app/maintenance/alerts"); resp.StatusCode != 200 || !strings.Contains(body, "Perangkat sering rusak") {
		t.Fatalf("halaman peringatan: %d", resp.StatusCode)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
