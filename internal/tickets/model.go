// Package tickets menangani pengajuan tiket (publik via QR & oleh petugas),
// penomoran, prioritas otomatis, antrean, klaim tiket, dan halaman pemantauan pelapor.
package tickets

import (
	"database/sql"
	"time"
)

// Status tiket.
const (
	StatusNew        = "NEW"
	StatusClaimed    = "CLAIMED"
	StatusInProgress = "IN_PROGRESS"
	StatusPending    = "PENDING"
	StatusResolved   = "RESOLVED"
	StatusClosed     = "CLOSED"
	StatusCancelled  = "CANCELLED"
)

// Sumber tiket.
const (
	ChannelQR      = "QR"
	ChannelPhone   = "PHONE"
	ChannelMessage = "MESSAGE"
	ChannelWalkIn  = "WALK_IN"
)

// Jenis kejadian histori.
const (
	EventCreated = "CREATED"
	EventClaimed = "CLAIMED"
)

// Ticket adalah tiket beserta label relasinya.
type Ticket struct {
	ID                int64
	TicketNo          string
	Channel           string
	Status            string
	Priority          string
	PrioritySource    string
	PriorityRuleName  sql.NullString
	DepartmentID      sql.NullInt64
	DepartmentName    sql.NullString
	RoomName          sql.NullString
	RoomLocation      sql.NullString
	AssetID           sql.NullInt64
	AssetCode         sql.NullString
	AssetName         sql.NullString
	ReporterName      string
	ReporterPhone     sql.NullString
	ReporterDeptName  sql.NullString
	CategoryName      string
	ITCategoryName    string
	Description       string
	Impact            int
	Scope             int
	Replacement       string
	CriticalService   bool
	IncidentStartedAt sql.NullTime
	AssigneeID        sql.NullInt64
	AssigneeName      sql.NullString
	CreatedByName     sql.NullString
	FirstResponseAt   sql.NullTime
	CreatedAt         time.Time
	AttachmentCount   int
}

// AssigneeFirstName untuk halaman pelapor (tanpa nama lengkap petugas).
func (t Ticket) AssigneeFirstName() string {
	n := t.AssigneeName.String
	for i, r := range n {
		if r == ' ' {
			return n[:i]
		}
	}
	return n
}

// Location menggabungkan unit dan ruangan untuk tampilan.
func (t Ticket) Location() string {
	switch {
	case t.DepartmentName.Valid && t.RoomName.Valid:
		return t.DepartmentName.String + " · " + t.RoomName.String
	case t.DepartmentName.Valid:
		return t.DepartmentName.String
	case t.RoomName.Valid:
		return t.RoomName.String
	}
	return "–"
}

// History adalah satu kejadian pada tiket.
type History struct {
	ID         int64
	EventType  string
	FromStatus sql.NullString
	ToStatus   sql.NullString
	ActorType  string
	ActorName  sql.NullString
	Note       sql.NullString
	CreatedAt  time.Time
}

// ActorFirstName untuk tampilan pelapor.
func (h History) ActorFirstName() string {
	n := h.ActorName.String
	for i, r := range n {
		if r == ' ' {
			return n[:i]
		}
	}
	return n
}

// Attachment adalah metadata lampiran.
type Attachment struct {
	ID         int64
	Width      sql.NullInt64
	Height     sql.NullInt64
	SizeBytes  int64
	UploadedBy string
	CreatedAt  time.Time
}

// Draft adalah data pengajuan yang sudah diparse dari form.
type Draft struct {
	Channel              string
	CreatedBy            int64 // 0 = pelapor publik
	QRCodeID             int64
	DepartmentID         int64
	RoomID               int64
	AssetID              int64
	ReporterName         string
	ReporterPhone        string
	ReporterDepartmentID int64
	TicketCategoryID     int64
	Description          string
	Impact               int
	Scope                int
	Replacement          string
	IncidentStartedAt    *time.Time
	// RoomFromQR: ruangan berasal dari QR ruangan; perangkat yang dipilih wajib di ruangan itu.
	RoomFromQR bool
}

// Created adalah hasil pembuatan tiket. Token hanya tersedia saat ini (yang
// disimpan di database hanya hash-nya).
type Created struct {
	ID       int64
	Number   string
	Token    string
	Priority string
}

// ClaimResult adalah hasil percobaan mengambil tiket.
type ClaimResult struct {
	OK        bool
	Status    string
	ClaimedBy string
	ClaimedAt sql.NullTime
}

// QueueFilter untuk daftar tiket petugas.
type QueueFilter struct {
	Mode           string // available | mine | active
	UserID         int64
	OnlyCompetency bool
	Priority       string
	DepartmentID   int64
	Limit, Offset  int
}

// Workload adalah beban kerja satu petugas.
type Workload struct {
	UserID  int64
	Name    string
	Role    string
	Primary int
	Support int
}

// Category adalah pilihan jenis gangguan.
type Category struct {
	ID           int64
	Name         string
	HelpText     sql.NullString
	ITCategoryID int64
}

// Option adalah pilihan select umum.
type Option struct {
	ID    int64
	Label string
}

// Summary adalah angka ringkasan tiket untuk beranda.
type Summary struct {
	ByStatus       map[string]int
	CriticalNew    int
	MineActive     int
	AvailableForMe int
	CreatedToday   int
}
