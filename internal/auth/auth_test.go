package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sikat/internal/auth"
	"sikat/internal/clock"
	"sikat/internal/settings"
	"sikat/internal/testutil"
)

func TestHashAndVerifyPassword(t *testing.T) {
	h, err := auth.HashPassword("rahasia-panjang-sekali")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("format hash tidak sesuai: %s", h)
	}
	ok, err := auth.VerifyPassword("rahasia-panjang-sekali", h)
	if err != nil || !ok {
		t.Fatalf("password benar ditolak: ok=%v err=%v", ok, err)
	}
	ok, _ = auth.VerifyPassword("rahasia-panjang-salah", h)
	if ok {
		t.Fatal("password salah diterima")
	}
	h2, _ := auth.HashPassword("rahasia-panjang-sekali")
	if h == h2 {
		t.Fatal("salt tidak acak: hash identik")
	}
	if _, err := auth.VerifyPassword("x", "bukan-hash"); err == nil {
		t.Fatal("hash rusak harus error")
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := []struct {
		pw, user string
		ok       bool
	}{
		{"pendek", "budi", false},
		{"cukup-panjang-1", "budi", true},
		{"BUDI-punya-password", "budi", false},
		{"          ", "budi", false},
		{strings.Repeat("a", 129), "budi", false},
	}
	for _, c := range cases {
		err := auth.ValidatePasswordPolicy(c.pw, c.user)
		if (err == nil) != c.ok {
			t.Errorf("policy(%q,%q) = %v, ingin ok=%v", c.pw, c.user, err, c.ok)
		}
	}
}

func TestGenerateTempPassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		p, err := auth.GenerateTempPassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(p) != 12 || strings.ContainsAny(p, "0O1lI") {
			t.Fatalf("password sementara tidak sesuai aturan: %q", p)
		}
		if seen[p] {
			t.Fatal("password sementara berulang")
		}
		seen[p] = true
	}
}

func newService(t *testing.T) (*auth.Service, *clock.Fake, func() int) {
	db := testutil.DB(t)
	fc := clock.NewFake(time.Date(2026, 11, 11, 2, 0, 0, 0, time.UTC))
	svc := auth.NewService(db, fc, settings.NewService(db, fc))
	testutil.CreateUser(t, db, "budi", "TECHNICIAN", "password-budi-123", false)
	audits := func() int {
		return testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'login.%'`)
	}
	return svc, fc, audits
}

func TestAuthenticateSuccessAndFailure(t *testing.T) {
	svc, _, audits := newService(t)
	ctx := context.Background()

	u, err := svc.Authenticate(ctx, "  BUDI ", "password-budi-123")
	if err != nil {
		t.Fatalf("login gagal: %v", err)
	}
	if u.Username != "budi" || u.RoleCode != "TECHNICIAN" {
		t.Fatalf("user tidak sesuai: %+v", u)
	}
	if _, err := svc.Authenticate(ctx, "budi", "salah"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("password salah harus ErrInvalidCredentials, dapat %v", err)
	}
	if _, err := svc.Authenticate(ctx, "tidakada", "apapun"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("user tidak ada harus ErrInvalidCredentials, dapat %v", err)
	}
	if n := audits(); n != 3 {
		t.Fatalf("audit login harus 3 baris, dapat %d", n)
	}
}

func TestAuthenticateLockout(t *testing.T) {
	svc, fc, _ := newService(t)
	ctx := context.Background()

	// default: 5 kali gagal → dikunci 15 menit
	for i := 1; i <= 4; i++ {
		if _, err := svc.Authenticate(ctx, "budi", "salah"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("percobaan %d: %v", i, err)
		}
	}
	var locked *auth.LockedError
	if _, err := svc.Authenticate(ctx, "budi", "salah"); !errors.As(err, &locked) {
		t.Fatalf("percobaan ke-5 harus mengunci akun, dapat %v", err)
	}
	// Password benar pun ditolak selama terkunci.
	if _, err := svc.Authenticate(ctx, "budi", "password-budi-123"); !errors.As(err, &locked) {
		t.Fatalf("akun terkunci harus menolak password benar, dapat %v", err)
	}
	fc.Advance(16 * time.Minute)
	if _, err := svc.Authenticate(ctx, "budi", "password-budi-123"); err != nil {
		t.Fatalf("setelah masa kunci, login harus berhasil: %v", err)
	}
}

func TestAuthenticateInactiveUser(t *testing.T) {
	db := testutil.DB(t)
	fc := clock.NewFake(time.Now())
	svc := auth.NewService(db, fc, settings.NewService(db, fc))
	id := testutil.CreateUser(t, db, "nonaktif", "TECHNICIAN", "password-nonaktif", false)
	if _, err := db.Exec(`UPDATE users SET is_active = FALSE WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), "nonaktif", "password-nonaktif"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("user nonaktif harus ditolak, dapat %v", err)
	}
	u, err := svc.LoadActiveUser(context.Background(), id)
	if err != nil || u != nil {
		t.Fatalf("LoadActiveUser user nonaktif harus nil, dapat %v %v", u, err)
	}
}

func TestChangePassword(t *testing.T) {
	db := testutil.DB(t)
	fc := clock.NewFake(time.Now())
	svc := auth.NewService(db, fc, settings.NewService(db, fc))
	id := testutil.CreateUser(t, db, "sari", "TECHNICIAN", "sementara-123", true)
	ctx := context.Background()

	errs, err := svc.ChangePassword(ctx, id, "salah", "kopi-pagi-di-igd", "kopi-pagi-di-igd")
	if err != nil || errs["current"] == "" {
		t.Fatalf("password lama salah harus ditolak: %v %v", errs, err)
	}
	errs, _ = svc.ChangePassword(ctx, id, "sementara-123", "kopi-pagi-di-igd", "beda")
	if errs["confirm"] == "" {
		t.Fatalf("konfirmasi berbeda harus ditolak: %v", errs)
	}
	errs, err = svc.ChangePassword(ctx, id, "sementara-123", "kopi-pagi-di-igd", "kopi-pagi-di-igd")
	if err != nil || len(errs) != 0 {
		t.Fatalf("ganti password gagal: %v %v", errs, err)
	}
	u, _ := svc.LoadActiveUser(ctx, id)
	if u.MustChangePassword {
		t.Fatal("must_change_password harus false setelah diganti")
	}
	if _, err := svc.Authenticate(ctx, "sari", "kopi-pagi-di-igd"); err != nil {
		t.Fatalf("login dengan password baru gagal: %v", err)
	}
}
