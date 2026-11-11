package assets

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"sikat/internal/audit"
	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyediakan halaman perangkat.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
	// RequireManage membungkus rute yang mengubah data (permission master.manage).
	RequireManage func(http.Handler) http.Handler
	// QRFor mengembalikan QR aktif perangkat (opsional, dirakit di server).
	QRFor func(ctx context.Context, assetID int64) (qrID int64, ok bool, err error)
	// TicketsFor mengembalikan tiket terbaru perangkat (opsional).
	TicketsFor func(ctx context.Context, assetID int64) ([]RelatedTicket, error)
}

// RelatedTicket adalah ringkasan tiket untuk halaman perangkat.
type RelatedTicket struct {
	ID        int64
	Number    string
	CreatedAt time.Time
	Category  string
	Status    string
	Priority  string
}

// Routes mendaftarkan rute di bawah /app/assets.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/rooms", h.roomOptions)
	r.Group(func(r chi.Router) {
		r.Use(h.RequireManage)
		r.Get("/new", h.newForm)
		r.Post("/", h.create)
		r.Get("/{id}/edit", h.editForm)
		r.Post("/{id}", h.update)
	})
	r.Get("/{id}", h.detail)
}

type listData struct {
	Assets      []Asset
	Pager       web.Pager
	Filter      Filter
	Departments []Option
	Categories  []Option
	CanManage   bool
}

type formData struct {
	ID          int64
	Form        map[string]string
	Categories  []Option
	Departments []Option
	Rooms       []RoomOption
	Source      string
}

type detailData struct {
	Asset     Asset
	History   []audit.Log
	CanManage bool
	QRID      int64
	HasQR     bool
	Tickets   []RelatedTicket
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pager := web.NewPager(r, 25)
	f := Filter{
		Search:       strings.TrimSpace(q.Get("q")),
		DepartmentID: atoi64(q.Get("department_id")),
		CategoryID:   atoi64(q.Get("category_id")),
		Status:       q.Get("status"),
		Limit:        pager.PerPage,
		Offset:       pager.Offset(),
	}
	if f.Status != "" && f.Status != "ALL" && !validStatus[f.Status] {
		f.Status = ""
	}
	list, total, err := h.Service.List(r.Context(), f)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager.Total = total
	depts, err := h.Service.Departments(r.Context(), 0)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	cats, err := h.Service.Categories(r.Context(), 0)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "assets/list", web.Page{
		Title: "Perangkat", Nav: "assets",
		Data: listData{Assets: list, Pager: pager, Filter: f, Departments: depts, Categories: cats,
			CanManage: reqctx.UserFrom(r.Context()).Has(rbac.PermMasterManage)},
	})
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, status int, id int64, form map[string]string, source string, errs map[string]string) {
	ctx := r.Context()
	cats, err := h.Service.Categories(ctx, atoi64(form["asset_category_id"]))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	depts, err := h.Service.Departments(ctx, atoi64(form["department_id"]))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	rooms, err := h.Service.Rooms(ctx, atoi64(form["department_id"]), atoi64(form["room_id"]))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	title := "Tambah perangkat"
	if id != 0 {
		title = "Ubah perangkat"
	}
	h.Renderer.Render(w, r, status, "assets/form", web.Page{
		Title: title, Nav: "assets", Errors: errs,
		Data: formData{ID: id, Form: form, Categories: cats, Departments: depts, Rooms: rooms, Source: source},
	})
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, http.StatusOK, 0, map[string]string{"status": StatusActive}, "LOCAL", nil)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	id, form, errs, err := h.Service.Create(r.Context(), r.PostFormValue)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if id == 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, 0, form, "LOCAL", errs)
		return
	}
	h.Renderer.Redirect(w, r, "/app/assets/"+strconv.FormatInt(id, 10), "success", "Perangkat berhasil ditambahkan.")
}

func assetForm(a Asset) map[string]string {
	f := map[string]string{
		"asset_code": a.AssetCode, "name": a.Name, "asset_category_id": strconv.FormatInt(a.CategoryID, 10),
		"brand": a.Brand.String, "model": a.Model.String, "serial_number": a.SerialNumber.String,
		"status": a.Status, "notes": a.Notes.String,
	}
	if a.DepartmentID.Valid {
		f["department_id"] = strconv.FormatInt(a.DepartmentID.Int64, 10)
	}
	if a.RoomID.Valid {
		f["room_id"] = strconv.FormatInt(a.RoomID.Int64, 10)
	}
	if a.ProcuredAt.Valid {
		f["procured_at"] = a.ProcuredAt.Time.Format("2006-01-02")
	}
	return f
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) (Asset, bool) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return Asset{}, false
	}
	a, err := h.Service.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return Asset{}, false
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return Asset{}, false
	}
	return a, true
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	a, ok := h.load(w, r)
	if !ok {
		return
	}
	hist, err := h.Service.History(r.Context(), a.ID)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	d := detailData{Asset: a, History: hist, CanManage: reqctx.UserFrom(r.Context()).Has(rbac.PermMasterManage)}
	if h.QRFor != nil {
		if d.QRID, d.HasQR, err = h.QRFor(r.Context(), a.ID); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	if h.TicketsFor != nil {
		if d.Tickets, err = h.TicketsFor(r.Context(), a.ID); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	h.Renderer.Render(w, r, http.StatusOK, "assets/detail", web.Page{Title: a.Name, Nav: "assets", Data: d})
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	a, ok := h.load(w, r)
	if !ok {
		return
	}
	h.renderForm(w, r, http.StatusOK, a.ID, assetForm(a), a.Source, nil)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	a, ok := h.load(w, r)
	if !ok {
		return
	}
	form, errs, err := h.Service.Update(r.Context(), a.ID, r.PostFormValue)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(errs) > 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, a.ID, form, a.Source, errs)
		return
	}
	h.Renderer.Redirect(w, r, "/app/assets/"+strconv.FormatInt(a.ID, 10), "success", "Perubahan perangkat berhasil disimpan.")
}

// roomOptions mengembalikan <option> ruangan untuk unit terpilih (HTMX).
func (h *Handler) roomOptions(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.Service.Rooms(r.Context(), atoi64(r.URL.Query().Get("department_id")), 0)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "assets/form", "room-options", web.Page{
		Data: formData{Rooms: rooms, Form: map[string]string{}},
	})
}
