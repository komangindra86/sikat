// Package assets mengelola master perangkat IT.
// Riwayat pemeliharaan & statistik perangkat ditambahkan pada PHASE 4.
package assets

import (
	"database/sql"
	"time"
)

// Status perangkat.
const (
	StatusActive   = "ACTIVE"
	StatusBroken   = "BROKEN"
	StatusInRepair = "IN_REPAIR"
	StatusUnused   = "UNUSED"
	StatusDisposed = "DISPOSED"
)

// Asset adalah satu perangkat beserta label relasinya.
type Asset struct {
	ID             int64
	AssetCode      string
	Name           string
	CategoryID     int64
	CategoryName   string
	Brand          sql.NullString
	Model          sql.NullString
	SerialNumber   sql.NullString
	DepartmentID   sql.NullInt64
	DepartmentName sql.NullString
	RoomID         sql.NullInt64
	RoomName       sql.NullString
	ProcuredAt     sql.NullTime
	Status         string
	DisposedAt     sql.NullTime
	Notes          sql.NullString
	Source         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	HasOpenAlert   bool
}

// Input adalah data form perangkat yang sudah divalidasi.
type Input struct {
	AssetCode    string
	Name         string
	CategoryID   int64
	Brand        string
	Model        string
	SerialNumber string
	DepartmentID int64
	RoomID       int64
	ProcuredAt   *time.Time
	Status       string
	Notes        string
}

// Filter untuk daftar perangkat.
type Filter struct {
	Search       string
	DepartmentID int64
	CategoryID   int64
	Status       string // "" = semua kecuali DISPOSED, "ALL" = semua
	Limit        int
	Offset       int
}

// Option adalah pilihan select.
type Option struct {
	ID    int64
	Label string
}

// RoomOption adalah pilihan ruangan beserta unitnya.
type RoomOption struct {
	ID           int64
	Label        string
	DepartmentID int64
}

// StatusCount adalah jumlah perangkat per status.
type StatusCount struct {
	Status string
	Count  int
}
