// Package server merakit seluruh dependensi dan rute HTTP aplikasi.
package server

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"sikat/internal/activities"
	"sikat/internal/assets"
	"sikat/internal/attachments"
	"sikat/internal/auth"
	"sikat/internal/backup"
	"sikat/internal/clock"
	"sikat/internal/config"
	"sikat/internal/dashboard"
	"sikat/internal/integration"
	"sikat/internal/licenses"
	"sikat/internal/maintenance"
	"sikat/internal/master"
	"sikat/internal/middleware"
	"sikat/internal/notifications"
	"sikat/internal/priority"
	"sikat/internal/proposals"
	"sikat/internal/qrcodes"
	"sikat/internal/rbac"
	"sikat/internal/reports"
	"sikat/internal/settings"
	"sikat/internal/simrs"
	"sikat/internal/sla"
	"sikat/internal/tickets"
	"sikat/internal/users"
	"sikat/internal/web"
	"sikat/internal/worker"
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
	// PushSender menggantikan pengirim Web Push (test). Kunci VAPID tetap dari Config.
	PushSender notifications.Sender
	// DisableBackup mematikan backup terjadwal (test).
	DisableBackup bool
	// Employees menggantikan sumber nama pegawai SIMRS (test). Nil = dari Config.
	Employees tickets.EmployeeSource
}

