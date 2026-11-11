package settings_test

import (
	"context"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/settings"
	"sikat/internal/testutil"
)

func TestValidate(t *testing.T) {
	d, _ := settings.DefByKey(settings.KeyLoginMaxAttempts)
	if _, err := settings.Validate(d, "2"); err == nil {
		t.Fatal("di bawah minimum harus ditolak")
	}
	if _, err := settings.Validate(d, "abc"); err == nil {
		t.Fatal("bukan angka harus ditolak")
	}
	if v, err := settings.Validate(d, " 07 "); err != nil || v != "7" {
		t.Fatalf("angka harus dinormalisasi: %q %v", v, err)
	}
	h, _ := settings.DefByKey(settings.KeyHospitalName)
	if _, err := settings.Validate(h, "  "); err == nil {
		t.Fatal("nama RS wajib diisi")
	}
}

func TestUpdateAndCache(t *testing.T) {
	db := testutil.DB(t)
	svc := settings.NewService(db, clock.NewFake(time.Now()))
	ctx := context.Background()

	if got := svc.Int(ctx, settings.KeyLoginMaxAttempts); got != 5 {
		t.Fatalf("nilai seed harus 5, dapat %d", got)
	}
	errs, err := svc.Update(ctx, 0, map[string]string{settings.KeyLoginMaxAttempts: "99"})
	if err != nil || errs[settings.KeyLoginMaxAttempts] == "" {
		t.Fatalf("nilai di luar batas harus error: %v %v", errs, err)
	}
	errs, err = svc.Update(ctx, 0, map[string]string{settings.KeyLoginMaxAttempts: "8", settings.KeyHospitalName: "RSUD Uji"})
	if err != nil || len(errs) != 0 {
		t.Fatalf("update gagal: %v %v", errs, err)
	}
	if svc.Int(ctx, settings.KeyLoginMaxAttempts) != 8 || svc.HospitalName(ctx) != "RSUD Uji" {
		t.Fatal("cache harus diperbarui setelah update")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'settings.update'`); n != 2 {
		t.Fatalf("setiap kunci yang berubah tercatat di audit, dapat %d", n)
	}
	if _, err := svc.Update(ctx, 0, map[string]string{"tidak.dikenal": "x"}); err != settings.ErrUnknownKey {
		t.Fatalf("kunci tak dikenal harus ditolak, dapat %v", err)
	}
}
