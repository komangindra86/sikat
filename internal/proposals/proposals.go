// Package proposals menyimpan arsip kajian kebutuhan perangkat/sistem baru.
// Pemohon (pegawai unit) mengirim dokumen kajian lewat QR lapor tanpa login;
// setiap kajian masuk diterima oleh Admin IT. Tidak ada alur persetujuan:
// kajian hanya diarsipkan beserta dokumennya.
package proposals

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sikat/internal/attachments"
	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/notifications"
	"sikat/internal/rbac"
	"sikat/internal/web"
)

// Status kajian.
const (
	StatusSubmitted = "SUBMITTED"
	StatusReceived  = "RECEIVED"
)

// MaxFileBytes: batas ukuran dokumen kajian.
const MaxFileBytes = 10 << 20

// Error domain.
var (
	ErrNotFound = errors.New("kajian tidak ditemukan")
	// ErrReceived: kajian sudah diterima (mis. oleh admin lain).
	ErrReceived = errors.New("kajian sudah diterima")
)

// Proposal adalah satu kajian.
type Proposal struct {
	ID             int64
	Kind           string
	Title          string
	Summary        string
	DepartmentName sql.NullString
	RequesterName  string
	RequesterPhone sql.NullString
	FileKey        string
	FileName       string
	FileMime       string
	FileSize       int64
	Status         string
	ReceivedByName sql.NullString
	ReceivedAt     sql.NullTime
	ReceivedNote   sql.NullString
	CreatedAt      time.Time
}

// No adalah nomor tampilan kajian.
func (p Proposal) No() string { return fmt.Sprintf("KJN-%05d", p.ID) }

// Received bernilai true bila kajian sudah diterima admin.
func (p Proposal) Received() bool { return p.Status == StatusReceived }

// SizeText adalah ukuran dokumen yang mudah dibaca ("850 KB", "2,4 MB").
func (p Proposal) SizeText() string { return SizeText(p.FileSize) }

// SizeText memformat ukuran file dalam KB/MB (desimal koma).
func SizeText(n int64) string {
	if n < 1<<20 {
		kb := (n + 1023) / 1024
		return strconv.FormatInt(kb, 10) + " KB"
	}
	return strings.Replace(strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64), ".", ",", 1) + " MB"
}

// Draft adalah isian form kajian dari pemohon.
type Draft struct {
	Kind           string
	Title          string
	Summary        string
	DepartmentID   int64
	RequesterName  string
	RequesterPhone string
	QRCodeID       int64
}

// File adalah dokumen kajian yang sudah diperiksa.
type File struct {
	Data []byte
	Name string // nama asli (sudah dibersihkan)
	Ext  string
	Mime string
}

// Jenis dokumen yang diterima: ekstensi → (MIME, tanda awal file).
var docTypes = map[string]struct {
	mime  string
	magic []byte
}{
	"pdf":  {"application/pdf", []byte("%PDF-")},
	"docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK\x03\x04")},
	"xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("PK\x03\x04")},
	"doc":  {"application/msword", []byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1")},
	"xls":  {"application/vnd.ms-excel", []byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1")},
}

// Accept adalah nilai atribut accept input file.
const Accept = ".pdf,.doc,.docx,.xls,.xlsx"

// ReadFile membaca dan memeriksa dokumen kajian dari form. Mengembalikan pesan
// untuk pemohon bila dokumen ditolak (kosong, terlalu besar, atau bukan PDF/Word/Excel).
func ReadFile(fh *multipart.FileHeader) (File, string, error) {
	if fh == nil || fh.Size == 0 {
		return File{}, "Lampirkan dokumen kajian (PDF, Word, atau Excel).", nil
	}
	if fh.Size > MaxFileBytes {
		return File{}, fmt.Sprintf("Dokumen melebihi %d MB. Perkecil ukurannya (mis. simpan ulang sebagai PDF).", MaxFileBytes>>20), nil
	}
	name := cleanName(fh.Filename)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	t, ok := docTypes[ext]
	if !ok {
		return File{}, "Dokumen harus berupa PDF, Word (.doc/.docx), atau Excel (.xls/.xlsx).", nil
	}
	f, err := fh.Open()
	if err != nil {
		return File{}, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return File{}, "", err
	}
	if int64(len(data)) > MaxFileBytes {
		return File{}, fmt.Sprintf("Dokumen melebihi %d MB.", MaxFileBytes>>20), nil
	}
	if !bytes.HasPrefix(data, t.magic) {
		return File{}, "Isi dokumen tidak sesuai dengan jenis filenya atau file rusak. Simpan ulang dokumen lalu coba lagi.", nil
	}
	return File{Data: data, Name: name, Ext: ext, Mime: t.mime}, "", nil
}

// cleanName mengambil nama file saja (tanpa folder) dan membuang karakter kontrol.
func cleanName(n string) string {
	n = strings.ReplaceAll(n, "\\", "/")
	if i := strings.LastIndexByte(n, '/'); i >= 0 {
		n = n[i+1:]
	}
	n = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, strings.TrimSpace(n))
	if utf8.RuneCountInString(n) > 200 {
		ext := filepath.Ext(n)
		n = string([]rune(n)[:200-utf8.RuneCountInString(ext)]) + ext
	}
	if n == "" {
		n = "kajian"
	}
	return n
}

