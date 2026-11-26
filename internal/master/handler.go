package master

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"sikat/internal/clock"
	"sikat/internal/web"
)

// Handler menyediakan halaman CRUD master data.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
}

// Routes mendaftarkan rute di bawah /app/master.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.index)
	r.Route("/{slug}", func(r chi.Router) {
		r.Get("/", h.list)
		r.Get("/new", h.newForm)
		r.Post("/", h.create)
		r.Get("/{id}/edit", h.editForm)
		r.Post("/{id}", h.update)
		r.Post("/{id}/active", h.toggleActive)
		r.Post("/{id}/delete", h.delete)
	})
}

type indexData struct {
	Entities []*Entity
}

type listData struct {
	Entity    *Entity
	Records   []Record
	RefLabels map[string]map[string]string
	Pager     web.Pager
	Search    string
	Inactive  bool
	Today     time.Time // tanggal hari ini (WITA) untuk sisa hari field Expiry
}

// Badge adalah penanda status di samping nilai sel.
type Badge struct {
	Text, Class string
}

// Badge mengembalikan sisa hari field Expiry (nil bila bukan field Expiry).
// Kosong = tanpa batas; ≤ 30 hari = kuning; hari ini/lewat = merah.
func (d listData) Badge(rec Record, f Field) *Badge {
	if !f.Expiry {
		return nil
	}
	v := rec.Get(f.Column)
	if v == "" {
		return &Badge{"Tanpa batas", "badge-muted"}
	}
	exp, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil
	}
	days := DaysUntil(d.Today, exp)
	switch {
	case !rec.IsActive:
		return nil
	case days < 0:
		return &Badge{fmt.Sprintf("Kedaluwarsa %d hari lalu", -days), "badge-alert"}
	case days == 0:
		return &Badge{"Berakhir hari ini", "badge-alert"}
	case days <= 30:
		return &Badge{fmt.Sprintf("%d hari lagi", days), "badge-warn"}
	}
	return &Badge{fmt.Sprintf("%d hari lagi", days), "badge-selesai"}
}

// DaysUntil menghitung selisih hari kalender dari today ke date (negatif = lewat).
func DaysUntil(today, date time.Time) int {
	a := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// Cell mengembalikan teks tampilan sel daftar.
func (d listData) Cell(rec Record, f Field) string {
	v := rec.Get(f.Column)
	switch f.Kind {
	case KindBool:
		if v == "1" {
			return "Ya"
		}
		return "Tidak"
	case KindSelect:
		if v == "" {
			return "–"
		}
		if l, ok := d.RefLabels[f.Column][v]; ok {
			return l
		}
		return v
	case KindEnum:
		return f.ChoiceLabel(v)
	case KindQty:
		return web.FormatQty(v)
	case KindDate:
		return web.FormatDate(v)
	}
	if v == "" {
		return "–"
	}
	return v
}

type formData struct {
	Entity  *Entity
	ID      int64
	Form    map[string]string
	Options map[string][]Choice
	Record  *Record
}

func (h *Handler) entity(w http.ResponseWriter, r *http.Request) (*Entity, bool) {
	e, ok := EntityBySlug(chi.URLParam(r, "slug"))
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	}
	return e, ok
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.Renderer.Render(w, r, http.StatusOK, "master/index", web.Page{
		Title: "Master Data", Nav: "master", Data: indexData{Entities: Entities},
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	e, ok := h.entity(w, r)
	if !ok {
		return
	}
	pager := web.NewPager(r, 25)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	inactive := r.URL.Query().Get("inactive") == "1"
	recs, total, err := h.Service.List(r.Context(), e, ListQuery{
		Search: search, IncludeInactive: inactive, Limit: pager.PerPage, Offset: pager.Offset(),
	})
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager.Total = total
	labels, err := h.Service.RefLabels(r.Context(), e)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "master/list", web.Page{
		Title: e.Title, Nav: "master",
		Data: listData{Entity: e, Records: recs, RefLabels: labels, Pager: pager, Search: search, Inactive: inactive,
			Today: h.Service.clock.Now().In(clock.Location)},
	})
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, status int, e *Entity, id int64, form map[string]string, rec *Record, errs map[string]string) {
	opts, err := h.Service.Options(r.Context(), e, form)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	title := "Tambah " + e.Singular
	if id != 0 {
		title = "Ubah " + e.Singular
	}
	h.Renderer.Render(w, r, status, "master/form", web.Page{
		Title: title, Nav: "master", Errors: errs,
		Data: formData{Entity: e, ID: id, Form: form, Options: opts, Record: rec},
	})
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	e, ok := h.entity(w, r)
	if !ok {
		return
	}
	form := map[string]string{}
	for _, f := range e.Fields {
		form[f.Column] = f.Default
	}
	h.renderForm(w, r, http.StatusOK, e, 0, form, nil, nil)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	e, ok := h.entity(w, r)
	if !ok {
		return
	}
	id, v, err := h.Service.Create(r.Context(), e, r.PostFormValue)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if id == 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, e, 0, v.Form, nil, v.Errors)
		return
	}
	h.Renderer.Redirect(w, r, "/app/master/"+e.Slug, "success", upperFirst(e.Singular)+" berhasil ditambahkan.")
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	e, ok := h.entity(w, r)
	if !ok {
		return
	}
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	rec, err := h.Service.Get(r.Context(), e, id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	form := map[string]string{}
	for k, v := range rec.Values {
		form[k] = v
	}
	for _, f := range e.Fields {
		if f.Kind == KindQty {
			form[f.Column] = web.FormatQty(form[f.Column]) // "12.50" → "12,5"
		}
	}
	h.renderForm(w, r, http.StatusOK, e, id, form, &rec, nil)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	e, ok := h.entity(w, r)
	if !ok {
		return
	}
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	v, err := h.Service.Update(r.Context(), e, id, r.PostFormValue)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if !v.OK() {
		rec, _ := h.Service.Get(r.Context(), e, id)
		h.renderForm(w, r, http.StatusUnprocessableEntity, e, id, v.Form, &rec, v.Errors)
		return
	}
	h.Renderer.Redirect(w, r, "/app/master/"+e.Slug, "success", "Perubahan "+e.Singular+" berhasil disimpan.")
}

func (h *Handler) toggleActive(w http.ResponseWriter, r *http.Request) {
	e, ok := h.entity(w, r)
	if !ok {
		return
	}
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	active := r.PostFormValue("active") == "1"
	err := h.Service.SetActive(r.Context(), e, id, active)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	msg := upperFirst(e.Singular) + " dinonaktifkan."
	if active {
		msg = upperFirst(e.Singular) + " diaktifkan kembali."
	}
	h.Renderer.Redirect(w, r, backTo(r, "/app/master/"+e.Slug), "success", msg)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	e, ok := h.entity(w, r)
	if !ok {
		return
	}
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	err := h.Service.Delete(r.Context(), e, id)
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, ErrInUse):
		h.Renderer.Redirect(w, r, "/app/master/"+e.Slug, "error", err.Error())
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	default:
		h.Renderer.Redirect(w, r, "/app/master/"+e.Slug, "success", upperFirst(e.Singular)+" berhasil dihapus.")
	}
}

// backTo mengembalikan ke halaman daftar dengan filter yang sama, hanya untuk path lokal.
func backTo(r *http.Request, fallback string) string {
	ret := r.PostFormValue("return")
	if strings.HasPrefix(ret, fallback) && !strings.Contains(ret, "//") {
		return ret
	}
	return fallback
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
