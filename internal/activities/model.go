// Package activities mengelola kegiatan internal IT yang tidak berasal dari
// tiket (maintenance, instalasi, rapat, proyek, dll.). Kegiatan diberi nomor
// KEG-YYYY-NNNNNN, durasi dihitung otomatis oleh database, dan dipisahkan jelas
// dari tiket pengaduan agar laporan dapat mengidentifikasi jenis pekerjaan.
package activities

import (
	"database/sql"
	"time"
)

// Status kegiatan.
const (
	StatusPlanned    = "PLANNED"
	StatusInProgress = "IN_PROGRESS"
	StatusDone       = "DONE"
	StatusCancelled  = "CANCELLED"
)

var validStatus = map[string]bool{StatusPlanned: true, StatusInProgress: true, StatusDone: true, StatusCancelled: true}

// Activity adalah satu kegiatan beserta label relasinya.
type Activity struct {
	ID              int64
	No              string
	Date            time.Time
	TypeID          int64
	TypeName        string
	ITCategoryID    int64
	ITCategoryName  string
	DepartmentID    sql.NullInt64
	DepartmentName  sql.NullString
	RoomID          sql.NullInt64
	RoomName        sql.NullString
	AssetID         sql.NullInt64
	AssetCode       sql.NullString
	AssetName       sql.NullString
	RelatedTicketID sql.NullInt64
	RelatedTicketNo sql.NullString
	Title           string
	Description     sql.NullString
	Result          sql.NullString
	Status          string
	StartedAt       sql.NullTime
	EndedAt         sql.NullTime
	DurationMinutes sql.NullInt64
	CreatedBy       int64
	CreatedByName   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LeadID          sql.NullInt64
	LeadName        sql.NullString
	Members         []Member
}

// DurationText menampilkan durasi ("1 j 35 mnt").
func (a Activity) DurationText() string {
	if !a.DurationMinutes.Valid {
		return "–"
	}
	m := int(a.DurationMinutes.Int64)
	switch {
	case m < 60:
		return itoa(m) + " mnt"
	case m%60 == 0:
		return itoa(m/60) + " j"
	default:
		return itoa(m/60) + " j " + itoa(m%60) + " mnt"
	}
}

// Supports mengembalikan nama anggota pendukung.
func (a Activity) Supports() []string {
	var out []string
	for _, m := range a.Members {
		if m.Role == "SUPPORT" {
			out = append(out, m.Name)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Member adalah pelaksana/pendukung kegiatan.
type Member struct {
	UserID int64
	Name   string
	Role   string // LEAD | SUPPORT
}

// Input adalah isian form yang sudah diparse.
type Input struct {
	Date            time.Time
	TypeID          int64
	ITCategoryID    int64
	DepartmentID    int64
	RoomID          int64
	AssetID         int64
	RelatedTicketNo string
	Title           string
	Description     string
	Result          string
	Status          string
	StartedAt       *time.Time
	EndedAt         *time.Time
	LeadID          int64
	SupportIDs      []int64
}

// Filter daftar kegiatan.
type Filter struct {
	From, To      time.Time // tanggal WITA (inklusif)
	TypeID        int64
	ITCategoryID  int64
	MemberID      int64 // 0 = semua
	Status        string
	Search        string
	Limit, Offset int
}

// Option adalah pilihan select.
type Option struct {
	ID    int64
	Label string
	Extra int64 // mis. bidang bawaan jenis kegiatan
}

// Attachment adalah metadata lampiran kegiatan.
type Attachment struct {
	ID        int64
	CreatedAt time.Time
}
