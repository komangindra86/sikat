// Package server merakit seluruh dependensi dan rute HTTP aplikasi.
package server

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"sikat/internal/assets"
	"sikat/internal/attachments"
	"sikat/internal/auth"
	"sikat/internal/clock"
	"sikat/internal/config"
	"sikat/internal/dashboard"
	"sikat/internal/master"
	"sikat/internal/middleware"
	"sikat/internal/priority"
	"sikat/internal/qrcodes"
	"sikat/internal/rbac"
	"sikat/internal/settings"
	"sikat/internal/tickets"
	"sikat/internal/users"
	"sikat/internal/web"
)

// Deps adalah dependensi untuk membangun server.
type Deps struct {
	Config    config.Config
	DB        *sql.DB
	Log       *slog.Logger
	Clock     clock.Clock
	Templates fs.FS // root berisi templates/
	Static    fs.FS // root berisi static/
	// DevReloadDir: folder sumber untuk membaca ulang template di mode development.
	DevReloadDir string
	// SessionCleanup: interval pembersihan session kedaluwarsa (0 = nonaktif).
	SessionCleanup time.Duration
	// LoginRate: batas percobaan login per IP per menit (0 = default 10).
	LoginRate int
	// PublicRate: batas kiriman laporan per IP per 10 menit (0 = default 60).
	PublicRate int
	// QRRate: batas kiriman laporan per QR per 10 menit (0 = default 10).
	QRRate int
}

// App adalah server yang sudah dirakit.
type App struct {
	Handler  http.Handler
	Sessions *scs.SessionManager
	Settings *settings.Service
}

