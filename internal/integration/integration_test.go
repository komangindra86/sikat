package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/integration"
	"sikat/internal/testutil"
)

func TestParseCSV(t *testing.T) {
	// Pemisah ';', BOM Excel, kolom tambahan diabaikan, baris kosong dilewati.
	data := []byte("\xef\xbb\xbfexternal_id;kode;nama;layanan_kritis;aktif;catatan\nU-1;igd;Instalasi Gawat Darurat;ya;;x\n\nU-2;KEU;;tidak;ya;\nU-3;LAB;Laboratorium;mungkin;ya;\n")
	b, err := integration.ParseCSV(integration.EntityDepartment, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Departments) != 1 || !b.Departments[0].Critical || !b.Departments[0].Active || b.Departments[0].Row != 2 {
		t.Fatalf("baris valid: %+v", b.Departments)
	}
	if !strings.Contains(b.RowErrors[4], "nama") || !strings.Contains(b.RowErrors[5], "layanan_kritis") {
		t.Fatalf("galat per baris: %v", b.RowErrors)
	}
	if _, err := integration.ParseCSV(integration.EntityRoom, []byte("external_id,kode,nama\nR-1,A,B\n")); err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("kolom wajib yang hilang harus ditolak: %v", err)
	}
	if _, err := integration.ParseCSV(integration.EntityDepartment, []byte{0xff, 0xfe, 'a'}); err == nil {
		t.Fatal("file bukan UTF-8 ditolak")
	}
	tpl := integration.Template(integration.EntityAsset)
	if b, err := integration.ParseCSV(integration.EntityAsset, tpl); err != nil || len(b.Assets) != 1 {
		t.Fatalf("template harus dapat dibaca ulang: %v", err)
	}
}

type fx struct {
	db  *sql.DB
	clk *clock.Fake
}

func setup(t *testing.T) fx {
	db := testutil.DB(t)
	return fx{db: db, clk: clock.NewFake(time.Date(2026, 11, 18, 2, 0, 0, 0, time.UTC))}
}

