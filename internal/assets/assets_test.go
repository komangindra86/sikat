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
		"asset_code": "igd-pc-01", "name": "PC Triase", "asset_category_id": s(f.cat), "room_id": s(f.r1),
	}))
	if err != nil || id == 0 {
		t.Fatalf("create gagal: %v %v", errs, err)
	}
	a, err := f.svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.AssetCode != "IGD-PC-01" || a.DepartmentID.Int64 != f.d1 || a.Status != assets.StatusActive {
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
		{"ruangan bukan milik unit", map[string]string{"asset_code": "A1", "name": "x", "asset_category_id": s(f.cat), "department_id": s(f.d2), "room_id": s(f.r1)}, "room_id"},
		{"kategori tidak ada", map[string]string{"asset_code": "A2", "name": "x", "asset_category_id": "9999"}, "asset_category_id"},
		{"tanggal masa depan", map[string]string{"asset_code": "A3", "name": "x", "asset_category_id": s(f.cat), "procured_at": "2027-01-01"}, "procured_at"},
		{"status tidak valid", map[string]string{"asset_code": "A4", "name": "x", "asset_category_id": s(f.cat), "status": "HILANG"}, "status"},
		{"kode wajib", map[string]string{"name": "x", "asset_category_id": s(f.cat)}, "asset_code"},
	}
	for _, c := range cases {
		id, _, errs, err := f.svc.Create(ctx, testutil.Form(c.form))
		if err != nil || id != 0 || errs[c.field] == "" {
			t.Errorf("%s: harus error di %s, dapat id=%d errs=%v err=%v", c.name, c.field, id, errs, err)
		}
	}
	// kode ganda (case-insensitive karena di-uppercase)
	if id, _, errs, _ := f.svc.Create(ctx, testutil.Form(map[string]string{"asset_code": "dup", "name": "x", "asset_category_id": s(f.cat)})); id == 0 {
		t.Fatalf("create pertama gagal: %v", errs)
	}
	if _, _, errs, err := f.svc.Create(ctx, testutil.Form(map[string]string{"asset_code": "DUP", "name": "y", "asset_category_id": s(f.cat)})); err != nil || errs["asset_code"] == "" {
		t.Fatalf("kode ganda harus error field: %v %v", errs, err)
	}
}

func TestDisposedTimestamp(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	base := map[string]string{"asset_code": "P1", "name": "Printer", "asset_category_id": s(f.cat)}
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
