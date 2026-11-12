package activities

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"sikat/internal/attachments"
	"sikat/internal/clock"
	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/settings"
	"sikat/internal/web"
)

// Handler menyediakan halaman kegiatan internal.
type Handler struct {
	Service  *Service
	Settings *settings.Service
	Renderer *web.Renderer
}

// Routes mendaftarkan rute di bawah /app/activities.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/new", h.newForm)
	r.Post("/", h.create)
	r.Get("/attachments/{aid}", h.attachment)
	r.Get("/{id}", h.detail)
	r.Get("/{id}/edit", h.editForm)
	r.Post("/{id}", h.update)
	r.Post("/{id}/photos", h.photos)
}

type listData struct {
	Activities []Activity
	Pager      web.Pager
	Filter     Filter
	Month      string
	Types      []Option
	Categories []Option
	Staff      []Option
	All        bool
}

type formData struct {
	ID          int64
	No          string
	Form        map[string]string
	Supports    map[int64]bool
	Types       []Option
	Categories  []Option
	Departments []Option
	Rooms       []Option
	Assets      []Option
	Staff       []Option
	CanSetLead  bool
}

type detailData struct {
	A           Activity
	Attachments []Attachment
	CanEdit     bool
	MaxFiles    int
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := reqctx.UserFrom(ctx)
	q := r.URL.Query()
	pager := web.NewPager(r, 30)
	month := q.Get("bulan") // YYYY-MM (WITA); default bulan berjalan
	now := time.Now().In(clock.Location)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.Location)
	if t, err := time.ParseInLocation("2006-01", month, clock.Location); err == nil {
		start = t
	}
	month = start.Format("2006-01")
	f := Filter{
		From: start, To: start.AddDate(0, 1, -1), TypeID: atoi(q.Get("type_id")), ITCategoryID: atoi(q.Get("it_category_id")),
		Status: q.Get("status"), Search: q.Get("q"), Limit: pager.PerPage, Offset: pager.Offset(),
	}
	all := u.Has(rbac.PermActivityAll)
	if all {
		f.MemberID = atoi(q.Get("user_id"))
	} else {
		f.MemberID = u.ID // petugas melihat kegiatannya sendiri
	}
	list, total, err := h.Service.List(ctx, f)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager.Total = total
	types, err := h.Service.Types(ctx, 0)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	cats, err := h.Service.ITCategories(ctx, 0)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	var staff []Option
	if all {
		if staff, err = h.Service.Staff(ctx); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	h.Renderer.Render(w, r, http.StatusOK, "activities/list", web.Page{
		Title: "Kegiatan Internal", Nav: "activities",
		Data: listData{Activities: list, Pager: pager, Filter: f, Month: month, Types: types, Categories: cats, Staff: staff, All: all},
	})
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, status int, d formData, errs map[string]string) {
	ctx := r.Context()
	var err error
	if d.Types, err = h.Service.Types(ctx, atoi(d.Form["activity_type_id"])); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if d.Categories, err = h.Service.ITCategories(ctx, atoi(d.Form["it_category_id"])); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	dept, room := atoi(d.Form["department_id"]), atoi(d.Form["room_id"])
	if d.Departments, err = h.Service.Departments(ctx, dept); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if d.Rooms, err = h.Service.Rooms(ctx, dept, room); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if d.Assets, err = h.Service.Assets(ctx, dept, room, atoi(d.Form["asset_id"])); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if d.Staff, err = h.Service.Staff(ctx); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	d.CanSetLead = reqctx.UserFrom(ctx).Has(rbac.PermActivityAll)
	title := "Catat kegiatan"
	if d.ID != 0 {
		title = "Ubah kegiatan " + d.No
	}
	h.Renderer.Render(w, r, status, "activities/form", web.Page{Title: title, Nav: "activities", Errors: errs, Data: d})
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	form := map[string]string{
		"status": StatusDone, "activity_date": time.Now().In(clock.Location).Format("2006-01-02"),
		"lead_id": strconv.FormatInt(u.ID, 10),
	}
	h.renderForm(w, r, http.StatusOK, formData{Form: form, Supports: map[int64]bool{}}, nil)
}

func supportSet(vals []string) map[int64]bool {
	m := map[int64]bool{}
	for _, v := range vals {
		if id := atoi(v); id > 0 {
			m[id] = true
		}
	}
	return m
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	u := reqctx.UserFrom(r.Context())
	id, form, errs, err := h.Service.Create(r.Context(), u, r.PostFormValue, func(k string) []string { return r.PostForm[k] })
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if id == 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, formData{Form: form, Supports: supportSet(r.PostForm["support_ids"])}, errs)
		return
	}
	h.Renderer.Redirect(w, r, "/app/activities/"+strconv.FormatInt(id, 10), "success", "Kegiatan berhasil dicatat.")
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) (Activity, bool) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return Activity{}, false
	}
	a, err := h.Service.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return Activity{}, false
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return Activity{}, false
	}
	if !CanView(a, reqctx.UserFrom(r.Context())) {
		h.Renderer.Error(w, r, http.StatusForbidden, "")
		return Activity{}, false
	}
	return a, true
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	a, ok := h.load(w, r)
	if !ok {
		return
	}
	atts, err := h.Service.Attachments(r.Context(), a.ID)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "activities/detail", web.Page{
		Title: a.No, Nav: "activities",
		Data: detailData{A: a, Attachments: atts, CanEdit: CanEdit(a, reqctx.UserFrom(r.Context())),
			MaxFiles: h.Settings.Int(r.Context(), settings.KeyUploadMaxFiles)},
	})
}