func (f fx) apply(t *testing.T, entity, csv string, o integration.Options) integration.Result {
	t.Helper()
	b, err := integration.ParseCSV(entity, []byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	r, err := integration.Apply(context.Background(), f.db, b, o, f.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func action(r integration.Result, ext string) string {
	for _, x := range r.Rows {
		if x.ExternalID == ext {
			return x.Action
		}
	}
	return ""
}

const deptHeader = "external_id,kode,nama,layanan_kritis,aktif\n"

func TestApplyMappingConflictsAndMissing(t *testing.T) {
	f := setup(t)
	csv := deptHeader + "U-1,IGD,Instalasi Gawat Darurat,ya,ya\nU-2,KEU,Keuangan,tidak,ya\n"

	// Pratinjau tidak menyimpan apa pun.
	if r := f.apply(t, integration.EntityDepartment, csv, integration.Options{DryRun: true}); r.Inserted != 2 {
		t.Fatalf("pratinjau: %+v", r)
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments`) != 0 {
		t.Fatal("pratinjau tidak boleh menyimpan")
	}

	r := f.apply(t, integration.EntityDepartment, csv, integration.Options{})
	if r.Inserted != 2 || testutil.Count(t, f.db, `SELECT COUNT(*) FROM external_id_mappings WHERE source = 'CSV' AND entity_type = 'DEPARTMENT'`) != 2 {
		t.Fatalf("impor pertama: %+v", r)
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments WHERE source = 'CSV' AND is_critical_service AND code = 'IGD'`) != 1 {
		t.Fatal("data tersimpan dengan sumber CSV")
	}
	// Impor ulang file yang sama: tidak ada perubahan.
	f.clk.Advance(time.Hour)
	if r := f.apply(t, integration.EntityDepartment, csv, integration.Options{}); r.Unchanged != 2 || r.Inserted+r.Updated != 0 {
		t.Fatalf("impor ulang: %+v", r)
	}
	// Sumber berubah, lokal tidak diubah → diperbarui.
	f.clk.Advance(time.Hour)
	csv2 := deptHeader + "U-1,IGD,IGD Terpadu,ya,ya\nU-2,KEU,Keuangan,tidak,ya\n"
	if r := f.apply(t, integration.EntityDepartment, csv2, integration.Options{}); action(r, "U-1") != integration.ActUpdate {
		t.Fatalf("perubahan sumber diterapkan: %+v", r.Rows)
	}

	// Admin mengubah data secara manual setelah sinkron → sumber TIDAK menimpa.
	f.clk.Advance(time.Hour)
	if _, err := f.db.Exec(`UPDATE departments SET name = 'IGD (diedit admin)', local_modified_at = ? WHERE code = 'IGD'`, f.clk.Now()); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(time.Hour)
	csv3 := deptHeader + "U-1,IGD,Instalasi Gawat Darurat Baru,ya,ya\nU-2,KEU,Keuangan,tidak,ya\n"
	r = f.apply(t, integration.EntityDepartment, csv3, integration.Options{})
	if action(r, "U-1") != integration.ActConflict || r.Conflicts != 1 {
		t.Fatalf("perubahan lokal harus dilindungi (konflik): %+v", r.Rows)
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments WHERE name = 'IGD (diedit admin)'`) != 1 {
		t.Fatal("data lokal tidak boleh tertimpa")
	}
	// Sumber yang tidak berubah sejak sinkron terakhir bukan konflik walau lokal diedit.
	if r := f.apply(t, integration.EntityDepartment, csv2, integration.Options{}); action(r, "U-1") != integration.ActUnchanged {
		t.Fatalf("sumber sama dengan sinkron terakhir: %+v", r.Rows)
	}
	// Keputusan admin: utamakan data sumber.
	r = f.apply(t, integration.EntityDepartment, csv3, integration.Options{Policy: integration.PreferSource})
	if action(r, "U-1") != integration.ActUpdate || testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments WHERE name = 'Instalasi Gawat Darurat Baru' AND local_modified_at IS NULL`) != 1 {
		t.Fatalf("PreferSource menimpa dan membersihkan tanda lokal: %+v", r.Rows)
	}

	// Data lengkap: yang hilang dinonaktifkan (tidak dihapus).
	f.clk.Advance(time.Hour)
	r = f.apply(t, integration.EntityDepartment, deptHeader+"U-1,IGD,Instalasi Gawat Darurat Baru,ya,ya\n", integration.Options{MarkMissing: true})
	if action(r, "U-2") != integration.ActDeactivate || testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments WHERE code = 'KEU' AND is_active = FALSE`) != 1 {
		t.Fatalf("data hilang dinonaktifkan: %+v", r.Rows)
	}
	// Muncul lagi → aktif kembali.
	if r := f.apply(t, integration.EntityDepartment, csv3, integration.Options{}); testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments WHERE code = 'KEU' AND is_active`) != 1 {
		t.Fatalf("muncul lagi di sumber → aktif: %+v", r.Rows)
	}
}

func TestApplyLinksExistingLocalDataSafely(t *testing.T) {
	f := setup(t)
	now := f.clk.Now()
	if _, err := f.db.Exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('IGD','Instalasi Gawat Darurat',?,?), ('KEU','Bagian Keuangan',?,?)`, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	r := f.apply(t, integration.EntityDepartment, deptHeader+"U-1,igd,Instalasi Gawat Darurat,tidak,ya\nU-2,KEU,Keuangan,tidak,ya\nU-3,LAB,Laboratorium,tidak,ya\nU-3,LAB2,Lab Dua,tidak,ya\n", integration.Options{})
	if action(r, "U-1") != integration.ActLink {
		t.Fatalf("data lokal berkode sama & isi sama ditautkan: %+v", r.Rows)
	}
	if action(r, "U-2") != integration.ActConflict || testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments WHERE name = 'Bagian Keuangan'`) != 1 {
		t.Fatalf("data lokal berbeda tidak ditimpa: %+v", r.Rows)
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM external_id_mappings WHERE external_id = 'U-2'`) != 0 {
		t.Fatal("konflik penautan tidak membuat pemetaan (agar tetap terlindungi)")
	}
	if r.Inserted != 1 || r.Failed != 1 {
		t.Fatalf("external_id ganda ditolak: %+v", r)
	}
}

func TestApplyRoomsAssetsResolveReferences(t *testing.T) {
	f := setup(t)
	now := f.clk.Now()
	if _, err := f.db.Exec(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('PC','Komputer',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	f.apply(t, integration.EntityDepartment, deptHeader+"U-1,IGD,Instalasi Gawat Darurat,ya,ya\nU-2,LAB,Laboratorium,tidak,ya\n", integration.Options{})

	r := f.apply(t, integration.EntityRoom, "external_id,kode,nama,unit,lokasi\nR-1,IGD-TRS,Ruang Triase,U-1,Gedung A\nR-2,LAB-1,Lab Darah,LAB,\nR-3,X,Ruang X,U-9,\n", integration.Options{})
	if r.Inserted != 2 || action(r, "R-3") != integration.ActError {
		t.Fatalf("ruangan: unit via external_id atau kode; unit tak dikenal ditolak: %+v", r.Rows)
	}
	r = f.apply(t, integration.EntityAsset, "external_id,kode_aset,nama,kategori,unit,ruangan,merek\nA-1,IGD-PC-01,PC Triase,PC,,R-1,Lenovo\nA-2,LAB-PC-01,PC Lab,Komputer,U-1,R-2,\nA-3,X-1,Mesin X,Alat Lab,,,\n", integration.Options{})
	if action(r, "A-1") != integration.ActInsert || action(r, "A-2") != integration.ActError || action(r, "A-3") != integration.ActError {
		t.Fatalf("perangkat: unit dari ruangan; ruangan beda unit & kategori tak dikenal ditolak: %+v", r.Rows)
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM assets a JOIN departments d ON d.id = a.department_id WHERE a.asset_code = 'IGD-PC-01' AND d.code = 'IGD' AND a.status = 'ACTIVE'`) != 1 {
		t.Fatal("perangkat tersimpan dengan unit dari ruangan")
	}
	// Perangkat yang hilang dari sumber tidak diubah statusnya.
	r = f.apply(t, integration.EntityAsset, "external_id,kode_aset,nama,kategori\nA-9,LAB-PC-09,PC Baru,PC\n", integration.Options{MarkMissing: true})
	if action(r, "A-1") != integration.ActMissing || testutil.Count(t, f.db, `SELECT COUNT(*) FROM assets WHERE asset_code = 'IGD-PC-01' AND status = 'ACTIVE'`) != 1 {
		t.Fatalf("perangkat hilang hanya ditandai: %+v", r.Rows)
	}
}

// fakeSIMRS mensimulasikan adapter SIMRS untuk menguji jadwal & retry.
type fakeSIMRS struct{ fails int }

func (p *fakeSIMRS) Kind() string   { return integration.ProviderSimrsAPI }
func (p *fakeSIMRS) Source() string { return "SIMRS" }
func (p *fakeSIMRS) Departments(context.Context) ([]integration.Department, error) {
	if p.fails > 0 {
		p.fails--
		return nil, errors.New("timeout menghubungi SIMRS")
	}
	return []integration.Department{{ExternalID: "S-1", Code: "IGD", Name: "Instalasi Gawat Darurat", Active: true}}, nil
}

func TestScheduledSyncRetriesAndLogs(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	svc := integration.NewService(f.db, f.clk, nil)
	admin := testutil.CreateUser(t, f.db, "admin", "ADMIN", "password-admin-123", false)

	// Tanpa adapter: tidak bisa memilih SIMRS (tidak ada integrasi palsu); sinkron lokal = no-op jujur.
	if err := svc.SetProvider(ctx, admin, integration.EntityDepartment, integration.ProviderSimrsAPI, 60, ""); err == nil {
		t.Fatal("SIMRS tidak boleh dipilih sebelum adapternya tersedia")
	}
	if _, err := svc.Sync(ctx, integration.EntityDepartment, "MANUAL", admin); !errors.Is(err, integration.ErrLocalSource) {
		t.Fatalf("sumber lokal: %v", err)
	}
	if n, err := svc.RunDue(ctx); err != nil || n != 0 {
		t.Fatalf("worker tanpa sumber luar: %d %v", n, err)
	}

	p := &fakeSIMRS{fails: 2}
	svc.RegisterProvider(p)
	if err := svc.SetProvider(ctx, admin, integration.EntityDepartment, integration.ProviderSimrsAPI, 60, ""); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.RunDue(ctx); n != 0 {
		t.Fatal("percobaan pertama gagal")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_sync_logs WHERE status = 'FAILED' AND error_message LIKE '%timeout%'`) != 1 ||
		testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_configs WHERE entity_type = 'DEPARTMENT' AND retry_count = 1 AND next_retry_at IS NOT NULL`) != 1 {
		t.Fatal("kegagalan tercatat dan dijadwalkan ulang")
	}
	// Belum jatuh tempo → tidak dicoba.
	svc.RunDue(ctx)
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_sync_logs`) != 1 {
		t.Fatal("retry menunggu jedanya")
	}
	f.clk.Advance(6 * time.Minute)
	svc.RunDue(ctx) // gagal lagi (percobaan ke-2)
	f.clk.Advance(11 * time.Minute)
	if n, _ := svc.RunDue(ctx); n != 1 {
		t.Fatal("percobaan ketiga berhasil")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_sync_logs WHERE trigger_type = 'RETRY' AND attempt = 3 AND status = 'SUCCESS' AND inserted = 1`) != 1 {
		t.Fatal("sinkron hasil retry tercatat")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_configs WHERE entity_type = 'DEPARTMENT' AND retry_count = 0 AND next_retry_at IS NULL AND last_success_at IS NOT NULL`) != 1 {
		t.Fatal("retry direset setelah berhasil")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM departments WHERE source = 'SIMRS' AND code = 'IGD'`) != 1 {
		t.Fatal("data SIMRS tersimpan lokal")
	}

	// Batas percobaan ulang: setelah max_retries, berhenti sampai jadwal berikutnya.
	if _, err := f.db.Exec(`UPDATE integration_configs SET max_retries = 1 WHERE entity_type = 'DEPARTMENT'`); err != nil {
		t.Fatal(err)
	}
	p.fails = 5
	svc.Sync(ctx, integration.EntityDepartment, "MANUAL", admin)
	f.clk.Advance(6 * time.Minute)
	svc.RunDue(ctx)
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_configs WHERE entity_type = 'DEPARTMENT' AND next_retry_at IS NULL AND retry_count = 2`) != 1 {
		t.Fatal("setelah batas retry, tidak dijadwalkan ulang")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_sync_logs WHERE error_message LIKE '%batas 1 percobaan ulang%'`) != 1 {
		t.Fatal("log menjelaskan batas percobaan ulang")
	}
}

