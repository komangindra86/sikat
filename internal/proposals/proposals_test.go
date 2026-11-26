package proposals_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"sikat/internal/attachments"
	"sikat/internal/clock"
	"sikat/internal/notifications"
	"sikat/internal/proposals"
	"sikat/internal/rbac"
	"sikat/internal/testutil"
)

// fileHeader membuat *multipart.FileHeader seperti hasil unggahan form.
func fileHeader(t *testing.T, name string, data []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("document", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	form, err := multipart.NewReader(&buf, mw.Boundary()).ReadForm(32 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form.File["document"][0]
}

func TestReadFile(t *testing.T) {
	pdf := []byte("%PDF-1.7\n isi kajian")
	f, msg, err := proposals.ReadFile(fileHeader(t, `C:\Dokumen\Kajian Printer.PDF`, pdf))
	if err != nil || msg != "" || f.Ext != "pdf" || f.Mime != "application/pdf" || f.Name != "Kajian Printer.PDF" {
		t.Fatalf("PDF valid: %+v %q %v", f, msg, err)
	}
	if f, msg, _ := proposals.ReadFile(fileHeader(t, "kajian.docx", []byte("PK\x03\x04 zip"))); msg != "" || !strings.Contains(f.Mime, "wordprocessingml") {
		t.Fatalf("docx valid: %q", msg)
	}
	for name, data := range map[string][]byte{
		"kajian.exe":  []byte("MZ\x90\x00"),      // jenis tidak diterima
		"kajian.pdf":  []byte("<html>bukan pdf"), // isi tidak sesuai ekstensi
		"kajian.xlsx": []byte("%PDF-1.4"),
		"kajian.doc":  {},
	} {
		if _, msg, err := proposals.ReadFile(fileHeader(t, name, data)); err != nil || msg == "" {
			t.Errorf("%s harus ditolak: %q %v", name, msg, err)
		}
	}
	if _, msg, _ := proposals.ReadFile(nil); msg == "" {
		t.Error("dokumen wajib dilampirkan")
	}
	big := append([]byte("%PDF-"), make([]byte, proposals.MaxFileBytes)...)
	if _, msg, _ := proposals.ReadFile(fileHeader(t, "besar.pdf", big)); !strings.Contains(msg, "10 MB") {
		t.Errorf("dokumen > 10 MB ditolak: %q", msg)
	}
}

func TestParseDraftAndSize(t *testing.T) {
	_, _, errs := proposals.ParseDraft(testutil.Form(map[string]string{"kind": "LAIN", "title": strings.Repeat("x", 201)}))
	for _, f := range []string{"kind", "title", "summary", "requester_name", "department_id"} {
		if errs[f] == "" {
			t.Errorf("field %s harus error: %v", f, errs)
		}
	}
	d, _, errs := proposals.ParseDraft(testutil.Form(map[string]string{"kind": "SYSTEM", "title": " Aplikasi antrean ", "summary": "x",
		"requester_name": "Sari", "department_id": "3"}))
	if len(errs) != 0 || d.Title != "Aplikasi antrean" || d.DepartmentID != 3 {
		t.Fatalf("draft valid: %+v %v", d, errs)
	}
	for n, want := range map[int64]string{1: "1 KB", 850 * 1024: "850 KB", 2516582: "2,4 MB"} {
		if got := proposals.SizeText(n); got != want {
			t.Errorf("SizeText(%d) = %q, ingin %q", n, got, want)
		}
	}
}

func TestSubmitNotifiesAdminAndReceive(t *testing.T) {
	db := testutil.DB(t)
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC))
	adminID := testutil.CreateUser(t, db, "admin", "ADMIN", "password-admin-123", false)
	testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)
	now := clk.Now()
	res, err := db.Exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('FAR', 'Farmasi', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	dept, _ := res.LastInsertId()
	store := attachments.Store{Dir: t.TempDir()}
	svc := proposals.NewService(db, clk, store)

	pdf := []byte("%PDF-1.7 kajian printer label")
	f, _, _ := proposals.ReadFile(fileHeader(t, "kajian.pdf", pdf))
	d := proposals.Draft{Kind: "DEVICE", Title: "Printer label Farmasi", Summary: "2 unit printer label", DepartmentID: dept, RequesterName: "Sari"}
	if _, errs, err := svc.Submit(ctx, proposals.Draft{Kind: "DEVICE", Title: "x", Summary: "x", DepartmentID: 999, RequesterName: "x"}, f); err != nil || errs["department_id"] == "" {
		t.Fatalf("unit tidak dikenal ditolak: %v %v", errs, err)
	}
	sub, errs, err := svc.Submit(ctx, d, f)
	if err != nil || len(errs) > 0 {
		t.Fatalf("submit: %v %v", errs, err)
	}
	p, err := svc.GetByToken(ctx, sub.Token)
	if err != nil || p.ID != sub.ID || p.No() != "KJN-00001" || p.Status != proposals.StatusSubmitted || p.DepartmentName.String != "Farmasi" {
		t.Fatalf("kajian dari token: %+v %v", p, err)
	}
	if fh, err := store.Open(p.FileKey); err != nil {
		t.Fatal(err)
	} else {
		got, _ := io.ReadAll(fh)
		fh.Close()
		if !bytes.Equal(got, pdf) || !strings.HasSuffix(p.FileKey, ".pdf") {
			t.Fatal("dokumen tersimpan utuh dengan ekstensi aslinya")
		}
	}
	if _, err := svc.GetByToken(ctx, strings.Repeat("A", 43)); err != proposals.ErrNotFound {
		t.Fatal("token salah tidak membuka kajian")
	}

	notif := notifications.NewService(db, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, "")
	if _, err := notif.Process(ctx); err != nil {
		t.Fatal(err)
	}
	var title, body, url, channel string
	if err := db.QueryRow(`SELECT title, body, url, channel FROM notifications WHERE user_id = ?`, adminID).Scan(&title, &body, &url, &channel); err != nil {
		t.Fatal(err)
	}
	if title != "Kajian baru: Printer label Farmasi" || body != "Perangkat baru · Farmasi · Dari Sari" || url != "/app/kajian/1" || channel != "PUSH" {
		t.Fatalf("notifikasi admin: %q | %q | %q | %s", title, body, url, channel)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.user_id WHERE u.username = 'koor'`); n != 0 {
		t.Fatal("koordinator tidak menerima notifikasi kajian secara bawaan")
	}

	admin := &rbac.User{ID: adminID, RoleCode: rbac.RoleAdmin}
	if errs, err := svc.Receive(ctx, p.ID, admin, strings.Repeat("x", 1001)); err != nil || errs["received_note"] == "" {
		t.Fatal("catatan > 1000 karakter ditolak")
	}
	if errs, err := svc.Receive(ctx, p.ID, admin, "Dibahas di rapat anggaran"); err != nil || len(errs) > 0 {
		t.Fatalf("terima: %v %v", errs, err)
	}
	if _, err := svc.Receive(ctx, p.ID, admin, ""); err != proposals.ErrReceived {
		t.Fatalf("terima dua kali: %v", err)
	}
	p, _ = svc.Get(ctx, p.ID)
	if !p.Received() || p.ReceivedNote.String != "Dibahas di rapat anggaran" || !p.ReceivedAt.Valid {
		t.Fatalf("status diterima: %+v", p)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'proposal.receive' AND entity_id = ?`, p.ID); n != 1 {
		t.Fatal("penerimaan kajian tercatat di jejak audit")
	}
}
