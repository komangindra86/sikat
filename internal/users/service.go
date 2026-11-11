package users

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"sikat/internal/audit"
	"sikat/internal/auth"
	"sikat/internal/clock"
	"sikat/internal/database"
	"sikat/internal/rbac"
)

// Error aturan bisnis.
var (
	ErrSelfDeactivate = errors.New("Anda tidak dapat menonaktifkan akun Anda sendiri.")
	ErrLastAdmin      = errors.New("Harus tersisa minimal satu Admin IT aktif.")
	ErrSelfRole       = errors.New("Anda tidak dapat mengubah role akun Anda sendiri.")
)

// Service berisi aturan bisnis akun.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	repo  repository
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock) *Service {
	return &Service{db: db, clock: c}
}

// List mengembalikan daftar pengguna dan total.
func (s *Service) List(ctx context.Context, f Filter) ([]User, int, error) {
	return s.repo.list(ctx, s.db, f)
}

// Get mengambil satu pengguna beserta kompetensinya.
func (s *Service) Get(ctx context.Context, id int64) (User, error) {
	return s.repo.get(ctx, s.db, id, false)
}

// Roles mengembalikan daftar role.
func (s *Service) Roles(ctx context.Context) ([]Role, error) { return s.repo.roles(ctx, s.db) }

// Departments mengembalikan pilihan unit.
func (s *Service) Departments(ctx context.Context, include int64) ([]Option, error) {
	var inc []int64
	if include != 0 {
		inc = []int64{include}
	}
	return s.repo.options(ctx, s.db, "departments", inc)
}

// ITCategories mengembalikan pilihan bidang (kompetensi).
func (s *Service) ITCategories(ctx context.Context, include []int64) ([]Option, error) {
	return s.repo.options(ctx, s.db, "it_categories", include)
}

func contains(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// checkRefs memvalidasi role, unit, dan kompetensi. Referensi lama boleh nonaktif.
func (s *Service) checkRefs(ctx context.Context, tx *sql.Tx, in *Input, errs map[string]string, before *User) (string, error) {
	role := ""
	if in.RoleID != 0 {
		code, err := s.repo.roleCode(ctx, tx, in.RoleID)
		if err != nil {
			return "", err
		}
		if code == "" || !rbac.ValidRole(code) {
			errs["role_id"] = "Role tidak dikenal."
		}
		role = code
	}
	if in.DepartmentID != 0 && (before == nil || before.DepartmentID.Int64 != in.DepartmentID) {
		n, err := s.repo.countActive(ctx, tx, "departments", []int64{in.DepartmentID})
		if err != nil {
			return "", err
		}
		if n == 0 {
			errs["department_id"] = "Unit tidak ditemukan atau nonaktif."
		}
	}
	if role == rbac.RoleAdmin {
		in.Competencies = nil // kompetensi hanya relevan untuk petugas & koordinator
	}
	var fresh []int64
	for _, c := range in.Competencies {
		if before == nil || !contains(before.Competencies, c) {
			fresh = append(fresh, c)
		}
	}
	if len(fresh) > 0 {
		n, err := s.repo.countActive(ctx, tx, "it_categories", fresh)
		if err != nil {
			return "", err
		}
		if n != len(fresh) {
			errs["competencies"] = "Sebagian bidang kompetensi tidak ditemukan atau nonaktif."
		}
	}
	return role, nil
}

func mapDBError(err error, errs map[string]string) bool {
	if key, ok := database.DuplicateKey(err); ok {
		switch key {
		case "uq_users_username":
			errs["username"] = "Username sudah dipakai."
		case "uq_users_nip":
			errs["nip"] = "NIP sudah terdaftar pada akun lain."
		default:
			errs["_form"] = "Data yang sama sudah ada."
		}
		return true
	}
	return false
}

func snapshot(in Input, roleCode string) map[string]any {
	return map[string]any{
		"username": in.Username, "full_name": in.FullName, "nip": in.NIP, "phone": in.Phone, "email": in.Email,
		"role": roleCode, "department_id": in.DepartmentID, "competencies": in.Competencies,
	}
}

func userSnapshot(u User) map[string]any {
	return map[string]any{
		"username": u.Username, "full_name": u.FullName, "nip": u.NIP.String, "phone": u.Phone.String,
		"email": u.Email.String, "role": u.RoleCode, "department_id": u.DepartmentID.Int64,
		"competencies": u.Competencies, "is_active": u.IsActive,
	}
}

// Created adalah hasil pembuatan akun.
type Created struct {
	ID           int64
	TempPassword string
}

// Create membuat akun dengan password sementara yang wajib diganti saat login pertama.
func (s *Service) Create(ctx context.Context, get func(string) string, getAll func(string) []string) (Created, map[string]string, map[string]string, error) {
	in, form, errs := Validate(get, getAll, true)
	now := s.clock.Now()
	var out Created
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		role, err := s.checkRefs(ctx, tx, &in, errs, nil)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			return nil
		}
		temp, err := auth.GenerateTempPassword()
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(temp)
		if err != nil {
			return err
		}
		id, err := s.repo.insert(ctx, tx, in, hash, now)
		if err != nil {
			return err
		}
		if err := s.repo.replaceCompetencies(ctx, tx, id, in.Competencies, now); err != nil {
			return err
		}
		out = Created{ID: id, TempPassword: temp}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "user.create", EntityType: "users", EntityID: id, After: snapshot(in, role)})
	})
	if err != nil {
		if mapDBError(err, errs) {
			return Created{}, form, errs, nil
		}
		return Created{}, form, errs, err
	}
	if len(errs) > 0 {
		return Created{}, form, errs, nil
	}
	return out, form, nil, nil
}

