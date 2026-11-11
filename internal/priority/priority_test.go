package priority

import (
	"database/sql"
	"testing"
)

func i64(v int64) sql.NullInt64   { return sql.NullInt64{Int64: v, Valid: true} }
func str(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }
func boolean(v bool) sql.NullBool { return sql.NullBool{Bool: v, Valid: true} }

// defaultRules sama dengan seed migrasi 00004_seed_phase2.sql.
func defaultRules() []Rule {
	return []Rule{
		{ID: 1, SortOrder: 10, MinImpact: i64(2), MinScope: i64(3), CriticalService: boolean(true), Result: Critical},
		{ID: 2, SortOrder: 20, MinImpact: i64(2), MinScope: i64(3), Result: High},
		{ID: 3, SortOrder: 30, MinImpact: i64(2), ReplacementIn: str("NO,UNKNOWN"), Result: High},
		{ID: 4, SortOrder: 40, MaxScope: i64(2), ReplacementIn: str("YES"), Result: Medium},
		{ID: 5, SortOrder: 50, MaxImpact: i64(0), Result: Low},
	}
}

// Contoh dari master prompt §9 serta kasus batas lainnya.
func TestEvaluateMasterPromptExamples(t *testing.T) {
	rules := defaultRules()
	cases := []struct {
		name     string
		in       Input
		want     string
		wantRule int64
	}{
		{"satu komputer rusak, ada pengganti → Sedang",
			Input{Impact: 2, Scope: 1, Replacement: ReplacementYes, DefaultPriority: Medium}, Medium, 4},
		{"seluruh komputer satu unit mati → Tinggi",
			Input{Impact: 2, Scope: 3, Replacement: ReplacementNo, DefaultPriority: Medium}, High, 2},
		{"jaringan mati di layanan klinis kritis → Kritis",
			Input{Impact: 2, Scope: 4, Replacement: ReplacementNo, CriticalService: true, DefaultPriority: High}, Critical, 1},
		{"unit kritis tapi hanya satu orang terdampak, ada pengganti → Sedang (bukan hanya karena kategori)",
			Input{Impact: 2, Scope: 1, Replacement: ReplacementYes, CriticalService: true, DefaultPriority: High}, Medium, 4},
		{"berhenti, satu orang, pengganti tidak diketahui → Tinggi",
			Input{Impact: 2, Scope: 1, Replacement: ReplacementUnknown, DefaultPriority: Low}, High, 3},
		{"tidak mengganggu → Rendah",
			Input{Impact: 0, Scope: 1, Replacement: ReplacementNo, DefaultPriority: High}, Low, 5},
		{"sebagian terganggu, beberapa orang, tanpa pengganti → bawaan kategori",
			Input{Impact: 1, Scope: 2, Replacement: ReplacementNo, DefaultPriority: High}, High, 0},
	}
	for _, c := range cases {
		res := Evaluate(rules, c.in)
		var got int64
		if res.Rule != nil {
			got = res.Rule.ID
		}
		if res.Priority != c.want || got != c.wantRule {
			t.Errorf("%s: dapat %s (aturan %d), ingin %s (aturan %d)", c.name, res.Priority, got, c.want, c.wantRule)
		}
	}
}

func TestRuleConditions(t *testing.T) {
	r := Rule{TicketCategoryID: i64(7), DepartmentID: i64(3), ITCategoryID: i64(2), Result: High}
	if !r.Matches(Input{TicketCategoryID: 7, DepartmentID: 3, ITCategoryID: 2}) {
		t.Fatal("semua kondisi cocok harus true")
	}
	for _, in := range []Input{
		{TicketCategoryID: 8, DepartmentID: 3, ITCategoryID: 2},
		{TicketCategoryID: 7, DepartmentID: 4, ITCategoryID: 2},
		{TicketCategoryID: 7, DepartmentID: 3, ITCategoryID: 1},
	} {
		if r.Matches(in) {
			t.Errorf("kondisi berbeda harus false: %+v", in)
		}
	}
	if !(Rule{}).Matches(Input{}) {
		t.Fatal("aturan tanpa kondisi cocok untuk semua")
	}
	if Evaluate(nil, Input{}).Priority != Medium {
		t.Fatal("tanpa aturan dan tanpa bawaan → Sedang")
	}
}

func TestRank(t *testing.T) {
	if !(Rank(Critical) > Rank(High) && Rank(High) > Rank(Medium) && Rank(Medium) > Rank(Low)) || ValidPriority("X") {
		t.Fatal("urutan prioritas salah")
	}
}
