package auth

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"sikat/internal/rbac"
	"sikat/internal/reqctx"
)

const (
	sessionUserKey = "user_id"
	sessionCredKey = "cred" // cap kredensial saat login
)

// NewSessionManager membuat session manager dengan cookie aman.
func NewSessionManager(store scs.Store, lifetime, idle time.Duration, secure bool) *scs.SessionManager {
	s := scs.New()
	s.Store = store
	s.Lifetime = lifetime
	s.IdleTimeout = idle
	s.Cookie.Name = "sikat_session"
	s.Cookie.HttpOnly = true
	s.Cookie.Secure = secure
	s.Cookie.SameSite = http.SameSiteLaxMode
	s.Cookie.Path = "/"
	return s
}

// Guard menyediakan middleware autentikasi & otorisasi.
type Guard struct {
	Sessions  *scs.SessionManager
	Service   *Service
	Forbidden func(w http.ResponseWriter, r *http.Request)
	Fail      func(w http.ResponseWriter, r *http.Request, err error)
}

// StartSession mengganti token session (mencegah session fixation) lalu menyimpan user.
func (g *Guard) StartSession(r *http.Request, userID int64) error {
	if err := g.Sessions.RenewToken(r.Context()); err != nil {
		return err
	}
	g.Sessions.Put(r.Context(), sessionUserKey, userID)
	if _, stamp, err := g.Service.LoadSessionUser(r.Context(), userID); err != nil {
		return err
	} else if stamp != "" {
		g.Sessions.Put(r.Context(), sessionCredKey, stamp)
	}
	return nil
}

// EndSession menghapus seluruh data session.
func (g *Guard) EndSession(r *http.Request) error {
	return g.Sessions.Destroy(r.Context())
}

// LoadUser memuat user dari session ke context. User yang sudah dinonaktifkan
// otomatis keluar karena dimuat ulang dari database setiap request.
func (g *Guard) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := g.Sessions.GetInt64(r.Context(), sessionUserKey)
		if id == 0 {
			next.ServeHTTP(w, r)
			return
		}
		u, stamp, err := g.Service.LoadSessionUser(r.Context(), id)
		if err != nil {
			g.Fail(w, r, err)
			return
		}
		// Password diganti/di-reset sejak login → session lama tidak berlaku
		// (mis. HP yang hilang). Session lama tanpa cap diberi cap saat ini.
		if u != nil {
			switch prev := g.Sessions.GetString(r.Context(), sessionCredKey); {
			case prev == "":
				g.Sessions.Put(r.Context(), sessionCredKey, stamp)
			case prev != stamp:
				u = nil
			}
		}
		if u == nil {
			_ = g.Sessions.Destroy(r.Context())
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(reqctx.WithUser(r.Context(), u)))
	})
}

// RequireLogin mengalihkan pengguna anonim ke halaman login.
func (g *Guard) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reqctx.UserFrom(r.Context()) == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePasswordChanged memaksa pengguna dengan password sementara untuk
// menggantinya sebelum mengakses halaman lain.
func (g *Guard) RequirePasswordChanged(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := reqctx.UserFrom(r.Context())
			if u != nil && u.MustChangePassword {
				for _, p := range allowed {
					if r.URL.Path == p {
						next.ServeHTTP(w, r)
						return
					}
				}
				http.Redirect(w, r, "/app/password", http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Require membatasi akses ke pengguna yang memiliki permission p.
func (g *Guard) Require(p rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !reqctx.UserFrom(r.Context()).Has(p) {
				g.Forbidden(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
