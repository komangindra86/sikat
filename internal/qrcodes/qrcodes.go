// Package qrcodes mengelola QR ruangan, QR perangkat, dan satu QR global.
//
// QR global tidak terikat lokasi: pelapor memilih unit dan ruangan sendiri.
// Hanya satu QR global yang aktif pada satu waktu.
//
// Token QR adalah 16 byte acak (crypto/rand) yang dikodekan base62 menjadi 22
// karakter — bukan ID database berurutan. Satu ruangan/perangkat hanya memiliki
// satu QR aktif (dijaga UNIQUE index di database). Regenerate menonaktifkan QR
// lama dan membuat token baru, sehingga stiker lama tidak berlaku lagi.
package qrcodes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
)

// Jenis QR.
const (
	TypeRoom   = "ROOM"
	TypeAsset  = "ASSET"
	TypeGlobal = "GLOBAL"
)

// Kode dan nama tampilan QR global (tidak punya ruangan/perangkat).
const (
	GlobalCode = "UMUM"
	GlobalName = "QR global pelapor"
)

// Error aturan bisnis.
var (
	ErrNotFound      = errors.New("QR tidak ditemukan")
	ErrTargetInvalid = errors.New("Ruangan/perangkat tidak ditemukan, nonaktif, atau sudah dihapus dari inventaris.")
	ErrAlreadyExists = errors.New("Ruangan/perangkat ini sudah memiliki QR aktif.")
	ErrGlobalExists  = errors.New("QR global aktif sudah ada. Buat ulang dari halaman QR tersebut bila perlu mengganti.")
	ErrInactive      = errors.New("QR ini sudah tidak aktif.")
)

// QR adalah satu QR beserta label sasarannya.
type QR struct {
	ID             int64
	Token          string
	Type           string
	RoomID         sql.NullInt64
	AssetID        sql.NullInt64
	IsActive       bool
	Generation     int
	ScanCount      int
	LastScannedAt  sql.NullTime
	CreatedAt      time.Time
	DeactivatedAt  sql.NullTime
	TargetCode     string
	TargetName     string
	DepartmentName sql.NullString
	RoomName       sql.NullString
}

// Label untuk nama file dan stiker.
func (q QR) Label() string { return q.TargetCode + " · " + q.TargetName }

// Filter daftar QR.
type Filter struct {
	Type            string
	Search          string
	IncludeInactive bool
	IDs             []int64
	Limit, Offset   int
}

// Service mengelola QR.
type Service struct {
	db      *sql.DB
	clock   clock.Clock
	baseURL string
}

// NewService membuat Service. baseURL dipakai untuk isi QR (…/q/{token}).
func NewService(db *sql.DB, c clock.Clock, baseURL string) *Service {
	return &Service{db: db, clock: c, baseURL: strings.TrimRight(baseURL, "/")}
}

