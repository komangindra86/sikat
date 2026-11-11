package users

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyediakan halaman pengelolaan akun.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
}

// Routes mendaftarkan rute di bawah /app/users.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/new", h.newForm)
	r.Post("/", h.create)
	r.Get("/{id}/edit", h.editForm)
	r.Post("/{id}", h.update)
	r.Post("/{id}/active", h.toggleActive)
	r.Post("/{id}/reset-password", h.resetPassword)
}

type listData struct {
	Users    []User
	Roles    []Role
	Pager    web.Pager
	Search   string
	RoleID   int64
	Inactive bool
	SelfID   int64
}

type formData struct {
	ID           int64
	User         *User
	Form         map[string]string
	Selected     map[int64]bool
	Roles        []Role
	Departments  []Option
	Categories   []Option
	TempPassword string
	SelfID       int64
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pager := web.NewPager(r, 25)
	roleID, _ := strconv.ParseInt(q.Get("role_id"), 10, 64)
	f := Filter{
		Search: strings.TrimSpace(q.Get("q")), RoleID: roleID, IncludeInactive: q.Get("inactive") == "1",
		Limit: pager.PerPage, Offset: pager.Offset(),
	}
	list, total, err := h.Service.List(r.Context(), f)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager.Total = total
	roles, err := h.Service.Roles(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "users/list", web.Page{
		Title: "Pengguna", Nav: "users",
		Data: listData{Users: list, Roles: roles, Pager: pager, Search: f.Search, RoleID: roleID,
			Inactive: f.IncludeInactive, SelfID: reqctx.UserFrom(r.Context()).ID},
	})
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, status int, d formData, errs map[string]string) {
	ctx := r.Context()
	roles, err := h.Service.Roles(ctx)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	dept, _ := strconv.ParseInt(d.Form["department_id"], 10, 64)
	depts, err := h.Service.Departments(ctx, dept)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	var sel []int64
	for id := range d.Selected {
		sel = append(sel, id)
	}
	cats, err := h.Service.ITCategories(ctx, sel)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	d.Roles, d.Departments, d.Categories = roles, depts, cats
	d.SelfID = reqctx.UserFrom(ctx).ID
	title := "Tambah pengguna"
	if d.ID != 0 {
		title = "Ubah pengguna"
	}
	h.Renderer.Render(w, r, status, "users/form", web.Page{Title: title, Nav: "users", Errors: errs, Data: d})
}

func selected(vals []string) map[int64]bool {
	m := map[int64]bool{}
	for _, v := range vals {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			m[n] = true
		}
	}
	return m
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, http.StatusOK, formData{Form: map[string]string{}, Selected: map[int64]bool{}}, nil)
}

func getAll(r *http.Request) func(string) []string {
	return func(k string) []string { return r.PostForm[k] }
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	created, form, errs, err := h.Service.Create(r.Context(), r.PostFormValue, getAll(r))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if created.ID == 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity,
			formData{Form: form, Selected: selected(r.PostForm["competencies"])}, errs)
		return
	}
	h.showTempPassword(w, r, created.ID, created.TempPassword, "Akun berhasil dibuat.")
}

// showTempPassword menampilkan password sementara satu kali (tidak disimpan dalam bentuk asli).
func (h *Handler) showTempPassword(w http.ResponseWriter, r *http.Request, id int64, temp, msg string) {
	u, err := h.Service.Get(r.Context(), id)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.Renderer.Render(w, r, http.StatusOK, "users/password", web.Page{
		Title: "Password sementara", Nav: "users",
		Flash: &web.Flash{Kind: "success", Message: msg},
		Data:  formData{ID: id, User: &u, TempPassword: temp},
	})
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) (User, bool) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return User{}, false
	}
	u, err := h.Service.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return User{}, false
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return User{}, false
	}
	return u, true
}

func userForm(u User) map[string]string {
	f := map[string]string{
		"username": u.Username, "full_name": u.FullName, "nip": u.NIP.String, "phone": u.Phone.String,
		"email": u.Email.String, "role_id": strconv.FormatInt(u.RoleID, 10),
	}
	if u.DepartmentID.Valid {
		f["department_id"] = strconv.FormatInt(u.DepartmentID.Int64, 10)
	}
	return f
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	u, ok := h.load(w, r)
	if !ok {
		return
	}
	sel := map[int64]bool{}
	for _, c := range u.Competencies {
		sel[c] = true
	}
	h.renderForm(w, r, http.StatusOK, formData{ID: u.ID, User: &u, Form: userForm(u), Selected: sel}, nil)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	u, ok := h.load(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	actor := reqctx.UserFrom(r.Context())
	form, errs, err := h.Service.Update(r.Context(), actor.ID, u.ID, r.PostFormValue, getAll(r))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(errs) > 0 {
		form["username"] = u.Username
		h.renderForm(w, r, http.StatusUnprocessableEntity,
			formData{ID: u.ID, User: &u, Form: form, Selected: selected(r.PostForm["competencies"])}, errs)
		return
	}
	h.Renderer.Redirect(w, r, "/app/users", "success", "Perubahan akun "+u.Username+" berhasil disimpan.")
}

func (h *Handler) toggleActive(w http.ResponseWriter, r *http.Request) {
	u, ok := h.load(w, r)
	if !ok {
		return
	}
	active := r.PostFormValue("active") == "1"
	err := h.Service.SetActive(r.Context(), reqctx.UserFrom(r.Context()).ID, u.ID, active)
	switch {
	case errors.Is(err, ErrSelfDeactivate), errors.Is(err, ErrLastAdmin):
		h.Renderer.Redirect(w, r, "/app/users", "error", err.Error())
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case active:
		h.Renderer.Redirect(w, r, "/app/users", "success", "Akun "+u.Username+" diaktifkan kembali.")
	default:
		h.Renderer.Redirect(w, r, "/app/users", "success", "Akun "+u.Username+" dinonaktifkan. Pengguna langsung keluar dari aplikasi.")
	}
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := h.load(w, r)
	if !ok {
		return
	}
	temp, err := h.Service.ResetPassword(r.Context(), u.ID)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.showTempPassword(w, r, u.ID, temp, "Password akun "+u.Username+" telah direset.")
}
