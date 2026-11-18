package reports

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"sikat/internal/audit"
	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyajikan daftar laporan, tampilan laporan, dan ekspor.
type Handler struct {
	Service  *Service
	DB       *sql.DB
	Renderer *web.Renderer
	// Hospital mengembalikan nama rumah sakit untuk kop ekspor.
	Hospital func(ctx context.Context) string
}

// Routes di bawah /app/reports (report.view_all atau report.view_own).
func (h *Handler) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			u := reqctx.UserFrom(req.Context())
			if !u.Has(rbac.PermReportAll) && !u.Has(rbac.PermReportOwn) {
				h.Renderer.Error(w, req, http.StatusForbidden, "")
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/", h.index)
	r.Get("/{slug}", h.view)
}

// Group adalah kelompok laporan di halaman daftar.
type Group struct {
	Name string
	Defs []Def
}

func groups() []Group {
	var out []Group
	idx := map[string]int{}
	for _, d := range Defs {
		i, ok := idx[d.Group]
		if !ok {
			i = len(out)
			idx[d.Group] = i
			out = append(out, Group{Name: d.Group})
		}
		out[i].Defs = append(out[i].Defs, d)
	}
	return out
}

type indexData struct {
	Groups  []Group
	OwnOnly bool
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	h.Renderer.Render(w, r, http.StatusOK, "reports/index", web.Page{Title: "Laporan", Nav: "reports",
		Data: indexData{Groups: groups(), OwnOnly: !u.Has(rbac.PermReportAll)}})
}

// Option untuk pilihan filter.
type Option struct {
	ID   int64
	Name string
}

type viewData struct {
	Def     Def
	Filter  Filter
	Table   Table
	Rows    [][]Cell // dibatasi HTMLMaxRows
	Chart   *ChartLayout
	OwnOnly bool
	Units   []Option
	Fields  []Option
	Staff   []Option
	Years   []int
	Query   string
}

func (h *Handler) options(ctx context.Context, q string, args ...any) ([]Option, error) {
	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Option
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (h *Handler) view(w http.ResponseWriter, r *http.Request) {
	d, ok := Find(chi.URLParam(r, "slug"))
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	ctx := r.Context()
	u := reqctx.UserFrom(ctx)
	f, errs := ParseFilter(r.URL.Query(), h.Service.Now())
	ownOnly := !u.Has(rbac.PermReportAll)
	if ownOnly {
		f.ScopeUserID = u.ID
		f.UserID = 0
	}
	// Filter yang tidak dipakai laporan diabaikan agar hasil tidak membingungkan.
	if !d.Uses(FUnit) {
		f.DepartmentID = 0
	}
	if !d.Uses(FBidang) {
		f.ITCategoryID = 0
		f.FieldIDs = nil
	}
	if !d.Uses(FPetugas) {
		f.UserID = 0
	}
	if !d.Uses(FPrioritas) {
		f.Priority = ""
	}
	if !d.Uses(FStatus) {
		f.Status = ""
	}

	t, err := h.Service.Run(ctx, d, f)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	t.Subtitle, err = h.subtitle(ctx, f, u, ownOnly)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}

	switch format := r.URL.Query().Get("format"); format {
	case "xlsx", "pdf":
		h.export(w, r, d, f, t, format)
		return
	}

	v := viewData{Def: d, Filter: f, Table: t, Rows: t.Rows, OwnOnly: ownOnly, Query: f.QueryString()}
	if len(v.Rows) > HTMLMaxRows {
		v.Rows = v.Rows[:HTMLMaxRows]
		v.Table.Truncated = true
	}
	if t.Chart != nil && len(t.Chart.Bars) > 0 {
		l := t.Chart.Layout()
		v.Chart = &l
	}
	if v.Units, err = h.options(ctx, `SELECT id, name FROM departments ORDER BY is_active DESC, name`); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if v.Fields, err = h.options(ctx, `SELECT id, name FROM it_categories ORDER BY sort_order, name`); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if v.Staff, err = h.options(ctx, `SELECT u.id, u.full_name FROM users u JOIN roles r ON r.id = u.role_id
		WHERE r.code IN ('TECHNICIAN','COORDINATOR') ORDER BY u.is_active DESC, u.full_name`); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	now := h.Service.Now()
	for y := now.Year(); y >= now.Year()-5; y-- {
		v.Years = append(v.Years, y)
	}
	status := http.StatusOK
	if len(errs) > 0 {
		status = http.StatusUnprocessableEntity
	}
	h.Renderer.Render(w, r, status, "reports/view", web.Page{Title: d.Title, Nav: "reports", Data: v, Errors: errs})
}

// subtitle: periode + filter aktif dalam kata-kata (untuk layar & kop ekspor).
func (h *Handler) subtitle(ctx context.Context, f Filter, u *rbac.User, ownOnly bool) (string, error) {
	parts := []string{"Periode " + f.PeriodLabel()}
	name := func(q string, id int64) (string, error) {
		var s string
		err := h.DB.QueryRowContext(ctx, q, id).Scan(&s)
		if err == sql.ErrNoRows {
			return "#" + strconv.FormatInt(id, 10), nil
		}
		return s, err
	}
	if f.DepartmentID > 0 {
		n, err := name(`SELECT name FROM departments WHERE id = ?`, f.DepartmentID)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Unit: "+n)
	}
	if f.ITCategoryID > 0 {
		n, err := name(`SELECT name FROM it_categories WHERE id = ?`, f.ITCategoryID)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Bidang: "+n)
	}
	if len(f.FieldIDs) > 0 {
		in, args := inList(f.FieldIDs)
		rows, err := h.DB.QueryContext(ctx, `SELECT name FROM it_categories WHERE id IN `+in+` ORDER BY sort_order, name`, args...)
		if err != nil {
			return "", err
		}
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return "", err
			}
			names = append(names, n)
		}
		rows.Close()
		parts = append(parts, "Bidang: "+strings.Join(names, ", "))
	}
	if f.UserID > 0 {
		n, err := name(`SELECT full_name FROM users WHERE id = ?`, f.UserID)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Petugas: "+n)
	}
	if ownOnly {
		parts = append(parts, "Data pribadi: "+u.FullName)
	}
	if f.Priority != "" {
		parts = append(parts, "Prioritas: "+priorityLabel(f.Priority))
	}
	if f.Status == "ACTIVE" {
		parts = append(parts, "Status: Masih aktif")
	} else if f.Status != "" {
		parts = append(parts, "Status: "+web.Label("ticket_status", f.Status))
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " · " + p
	}
	return out, nil
}

func (h *Handler) export(w http.ResponseWriter, r *http.Request, d Def, f Filter, t Table, format string) {
	ctx := r.Context()
	u := reqctx.UserFrom(ctx)
	m := Meta{GeneratedAt: h.Service.Now(), GeneratedBy: u.FullName}
	if h.Hospital != nil {
		m.Hospital = h.Hospital(ctx)
	}
	var buf bytes.Buffer
	var err error
	ctype := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	if format == "pdf" {
		ctype = "application/pdf"
		err = WritePDF(&buf, t, m)
	} else {
		err = WriteXLSX(&buf, t, m)
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	_ = audit.Record(ctx, h.DB, h.Service.Now(), audit.Entry{Action: "report.export", EntityType: "reports",
		After: map[string]any{"report": d.Slug, "format": format, "filter": f.QueryString(), "rows": len(t.Rows)}})
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename="`+Filename(d.Slug, f, format)+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = buf.WriteTo(w)
}