// URL mengembalikan alamat publik yang dikodekan di QR.
func (s *Service) URL(token string) string { return s.baseURL + "/q/" + token }

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// NewToken membuat token acak 22 karakter base62 (128 bit).
func NewToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(62)
	mod := new(big.Int)
	out := make([]byte, 0, 22)
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append(out, base62[mod.Int64()])
	}
	for len(out) < 22 {
		out = append(out, '0')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

// ValidTokenFormat memeriksa format token sebelum query (menolak input aneh lebih awal).
func ValidTokenFormat(t string) bool {
	if len(t) != 22 {
		return false
	}
	for i := 0; i < len(t); i++ {
		if !strings.ContainsRune(base62, rune(t[i])) {
			return false
		}
	}
	return true
}

// PNG menghasilkan gambar QR untuk token.
func (s *Service) PNG(token string, size int) ([]byte, error) {
	return qrcode.Encode(s.URL(token), qrcode.Medium, size)
}

const selectQR = `
	SELECT q.id, q.token, q.type, q.room_id, q.asset_id, q.is_active, q.generation, q.scan_count,
	       q.last_scanned_at, q.created_at, q.deactivated_at,
	       COALESCE(r.code, a.asset_code, '` + GlobalCode + `'), COALESCE(r.name, a.name, '` + GlobalName + `'),
	       COALESCE(rd.name, ad.name), COALESCE(r.name, ar.name)
	  FROM qr_codes q
	  LEFT JOIN rooms r ON r.id = q.room_id
	  LEFT JOIN departments rd ON rd.id = r.department_id
	  LEFT JOIN assets a ON a.id = q.asset_id
	  LEFT JOIN rooms ar ON ar.id = a.room_id
	  LEFT JOIN departments ad ON ad.id = a.department_id`

func scanQR(sc interface{ Scan(...any) error }) (QR, error) {
	var q QR
	err := sc.Scan(&q.ID, &q.Token, &q.Type, &q.RoomID, &q.AssetID, &q.IsActive, &q.Generation, &q.ScanCount,
		&q.LastScannedAt, &q.CreatedAt, &q.DeactivatedAt, &q.TargetCode, &q.TargetName, &q.DepartmentName, &q.RoomName)
	return q, err
}

// List mengembalikan daftar QR dan total.
func (s *Service) List(ctx context.Context, f Filter) ([]QR, int, error) {
	where := []string{"1=1"}
	var args []any
	if !f.IncludeInactive {
		where = append(where, "q.is_active = TRUE")
	}
	if f.Type == TypeRoom || f.Type == TypeAsset || f.Type == TypeGlobal {
		where = append(where, "q.type = ?")
		args = append(args, f.Type)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
		where = append(where, "(r.code LIKE ? OR r.name LIKE ? OR a.asset_code LIKE ? OR a.name LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if len(f.IDs) > 0 {
		where = append(where, "q.id IN (?"+strings.Repeat(",?", len(f.IDs)-1)+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	w := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM qr_codes q
		LEFT JOIN rooms r ON r.id = q.room_id LEFT JOIN assets a ON a.id = q.asset_id`+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, selectQR+w+" ORDER BY q.type DESC, COALESCE(r.code, a.asset_code), q.id DESC LIMIT ? OFFSET ?",
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []QR
	for rows.Next() {
		q, err := scanQR(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, q)
	}
	return out, total, rows.Err()
}

// Get mengambil satu QR.
func (s *Service) Get(ctx context.Context, id int64) (QR, error) {
	q, err := scanQR(s.db.QueryRowContext(ctx, selectQR+" WHERE q.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return QR{}, ErrNotFound
	}
	return q, err
}

// ActiveGlobal mengembalikan QR global yang aktif (ok=false bila belum ada).
func (s *Service) ActiveGlobal(ctx context.Context) (QR, bool, error) {
	q, err := scanQR(s.db.QueryRowContext(ctx, selectQR+" WHERE q.active_global = 1"))
	if errors.Is(err, sql.ErrNoRows) {
		return QR{}, false, nil
	}
	return q, err == nil, err
}

// ActiveFor mengembalikan QR aktif milik ruangan/perangkat (ok=false bila belum ada).
func (s *Service) ActiveFor(ctx context.Context, typ string, targetID int64) (QR, bool, error) {
	col := "q.room_id"
	if typ == TypeAsset {
		col = "q.asset_id"
	}
	q, err := scanQR(s.db.QueryRowContext(ctx, selectQR+" WHERE q.is_active = TRUE AND q.type = ? AND "+col+" = ?", typ, targetID))
	if errors.Is(err, sql.ErrNoRows) {
		return QR{}, false, nil
	}
	return q, err == nil, err
}

func targetValid(ctx context.Context, tx *sql.Tx, typ string, id int64) (bool, error) {
	var n int
	var err error
	switch typ {
	case TypeRoom:
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rooms WHERE id = ? AND is_active = TRUE`, id).Scan(&n)
	case TypeAsset:
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE id = ? AND status <> 'DISPOSED'`, id).Scan(&n)
	case TypeGlobal:
		return true, nil
	default:
		return false, nil
	}
	return n > 0, err
}

func (s *Service) insert(ctx context.Context, tx *sql.Tx, actorID int64, typ string, targetID int64, generation int, now time.Time) (int64, error) {
	token, err := NewToken()
	if err != nil {
		return 0, err
	}
	var room, asset sql.NullInt64
	switch typ {
	case TypeRoom:
		room = sql.NullInt64{Int64: targetID, Valid: true}
	case TypeAsset:
		asset = sql.NullInt64{Int64: targetID, Valid: true}
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO qr_codes (token, type, room_id, asset_id, generation, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, token, typ, room, asset, generation, database.NullInt64(actorID), now, now)
	if err != nil {
		if _, dup := database.DuplicateKey(err); dup {
			if typ == TypeGlobal {
				return 0, ErrGlobalExists
			}
			return 0, ErrAlreadyExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

// Generate membuat QR untuk satu ruangan/perangkat.
func (s *Service) Generate(ctx context.Context, actorID int64, typ string, targetID int64) (int64, error) {
	now := s.clock.Now()
	var id int64
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		ok, err := targetValid(ctx, tx, typ, targetID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrTargetInvalid
		}
		if id, err = s.insert(ctx, tx, actorID, typ, targetID, 1, now); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "qr.generate", EntityType: "qr_codes", EntityID: id,
			After: map[string]any{"type": typ, "target_id": targetID}})
	})
	return id, err
}

// GenerateMissing membuat QR untuk semua ruangan aktif / perangkat yang belum punya QR aktif.
func (s *Service) GenerateMissing(ctx context.Context, actorID int64, typ string) (int, error) {
	now := s.clock.Now()
	q := `SELECT r.id FROM rooms r WHERE r.is_active = TRUE
	        AND NOT EXISTS (SELECT 1 FROM qr_codes q WHERE q.active_room_id = r.id)`
	if typ == TypeAsset {
		q = `SELECT a.id FROM assets a WHERE a.status <> 'DISPOSED'
		       AND NOT EXISTS (SELECT 1 FROM qr_codes q WHERE q.active_asset_id = a.id)`
	} else if typ != TypeRoom {
		return 0, ErrTargetInvalid
	}
	count := 0
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, q+" FOR UPDATE")
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if _, err := s.insert(ctx, tx, actorID, typ, id, 1, now); err != nil {
				return err
			}
		}
		count = len(ids)
		if count == 0 {
			return nil
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "qr.generate_bulk", EntityType: "qr_codes",
			After: map[string]any{"type": typ, "count": count}})
	})
	return count, err
}

// MissingCounts menghitung ruangan aktif & perangkat yang belum punya QR aktif.
func (s *Service) MissingCounts(ctx context.Context) (rooms, assets int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM rooms r WHERE r.is_active = TRUE
		          AND NOT EXISTS (SELECT 1 FROM qr_codes q WHERE q.active_room_id = r.id)),
		       (SELECT COUNT(*) FROM assets a WHERE a.status <> 'DISPOSED'
		          AND NOT EXISTS (SELECT 1 FROM qr_codes q WHERE q.active_asset_id = a.id))`).Scan(&rooms, &assets)
	return
}

// RoomOption adalah ruangan yang belum memiliki QR aktif.
type RoomOption struct {
	ID    int64
	Label string
}

// RoomsWithoutQR mengembalikan ruangan aktif tanpa QR aktif.
func (s *Service) RoomsWithoutQR(ctx context.Context) ([]RoomOption, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, CONCAT(d.name, ' · ', r.name, ' (', r.code, ')') FROM rooms r JOIN departments d ON d.id = r.department_id
		 WHERE r.is_active = TRUE AND NOT EXISTS (SELECT 1 FROM qr_codes q WHERE q.active_room_id = r.id)
		 ORDER BY d.name, r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoomOption
	for rows.Next() {
		var o RoomOption
		if err := rows.Scan(&o.ID, &o.Label); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Deactivate menonaktifkan QR (stiker tidak bisa dipakai lagi).
func (s *Service) Deactivate(ctx context.Context, actorID, id int64) error {
	now := s.clock.Now()
	return database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT is_active FROM qr_codes WHERE id = ? FOR UPDATE`, id).Scan(&active); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if !active {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE qr_codes SET is_active = FALSE, deactivated_at = ?, deactivated_by = ?, updated_at = ? WHERE id = ?`,
			now, database.NullInt64(actorID), now, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "qr.deactivate", EntityType: "qr_codes", EntityID: id})
	})
}

