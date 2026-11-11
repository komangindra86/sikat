package qrcodes_test

import (
	"context"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/qrcodes"
	"sikat/internal/testutil"
)

func TestNewToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		tok, err := qrcodes.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if !qrcodes.ValidTokenFormat(tok) {
			t.Fatalf("format token tidak valid: %q", tok)
		}
		if seen[tok] {
			t.Fatal("token berulang")
		}
		seen[tok] = true
	}
	for _, bad := range []string{"", "1", "aaaaaaaaaaaaaaaaaaaaa!", "aaaaaaaaaaaaaaaaaaaaaaa", "1 OR 1=1--aaaaaaaaaaaa"} {
		if qrcodes.ValidTokenFormat(bad) {
			t.Errorf("token %q harus ditolak", bad)
		}
	}
}

func TestQRLifecycle(t *testing.T) {
	db := testutil.DB(t)
	svc := qrcodes.NewService(db, clock.NewFake(time.Now()), "https://sikat.example/")
	ctx := context.Background()
	now := time.Now().UTC()
	exec := func(q string, args ...any) int64 {
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	dept := exec(`INSERT INTO departments (code, name, is_critical_service, created_at, updated_at) VALUES ('IGD','IGD',TRUE,?,?)`, now, now)
	room := exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'T1','Triase',?,?)`, dept, now, now)
	exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'T2','Resusitasi',?,?)`, dept, now, now)
	cat := exec(`INSERT INTO asset_categories (code, name, it_category_id, created_at, updated_at) VALUES ('PC','PC',1,?,?)`, now, now)
	asset := exec(`INSERT INTO assets (asset_code, name, asset_category_id, room_id, department_id, created_at, updated_at) VALUES ('IGD-01','PC IGD',?,?,?,?,?)`, cat, room, dept, now, now)

	id, err := svc.Generate(ctx, 0, qrcodes.TypeRoom, room)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Generate(ctx, 0, qrcodes.TypeRoom, room); err != qrcodes.ErrAlreadyExists {
		t.Fatalf("QR aktif kedua harus ditolak, dapat %v", err)
	}
	if _, err := svc.Generate(ctx, 0, qrcodes.TypeRoom, 99999); err != qrcodes.ErrTargetInvalid {
		t.Fatalf("ruangan tidak ada harus ditolak, dapat %v", err)
	}
	q, _ := svc.Get(ctx, id)
	if svc.URL(q.Token) != "https://sikat.example/q/"+q.Token {
		t.Fatalf("URL QR salah: %s", svc.URL(q.Token))
	}
	png, err := svc.PNG(q.Token, 256)
	if err != nil || len(png) < 100 || string(png[1:4]) != "PNG" {
		t.Fatalf("PNG tidak valid: %v", err)
	}

	tgt, err := svc.Resolve(ctx, q.Token)
	if err != nil || tgt.RoomID.Int64 != room || tgt.DepartmentID.Int64 != dept || !tgt.CriticalService || tgt.AssetID.Valid {
		t.Fatalf("resolve QR ruangan: %+v %v", tgt, err)
	}

	// QR perangkat mengikuti ruangan perangkat saat ini.
	aid, err := svc.Generate(ctx, 0, qrcodes.TypeAsset, asset)
	if err != nil {
		t.Fatal(err)
	}
	aq, _ := svc.Get(ctx, aid)
	tgt, err = svc.Resolve(ctx, aq.Token)
	if err != nil || tgt.AssetID.Int64 != asset || tgt.RoomID.Int64 != room || tgt.AssetITCategoryID.Int64 != 1 {
		t.Fatalf("resolve QR perangkat: %+v %v", tgt, err)
	}

	// Regenerate: token lama mati, token baru aktif.
	newID, err := svc.Regenerate(ctx, 0, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, q.Token); err != qrcodes.ErrInactive {
		t.Fatalf("token lama harus tidak berlaku, dapat %v", err)
	}
	nq, _ := svc.Get(ctx, newID)
	if nq.Generation != 2 || nq.Token == q.Token {
		t.Fatalf("QR baru: %+v", nq)
	}
	if _, err := svc.Resolve(ctx, nq.Token); err != nil {
		t.Fatalf("token baru harus aktif: %v", err)
	}

	// Generate massal hanya untuk yang belum punya QR.
	mr, ma, _ := svc.MissingCounts(ctx)
	if mr != 1 || ma != 0 {
		t.Fatalf("missing: rooms=%d assets=%d", mr, ma)
	}
	n, err := svc.GenerateMissing(ctx, 0, qrcodes.TypeRoom)
	if err != nil || n != 1 {
		t.Fatalf("generate massal: %d %v", n, err)
	}

	// Nonaktifkan & perangkat dihapus dari inventaris → QR tidak berlaku.
	if err := svc.Deactivate(ctx, 0, newID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, nq.Token); err != qrcodes.ErrInactive {
		t.Fatal("QR nonaktif harus ditolak")
	}
	exec(`UPDATE assets SET status = 'DISPOSED' WHERE id = ?`, asset)
	if _, err := svc.Resolve(ctx, aq.Token); err != qrcodes.ErrInactive {
		t.Fatal("QR perangkat yang dihapus dari inventaris harus ditolak")
	}
	if _, err := svc.Resolve(ctx, "AAAAAAAAAAAAAAAAAAAAAA"); err != qrcodes.ErrNotFound {
		t.Fatal("token tidak dikenal harus ErrNotFound")
	}
	if c := testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'qr.%'`); c < 5 {
		t.Fatalf("aksi QR harus tercatat di audit, dapat %d", c)
	}
}
