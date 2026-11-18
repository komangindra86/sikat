// Package auth menangani login, logout, session, dan penggantian password.
package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
	"sikat/internal/rbac"
	"sikat/internal/settings"
)

// Error yang ditampilkan ke pengguna.
var (
	ErrInvalidCredentials = errors.New("Username atau password salah.")
	ErrWrongCurrent       = errors.New("Password saat ini salah.")
)

// LockedError dikembalikan saat akun dikunci sementara.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	return "Terlalu banyak percobaan gagal. Akun dikunci sementara, coba lagi beberapa menit lagi."
}

// Service berisi logika autentikasi.
type Service struct {
	db       *sql.DB
	clock    clock.Clock
	settings *settings.Service
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock, s *settings.Service) *Service {
	return &Service{db: db, clock: c, settings: s}
}

// Authenticate memverifikasi username dan password. Penghitung gagal login dan
// penguncian akun dikelola dalam transaksi dengan baris user terkunci.
func (s *Service) Authenticate(ctx context.Context, username, password string) (*rbac.User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	maxAttempts := s.settings.Int(ctx, settings.KeyLoginMaxAttempts)
	lockout := time.Duration(s.settings.Int(ctx, settings.KeyLoginLockoutMinutes)) * time.Minute
	now := s.clock.Now()

	var result *rbac.User
	var authErr error
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var (
			id          int64
			hash        string
			active      bool
			failed      int
			lockedUntil sql.NullTime
		)
		err := tx.QueryRowContext(ctx, `
			SELECT id, password_hash, is_active, failed_login_count, locked_until
			  FROM users WHERE username = ? FOR UPDATE`, username).
			Scan(&id, &hash, &active, &failed, &lockedUntil)
		if errors.Is(err, sql.ErrNoRows) {
			_, _ = VerifyPassword(password, dummyHash) // samakan waktu respons
			authErr = ErrInvalidCredentials
			return audit.Record(ctx, tx, now, audit.Entry{
				Action: "login.failed", After: map[string]string{"username": username, "reason": "unknown_user"},
			})
		}
		if err != nil {
			return err
		}

		if lockedUntil.Valid && lockedUntil.Time.After(now) {
			_, _ = VerifyPassword(password, dummyHash)
			authErr = &LockedError{Until: lockedUntil.Time}
			return audit.Record(ctx, tx, now, audit.Entry{
				Action: "login.blocked", EntityType: "user", EntityID: id, ActorUserID: id,
				After: map[string]string{"reason": "locked"},
			})
		}

		ok, verr := VerifyPassword(password, hash)
		if verr != nil {
			return fmt.Errorf("verifikasi password user %d: %w", id, verr)
		}
		if !ok || !active {
			reason := "wrong_password"
			if ok && !active {
				reason = "inactive"
			}
			if !ok {
				failed++
			}
			var newLock sql.NullTime
			if failed >= maxAttempts {
				newLock = sql.NullTime{Time: now.Add(lockout), Valid: true}
				failed = 0
				reason = "locked_now"
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET failed_login_count = ?, locked_until = ? WHERE id = ?`,
				failed, newLock, id); err != nil {
				return err
			}
			if newLock.Valid {
				authErr = &LockedError{Until: newLock.Time}
			} else {
				authErr = ErrInvalidCredentials
			}
			return audit.Record(ctx, tx, now, audit.Entry{
				Action: "login.failed", EntityType: "user", EntityID: id, ActorUserID: id,
				After: map[string]string{"reason": reason},
			})
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET failed_login_count = 0, locked_until = NULL, last_login_at = ? WHERE id = ?`, now, id); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, now, audit.Entry{
			Action: "login.success", EntityType: "user", EntityID: id, ActorUserID: id,
		}); err != nil {
			return err
		}
		u, err := loadUser(ctx, tx, id, nil)
		if err != nil {
			return err
		}
		result = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	if authErr != nil {
		return nil, authErr
	}
	return result, nil
}

// LoadActiveUser memuat pengguna aktif berdasarkan ID (dipakai tiap request).
// Mengembalikan nil tanpa error bila user tidak ada atau sudah nonaktif.
func (s *Service) LoadActiveUser(ctx context.Context, id int64) (*rbac.User, error) {
	u, _, err := s.LoadSessionUser(ctx, id)
	return u, err
}

// LoadSessionUser memuat pengguna aktif beserta cap kredensialnya (sidik dari
// hash password). Cap berubah setiap kali password diganti/di-reset, sehingga
// session lama tidak berlaku lagi.
func (s *Service) LoadSessionUser(ctx context.Context, id int64) (*rbac.User, string, error) {
	var hash string
	u, err := loadUser(ctx, s.db, id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return u, credentialStamp(hash), nil
}

// credentialStamp: 16 karakter heksadesimal pertama SHA-256 dari hash password
// (bukan rahasia yang dapat dipakai menebak password).
func credentialStamp(passwordHash string) string {
	sum := sha256.Sum256([]byte(passwordHash))
	return hex.EncodeToString(sum[:8])
}

func loadUser(ctx context.Context, q database.DBTX, id int64, passwordHash *string) (*rbac.User, error) {
	var u rbac.User
	var hash string
	err := q.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.full_name, r.code, r.name, u.must_change_password, u.password_hash
		  FROM users u JOIN roles r ON r.id = u.role_id
		 WHERE u.id = ? AND u.is_active = TRUE`, id).
		Scan(&u.ID, &u.Username, &u.FullName, &u.RoleCode, &u.RoleName, &u.MustChangePassword, &hash)
	if passwordHash != nil {
		*passwordHash = hash
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ChangePassword mengganti password pengguna setelah memverifikasi password lama.
func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next, confirm string) (map[string]string, error) {
	errs := map[string]string{}
	if next != confirm {
		errs["confirm"] = "Konfirmasi password tidak sama."
	}
	if current == next {
		errs["new"] = "Password baru harus berbeda dari password saat ini."
	}

	now := s.clock.Now()
	var fieldErrs map[string]string
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var username, hash string
		if err := tx.QueryRowContext(ctx, `SELECT username, password_hash FROM users WHERE id = ? AND is_active = TRUE FOR UPDATE`, userID).
			Scan(&username, &hash); err != nil {
			return err
		}
		ok, err := VerifyPassword(current, hash)
		if err != nil {
			return err
		}
		if !ok {
			errs["current"] = ErrWrongCurrent.Error()
		}
		if perr := ValidatePasswordPolicy(next, username); perr != nil {
			if _, exists := errs["new"]; !exists {
				errs["new"] = upperFirst(perr.Error()) + "."
			}
		}
		if len(errs) > 0 {
			fieldErrs = errs
			return nil
		}
		newHash, err := HashPassword(next)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET password_hash = ?, must_change_password = FALSE, password_changed_at = ?, updated_at = ?
			 WHERE id = ?`, newHash, now, now, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "password.change", EntityType: "user", EntityID: userID})
	})
	if err != nil {
		return nil, err
	}
	return fieldErrs, nil
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
