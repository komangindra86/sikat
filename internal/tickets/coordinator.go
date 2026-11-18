package tickets

import (
	"context"
	"database/sql"
	"errors"

	"sikat/internal/database"
	"sikat/internal/rbac"
)

// Koordinator per bidang (keputusan 13/11/2026): koordinator tetap dapat melihat
// semua tiket, tetapi tindakan koordinator (ambil alih, ubah prioritas, batalkan,
// tutup, buka kembali, atur anggota tim) hanya untuk tiket bidangnya. Bila bidang
// belum punya koordinator aktif, semua koordinator boleh menindak (cadangan).

// FieldCoordinator adalah koordinator sebuah bidang.
type FieldCoordinator struct {
	FieldName string
	UserID    int64
	Name      string
	// Active: koordinator ditetapkan, akunnya aktif, dan masih ber-role Koordinator.
	Active bool
}

// fieldCoordinator membaca koordinator bidang.
func fieldCoordinator(ctx context.Context, q database.DBTX, itCategoryID int64) (FieldCoordinator, error) {
	var fc FieldCoordinator
	var uid sql.NullInt64
	var name sql.NullString
	var active sql.NullBool
	err := q.QueryRowContext(ctx, `
		SELECT ic.name, ic.coordinator_user_id, u.full_name, u.is_active AND r.code = 'COORDINATOR'
		  FROM it_categories ic
		  LEFT JOIN users u ON u.id = ic.coordinator_user_id
		  LEFT JOIN roles r ON r.id = u.role_id
		 WHERE ic.id = ?`, itCategoryID).Scan(&fc.FieldName, &uid, &name, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return fc, nil
	}
	fc.UserID, fc.Name, fc.Active = uid.Int64, name.String, uid.Valid && active.Bool
	return fc, err
}

// TicketFieldCoordinator mengembalikan koordinator bidang sebuah tiket (detail tiket).
func (s *Service) TicketFieldCoordinator(ctx context.Context, ticketID int64) (FieldCoordinator, error) {
	var cat int64
	if err := s.db.QueryRowContext(ctx, `SELECT it_category_id FROM tickets WHERE id = ?`, ticketID).Scan(&cat); err != nil {
		return FieldCoordinator{}, err
	}
	return fieldCoordinator(ctx, s.db, cat)
}

// CanCoordinate: apakah pengguna boleh melakukan tindakan koordinator pada bidang.
// Hanya membatasi role Koordinator; izin dasar (permission) tetap diperiksa terpisah.
func (fc FieldCoordinator) CanCoordinate(u *rbac.User) bool {
	if u == nil || u.RoleCode != rbac.RoleCoordinator {
		return true
	}
	return !fc.Active || fc.UserID == u.ID
}

// fieldError adalah penolakan karena tiket milik bidang koordinator lain.
type fieldError struct{ msg string }

func (e fieldError) Error() string        { return e.msg }
func (e fieldError) Is(target error) bool { return target == ErrForbidden }

// requireField menolak tindakan koordinator di luar bidangnya.
func requireField(ctx context.Context, q database.DBTX, u *rbac.User, itCategoryID int64) error {
	if u == nil || u.RoleCode != rbac.RoleCoordinator {
		return nil
	}
	fc, err := fieldCoordinator(ctx, q, itCategoryID)
	if err != nil {
		return err
	}
	if fc.CanCoordinate(u) {
		return nil
	}
	return fieldError{"Tiket ini termasuk bidang " + fc.FieldName + " yang dikoordinasi oleh " + fc.Name +
		". Hubungi beliau atau minta Admin IT mengubah koordinator bidang."}
}

// CoordinatedFields mengembalikan nama bidang yang dikoordinasi pengguna.
func (s *Service) CoordinatedFields(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM it_categories WHERE coordinator_user_id = ? ORDER BY sort_order, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
