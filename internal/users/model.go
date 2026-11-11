// Package users mengelola akun petugas IT, role, dan kompetensi.
package users

import (
	"database/sql"
	"time"
)

// User adalah akun beserta label relasinya.
type User struct {
	ID                 int64
	Username           string
	FullName           string
	NIP                sql.NullString
	Phone              sql.NullString
	Email              sql.NullString
	RoleID             int64
	RoleCode           string
	RoleName           string
	DepartmentID       sql.NullInt64
	DepartmentName     sql.NullString
	IsActive           bool
	MustChangePassword bool
	LockedUntil        sql.NullTime
	LastLoginAt        sql.NullTime
	CreatedAt          time.Time
	Competencies       []int64
}

// Input adalah data form yang sudah divalidasi.
type Input struct {
	Username     string
	FullName     string
	NIP          string
	Phone        string
	Email        string
	RoleID       int64
	DepartmentID int64
	Competencies []int64
}

// Role adalah pilihan role.
type Role struct {
	ID   int64
	Code string
	Name string
}

// Option adalah pilihan select.
type Option struct {
	ID    int64
	Label string
}

// Filter untuk daftar pengguna.
type Filter struct {
	Search          string
	RoleID          int64
	IncludeInactive bool
	Limit, Offset   int
}
