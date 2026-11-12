package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

func TestSLAPagesOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)

	tech := newClient()
	tech.login("budi", "password-budi-123")
	for _, p := range []string{"/app/sla", "/app/sla/report", "/app/sla/calendars"} {
		if resp, _ := tech.get(p); resp.StatusCode != http.StatusForbidden {
			t.Errorf("petugas GET %s = %d, mau 403", p, resp.StatusCode)
		}
	}

	koor := newClient()
	koor.login("koor", "password-koor-123")
	if resp, body := koor.get("/app/sla"); resp.StatusCode != 200 || !strings.Contains(body, "Eskalasi terbaru") {
		t.Fatalf("koordinator melihat dashboard SLA: %d", resp.StatusCode)
	}
	if resp, body := koor.get("/app/sla/report?bulan=2026-11"); resp.StatusCode != 200 || !strings.Contains(body, "Laporan SLA November 2026") {
		t.Fatalf("laporan bulanan: %d", resp.StatusCode)
	}
	if resp, _ := koor.get("/app/sla/calendars"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("koordinator tidak mengelola kalender, dapat %d", resp.StatusCode)
	}

	admin := newClient()
	admin.login("admin", "password-admin-123")
	_, body := admin.get("/app/sla/calendars")
	if !strings.Contains(body, "Jam kerja") || !strings.Contains(body, "Sen–Jum 08:00–16:00") {
		t.Fatal("daftar kalender harus menampilkan ringkasan jam kerja")
	}
	if _, body := admin.get("/app/master/sla-policies"); !strings.Contains(body, "Kritis (standar)") {
		t.Fatal("kebijakan SLA standar tampil di master data")
	}

	// Validasi: hari kerja dengan jam terbalik ditolak.
	tok := admin.token("/app/sla/calendars/new")
	resp, body := admin.post("/app/sla/calendars/new", url.Values{"csrf_token": {tok}, "name": {"Shift IGD"},
		"day_1": {"1"}, "start_1": {"16:00"}, "end_1": {"08:00"}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Jam selesai harus setelah jam mulai") {
		t.Fatalf("jam terbalik harus 422: %d", resp.StatusCode)
	}
	resp, body = admin.post("/app/sla/calendars/new", url.Values{"csrf_token": {tok}, "name": {"Shift IGD"},
		"day_6": {"1"}, "start_6": {"07:00"}, "end_6": {"14:00"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "Kalender disimpan") || !strings.Contains(body, "Hari libur") {
		t.Fatalf("kalender baru tersimpan: %d", resp.StatusCode)
	}
	var calID int64
	if err := db.QueryRow(`SELECT id FROM sla_calendars WHERE name = 'Shift IGD'`).Scan(&calID); err != nil {
		t.Fatal(err)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM sla_calendar_hours WHERE calendar_id = ? AND weekday = 6 AND start_time = '07:00:00'`, calID) != 1 {
		t.Fatal("jam kerja Sabtu tersimpan")
	}
	path := "/app/sla/calendars/" + itoa(calID)
	resp, body = admin.post(path+"/holidays", url.Values{"csrf_token": {tok}, "holiday_date": {"2026-12-31"}, "holiday_name": {"Cuti bersama"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "Cuti bersama") {
		t.Fatalf("hari libur ditambahkan: %d", resp.StatusCode)
	}
	resp, body = admin.post(path+"/holidays", url.Values{"csrf_token": {tok}, "holiday_date": {"2026-12-31"}, "holiday_name": {"Lagi"}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "sudah terdaftar") {
		t.Fatalf("tanggal libur ganda harus ditolak: %d", resp.StatusCode)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action IN ('sla.calendar.create','sla.holiday.create')`) != 2 {
		t.Fatal("perubahan kalender tercatat di audit")
	}
}