// Regenerate menonaktifkan QR lama dan membuat token baru untuk sasaran yang sama.
func (s *Service) Regenerate(ctx context.Context, actorID, id int64) (int64, error) {
	now := s.clock.Now()
	var newID int64
	err := database.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var (
			typ         string
			room, asset sql.NullInt64
			active      bool
			generation  int
		)
		if err := tx.QueryRowContext(ctx, `SELECT type, room_id, asset_id, is_active, generation FROM qr_codes WHERE id = ? FOR UPDATE`, id).
			Scan(&typ, &room, &asset, &active, &generation); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		target := room.Int64
		if typ == TypeAsset {
			target = asset.Int64
		}
		ok, err := targetValid(ctx, tx, typ, target)
		if err != nil {
			return err
		}
		if !ok {
			return ErrTargetInvalid
		}
		if active {
			if _, err := tx.ExecContext(ctx, `UPDATE qr_codes SET is_active = FALSE, deactivated_at = ?, deactivated_by = ?, updated_at = ? WHERE id = ?`,
				now, database.NullInt64(actorID), now, id); err != nil {
				return err
			}
		}
		if newID, err = s.insert(ctx, tx, actorID, typ, target, generation+1, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE qr_codes SET replaced_by_id = ? WHERE id = ?`, newID, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "qr.regenerate", EntityType: "qr_codes", EntityID: newID,
			Before: map[string]int64{"qr_id": id}, After: map[string]any{"qr_id": newID, "generation": generation + 1}})
	})
	return newID, err
}

// Target adalah lokasi/perangkat yang dikenali dari token QR saat dipindai.
type Target struct {
	QRID              int64
	Type              string
	DepartmentID      sql.NullInt64
	DepartmentName    sql.NullString
	RoomID            sql.NullInt64
	RoomName          sql.NullString
	RoomLocation      sql.NullString
	AssetID           sql.NullInt64
	AssetCode         sql.NullString
	AssetName         sql.NullString
	AssetBrandModel   sql.NullString
	AssetITCategoryID sql.NullInt64
	CriticalService   bool
}

// Resolve mencari sasaran QR aktif dari token. Untuk QR perangkat, ruangan dan
// unit diambil dari data perangkat saat ini (perangkat bisa berpindah ruangan).
// Mengembalikan ErrNotFound (token tidak dikenal) atau ErrInactive.
func (s *Service) Resolve(ctx context.Context, token string) (Target, error) {
	if !ValidTokenFormat(token) {
		return Target{}, ErrNotFound
	}
	var t Target
	var active bool
	err := s.db.QueryRowContext(ctx, `
		SELECT q.id, q.type, q.is_active,
		       COALESCE(r.department_id, a.department_id, ar.department_id),
		       d.name,
		       COALESCE(q.room_id, a.room_id),
		       COALESCE(r.name, ar.name), COALESCE(r.location_note, ar.location_note),
		       a.id, a.asset_code, a.name, NULLIF(TRIM(CONCAT_WS(' ', a.brand, a.model)), ''), ac.it_category_id,
		       COALESCE(COALESCE(r.is_critical_service, ar.is_critical_service), d.is_critical_service, FALSE)
		  FROM qr_codes q
		  LEFT JOIN rooms r ON r.id = q.room_id
		  LEFT JOIN assets a ON a.id = q.asset_id AND a.status <> 'DISPOSED'
		  LEFT JOIN asset_categories ac ON ac.id = a.asset_category_id
		  LEFT JOIN rooms ar ON ar.id = a.room_id
		  LEFT JOIN departments d ON d.id = COALESCE(r.department_id, a.department_id, ar.department_id)
		 WHERE q.token = ?`, token).
		Scan(&t.QRID, &t.Type, &active, &t.DepartmentID, &t.DepartmentName, &t.RoomID, &t.RoomName, &t.RoomLocation,
			&t.AssetID, &t.AssetCode, &t.AssetName, &t.AssetBrandModel, &t.AssetITCategoryID, &t.CriticalService)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, err
	}
	if !active || (t.Type == TypeAsset && !t.AssetID.Valid) {
		return Target{}, ErrInactive
	}
	return t, nil
}

// RecordScan menambah penghitung scan (tidak kritis; error diabaikan pemanggil).
func (s *Service) RecordScan(ctx context.Context, qrID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE qr_codes SET scan_count = scan_count + 1, last_scanned_at = ? WHERE id = ?`, s.clock.Now(), qrID)
	return err
}
