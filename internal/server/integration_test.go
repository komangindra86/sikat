package server_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

// postFile mengunggah form multipart (dengan token CSRF dari halaman).
func (c *client) postFile(path, csrf string, fields map[string]string, fileField, fileName string, data []byte) (*http.Response, string) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", csrf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile(fileField, fileName)
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.base+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestIntegrationPagesOverHTTP(t *testing.T) {
	_, newClient, db := newAppDB(t, server.Deps{})
	testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)

	koor := newClient()
	koor.login("koor", "password-koor-123")
	if resp, _ := koor.get("/app/integrations"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("hanya admin yang mengelola integrasi, dapat %d", resp.StatusCode)
	}

	admin := newClient()
	admin.login("admin", "password-admin-123")
	_, body := admin.get("/app/integrations")
	if !strings.Contains(body, "SIMRS belum terhubung") || !strings.Contains(body, "Impor CSV") || strings.Contains(body, `value="EMPLOYEE"`) {
		t.Fatal("halaman integrasi menjelaskan status SIMRS & menyediakan impor CSV")
	}
	resp, tpl := admin.get("/app/integrations/template/DEPARTMENT.csv")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") || !strings.Contains(tpl, "external_id;kode;nama") {
		t.Fatalf("template CSV: %d %q", resp.StatusCode, tpl)
	}

	// Unggah → pratinjau (belum ada perubahan) → terapkan → log.
	tok := admin.token("/app/integrations")
	csv := "external_id;kode;nama;layanan_kritis;aktif\nU-1;IGD;Instalasi Gawat Darurat;ya;ya\nU-2;KEU;;tidak;ya\n"
	resp, body = admin.postFile("/app/integrations/imports", tok, map[string]string{"entity": "DEPARTMENT", "policy": "KEEP_LOCAL"}, "file", "unit.csv", []byte(csv))
	if resp.StatusCode != 200 || !strings.Contains(body, "Pratinjau impor Unit") || !strings.Contains(body, "Belum ada data yang diubah") {
		t.Fatalf("pratinjau: %d", resp.StatusCode)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM departments`) != 0 {
		t.Fatal("pratinjau tidak menyimpan data")
	}
	if !strings.Contains(body, "Kolom wajib kosong: nama") {
		t.Fatal("pratinjau menampilkan galat per baris")
	}
	action := regexp.MustCompile(`action="(/app/integrations/imports/\d+/apply)"`).FindStringSubmatch(body)
	if action == nil {
		t.Fatal("tombol terapkan tidak ada")
	}
	resp, body = admin.post(action[1], map[string][]string{"csrf_token": {tok}})
	if resp.StatusCode != 200 || !strings.Contains(body, "Impor diterapkan") || !strings.Contains(body, "Sebagian") {
		t.Fatalf("terapkan: %d", resp.StatusCode)
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM departments WHERE code = 'IGD' AND source = 'CSV'`) != 1 {
		t.Fatal("data unit tersimpan dari CSV")
	}
	if _, body := admin.get("/app/integrations/logs"); !strings.Contains(body, "unit.csv") {
		t.Fatal("log sinkronisasi mencatat impor")
	}

	// File tidak valid ditolak saat unggah dengan pesan jelas.
	resp, body = admin.postFile("/app/integrations/imports", tok, map[string]string{"entity": "ROOM"}, "file", "ruang.csv", []byte("kode,nama\nA,B\n"))
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "kolom wajib tidak ada") {
		t.Fatalf("CSV tanpa kolom wajib: %d", resp.StatusCode)
	}

	// Pegawai tidak lagi diimpor ke SIKAT (diambil dari API pegawai SIMRS).
	if resp, _ := admin.postFile("/app/integrations/imports", tok, map[string]string{"entity": "EMPLOYEE"}, "file", "pegawai.csv",
		[]byte("nip,nama,unit\n1987,Ners Ani Lestari,U-1\n")); resp.StatusCode == http.StatusOK {
		t.Fatal("impor pegawai harus ditolak")
	}
	if resp, _ := admin.get("/app/master/employees"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("master data pegawai sudah dihapus, dapat %d", resp.StatusCode)
	}
	if _, body := admin.get("/app"); !strings.Contains(body, "Impor terakhir") {
		t.Fatal("dashboard admin menampilkan aktivitas integrasi")
	}
}