// Update mengubah data akun (username tidak dapat diubah).
func (s *Service) Update(ctx context.Context, actorID, id int64, get func(string) string, getAll func(string) []string) (map[string]string, map[string]string, error) {
	in, form, errs := Validate(get, getAll, false)
	now := s.clock.Now()
	var bizErr error
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.repo.get(ctx, tx, id, true)
		if err != nil {
			return err
		}
		in.Username = before.Username
		role, err := s.checkRefs(ctx, tx, &in, errs, &before)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			return nil
		}
		if role != before.RoleCode {
			if id == actorID {
				bizErr = ErrSelfRole
				return nil
			}
			if before.RoleCode == rbac.RoleAdmin && before.IsActive {
				n, err := s.repo.countActiveAdmins(ctx, tx)
				if err != nil {
					return err
				}
				if n <= 1 {
					bizErr = ErrLastAdmin
					return nil
				}
			}
		}
		if err := s.repo.update(ctx, tx, id, in, now); err != nil {
			return err
		}
		if err := s.repo.replaceCompetencies(ctx, tx, id, in.Competencies, now); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "user.update", EntityType: "users", EntityID: id, Before: userSnapshot(before), After: snapshot(in, role),
		})
	})
	if err != nil {
		if mapDBError(err, errs) {
			return form, errs, nil
		}
		return form, errs, err
	}
	if bizErr != nil {
		errs["_form"] = bizErr.Error()
	}
	if len(errs) > 0 {
		return form, errs, nil
	}
	return form, nil, nil
}

// SetActive mengaktifkan/menonaktifkan akun.
func (s *Service) SetActive(ctx context.Context, actorID, id int64, active bool) error {
	now := s.clock.Now()
	return database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.repo.get(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if before.IsActive == active {
			return nil
		}
		if !active {
			if id == actorID {
				return ErrSelfDeactivate
			}
			if before.RoleCode == rbac.RoleAdmin {
				n, err := s.repo.countActiveAdmins(ctx, tx)
				if err != nil {
					return err
				}
				if n <= 1 {
					return ErrLastAdmin
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET is_active = ?, updated_at = ? WHERE id = ?`, active, now, id); err != nil {
			return err
		}
		action := "user.deactivate"
		if active {
			action = "user.activate"
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: action, EntityType: "users", EntityID: id})
	})
}

// ResetPassword membuat password sementara baru dan membuka kunci akun.
func (s *Service) ResetPassword(ctx context.Context, id int64) (string, error) {
	now := s.clock.Now()
	temp, err := auth.GenerateTempPassword()
	if err != nil {
		return "", err
	}
	hash, err := auth.HashPassword(temp)
	if err != nil {
		return "", err
	}
	err = database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := s.repo.get(ctx, tx, id, true); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET password_hash = ?, must_change_password = TRUE, failed_login_count = 0,
			                 locked_until = NULL, updated_at = ? WHERE id = ?`, hash, now, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "user.reset_password", EntityType: "users", EntityID: id})
	})
	if err != nil {
		return "", err
	}
	return temp, nil
}

// CreateAdmin membuat akun Admin IT dari CLI dengan password yang ditentukan operator.
func (s *Service) CreateAdmin(ctx context.Context, username, fullName, password string) (int64, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	fullName = strings.TrimSpace(fullName)
	if msg := ValidateUsername(username); msg != "" {
		return 0, errors.New(msg)
	}
	if fullName == "" {
		return 0, errors.New("nama lengkap wajib diisi")
	}
	if err := auth.ValidatePasswordPolicy(password, username); err != nil {
		return 0, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	now := s.clock.Now()
	var id int64
	err = database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var roleID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE code = ?`, rbac.RoleAdmin).Scan(&roleID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("role ADMIN belum ada; jalankan 'migrate up' terlebih dahulu")
			}
			return err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO users (role_id, username, full_name, password_hash, must_change_password, password_changed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, FALSE, ?, ?, ?)`, roleID, username, fullName, hash, now, now, now)
		if err != nil {
			if key, ok := database.DuplicateKey(err); ok && key == "uq_users_username" {
				return errors.New("username sudah dipakai")
			}
			return err
		}
		id, _ = res.LastInsertId()
		return audit.Record(ctx, tx, now, audit.Entry{
			Action: "user.create_admin_cli", EntityType: "users", EntityID: id, System: true,
			After: map[string]string{"username": username, "full_name": fullName, "role": rbac.RoleAdmin},
		})
	})
	return id, err
}
