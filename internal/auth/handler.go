package auth

import (
	"errors"
	"net/http"

	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menangani halaman login, logout, dan ganti password.
type Handler struct {
	Guard    *Guard
	Service  *Service
	Renderer *web.Renderer
	// BeforeLogout (opsional) dipanggil sebelum session diakhiri.
	BeforeLogout func(r *http.Request)
}

type loginData struct {
	Username string
}

// LoginForm menampilkan form login.
func (h *Handler) LoginForm(w http.ResponseWriter, r *http.Request) {
	if reqctx.UserFrom(r.Context()) != nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "auth/login", web.Page{Title: "Masuk", Data: loginData{}})
}

// Login memproses form login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	if username == "" || password == "" {
		h.Renderer.Render(w, r, http.StatusUnprocessableEntity, "auth/login", web.Page{
			Title: "Masuk", Data: loginData{Username: username},
			Flash: &web.Flash{Kind: "error", Message: "Username dan password wajib diisi."},
		})
		return
	}

	u, err := h.Service.Authenticate(r.Context(), username, password)
	if err != nil {
		var locked *LockedError
		if errors.Is(err, ErrInvalidCredentials) || errors.As(err, &locked) {
			h.Renderer.Render(w, r, http.StatusUnauthorized, "auth/login", web.Page{
				Title: "Masuk", Data: loginData{Username: username},
				Flash: &web.Flash{Kind: "error", Message: err.Error()},
			})
			return
		}
		h.Renderer.ServerError(w, r, err)
		return
	}
	if err := h.Guard.StartSession(r, u.ID); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if u.MustChangePassword {
		h.Renderer.Redirect(w, r, "/app/password", "info", "Silakan ganti password sementara Anda sebelum melanjutkan.")
		return
	}
	h.Renderer.Redirect(w, r, "/app", "success", "Selamat datang, "+u.FirstName()+".")
}

// Logout mengakhiri session.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.BeforeLogout != nil {
		h.BeforeLogout(r)
	}
	if err := h.Guard.EndSession(r); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// PasswordForm menampilkan form ganti password.
func (h *Handler) PasswordForm(w http.ResponseWriter, r *http.Request) {
	h.Renderer.Render(w, r, http.StatusOK, "auth/password", web.Page{Title: "Ganti Password", Nav: "password"})
}

// ChangePassword memproses ganti password.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	errs, err := h.Service.ChangePassword(r.Context(), u.ID,
		r.PostFormValue("current"), r.PostFormValue("new"), r.PostFormValue("confirm"))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(errs) > 0 {
		h.Renderer.Render(w, r, http.StatusUnprocessableEntity, "auth/password", web.Page{
			Title: "Ganti Password", Nav: "password", Errors: errs,
		})
		return
	}
	// Token session diganti setelah perubahan kredensial.
	if err := h.Guard.StartSession(r, u.ID); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Redirect(w, r, "/app", "success", "Password berhasil diganti.")
}
