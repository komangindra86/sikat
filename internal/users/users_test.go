package users_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"sikat/internal/auth"
	"sikat/internal/clock"
	"sikat/internal/testutil"
	"sikat/internal/users"
)

func getAll(m map[string][]string) func(string) []string {
	return func(k string) []string { return m[k] }
}

func TestCreateUserWithTempPassword(t *testing.T) {
	db := testutil.DB(t)
	svc := users.NewService(db, clock.NewFake(time.Now()))
	ctx := context.Background()
	tech := strconv.FormatInt(testutil.RoleID(t, db, "TECHNICIAN"), 10)

	created, _, errs, err := svc.Create(ctx,
		testutil.Form(map[string]string{"username": "Budi.S", "full_name": "Budi Santoso", "role_id": tech}),
		getAll(map[string][]string{"competencies": {"1", "3", "3"}}))
	if err != nil || created.ID == 0 {
		t.Fatalf("create gagal: %v %v", errs, err)
	}
	u, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "budi.s" || !u.MustChangePassword || len(u.Competencies) != 2 {
		t.Fatalf("user tidak sesuai: %+v", u)
	}
	var hash string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, created.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if ok, _ := auth.VerifyPassword(created.TempPassword, hash); !ok {
		t.Fatal("password sementara harus cocok dengan hash tersimpan")
	}
	// audit tidak boleh memuat password/hash
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE after_data LIKE ? OR after_data LIKE '%argon2%'`, "%"+created.TempPassword+"%"); n != 0 {
		t.Fatal("audit log tidak boleh berisi password")
	}

	_, _, errs, err = svc.Create(ctx,
		testutil.Form(map[string]string{"username": "budi.s", "full_name": "Lain", "role_id": tech}), getAll(nil))
	if err != nil || errs["username"] == "" {
		t.Fatalf("username ganda harus error field: %v %v", errs, err)
	}
	_, _, errs, _ = svc.Create(ctx,
		testutil.Form(map[string]string{"username": "x y", "full_name": "", "role_id": "999", "email": "salah"}), getAll(nil))
	for _, f := range []string{"username", "full_name", "email"} {
		if errs[f] == "" {
			t.Errorf("field %s harus error", f)
		}
	}
}

func TestAdminCompetenciesCleared(t *testing.T) {
	db := testutil.DB(t)
	svc := users.NewService(db, clock.NewFake(time.Now()))
	admin := strconv.FormatInt(testutil.RoleID(t, db, "ADMIN"), 10)
	created, _, errs, err := svc.Create(context.Background(),
		testutil.Form(map[string]string{"username": "adm2", "full_name": "Admin Dua", "role_id": admin}),
		getAll(map[string][]string{"competencies": {"1"}}))
	if err != nil || created.ID == 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM user_competencies WHERE user_id = ?`, created.ID); n != 0 {
		t.Fatal("admin tidak menyimpan kompetensi")
	}
}

func TestLastAdminProtection(t *testing.T) {
	db := testutil.DB(t)
	svc := users.NewService(db, clock.NewFake(time.Now()))
	ctx := context.Background()
	a1 := testutil.CreateUser(t, db, "admin1", "ADMIN", "password-admin-1", false)
	tech := testutil.CreateUser(t, db, "tech1", "TECHNICIAN", "password-tech-1", false)

	if err := svc.SetActive(ctx, a1, a1, false); !errors.Is(err, users.ErrSelfDeactivate) {
		t.Fatalf("menonaktifkan diri sendiri harus ditolak, dapat %v", err)
	}
	a2 := testutil.CreateUser(t, db, "admin2", "ADMIN", "password-admin-2", false)
	if err := svc.SetActive(ctx, a2, a1, false); err != nil {
		t.Fatalf("admin2 menonaktifkan admin1 harus boleh (masih ada 2 admin): %v", err)
	}
	// Sekarang hanya admin2 aktif. Admin lain (tech dinaikkan) tidak bisa menurunkan admin2 bila ia satu-satunya.
	techRole := strconv.FormatInt(testutil.RoleID(t, db, "TECHNICIAN"), 10)
	_, errs, err := svc.Update(ctx, tech, a2, testutil.Form(map[string]string{"full_name": "Admin Dua", "role_id": techRole}), getAll(nil))
	if err != nil || errs["_form"] != users.ErrLastAdmin.Error() {
		t.Fatalf("menurunkan admin terakhir harus ditolak: %v %v", errs, err)
	}
	_, errs, _ = svc.Update(ctx, a2, a2, testutil.Form(map[string]string{"full_name": "Admin Dua", "role_id": techRole}), getAll(nil))
	if errs["_form"] != users.ErrSelfRole.Error() {
		t.Fatalf("mengubah role sendiri harus ditolak: %v", errs)
	}
}

func TestResetPassword(t *testing.T) {
	db := testutil.DB(t)
	svc := users.NewService(db, clock.NewFake(time.Now()))
	id := testutil.CreateUser(t, db, "lupa", "TECHNICIAN", "password-lama-123", false)
	if _, err := db.Exec(`UPDATE users SET failed_login_count = 3, locked_until = UTC_TIMESTAMP() + INTERVAL 1 HOUR WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	temp, err := svc.ResetPassword(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := svc.Get(context.Background(), id)
	if !u.MustChangePassword || u.LockedUntil.Valid {
		t.Fatalf("reset harus mewajibkan ganti password dan membuka kunci: %+v", u)
	}
	var hash string
	_ = db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	if ok, _ := auth.VerifyPassword(temp, hash); !ok {
		t.Fatal("password baru tidak tersimpan")
	}
}

func TestCreateAdminCLI(t *testing.T) {
	db := testutil.DB(t)
	svc := users.NewService(db, clock.NewFake(time.Now()))
	if _, err := svc.CreateAdmin(context.Background(), "Admin", "Admin IT", "pendek"); err == nil {
		t.Fatal("password lemah harus ditolak")
	}
	id, err := svc.CreateAdmin(context.Background(), "Admin", "Admin IT", "password-kuat-sekali")
	if err != nil || id == 0 {
		t.Fatalf("create admin gagal: %v", err)
	}
	if _, err := svc.CreateAdmin(context.Background(), "admin", "Admin IT", "password-kuat-sekali"); err == nil {
		t.Fatal("username ganda harus ditolak")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'user.create_admin_cli' AND actor_type = 'SYSTEM'`); n != 1 {
		t.Fatal("pembuatan admin via CLI harus tercatat sebagai SYSTEM")
	}
}
