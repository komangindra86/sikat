// Package priority adalah mesin prioritas otomatis berbasis aturan.
//
// Aturan dievaluasi berurutan (sort_order); aturan PERTAMA yang cocok menentukan
// prioritas. Setiap kondisi yang kosong (NULL) berarti "apa saja". Bila tidak
// ada aturan yang cocok, dipakai prioritas bawaan jenis gangguan.
// Aturan disimpan di tabel priority_rules sehingga dapat diubah Admin IT tanpa
// mengubah kode.
package priority

import (
	"context"
	"database/sql"
	"strings"

	"sikat/internal/database"
)

// Nilai prioritas.
const (
	Low      = "LOW"
	Medium   = "MEDIUM"
	High     = "HIGH"
	Critical = "CRITICAL"
)

// Nilai ketersediaan perangkat pengganti.
const (
	ReplacementYes     = "YES"
	ReplacementNo      = "NO"
	ReplacementUnknown = "UNKNOWN"
)

// Input adalah fakta tiket yang dinilai.
type Input struct {
	TicketCategoryID int64
	ITCategoryID     int64
	DepartmentID     int64
	Impact           int    // 0 tidak mengganggu, 1 sebagian, 2 pelayanan berhenti
	Scope            int    // 1 satu orang, 2 beberapa, 3 satu unit, 4 lebih dari satu unit
	Replacement      string // YES | NO | UNKNOWN
	CriticalService  bool   // dihitung dari unit/ruangan, bukan dari pelapor
	DefaultPriority  string // prioritas bawaan jenis gangguan
}

// Rule adalah satu aturan prioritas.
type Rule struct {
	ID               int64
	Name             string
	SortOrder        int
	TicketCategoryID sql.NullInt64
	ITCategoryID     sql.NullInt64
	DepartmentID     sql.NullInt64
	MinImpact        sql.NullInt64
	MaxImpact        sql.NullInt64
	MinScope         sql.NullInt64
	MaxScope         sql.NullInt64
	ReplacementIn    sql.NullString // daftar dipisah koma, mis. "NO,UNKNOWN"
	CriticalService  sql.NullBool
	Result           string
}

// Matches memeriksa apakah aturan berlaku untuk input.
func (r Rule) Matches(in Input) bool {
	if r.TicketCategoryID.Valid && r.TicketCategoryID.Int64 != in.TicketCategoryID {
		return false
	}
	if r.ITCategoryID.Valid && r.ITCategoryID.Int64 != in.ITCategoryID {
		return false
	}
	if r.DepartmentID.Valid && r.DepartmentID.Int64 != in.DepartmentID {
		return false
	}
	if r.MinImpact.Valid && int64(in.Impact) < r.MinImpact.Int64 {
		return false
	}
	if r.MaxImpact.Valid && int64(in.Impact) > r.MaxImpact.Int64 {
		return false
	}
	if r.MinScope.Valid && int64(in.Scope) < r.MinScope.Int64 {
		return false
	}
	if r.MaxScope.Valid && int64(in.Scope) > r.MaxScope.Int64 {
		return false
	}
	if r.ReplacementIn.Valid && r.ReplacementIn.String != "" {
		ok := false
		for _, v := range strings.Split(r.ReplacementIn.String, ",") {
			if v == in.Replacement {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if r.CriticalService.Valid && r.CriticalService.Bool != in.CriticalService {
		return false
	}
	return true
}

// Result adalah hasil penilaian.
type Result struct {
	Priority string
	Rule     *Rule // nil bila memakai prioritas bawaan jenis gangguan
}

// Evaluate menerapkan aturan (sudah terurut) pada input.
func Evaluate(rules []Rule, in Input) Result {
	for i := range rules {
		if rules[i].Matches(in) {
			r := rules[i]
			return Result{Priority: r.Result, Rule: &r}
		}
	}
	p := in.DefaultPriority
	if p == "" {
		p = Medium
	}
	return Result{Priority: p}
}

// LoadActive memuat aturan aktif berurutan.
func LoadActive(ctx context.Context, db database.DBTX) ([]Rule, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, sort_order, ticket_category_id, it_category_id, department_id,
		       min_impact, max_impact, min_scope, max_scope, replacement_in, critical_service, result_priority
		  FROM priority_rules WHERE is_active = TRUE ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Name, &r.SortOrder, &r.TicketCategoryID, &r.ITCategoryID, &r.DepartmentID,
			&r.MinImpact, &r.MaxImpact, &r.MinScope, &r.MaxScope, &r.ReplacementIn, &r.CriticalService, &r.Result); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Rank mengurutkan prioritas (untuk perbandingan & pengurutan).
func Rank(p string) int {
	switch p {
	case Critical:
		return 4
	case High:
		return 3
	case Medium:
		return 2
	case Low:
		return 1
	}
	return 0
}

// ValidPriority memeriksa nilai prioritas.
func ValidPriority(p string) bool { return Rank(p) > 0 }