// New merakit handler HTTP.
func New(ctx context.Context, d Deps) (*App, error) {
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.LoginRate == 0 {
		d.LoginRate = 10
	}
	if d.PublicRate == 0 {
		d.PublicRate = 60 // longgar: banyak pegawai berbagi satu IP publik (NAT Wi-Fi RS)
	}
	if d.QRRate == 0 {
		d.QRRate = 10
	}
	store := attachments.Store{Dir: filepath.Join(d.Config.StorageDir, "attachments")}
	if err := store.Check(); err != nil {
		return nil, err
	}

	sessionStore := mysqlstore.NewWithCleanupInterval(d.DB, d.SessionCleanup)
	sessions := auth.NewSessionManager(sessionStore, d.Config.SessionLifetime, d.Config.SessionIdleTimeout, d.Config.SecureCookies())

	settingsSvc := settings.NewService(d.DB, d.Clock)
	authSvc := auth.NewService(d.DB, d.Clock, settingsSvc)

	var renderer *web.Renderer
	csrf := middleware.NewCSRF(sessions, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		renderer.Error(w, r, http.StatusForbidden,
			"Formulir sudah kedaluwarsa atau tidak sah. Muat ulang halaman lalu coba lagi.")
	}))

	reloadDir := ""
	if !d.Config.IsProduction() {
		reloadDir = d.DevReloadDir
	}
	renderer, err := web.NewRenderer(web.Options{
		Templates: d.Templates, Static: d.Static, ReloadDir: reloadDir,
		Sessions: sessions, CSRF: csrf, Site: settingsSvc, Log: d.Log,
	})
	if err != nil {
		return nil, err
	}

	guard := &auth.Guard{
		Sessions: sessions, Service: authSvc,
		Forbidden: func(w http.ResponseWriter, r *http.Request) { renderer.Error(w, r, http.StatusForbidden, "") },
		Fail:      func(w http.ResponseWriter, r *http.Request, err error) { renderer.ServerError(w, r, err) },
	}
	authH := &auth.Handler{Guard: guard, Service: authSvc, Renderer: renderer}
	assetSvc := assets.NewService(d.DB, d.Clock)
	masterH := &master.Handler{Service: master.NewService(d.DB, d.Clock), Renderer: renderer}
	qrSvc := qrcodes.NewService(d.DB, d.Clock, d.Config.BaseURL)
	ticketSvc := tickets.NewService(d.DB, d.Clock, store)
	assetH := &assets.Handler{
		Service: assetSvc, Renderer: renderer, RequireManage: guard.Require(rbac.PermMasterManage),
		QRFor: func(ctx context.Context, id int64) (int64, bool, error) {
			q, ok, err := qrSvc.ActiveFor(ctx, qrcodes.TypeAsset, id)
			return q.ID, ok, err
		},
		TicketsFor: func(ctx context.Context, id int64) ([]assets.RelatedTicket, error) {
			list, err := ticketSvc.RelatedToAsset(ctx, id, 10)
			out := make([]assets.RelatedTicket, 0, len(list))
			for _, t := range list {
				out = append(out, assets.RelatedTicket{ID: t.ID, Number: t.TicketNo, CreatedAt: t.CreatedAt,
					Category: t.CategoryName, Status: t.Status, Priority: t.Priority})
			}
			return out, err
		},
	}
	qrH := &qrcodes.Handler{Service: qrSvc, Renderer: renderer}
	priorityH := &priority.Handler{DB: d.DB, Renderer: renderer}
	ticketH := &tickets.Handler{Service: ticketSvc, Settings: settingsSvc, Renderer: renderer, Sessions: sessions,
		BaseURL: d.Config.BaseURL, Require: guard.Require}
	publicLimiter := middleware.NewRateLimiter(d.PublicRate, 10*time.Minute, d.PublicRate)
	qrLimiter := middleware.NewRateLimiter(d.QRRate, 10*time.Minute, d.QRRate)
	go publicLimiter.Cleanup(ctx, 5*time.Minute)
	go qrLimiter.Cleanup(ctx, 5*time.Minute)
	publicH := &tickets.PublicHandler{Service: ticketSvc, QR: qrSvc, Settings: settingsSvc, Renderer: renderer,
		BaseURL: d.Config.BaseURL, IPLimiter: publicLimiter, QRLimiter: qrLimiter}
	userH := &users.Handler{Service: users.NewService(d.DB, d.Clock), Renderer: renderer}
	settingsH := &settings.Handler{Service: settingsSvc, Renderer: renderer}
	dashH := &dashboard.Handler{DB: d.DB, Assets: assetSvc, Tickets: ticketSvc, Renderer: renderer}
	auditH := &dashboard.AuditHandler{DB: d.DB, Renderer: renderer}

	loginLimiter := middleware.NewRateLimiter(d.LoginRate, time.Minute, d.LoginRate)
	go loginLimiter.Cleanup(ctx, 5*time.Minute)
	tooMany := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		renderer.Error(w, r, http.StatusTooManyRequests, "")
	})

	// Router aplikasi (dengan session).
	app := chi.NewRouter()
	app.Use(sessions.LoadAndSave)
	app.Use(middleware.Recoverer(d.Log, func(w http.ResponseWriter, r *http.Request) { renderer.Error(w, r, http.StatusInternalServerError, "") }))
	app.Use(csrf.Middleware)
	app.Use(guard.LoadUser)
	app.NotFound(func(w http.ResponseWriter, r *http.Request) { renderer.Error(w, r, http.StatusNotFound, "") })
	app.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { renderer.Error(w, r, http.StatusNotFound, "") })

	app.Get("/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/app", http.StatusSeeOther) })
	app.With(middleware.NoStore).Get("/login", authH.LoginForm)
	app.With(middleware.NoStore, loginLimiter.Middleware(true, tooMany)).Post("/login", authH.Login)
	app.Post("/logout", authH.Logout)
	app.Group(func(r chi.Router) {
		r.Use(middleware.NoStore)
		publicH.Routes(r)
	})

	app.Route("/app", func(r chi.Router) {
		r.Use(middleware.NoStore, guard.RequireLogin, guard.RequirePasswordChanged("/app/password"))
		r.Get("/", dashH.Home)
		r.Get("/password", authH.PasswordForm)
		r.Post("/password", authH.ChangePassword)
		r.With(guard.Require(rbac.PermMasterManage)).Route("/master", masterH.Routes)
		r.With(guard.Require(rbac.PermAssetView)).Route("/assets", assetH.Routes)
		r.With(guard.Require(rbac.PermUserManage)).Route("/users", userH.Routes)
		r.With(guard.Require(rbac.PermConfigManage)).Get("/settings", settingsH.Show)
		r.With(guard.Require(rbac.PermConfigManage)).Post("/settings", settingsH.Save)
		r.With(guard.Require(rbac.PermAuditView)).Get("/audit", auditH.List)
		r.With(guard.Require(rbac.PermMasterManage)).Route("/qr", qrH.Routes)
		r.With(guard.Require(rbac.PermConfigManage)).Get("/priority/test", priorityH.Test)
		r.With(guard.Require(rbac.PermTicketQueue)).Route("/tickets", ticketH.Routes)
	})

	// Router luar: aset statis & health check tanpa session.
	staticSub, err := fs.Sub(d.Static, "static")
	if err != nil {
		return nil, err
	}
	files := http.FileServerFS(staticSub)
	root := http.NewServeMux()
	root.Handle("/static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r) // tanpa daftar isi folder
			return
		}
		files.ServeHTTP(w, r)
	})))
	root.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Cache-Control", "no-store")
		if err := d.DB.PingContext(ctx); err != nil {
			http.Error(w, "database tidak tersedia", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	root.Handle("/", app)

	var h http.Handler = root
	h = middleware.BodyLimit(64<<20, 1<<20)(h)
	h = middleware.SecurityHeaders(d.Config.SecureCookies())(h)
	h = middleware.Logger(d.Log)(h)
	h = middleware.RequestInfo(d.Config.TrustProxyHeaders)(h)

	return &App{Handler: h, Sessions: sessions, Settings: settingsSvc}, nil
}