// ParseDraft membaca isian form kajian. form berisi nilai untuk diisi ulang.
func ParseDraft(get func(string) string) (Draft, map[string]string, map[string]string) {
	form := map[string]string{}
	for _, k := range []string{"kind", "title", "summary", "department_id", "requester_name", "requester_phone"} {
		form[k] = strings.TrimSpace(get(k))
	}
	errs := map[string]string{}
	d := Draft{Kind: form["kind"], Title: form["title"], Summary: form["summary"],
		RequesterName: form["requester_name"], RequesterPhone: form["requester_phone"]}
	if !web.ValidLabel("proposal_kind", d.Kind) {
		errs["kind"] = "Pilih jenis kebutuhan."
	}
	text := func(field, v, empty string, max int) {
		switch {
		case v == "" && empty != "":
			errs[field] = empty
		case utf8.RuneCountInString(v) > max:
			errs[field] = fmt.Sprintf("Maksimal %d karakter.", max)
		}
	}
	text("title", d.Title, "Isi judul kajian.", 200)
	text("summary", d.Summary, "Isi ringkasan kebutuhan.", 2000)
	text("requester_name", d.RequesterName, "Isi nama Anda.", 120)
	text("requester_phone", d.RequesterPhone, "", 30)
	d.DepartmentID, _ = strconv.ParseInt(form["department_id"], 10, 64)
	if d.DepartmentID <= 0 {
		errs["department_id"] = "Pilih unit pemohon."
	}
	return d, form, errs
}

// Option adalah pilihan dropdown.
type Option struct {
	ID    int64
	Label string
}

// Service mengelola kajian.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	store attachments.Store
}

// NewService membuat Service. store: folder dokumen kajian.
func NewService(db *sql.DB, c clock.Clock, store attachments.Store) *Service {
	return &Service{db: db, clock: c, store: store}
}

// Store adalah penyimpanan dokumen kajian.
func (s *Service) Store() attachments.Store { return s.store }

// Departments mengembalikan unit aktif untuk pilihan unit pemohon.
func (s *Service) Departments(ctx context.Context) ([]Option, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM departments WHERE is_active = TRUE ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Option
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Label); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func validToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(t)
	return err == nil
}

func hashToken(t string) []byte {
	sum := sha256.Sum256([]byte(t))
	return sum[:]
}

// Submitted adalah hasil pengiriman kajian.
type Submitted struct {
	ID    int64
	Token string // token link pemantauan (hanya diketahui pemohon)
}