func TestCSVImportPreviewApplyCancel(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	svc := integration.NewService(f.db, f.clk, nil)
	admin := testutil.CreateUser(t, f.db, "admin", "ADMIN", "password-admin-123", false)

	if _, err := svc.CreateImport(ctx, admin, integration.EntityDepartment, "salah.csv", []byte("a,b\n1,2\n"), false, ""); err == nil {
		t.Fatal("format salah ditolak saat unggah")
	}
	id, err := svc.CreateImport(ctx, admin, integration.EntityDepartment, "unit.csv", []byte(deptHeader+"U-1,IGD,Instalasi Gawat Darurat,ya,ya\n"), false, "")
	if err != nil {
		t.Fatal(err)
	}
	im, _ := svc.GetImport(ctx, id)
	if r, err := svc.Preview(ctx, im); err != nil || r.Inserted != 1 {
		t.Fatalf("pratinjau: %+v %v", r, err)
	}
	logID, err := svc.ApplyImport(ctx, id, admin)
	if err != nil {
		t.Fatal(err)
	}
	l, err := svc.Log(ctx, logID)
	if err != nil || l.Status != "SUCCESS" || l.Inserted != 1 || l.FileName.String != "unit.csv" || len(l.Rows) != 1 {
		t.Fatalf("log impor: %+v %v", l, err)
	}
	if _, err := svc.ApplyImport(ctx, id, admin); !errors.Is(err, integration.ErrImportClosed) {
		t.Fatal("impor tidak dapat diterapkan dua kali")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM integration_imports WHERE id = ? AND content IS NULL AND status = 'APPLIED'`, id) != 1 {
		t.Fatal("isi file dibuang setelah diterapkan")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'integration.csv_import'`) != 1 {
		t.Fatal("impor tercatat di audit")
	}
	id2, _ := svc.CreateImport(ctx, admin, integration.EntityDepartment, "unit2.csv", []byte(deptHeader+"U-2,KEU,Keuangan,,\n"), false, "")
	if err := svc.CancelImport(ctx, id2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyImport(ctx, id2, admin); !errors.Is(err, integration.ErrImportClosed) {
		t.Fatal("impor batal tidak dapat diterapkan")
	}
	st, err := svc.Statuses(ctx)
	if err != nil || len(st) != 3 || st[0].Total != 1 || st[0].FromSource != 1 || st[0].LastLog == nil {
		t.Fatalf("status integrasi: %+v %v", st, err)
	}
}
