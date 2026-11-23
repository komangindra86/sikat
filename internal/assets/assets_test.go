package assets_test

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"sikat/internal/assets"
	"sikat/internal/clock"
	"sikat/internal/testutil"
)

type fixture struct {
	db          *sql.DB
	svc         *assets.Service
	cat, d1, d2 int64
	r1, r2      int64
}

func setup(t *testing.T) fixture {
	db := testutil.DB(t)
	now := time.Now().UTC()
	exec := func(q string, args ...any) int64 {
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	f := fixture{db: db, svc: assets.NewService(db, clock.NewFake(time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)))}
	f.cat = exec(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('PC','Komputer',?,?)`, now, now)
	f.d1 = exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('IGD','IGD',?,?)`, now, now)
	f.d2 = exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('ICU','ICU',?,?)`, now, now)
	f.r1 = exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'IGD-1','Triase',?,?)`, f.d1, now, now)
	f.r2 = exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'ICU-1','Bed 1',?,?)`, f.d2, now, now)
	return f
}

func s(n int64) string { return strconv.FormatInt(n, 10) }

func TestCreateFillsDepartmentFromRoom(t *testing.T) {
	f := setup(t)
	id, _, errs, err := f.svc.Create(context.Background(), testutil.Form(map[string]string{
		"asset_code": "KETIK-MANUAL", "name": "PC Triase", "asset_category_id": s(f.cat), "room_id": s(f.r1),
	}))
	if err != nil || id == 0 {
		t.Fatalf("create gagal: %v %v", errs, err)
	}
	a, err := f.svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.AssetCode != "PRK-2026-0001" || a.DepartmentID.Int64 != f.d1 || a.Status != assets.StatusActive {
		t.Fatalf("data tidak sesuai: %+v", a)
	}
	if n := testutil.Count(t, f.db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'asset.create' AND entity_id = ?`, id); n != 1 {
		t.Fatal("pembuatan perangkat harus tercatat di audit")
	}
}

func TestCreateValidation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases := []struct {
		name  string
		form  map[string]string
		field string
	}{
		{"ruangan bukan milik unit", map[string]string{"name": "x", "asset_category_id": s(f.cat), "department_id": s(f.d2), "room_id": s(f.r1)}, "room_id"},
		{"kategori tidak ada", map[string]string{"name": "x", "asset_category_id": "9999"}, "asset_category_id"},
		{"tanggal masa depan", map[string]string{"name": "x", "asset_category_id": s(f.cat), "procured_at": "2027-01-01"}, "procured_at"},
		{"status tidak valid", map[string]string{"name": "x", "asset_category_id": s(f.cat), "status": "HILANG"}, "status"},
		{"nama wajib", map[string]string{"asset_category_id": s(f.cat)}, "name"},
	}
	for _, c := range cases {
		id, _, errs, err := f.svc.Create(ctx, testutil.Form(c.form))
		if err != nil || id != 0 || errs[c.field] == "" {
			t.Errorf("%s: harus error di %s, dapat id=%d errs=%v err=%v", c.name, c.field, id, errs, err)
		}
	}
	// Validasi gagal tidak memakan nomor urut kode.
	if n := testutil.Count(t, f.db, `SELECT COUNT(*) FROM number_sequences WHERE name = 'PRK'`); n != 0 {
		t.Fatalf("form gagal tidak boleh mengalokasikan kode, dapat %d", n)
	}
}

func TestAutoAssetCode(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	// Kode lama/impor yang kebetulan berformat sama dilewati.
	now := time.Now().UTC()
	if _, err := f.db.Exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('PRK-2026-0002','Impor',?,?,?)`, f.cat, now, now); err != nil {
		t.Fatal(err)
	}
	var codes []string
	for i := 0; i < 3; i++ {
		id, form, errs, err := f.svc.Create(ctx, testutil.Form(map[string]string{"name": "PC", "asset_category_id": s(f.cat)}))
		if err != nil || id == 0 {
			t.Fatalf("create gagal: %v %v", errs, err)
		}
		codes = append(codes, form["asset_code"])
	}
	if codes[0] != "PRK-2026-0001" || codes[1] != "PRK-2026-0003" || codes[2] != "PRK-2026-0004" {
		t.Fatalf("kode otomatis = %v, harus PRK-2026-0001, PRK-2026-0003, PRK-2026-0004", codes)
	}
	// Kode tidak dapat diubah lewat form ubah.
	id := int64(testutil.Count(t, f.db, `SELECT id FROM assets WHERE asset_code = 'PRK-2026-0001'`))
	form, errs, err := f.svc.Update(ctx, id, testutil.Form(map[string]string{"asset_code": "GANTI", "name": "PC baru", "asset_category_id": s(f.cat)}))
	if err != nil || errs != nil || form["asset_code"] != "PRK-2026-0001" {
		t.Fatalf("update: form=%v errs=%v err=%v", form, errs, err)
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM assets WHERE id = ? AND asset_code = 'PRK-2026-0001' AND name = 'PC baru'`, id) != 1 {
		t.Fatal("kode inventaris harus tetap setelah diubah")
	}
}

func TestDisposedTimestamp(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	base := map[string]string{"name": "Printer", "asset_category_id": s(f.cat)}
	id, _, errs, err := f.svc.Create(ctx, testutil.Form(base))
	if err != nil || id == 0 {
		t.Fatalf("%v %v", errs, err)
	}
	base["status"] = assets.StatusDisposed
	if _, errs, err := f.svc.Update(ctx, id, testutil.Form(base)); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	a, _ := f.svc.Get(ctx, id)
	if !a.DisposedAt.Valid {
		t.Fatal("status DISPOSED harus mengisi disposed_at")
	}
	base["status"] = assets.StatusActive
	if _, errs, err := f.svc.Update(ctx, id, testutil.Form(base)); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	a, _ = f.svc.Get(ctx, id)
	if a.DisposedAt.Valid {
		t.Fatal("kembali aktif harus mengosongkan disposed_at")
	}
	list, total, err := f.svc.List(ctx, assets.Filter{Limit: 10})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("daftar default harus berisi 1 perangkat aktif: %d %v", total, err)
	}
}
