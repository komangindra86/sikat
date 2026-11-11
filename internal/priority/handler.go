package priority

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"sikat/internal/web"
)

// Handler menampilkan ringkasan aturan dan simulator "Uji aturan".
type Handler struct {
	DB       *sql.DB
	Renderer *web.Renderer
}

type option struct {
	ID       int64
	Label    string
	Extra    string
	Critical bool
}

// RuleView adalah aturan beserta uraian kondisinya.
type RuleView struct {
	Rule
	Conditions []string
	Matched    bool
}

type testData struct {
	Rules       []RuleView
	Categories  []option
	Departments []option
	Form        map[string]string
	Tested      bool
	Result      Result
	Input       Input
}

var impactLabels = map[int64]string{0: "Tidak mengganggu", 1: "Sebagian terganggu", 2: "Pelayanan berhenti"}
var scopeLabels = map[int64]string{1: "Satu orang", 2: "Beberapa orang", 3: "Satu unit", 4: "Lebih dari satu unit"}
var replLabels = map[string]string{ReplacementYes: "ada", ReplacementNo: "tidak ada", ReplacementUnknown: "tidak tahu"}

// Describe menguraikan kondisi aturan dalam bahasa sehari-hari.
func Describe(r Rule, cats, depts, itcats map[int64]string) []string {
	var out []string
	if r.CriticalService.Valid {
		if r.CriticalService.Bool {
			out = append(out, "lokasi layanan klinis kritis")
		} else {
			out = append(out, "lokasi bukan layanan klinis kritis")
		}
	}
	rng := func(label string, min, max sql.NullInt64, names map[int64]string) {
		switch {
		case min.Valid && max.Valid && min.Int64 == max.Int64:
			out = append(out, label+" = "+names[min.Int64])
		case min.Valid && max.Valid:
			out = append(out, label+" "+names[min.Int64]+" s.d. "+names[max.Int64])
		case min.Valid:
			out = append(out, label+" minimal "+names[min.Int64])
		case max.Valid:
			out = append(out, label+" maksimal "+names[max.Int64])
		}
	}
	rng("dampak", r.MinImpact, r.MaxImpact, impactLabels)
	rng("terdampak", r.MinScope, r.MaxScope, scopeLabels)
	if r.ReplacementIn.Valid && r.ReplacementIn.String != "" {
		var parts []string
		for _, v := range strings.Split(r.ReplacementIn.String, ",") {
			parts = append(parts, replLabels[v])
		}
		out = append(out, "perangkat pengganti "+strings.Join(parts, " atau "))
	}
	if r.TicketCategoryID.Valid {
		out = append(out, "jenis gangguan "+cats[r.TicketCategoryID.Int64])
	}
	if r.ITCategoryID.Valid {
		out = append(out, "bidang "+itcats[r.ITCategoryID.Int64])
	}
	if r.DepartmentID.Valid {
		out = append(out, "unit "+depts[r.DepartmentID.Int64])
	}
	if len(out) == 0 {
		out = append(out, "semua tiket")
	}
	return out
}

func names(ctx context.Context, db *sql.DB, q string) (map[int64]string, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		m[id] = n
	}
	return m, rows.Err()
}

// Test menampilkan aturan aktif dan, bila form diisi, hasil simulasi.
func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rules, err := LoadActive(ctx, h.DB)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	cats, err := names(ctx, h.DB, `SELECT id, name FROM ticket_categories`)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	depts, err := names(ctx, h.DB, `SELECT id, name FROM departments`)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	itcats, err := names(ctx, h.DB, `SELECT id, name FROM it_categories`)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}

	d := testData{Form: map[string]string{}}
	catRows, err := h.DB.QueryContext(ctx, `SELECT id, name, it_category_id, default_priority FROM ticket_categories WHERE is_active = TRUE ORDER BY sort_order, name`)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	catIT := map[int64]int64{}
	catDefault := map[int64]string{}
	for catRows.Next() {
		var o option
		var it int64
		if err := catRows.Scan(&o.ID, &o.Label, &it, &o.Extra); err != nil {
			catRows.Close()
			h.Renderer.ServerError(w, r, err)
			return
		}
		catIT[o.ID], catDefault[o.ID] = it, o.Extra
		d.Categories = append(d.Categories, o)
	}
	catRows.Close()
	deptRows, err := h.DB.QueryContext(ctx, `SELECT id, name, is_critical_service FROM departments WHERE is_active = TRUE ORDER BY name`)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	deptCritical := map[int64]bool{}
	for deptRows.Next() {
		var o option
		if err := deptRows.Scan(&o.ID, &o.Label, &o.Critical); err != nil {
			deptRows.Close()
			h.Renderer.ServerError(w, r, err)
			return
		}
		deptCritical[o.ID] = o.Critical
		d.Departments = append(d.Departments, o)
	}
	deptRows.Close()

	q := r.URL.Query()
	for _, k := range []string{"ticket_category_id", "department_id", "impact", "scope", "replacement", "critical"} {
		d.Form[k] = q.Get(k)
	}
	if q.Get("uji") == "1" {
		catID, _ := strconv.ParseInt(q.Get("ticket_category_id"), 10, 64)
		deptID, _ := strconv.ParseInt(q.Get("department_id"), 10, 64)
		impact, _ := strconv.Atoi(q.Get("impact"))
		scope, _ := strconv.Atoi(q.Get("scope"))
		repl := q.Get("replacement")
		if repl != ReplacementYes && repl != ReplacementNo {
			repl = ReplacementUnknown
		}
		in := Input{
			TicketCategoryID: catID, ITCategoryID: catIT[catID], DepartmentID: deptID,
			Impact: clamp(impact, 0, 2), Scope: clamp(scope, 1, 4), Replacement: repl,
			CriticalService: deptCritical[deptID] || q.Get("critical") == "1",
			DefaultPriority: catDefault[catID],
		}
		d.Tested, d.Input, d.Result = true, in, Evaluate(rules, in)
	}
	for _, rule := range rules {
		v := RuleView{Rule: rule, Conditions: Describe(rule, cats, depts, itcats)}
		v.Matched = d.Tested && d.Result.Rule != nil && d.Result.Rule.ID == rule.ID
		d.Rules = append(d.Rules, v)
	}
	h.Renderer.Render(w, r, http.StatusOK, "priority/test", web.Page{Title: "Uji Aturan Prioritas", Nav: "master", Data: d})
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
