package server_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sikat/internal/server"
)

func TestTicketWorkflowOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	token := seedLocation(t, db)
	cat := categoryID(t, db)

	pelapor := newClient()
	ptok := pelapor.token("/q/" + token)
	resp, _ := pelapor.postMultipart("/q/"+token, map[string]string{
		"csrf_token": ptok, "reporter_name": "Ani", "ticket_category_id": cat,
		"description": "Printer lab macet total", "impact_level": "1", "affected_scope": "1",
	}, false)
	trackPath := resp.Request.URL.Path

	tech := newClient()
	tech.login("budi", "password-budi-123")
	_, body := tech.get("/app/tickets")
	id := regexp.MustCompile(`/app/tickets/(\d+)/claim`).FindStringSubmatch(body)[1]
	base := "/app/tickets/" + id
	tok := tech.token(base)
	tech.post(base+"/claim", url.Values{"csrf_token": {tok}})

	_, body = tech.get(base)
	if !strings.Contains(body, "Mulai kerjakan") || strings.Contains(body, "Selesaikan tiket") {
		t.Fatal("status Diterima: tombol Mulai kerjakan ada, Selesaikan belum")
	}
	tech.post(base+"/start", url.Values{"csrf_token": {tok}})

	// Validasi selesaikan: 422 dan panel selesaikan terbuka kembali.
	resp, body = tech.post(base+"/resolve", url.Values{"csrf_token": {tok}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Tindakan yang dilakukan wajib diisi") ||
		!regexp.MustCompile(`<details class="card"\s+open>\s*<summary[^>]*>Selesaikan tiket`).MatchString(body) {
		t.Fatalf("validasi selesaikan harus 422 dengan panel terbuka: %d", resp.StatusCode)
	}
	resp, body = tech.post(base+"/resolve", url.Values{"csrf_token": {tok}, "resolution_action": {"Bersihkan roller"}, "resolution_result": {"FIXED"}})
	if !strings.Contains(body, "Tiket diselesaikan") || !strings.Contains(body, "Penyelesaian") {
		t.Fatalf("selesaikan harus berhasil: %d", resp.StatusCode)
	}
	// Catatan internal & publik.
	tech.post(base+"/comments", url.Values{"csrf_token": {tok}, "body": {"Roller aus, perlu diganti bulan depan"}, "visibility": {"internal"}})
	tech.post(base+"/comments", url.Values{"csrf_token": {tok}, "body": {"Printer sudah bisa dipakai"}, "visibility": {"public"}})

	// Pelapor melihat catatan publik saja, lalu mengonfirmasi.
	_, body = pelapor.get(trackPath)
	if !strings.Contains(body, "Apakah masalah sudah teratasi") || !strings.Contains(body, "Printer sudah bisa dipakai") || strings.Contains(body, "Roller aus") {
		t.Fatal("halaman pelapor: konfirmasi tampil, catatan internal tidak bocor")
	}
	ptok = pelapor.token(trackPath)
	_, body = pelapor.post(trackPath+"/konfirmasi", url.Values{"csrf_token": {ptok}, "ok": {"1"}})
	if !strings.Contains(body, "Terima kasih. Tiket ditutup.") || !strings.Contains(body, "Ditutup") {
		t.Fatal("konfirmasi pelapor harus menutup tiket")
	}

	// Aksi pada tiket tertutup ditolak dengan pesan, bukan error server.
	resp, body = tech.post(base+"/start", url.Values{"csrf_token": {tok}})
	if resp.StatusCode != 200 || !strings.Contains(body, "tidak dapat dilakukan pada status tiket saat ini") {
		t.Fatalf("aksi tidak sah harus memberi pesan: %d", resp.StatusCode)
	}
	// Petugas lain tidak berhak mengubah tiket orang lain.
	testutilCreate(t, db, "sari")
	sari := newClient()
	sari.login("sari", "password-sari-123")
	stok := sari.token(base)
	if resp, _ := sari.post(base+"/reopen", url.Values{"csrf_token": {stok}, "reason": {"x"}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas tidak boleh membuka kembali tiket, dapat %d", resp.StatusCode)
	}
}

func TestActivitiesOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	var typeID, itID string
	if err := db.QueryRow(`SELECT id, it_category_id FROM activity_types WHERE name = 'Rapat'`).Scan(&typeID, new(any)); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT id FROM it_categories WHERE code = 'SW'`).Scan(&itID)

	admin := newClient()
	admin.login("admin", "password-admin-123")
	if resp, _ := admin.get("/app/activities"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin tidak mengelola kegiatan teknis, dapat %d", resp.StatusCode)
	}

	tech := newClient()
	tech.login("budi", "password-budi-123")
	tok := tech.token("/app/activities/new")
	resp, body := tech.post("/app/activities", url.Values{
		"csrf_token": {tok}, "activity_type_id": {typeID}, "it_category_id": {itID}, "title": {"Rapat koordinasi SIMRS"},
		"status": {"DONE"}, "result": {"Disepakati jadwal update"}, "started_at": {"2026-01-05T09:00"}, "ended_at": {"2026-01-05T10:30"},
	})
	if resp.StatusCode != 200 || !strings.Contains(body, "KEG-2026-000001") || !strings.Contains(body, "1 j 30 mnt") {
		t.Fatalf("catat kegiatan harus berhasil dengan nomor & durasi: %d", resp.StatusCode)
	}
	detail := resp.Request.URL.Path
	_, body = tech.get("/app/activities?bulan=2026-01")
	if !strings.Contains(body, "Rapat koordinasi SIMRS") {
		t.Fatal("kegiatan harus tampil di daftar bulan terkait")
	}
	testutilCreate(t, db, "sari")
	other := newClient()
	other.login("sari", "password-sari-123")
	if resp, _ := other.get(detail); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas lain tidak boleh melihat kegiatan yang bukan miliknya, dapat %d", resp.StatusCode)
	}
}