func wita(t time.Time) string { return t.In(clock.Location).Format("2006-01-02T15:04") }

func activityForm(a Activity) map[string]string {
	f := map[string]string{
		"activity_date": a.Date.Format("2006-01-02"), "activity_type_id": strconv.FormatInt(a.TypeID, 10),
		"it_category_id": strconv.FormatInt(a.ITCategoryID, 10), "title": a.Title, "description": a.Description.String,
		"result": a.Result.String, "status": a.Status, "related_ticket_no": a.RelatedTicketNo.String,
	}
	if a.DepartmentID.Valid {
		f["department_id"] = strconv.FormatInt(a.DepartmentID.Int64, 10)
	}
	if a.RoomID.Valid {
		f["room_id"] = strconv.FormatInt(a.RoomID.Int64, 10)
	}
	if a.AssetID.Valid {
		f["asset_id"] = strconv.FormatInt(a.AssetID.Int64, 10)
	}
	if a.MaintenanceType.Valid {
		f["maintenance_type"] = a.MaintenanceType.String
	}
	if a.StartedAt.Valid {
		f["started_at"] = wita(a.StartedAt.Time)
	}
	if a.EndedAt.Valid {
		f["ended_at"] = wita(a.EndedAt.Time)
	}
	if a.LeadID.Valid {
		f["lead_id"] = strconv.FormatInt(a.LeadID.Int64, 10)
	}
	return f
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	a, ok := h.load(w, r)
	if !ok {
		return
	}
	if !CanEdit(a, reqctx.UserFrom(r.Context())) {
		h.Renderer.Error(w, r, http.StatusForbidden, ErrForbidden.Error())
		return
	}
	sup := map[int64]bool{}
	for _, m := range a.Members {
		if m.Role == "SUPPORT" {
			sup[m.UserID] = true
		}
	}
	h.renderForm(w, r, http.StatusOK, formData{ID: a.ID, No: a.No, Form: activityForm(a), Supports: sup}, nil)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	a, ok := h.load(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	form, errs, err := h.Service.Update(r.Context(), reqctx.UserFrom(r.Context()), a.ID, r.PostFormValue,
		func(k string) []string { return r.PostForm[k] })
	switch {
	case errors.Is(err, ErrForbidden):
		h.Renderer.Error(w, r, http.StatusForbidden, err.Error())
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		h.renderForm(w, r, http.StatusUnprocessableEntity, formData{ID: a.ID, No: a.No, Form: form, Supports: supportSet(r.PostForm["support_ids"])}, errs)
	default:
		h.Renderer.Redirect(w, r, "/app/activities/"+strconv.FormatInt(a.ID, 10), "success", "Perubahan kegiatan disimpan.")
	}
}

func (h *Handler) photos(w http.ResponseWriter, r *http.Request) {
	a, ok := h.load(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	url := "/app/activities/" + strconv.FormatInt(a.ID, 10)
	maxFiles := h.Settings.Int(ctx, settings.KeyUploadMaxFiles)
	maxBytes := int64(h.Settings.Int(ctx, settings.KeyUploadMaxMB)) << 20
	var list []attachments.Processed
	if r.MultipartForm != nil {
		files := r.MultipartForm.File["photos"]
		if len(files) > maxFiles {
			h.Renderer.Redirect(w, r, url, "error", "Maksimal "+strconv.Itoa(maxFiles)+" foto sekali unggah.")
			return
		}
		for _, fh := range files {
			if fh.Size == 0 {
				continue
			}
			f, err := fh.Open()
			if err != nil {
				h.Renderer.ServerError(w, r, err)
				return
			}
			p, err := attachments.Process(f, maxBytes)
			f.Close()
			if err != nil {
				h.Renderer.Redirect(w, r, url, "error", "Foto ditolak: "+err.Error()+".")
				return
			}
			list = append(list, p)
		}
	}
	if len(list) == 0 {
		h.Renderer.Redirect(w, r, url, "error", "Pilih minimal satu foto.")
		return
	}
	if err := h.Service.AddPhotos(ctx, reqctx.UserFrom(ctx), a.ID, list); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Redirect(w, r, url, "success", "Foto ditambahkan.")
}

func (h *Handler) attachment(w http.ResponseWriter, r *http.Request) {
	aid, ok := web.IDParam(r, "aid")
	if !ok {
		http.NotFound(w, r)
		return
	}
	key, actID, err := h.Service.AttachmentKey(r.Context(), aid)
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	a, err := h.Service.Get(r.Context(), actID)
	if err != nil || !CanView(a, reqctx.UserFrom(r.Context())) {
		http.NotFound(w, r)
		return
	}
	f, err := h.Service.Store().Open(key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		h.Renderer.ServerError(w, r, err)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "foto.jpg", st.ModTime(), f)
}
