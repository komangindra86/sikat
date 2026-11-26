package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

func TestKajianFlowOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	qr := seedLocation(t, db)
	var dept string
	if err := db.QueryRow(`SELECT id FROM departments WHERE code = 'LAB'`).Scan(&dept); err != nil {
		t.Fatal(err)
	}

	pub := newClient()
	// Jenis gangguan jalur kajian tampil di kartu bidangnya sebagai tautan ke form kajian, bukan pilihan tiket.
	_, body := pub.get("/q/" + qr)
	for _, kind := range []string{"DEVICE", "SYSTEM"} {
		if !strings.Contains(body, `href="/q/`+qr+`/kajian?kind=`+kind+`"`) {
			t.Errorf("form lapor menautkan jenis gangguan jalur kajian %s ke form kajian", kind)
		}
	}
	var deviceCat string
	if err := db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Permintaan perangkat baru'`).Scan(&deviceCat); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, `name="ticket_category_id" value="`+deviceCat+`"`) {
		t.Error("Permintaan perangkat baru tidak lagi bisa dipilih sebagai tiket di form lapor")
	}
	// Kiriman tiket dengan jenis jalur kajian (mis. form lama) ditolak dengan arahan ke kajian.
	resp, body := pub.postMultipart("/q/"+qr, map[string]string{"csrf_token": pub.token("/q/" + qr), "reporter_name": "Sari",
		"ticket_category_id": deviceCat, "description": "Minta printer baru", "impact_level": "0", "affected_scope": "1"}, false)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "dikirim lewat kajian kebutuhan") {
		t.Fatalf("tiket publik jalur kajian ditolak: %d", resp.StatusCode)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM tickets`); n != 0 {
		t.Fatal("tidak ada tiket untuk permintaan perangkat baru")
	}

	resp, body = pub.get("/q/" + qr + "/kajian?kind=SYSTEM")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `value="`+dept+`" selected`) || !strings.Contains(body, `value="SYSTEM" class="sr-only" checked`) {
		t.Fatalf("form kajian dengan unit dari QR dan jenis dari tautan terpilih: %d", resp.StatusCode)
	}
	if resp, _ := pub.get("/q/tidak-ada/kajian"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("QR tidak dikenal: %d", resp.StatusCode)
	}
	tok := pub.token("/q/" + qr + "/kajian")
	fields := map[string]string{"requester_name": "Sari", "department_id": dept, "kind": "DEVICE",
		"title": "Printer label Laboratorium", "summary": "2 unit printer label untuk tabung sampel"}

	resp, body = pub.postFile("/q/"+qr+"/kajian", tok, fields, "document", "kajian.txt", []byte("teks biasa"))
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Dokumen harus berupa PDF, Word") {
		t.Fatalf("dokumen bukan PDF/Word/Excel ditolak: %d", resp.StatusCode)
	}
	pdf := []byte("%PDF-1.7\nkajian printer label")
	resp, body = pub.postFile("/q/"+qr+"/kajian", tok, fields, "document", "Kajian Printer.pdf", pdf)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Request.URL.Path, "/k/") ||
		!strings.Contains(body, "Kajian terkirim") || !strings.Contains(body, "KJN-00001") || !strings.Contains(body, "Menunggu diterima Admin IT") {
		t.Fatalf("kirim kajian → halaman status: %d %s", resp.StatusCode, resp.Request.URL.Path)
	}
	track := resp.Request.URL.Path
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'PROPOSAL_SUBMITTED'`); n != 1 {
		t.Fatal("kajian baru memicu notifikasi ke admin")
	}
	if resp, _ := pub.get("/k/" + strings.Repeat("A", 43)); resp.StatusCode != http.StatusNotFound {
		t.Fatal("token salah tidak membuka kajian")
	}

	tech := newClient()
	tech.login("budi", "password-budi-123")
	if resp, _ := tech.get("/app/kajian"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("petugas tidak membuka kajian: %d", resp.StatusCode)
	}

	admin := newClient()
	admin.login("admin", "password-admin-123")
	_, body = admin.get("/app/kajian")
	for _, want := range []string{"Printer label Laboratorium", "1 kajian belum diterima", `href="/app/kajian"`} {
		if !strings.Contains(body, want) {
			t.Errorf("daftar kajian memuat %q", want)
		}
	}
	if _, body := admin.get("/app/kajian?status=RECEIVED"); strings.Contains(body, "Printer label Laboratorium") {
		t.Error("filter status Diterima")
	}
	resp, body = admin.get("/app/kajian/1/dokumen")
	if resp.StatusCode != http.StatusOK || body != string(pdf) || resp.Header.Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatalf("unduh dokumen: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}

	tok = admin.token("/app/kajian/1")
	if resp, _ := admin.post("/app/kajian/1/terima", url.Values{"csrf_token": {tok}, "received_note": {"Masuk usulan anggaran 2027"}}); resp.Request.URL.Path != "/app/kajian/1" {
		t.Fatalf("terima kajian kembali ke detail, di %s", resp.Request.URL.Path)
	}
	if _, body := admin.get("/app/kajian/1"); !strings.Contains(body, "Diterima oleh") {
		t.Error("detail menampilkan penerima")
	}
	if _, body := admin.post("/app/kajian/1/terima", url.Values{"csrf_token": {tok}}); !strings.Contains(body, "sudah diterima sebelumnya") {
		t.Error("terima dua kali ditolak dengan pesan")
	}
	if _, body := pub.get(track); !strings.Contains(body, "diterima Admin IT") || !strings.Contains(body, "Masuk usulan anggaran 2027") {
		t.Error("pemohon melihat status diterima dan catatan admin")
	}
	if _, body := admin.get("/app/settings/notifications"); !strings.Contains(body, "Kajian baru masuk") {
		t.Error("event kajian dapat diatur di preferensi notifikasi")
	}
}