// Submit menyimpan dokumen dan data kajian, lalu memberi tahu Admin IT.
func (s *Service) Submit(ctx context.Context, d Draft, f File) (Submitted, map[string]string, error) {
	var deptName string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM departments WHERE id = ? AND is_active = TRUE`, d.DepartmentID).Scan(&deptName)
	if errors.Is(err, sql.ErrNoRows) {
		return Submitted{}, map[string]string{"department_id": "Unit tidak ditemukan atau sudah tidak aktif."}, nil
	}
	if err != nil {
		return Submitted{}, nil, err
	}
	token, err := newToken()
	if err != nil {
		return Submitted{}, nil, err
	}
	now := s.clock.Now()
	key, err := s.store.SaveExt(f.Data, now, f.Ext)
	if err != nil {
		return Submitted{}, nil, err
	}
	sum := sha256.Sum256(f.Data)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.store.Remove(key)
		return Submitted{}, nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO proposals (public_token_hash, kind, title, summary, department_id, requester_name, requester_phone,
		                       qr_code_id, file_key, file_name, file_mime, file_size, file_sha256, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hashToken(token), d.Kind, d.Title, d.Summary, d.DepartmentID, d.RequesterName, nullStr(d.RequesterPhone),
		nullInt(d.QRCodeID), key, f.Name, f.Mime, len(f.Data), sum[:], now, now)
	if err != nil {
		s.store.Remove(key)
		return Submitted{}, nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := notifications.Publish(ctx, tx, notifications.Event{
		Type: notifications.ProposalSubmitted, DedupeKey: fmt.Sprintf("proposal:%d", id),
		Payload: map[string]any{"proposal_id": id, "title": d.Title, "kind": d.Kind, "department": deptName, "requester": d.RequesterName},
	}, now); err != nil {
		s.store.Remove(key)
		return Submitted{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		s.store.Remove(key)
		return Submitted{}, nil, err
	}
	return Submitted{ID: id, Token: token}, nil, nil
}

const selectProposal = `
	SELECT p.id, p.kind, p.title, p.summary, d.name, p.requester_name, p.requester_phone,
	       p.file_key, p.file_name, p.file_mime, p.file_size, p.status, u.full_name, p.received_at, p.received_note, p.created_at
	  FROM proposals p
	  LEFT JOIN departments d ON d.id = p.department_id
	  LEFT JOIN users u ON u.id = p.received_by`

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Proposal, error) {
	var p Proposal
	err := r.Scan(&p.ID, &p.Kind, &p.Title, &p.Summary, &p.DepartmentName, &p.RequesterName, &p.RequesterPhone,
		&p.FileKey, &p.FileName, &p.FileMime, &p.FileSize, &p.Status, &p.ReceivedByName, &p.ReceivedAt, &p.ReceivedNote, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Get mengambil kajian berdasarkan ID.
func (s *Service) Get(ctx context.Context, id int64) (Proposal, error) {
	return scan(s.db.QueryRowContext(ctx, selectProposal+` WHERE p.id = ?`, id))
}

// GetByToken mengambil kajian dari token link pemantauan pemohon.
func (s *Service) GetByToken(ctx context.Context, token string) (Proposal, error) {
	if !validToken(token) {
		return Proposal{}, ErrNotFound
	}
	return scan(s.db.QueryRowContext(ctx, selectProposal+` WHERE p.public_token_hash = ?`, hashToken(token)))
}

// Filter untuk daftar kajian.
type Filter struct {
	Status string
	Kind   string
	Search string
}

// List mengembalikan kajian terbaru lebih dulu (belum diterima di atas) dan mengisi pager.Total.
func (s *Service) List(ctx context.Context, f Filter, pager *web.Pager) ([]Proposal, error) {
	where := []string{"1=1"}
	var args []any
	if web.ValidLabel("proposal_status", f.Status) {
		where = append(where, "p.status = ?")
		args = append(args, f.Status)
	}
	if web.ValidLabel("proposal_kind", f.Kind) {
		where = append(where, "p.kind = ?")
		args = append(args, f.Kind)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q) + "%"
		cond := "(p.title LIKE ? OR p.requester_name LIKE ? OR d.name LIKE ? OR p.summary LIKE ?"
		args = append(args, like, like, like, like)
		if n, err := strconv.ParseInt(strings.TrimLeft(strings.TrimPrefix(strings.ToUpper(q), "KJN-"), "0"), 10, 64); err == nil && n > 0 {
			cond += " OR p.id = ?"
			args = append(args, n)
		}
		where = append(where, cond+")")
	}
	cond := " WHERE " + strings.Join(where, " AND ")
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposals p LEFT JOIN departments d ON d.id = p.department_id`+cond, args...).
		Scan(&pager.Total); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, selectProposal+cond+` ORDER BY p.status = 'RECEIVED', p.created_at DESC, p.id DESC LIMIT ? OFFSET ?`,
		append(args, pager.PerPage, pager.Offset())...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountSubmitted adalah jumlah kajian yang belum diterima.
func (s *Service) CountSubmitted(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposals WHERE status = 'SUBMITTED'`).Scan(&n)
	return n, err
}

// Receive menandai kajian diterima Admin IT, dengan catatan opsional untuk pemohon.
func (s *Service) Receive(ctx context.Context, id int64, u *rbac.User, note string) (map[string]string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 1000 {
		return map[string]string{"received_note": "Maksimal 1000 karakter."}, nil
	}
	now := s.clock.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM proposals WHERE id = ? FOR UPDATE`, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if status != StatusSubmitted {
		return nil, ErrReceived
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proposals SET status = 'RECEIVED', received_by = ?, received_at = ?, received_note = ?, updated_at = ? WHERE id = ?`,
		u.ID, now, nullStr(note), now, id); err != nil {
		return nil, err
	}
	if err := audit.Record(ctx, tx, now, audit.Entry{Action: "proposal.receive", EntityType: "proposals", EntityID: id,
		ActorUserID: u.ID, Before: map[string]any{"status": status}, After: map[string]any{"status": StatusReceived, "note": note}}); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func nullInt(n int64) sql.NullInt64   { return sql.NullInt64{Int64: n, Valid: n != 0} }
