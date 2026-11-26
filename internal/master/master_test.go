package master_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/master"
	"sikat/internal/testutil"
)

func TestParseMoney(t *testing.T) {
	cases := map[string]string{
		"175000":     "175000.00",
		"175.000":    "175000.00",
		"Rp 175.000": "175000.00",
		"1.250,5":    "1250.50",
		"0":          "0.00",
	}
	for in, want := range cases {
		got, err := master.ParseMoney(in)
		if err != nil || got != want {
			t.Errorf("ParseMoney(%q) = %q, %v; ingin %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"-5", "abc", "1,234", "9.999.999.999.999", "1,2,3"} {
		if _, err := master.ParseMoney(bad); err == nil {
			t.Errorf("ParseMoney(%q) harus error", bad)
		}
	}
}

func TestParseQty(t *testing.T) {
	cases := map[string]string{"12": "12.00", "2,5": "2.50", "2.5": "2.50", " 0 ": "0.00", "0,05": "0.05", "99999999.99": "99999999.99"}
	for in, want := range cases {
		if got, err := master.ParseQty(in); err != nil || got != want {
			t.Errorf("ParseQty(%q) = %q, %v; ingin %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"-1", "abc", "1/2", "1e3", "2,555", "100000000", "1.000,5", ""} {
		if _, err := master.ParseQty(bad); err == nil {
			t.Errorf("ParseQty(%q) harus error", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	e, _ := master.EntityBySlug("departments")
	v := master.Validate(e, testutil.Form(map[string]string{"code": " igd ", "name": ""}))
	if v.Errors["name"] == "" {
		t.Fatal("nama wajib harus error")
	}
	if _, ok := v.Values["code"]; ok {
		t.Fatal("kode otomatis tidak boleh diambil dari form")
	}
	if *v.Values["is_critical_service"] != "0" {
		t.Fatal("checkbox kosong harus 0")
	}

	rooms, _ := master.EntityBySlug("rooms")
	v = master.Validate(rooms, testutil.Form(map[string]string{"department_id": "abc", "code": "A", "name": "B", "is_critical_service": "x"}))
	if v.Errors["department_id"] == "" || v.Errors["is_critical_service"] == "" {
		t.Fatalf("select/enum tidak valid harus error: %v", v.Errors)
	}
	v = master.Validate(rooms, testutil.Form(map[string]string{"department_id": "1", "code": "A", "name": "B"}))
	if !v.OK() || v.Values["is_critical_service"] != nil {
		t.Fatalf("enum kosong (ikut unit) harus NULL tanpa error: %v", v.Errors)
	}

	tc, _ := master.EntityBySlug("ticket-categories")
	v = master.Validate(tc, testutil.Form(map[string]string{"name": "Baris\nbaru", "it_category_id": "1", "default_priority": "MEDIUM", "sort_order": "-1"}))
	if v.Errors["name"] == "" || v.Errors["sort_order"] == "" {
		t.Fatalf("baris baru & angka negatif harus error: %v", v.Errors)
	}
}

func TestValidateLicenseDates(t *testing.T) {
	lic, _ := master.EntityBySlug("licenses")
	v := master.Validate(lic, testutil.Form(map[string]string{"name": "Office", "started_at": "2026-02-30", "expires_at": "1999-12-31", "quantity": "-1"}))
	if v.Errors["started_at"] == "" || v.Errors["expires_at"] == "" || v.Errors["quantity"] == "" {
		t.Fatalf("tanggal tidak valid & jumlah negatif harus error: %v", v.Errors)
	}
	v = master.Validate(lic, testutil.Form(map[string]string{"name": "Office", "expires_at": "2027-01-31"}))
	if !v.OK() || *v.Values["expires_at"] != "2027-01-31" || v.Values["started_at"] != nil {
		t.Fatalf("tanggal valid, mulai kosong = NULL: %v", v.Errors)
	}
	today := time.Date(2026, 11, 26, 23, 30, 0, 0, time.UTC)
	for date, want := range map[string]int{"2026-11-26": 0, "2026-12-26": 30, "2026-11-20": -6, "2027-11-26": 365} {
		d, _ := time.Parse("2006-01-02", date)
		if got := master.DaysUntil(today, d); got != want {
			t.Errorf("DaysUntil(%s) = %d, ingin %d", date, got, want)
		}
	}
}

func TestEntitiesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range master.Entities {
		if seen[e.Slug] {
			t.Errorf("slug ganda: %s", e.Slug)
		}
		seen[e.Slug] = true
		if e.Table == "" || e.OrderBy == "" || len(e.Fields) == 0 {
			t.Errorf("entitas %s tidak lengkap", e.Slug)
		}
		for _, f := range e.Fields {
			if f.Kind == master.KindSelect && f.Ref == nil {
				t.Errorf("%s.%s: select tanpa Ref", e.Slug, f.Column)
			}
			if f.Kind == master.KindEnum && len(f.Choices) == 0 {
				t.Errorf("%s.%s: enum tanpa pilihan", e.Slug, f.Column)
			}
		}
	}
}

func TestServiceCRUD(t *testing.T) {
	db := testutil.DB(t)
	svc := master.NewService(db, clock.NewFake(time.Now()))
	ctx := context.Background()
	depts, _ := master.EntityBySlug("departments")
	rooms, _ := master.EntityBySlug("rooms")

	id, v, err := svc.Create(ctx, depts, testutil.Form(map[string]string{"code": "igd", "name": "IGD", "is_critical_service": "1"}))
	if err != nil || id == 0 {
		t.Fatalf("create gagal: %v %v", v.Errors, err)
	}
	if rec, _ := svc.Get(ctx, depts, id); rec.Get("code") != "UNT-001" {
		t.Fatalf("kode unit otomatis harus UNT-001 (input form diabaikan), dapat %q", rec.Get("code"))
	}
	// Nama unik → error pada field, bukan error server.
	id2, v, err := svc.Create(ctx, depts, testutil.Form(map[string]string{"name": "IGD"}))
	if err != nil || id2 != 0 || v.Errors["name"] != "Nama unit sudah ada." {
		t.Fatalf("duplikat harus error field: id=%d errs=%v err=%v", id2, v.Errors, err)
	}

	// Ruangan dengan unit tidak ada → error field.
	_, v, err = svc.Create(ctx, rooms, testutil.Form(map[string]string{"department_id": "9999", "code": "R1", "name": "R"}))
	if err != nil || v.Errors["department_id"] == "" {
		t.Fatalf("referensi tidak ada harus error field: %v %v", v.Errors, err)
	}
	roomID, v, err := svc.Create(ctx, rooms, testutil.Form(map[string]string{"department_id": itoa(id), "code": "r1", "name": "Triase"}))
	if err != nil || roomID == 0 {
		t.Fatalf("create ruangan gagal: %v %v", v.Errors, err)
	}

	// Unit dipakai ruangan → tidak bisa dihapus.
	if err := svc.Delete(ctx, depts, id); !errors.Is(err, master.ErrInUse) {
		t.Fatalf("hapus unit terpakai harus ErrInUse, dapat %v", err)
	}
	// Nonaktifkan unit → referensi baru ditolak, referensi lama tetap boleh disimpan.
	if err := svc.SetActive(ctx, depts, id, false); err != nil {
		t.Fatal(err)
	}
	_, v, _ = svc.Create(ctx, rooms, testutil.Form(map[string]string{"department_id": itoa(id), "code": "r2", "name": "Lain"}))
	if v.Errors["department_id"] == "" {
		t.Fatal("unit nonaktif tidak boleh dipilih untuk data baru")
	}
	v, err = svc.Update(ctx, rooms, roomID, testutil.Form(map[string]string{"department_id": itoa(id), "code": "GANTI", "name": "Triase Baru"}))
	if err != nil || !v.OK() {
		t.Fatalf("edit dengan referensi lama yang nonaktif harus boleh: %v %v", v.Errors, err)
	}
	if rec, _ := svc.Get(ctx, rooms, roomID); rec.Get("code") != "RNG-0001" || rec.Get("name") != "Triase Baru" {
		t.Fatalf("kode ruangan otomatis harus tetap RNG-0001 setelah diubah, dapat %q", rec.Get("code"))
	}

	// Ruangan belum dipakai → bisa dihapus.
	if err := svc.Delete(ctx, rooms, roomID); err != nil {
		t.Fatalf("hapus ruangan gagal: %v", err)
	}
	if _, err := svc.Get(ctx, rooms, roomID); !errors.Is(err, master.ErrNotFound) {
		t.Fatal("ruangan harus sudah terhapus")
	}

	// Semua perubahan tercatat di audit.
	n := testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'master.%'`)
	if n != 5 { // create dept, create room, deactivate dept, update room, delete room
		t.Fatalf("audit master harus 5, dapat %d", n)
	}
}

func TestUpdateSyncedRecordMarksLocal(t *testing.T) {
	db := testutil.DB(t)
	svc := master.NewService(db, clock.NewFake(time.Now()))
	ctx := context.Background()
	depts, _ := master.EntityBySlug("departments")
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO departments (code, name, source, last_synced_at, created_at, updated_at) VALUES ('RAD','Radiologi','SIMRS',?,?,?)`, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if v, err := svc.Update(ctx, depts, id, testutil.Form(map[string]string{"code": "RAD", "name": "Radiologi Baru"})); err != nil || !v.OK() {
		t.Fatalf("update gagal: %v %v", v.Errors, err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM departments WHERE id = ? AND local_modified_at IS NOT NULL`, id); n != 1 {
		t.Fatal("data SIMRS yang diubah manual harus ditandai local_modified_at")
	}
	if err := svc.Delete(ctx, depts, id); !errors.Is(err, master.ErrInUse) {
		t.Fatalf("data hasil sinkronisasi tidak boleh dihapus, dapat %v", err)
	}
}

func TestPriorityRuleEntity(t *testing.T) {
	db := testutil.DB(t)
	svc := master.NewService(db, clock.NewFake(time.Now()))
	e, ok := master.EntityBySlug("priority-rules")
	if !ok {
		t.Fatal("entitas aturan prioritas tidak terdaftar")
	}
	ctx := context.Background()
	_, v, err := svc.Create(ctx, e, testutil.Form(map[string]string{
		"sort_order": "60", "name": "Salah", "result_priority": "LOW", "min_scope": "3", "max_scope": "1",
	}))
	if err != nil || v.Errors["max_scope"] == "" {
		t.Fatalf("rentang terdampak terbalik harus error di max_scope: %v %v", v.Errors, err)
	}
	id, v, err := svc.Create(ctx, e, testutil.Form(map[string]string{
		"sort_order": "5", "name": "Radiologi selalu tinggi", "result_priority": "HIGH", "replacement_in": "NO,UNKNOWN",
	}))
	if err != nil || id == 0 {
		t.Fatalf("aturan valid gagal disimpan: %v %v", v.Errors, err)
	}
	var repl string
	if err := db.QueryRow(`SELECT replacement_in FROM priority_rules WHERE id = ?`, id).Scan(&repl); err != nil || repl != "NO,UNKNOWN" {
		t.Fatalf("nilai SET tersimpan salah: %q %v", repl, err)
	}
}

func itoa(n int64) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestAutoCodeSkipsExisting(t *testing.T) {
	db := testutil.DB(t)
	svc := master.NewService(db, clock.NewFake(time.Now()))
	ctx := context.Background()
	parts, _ := master.EntityBySlug("spare-parts")
	now := time.Now().UTC()
	// Kode lama/impor yang kebetulan berformat sama dilewati.
	if _, err := db.Exec(`INSERT INTO spare_parts (code, name, unit, created_at, updated_at) VALUES ('SPR-0002','Lama','pcs',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, name := range []string{"PSU", "RAM", "SSD"} {
		id, v, err := svc.Create(ctx, parts, testutil.Form(map[string]string{"name": name, "unit": "pcs"}))
		if err != nil || id == 0 {
			t.Fatalf("create gagal: %v %v", v.Errors, err)
		}
		rec, _ := svc.Get(ctx, parts, id)
		got = append(got, rec.Get("code"))
	}
	if got[0] != "SPR-0001" || got[1] != "SPR-0003" || got[2] != "SPR-0004" {
		t.Fatalf("kode otomatis = %v, harus SPR-0001, SPR-0003, SPR-0004", got)
	}
}