// App adalah server yang sudah dirakit.
type App struct {
	Handler  http.Handler
	Sessions *scs.SessionManager
	Settings *settings.Service
	// Worker menjalankan pekerjaan latar belakang; dijalankan pemanggil dengan go app.Worker.Run(ctx).
	Worker *worker.Runner
	// Notifications: outbox, inbox, dan Web Push.
	Notifications *notifications.Service
	// Backup: backup database & uji restore (dipakai perintah CLI).
	Backup *backup.Service
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

	// Notifikasi: tanpa kunci VAPID, push nonaktif tetapi inbox tetap berjalan.
	var sender notifications.Sender
	vapidKey := ""
	if d.Config.VAPIDPublicKey != "" {
		v, err := notifications.ParseVAPID(d.Config.VAPIDPublicKey, d.Config.VAPIDPrivateKey, d.Config.VAPIDSubject)
		if err != nil {
			return nil, err
		}
		sender, vapidKey = &notifications.WebPush{VAPID: v, Now: d.Clock.Now}, v.PublicKey
	}
	if d.PushSender != nil && vapidKey != "" {
		sender = d.PushSender
	}
	notifSvc := notifications.NewService(d.DB, d.Clock, d.Log, sender, vapidKey)
	// Integrasi master data: lokal secara default; adapter SIMRS didaftarkan di sini
	// setelah spesifikasinya tersedia (integSvc.RegisterProvider).
	integSvc := integration.NewService(d.DB, d.Clock, d.Log)
	backupSvc := backup.NewService(d.DB, d.Clock, d.Log, backup.Config{Enabled: d.Config.BackupEnabled && !d.DisableBackup,
		Dir: d.Config.BackupDir, Hour: d.Config.BackupHour, KeepDays: d.Config.BackupKeepDays,
		Mysqldump: d.Config.Mysqldump, Mysql: d.Config.MysqlClient, DSN: d.Config.DBDSN})
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
		Sessions: sessions, CSRF: csrf, Site: settingsSvc, Log: d.Log, Inbox: notifSvc,
	})
	if err != nil {
		return nil, err
	}

	guard := &auth.Guard{
		Sessions: sessions, Service: authSvc,
		Forbidden: func(w http.ResponseWriter, r *http.Request) { renderer.Error(w, r, http.StatusForbidden, "") },
		Fail:      func(w http.ResponseWriter, r *http.Request, err error) { renderer.ServerError(w, r, err) },
	}
	notifH := &notifications.Handler{Service: notifSvc, Renderer: renderer, Sessions: sessions}
	authH := &auth.Handler{Guard: guard, Service: authSvc, Renderer: renderer, BeforeLogout: notifH.BeforeLogout}
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
	employees := d.Employees
	if employees == nil {
		if p := simrs.NewPegawai(simrs.PegawaiConfig{URL: d.Config.SimrsPegawaiURL, Token: d.Config.SimrsToken,
			Cookie: d.Config.SimrsCookie, Timeout: d.Config.SimrsTimeout}, d.Log); p != nil {
			p.Warm(ctx)
			employees = p
		}
	}
	ticketH := &tickets.Handler{Service: ticketSvc, Settings: settingsSvc, Renderer: renderer, Sessions: sessions,
		BaseURL: d.Config.BaseURL, Require: guard.Require, OnView: notifSvc.MarkTicketRead, Employees: employees}
	publicLimiter := middleware.NewRateLimiter(d.PublicRate, 10*time.Minute, d.PublicRate)
	qrLimiter := middleware.NewRateLimiter(d.QRRate, 10*time.Minute, d.QRRate)
	go publicLimiter.Cleanup(ctx, 5*time.Minute)
	go qrLimiter.Cleanup(ctx, 5*time.Minute)
	// Pencarian nama pegawai di form publik: longgar untuk banyak pelapor di balik satu IP
	// (NAT Wi-Fi RS), tetapi cukup untuk mencegah daftar pegawai disedot.
	suggestLimiter := middleware.NewRateLimiter(120, time.Minute, 60)
	go suggestLimiter.Cleanup(ctx, 5*time.Minute)
	activitySvc := activities.NewService(d.DB, d.Clock, store)
	activityH := &activities.Handler{Service: activitySvc, Settings: settingsSvc, Renderer: renderer}
	maintSvc := maintenance.NewService(d.DB, d.Clock, settingsSvc)
	ticketSvc.SetHooks(maintSvc)   // riwayat perangkat & deteksi kerusakan berulang
	activitySvc.SetHooks(maintSvc) // riwayat perangkat dari kegiatan
	assetH.MaintenanceFor = func(ctx context.Context, id int64, u *rbac.User, form, errs map[string]string) (any, error) {
		v, err := maintSvc.View(ctx, id, u)
		if form != nil {
			v.Form = form
		}
		v.Errors = errs
		return v, err
	}
	maintH := &maintenance.Handler{Service: maintSvc, Renderer: renderer, Require: guard.Require, RenderAsset: assetH.RenderDetail}
	slaSvc := ticketSvc.SLA()
	slaH := &sla.Handler{Service: slaSvc, Renderer: renderer, Require: guard.Require}
	licenseChecker := licenses.Checker{DB: d.DB, Clock: d.Clock}
	runner := &worker.Runner{DB: d.DB, Log: d.Log, LockName: "sikat_worker", Jobs: []worker.Job{{
		Name: "periksa-sla", Interval: time.Minute,
		Run: func(ctx context.Context) error {
			res, err := slaSvc.Scan(ctx)
			if res.Escalated > 0 {
				d.Log.Info("eskalasi SLA baru", "jumlah", res.Escalated, "tiket_aktif", res.Checked)
			}
			return err
		},
	}, {
		// Outbox → inbox → Web Push. Interval pendek agar notifikasi terasa segera.
		Name: "kirim-notifikasi", Interval: 5 * time.Second,
		Run: func(ctx context.Context) error {
			pr, err := notifSvc.Process(ctx)
			if pr.Events > 0 || pr.Failed > 0 {
				d.Log.Info("notifikasi diproses", "event", pr.Events, "notifikasi", pr.Notifications, "push_dijadwalkan", pr.Deliveries, "gagal", pr.Failed)
			}
			if err != nil {
				return err
			}
			sr, err := notifSvc.SendPush(ctx)
			if sr.Sent+sr.Retry+sr.Failed+sr.Gone > 0 {
				d.Log.Info("push dikirim", "terkirim", sr.Sent, "dicoba_lagi", sr.Retry, "gagal", sr.Failed, "langganan_kedaluwarsa", sr.Gone, "basi", sr.Expired)
			}
			return err
		},
	}, {
		Name: "bersih-notifikasi", Interval: 24 * time.Hour,
		Run: func(ctx context.Context) error {
			n, err := notifSvc.Cleanup(ctx)
			if n > 0 {
				d.Log.Info("data notifikasi lama dihapus", "baris", n)
			}
			return err
		},
	}, {
		// Sinkron master data terjadwal & percobaan ulang (hanya bila sumber SIMRS aktif).
		Name: "sinkron-master", Interval: time.Minute,
		Run: func(ctx context.Context) error {
			n, err := integSvc.RunDue(ctx)
			if n > 0 {
				d.Log.Info("sinkron master data", "entitas", n)
			}
			if err != nil {
				return err
			}
			return integSvc.Cleanup(ctx)
		},
	}, {
		// Backup harian pada jam yang dikonfigurasi (WITA); gagal → dicoba lagi tiap jam.
		Name: "backup-harian", Interval: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			_, err := backupSvc.RunDue(ctx)
			return err
		},
	}, {
		// Retensi foto lampiran (Q9): hapus file foto tiket lama, metadata tetap.
		Name: "retensi-lampiran", Interval: 6 * time.Hour,
		Run: func(ctx context.Context) error {
			n, err := attachments.PurgeExpired(ctx, d.DB, store, d.Clock.Now(), settingsSvc.Int(ctx, settings.KeyAttachmentRetention))
			if n > 0 {
				d.Log.Info("file foto dihapus sesuai retensi", "jumlah", n)
			}
			return err
		},
	}, {
		// Pengingat lisensi yang mendekati/melewati tanggal berakhir (ke Admin IT).
		Name: "periksa-lisensi", Interval: time.Hour,
		Run: func(ctx context.Context) error {
			n, err := licenseChecker.Check(ctx)
			if n > 0 {
				d.Log.Info("pengingat lisensi dikirim", "jumlah", n)
			}
			return err
		},
	}, {
		Name: "tutup-otomatis-tiket", Interval: time.Hour,
		Run: func(ctx context.Context) error {
			n, err := ticketSvc.AutoClose(ctx, settingsSvc.Int(ctx, settings.KeyAutoCloseDays))
			if n > 0 {
				d.Log.Info("tiket ditutup otomatis", "jumlah", n)
			}
			return err
		},
	}}}
	publicH := &tickets.PublicHandler{Service: ticketSvc, QR: qrSvc, Settings: settingsSvc, Renderer: renderer,
		BaseURL: d.Config.BaseURL, IPLimiter: publicLimiter, QRLimiter: qrLimiter,
		Employees: employees, SuggestLimiter: suggestLimiter}
	// Kajian kebutuhan: dokumen di folder terpisah dari foto (tanpa retensi otomatis).
	proposalStore := attachments.Store{Dir: filepath.Join(d.Config.StorageDir, "kajian")}
	if err := proposalStore.Check(); err != nil {
		return nil, err
	}
	proposalSvc := proposals.NewService(d.DB, d.Clock, proposalStore)
	proposalH := &proposals.Handler{Service: proposalSvc, Renderer: renderer, OnView: notifSvc.MarkURLRead}
	proposalPublicH := &proposals.PublicHandler{Service: proposalSvc, QR: qrSvc, Renderer: renderer, BaseURL: d.Config.BaseURL,
		IPLimiter: publicLimiter, QRLimiter: qrLimiter, Suggest: employees != nil}
	userH := &users.Handler{Service: users.NewService(d.DB, d.Clock), Renderer: renderer}
	settingsH := &settings.Handler{Service: settingsSvc, Renderer: renderer}
	integH := &integration.Handler{Service: integSvc, Renderer: renderer}
	reportSvc := reports.NewService(d.DB, d.Clock)
	reportSvc.RepeatThreshold = func(ctx context.Context) int { return settingsSvc.Int(ctx, settings.KeyRepeatThreshold) }
	reportH := &reports.Handler{Service: reportSvc, DB: d.DB, Renderer: renderer, Hospital: settingsSvc.HospitalName}
	dashH := &dashboard.Handler{DB: d.DB, Assets: assetSvc, Tickets: ticketSvc, Maintenance: maintSvc, Reports: reportSvc,
		Renderer: renderer, PushEnabled: notifSvc.PushEnabled, WorkerLeader: runner.IsLeader}
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
		proposalPublicH.Routes(r)
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
		r.With(guard.Require(rbac.PermActivityCreate)).Route("/activities", activityH.Routes)
		r.With(guard.Require(rbac.PermProposalManage)).Route("/kajian", proposalH.Routes)
		r.Route("/maintenance", maintH.Routes)
		r.Route("/sla", slaH.Routes)
		r.Route("/notifications", notifH.Routes)
		r.Route("/reports", reportH.Routes)
		r.With(guard.Require(rbac.PermIntegration)).Route("/integrations", integH.Routes)
		r.With(guard.Require(rbac.PermConfigManage)).Route("/backups", (&backup.Handler{Service: backupSvc, Renderer: renderer}).Routes)
		r.With(guard.Require(rbac.PermConfigManage)).Route("/settings/notifications", notifH.AdminRoutes)
	})

	// Router luar: aset statis & health check tanpa session.
	staticSub, err := fs.Sub(d.Static, "static")
	if err != nil {
		return nil, err
	}
	// Mode development: aset dibaca langsung dari folder static/ tanpa cache, sehingga
	// hasil build CSS/JS terbaru langsung terlihat tanpa restart server.
	devStatic := false
	if !d.Config.IsProduction() && d.DevReloadDir != "" {
		if st, err := os.Stat(filepath.Join(d.DevReloadDir, "static")); err == nil && st.IsDir() {
			staticSub = os.DirFS(filepath.Join(d.DevReloadDir, "static"))
			devStatic = true
		}
	}
	files := http.FileServerFS(staticSub)
	root := http.NewServeMux()
	root.Handle("/static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if devStatic {
			w.Header().Set("Cache-Control", "no-cache")
		} else if r.URL.Query().Get("v") != "" {
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
	pwa := &pwaHandler{static: staticSub, renderer: renderer, dev: devStatic}
	root.HandleFunc("/sw.js", pwa.serviceWorker)
	root.HandleFunc("/manifest.webmanifest", pwa.manifest)
	root.HandleFunc("/favicon.ico", pwa.favicon)
	root.HandleFunc("/offline", func(w http.ResponseWriter, r *http.Request) {
		// Tanpa session: halaman ini disimpan service worker untuk ditampilkan saat offline.
		w.Header().Set("Cache-Control", "no-cache")
		renderer.RenderPublic(w, r, http.StatusOK, "public/offline", web.Page{Title: "Tidak ada koneksi"})
	})
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

	return &App{Handler: h, Sessions: sessions, Settings: settingsSvc, Worker: runner, Notifications: notifSvc, Backup: backupSvc}, nil
}
