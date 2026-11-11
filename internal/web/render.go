// Package web merender template HTML, flash message, dan halaman error.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/alexedwards/scs/v2"

	"sikat/internal/rbac"
	"sikat/internal/reqctx"
)

// SiteInfo menyediakan informasi rumah sakit untuk layout.
type SiteInfo interface {
	HospitalName(ctx context.Context) string
	ITContact(ctx context.Context) string
}

// TokenSource menyediakan token CSRF untuk form.
type TokenSource interface {
	Token(r *http.Request) string
}

// Flash adalah pesan satu kali setelah redirect.
type Flash struct {
	Kind    string // success | error | info
	Message string
}

// Page adalah data yang diberikan ke setiap template halaman.
type Page struct {
	Title        string
	Nav          string
	User         *rbac.User
	CSRFToken    string
	Flash        *Flash
	HospitalName string
	ITContact    string
	Data         any
	Errors       map[string]string
}

// Err mengembalikan pesan error untuk field (dipakai template).
func (p Page) Err(field string) string {
	if p.Errors == nil {
		return ""
	}
	return p.Errors[field]
}

// Renderer merender template yang di-embed (atau dari disk di mode development).
type Renderer struct {
	fsys         fs.FS
	reloadFromOS bool
	sessions     *scs.SessionManager
	csrf         TokenSource
	site         SiteInfo
	log          *slog.Logger
	staticHashes map[string]string

	mu    sync.RWMutex
	pages map[string]*template.Template
}

// Options untuk NewRenderer.
type Options struct {
	Templates fs.FS // root berisi folder templates/
	Static    fs.FS // root berisi folder static/
	// ReloadDir: bila diisi (mode development) dan berisi folder templates/,
	// template dibaca ulang dari disk setiap request sehingga perubahan langsung terlihat.
	ReloadDir string
	Sessions  *scs.SessionManager
	CSRF      TokenSource
	Site      SiteInfo
	Log       *slog.Logger
}

// NewRenderer mem-parse seluruh template halaman.
func NewRenderer(o Options) (*Renderer, error) {
	r := &Renderer{
		fsys: o.Templates, sessions: o.Sessions, csrf: o.CSRF, site: o.Site, log: o.Log,
		staticHashes: map[string]string{},
	}
	if o.ReloadDir != "" {
		if st, err := os.Stat(path.Join(o.ReloadDir, "templates")); err == nil && st.IsDir() {
			r.fsys = os.DirFS(o.ReloadDir)
			r.reloadFromOS = true
		}
	}
	if err := r.hashStatic(o.Static); err != nil {
		return nil, err
	}
	pages, err := r.parse()
	if err != nil {
		return nil, err
	}
	r.pages = pages
	return r, nil
}

func (r *Renderer) hashStatic(static fs.FS) error {
	if static == nil {
		return nil
	}
	return fs.WalkDir(static, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(static, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		r.staticHashes[strings.TrimPrefix(p, "static/")] = hex.EncodeToString(sum[:])[:10]
		return nil
	})
}

func (r *Renderer) parse() (map[string]*template.Template, error) {
	base := template.New("").Funcs(r.funcs())
	shared, err := fs.Glob(r.fsys, "templates/layouts/*.html")
	if err != nil {
		return nil, err
	}
	comps, err := fs.Glob(r.fsys, "templates/components/*.html")
	if err != nil {
		return nil, err
	}
	shared = append(shared, comps...)
	if len(shared) > 0 {
		if base, err = base.ParseFS(r.fsys, shared...); err != nil {
			return nil, fmt.Errorf("parse layout/komponen: %w", err)
		}
	}

	pages := map[string]*template.Template{}
	err = fs.WalkDir(r.fsys, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		dir := path.Dir(p)
		if dir == "templates/layouts" || dir == "templates/components" {
			return nil
		}
		t, err := base.Clone()
		if err != nil {
			return err
		}
		if t, err = t.ParseFS(r.fsys, p); err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(p, "templates/"), ".html")
		pages[name] = t
		return nil
	})
	return pages, err
}

func (r *Renderer) page(name string) (*template.Template, error) {
	if r.reloadFromOS {
		pages, err := r.parse()
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.pages = pages
		r.mu.Unlock()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.pages[name]
	if !ok {
		return nil, fmt.Errorf("template %q tidak ditemukan", name)
	}
	return t, nil
}

// fill melengkapi Page dengan data umum dari request.
func (r *Renderer) fill(req *http.Request, p *Page) {
	ctx := req.Context()
	p.User = reqctx.UserFrom(ctx)
	if r.csrf != nil {
		p.CSRFToken = r.csrf.Token(req)
	}
	if r.site != nil {
		p.HospitalName = r.site.HospitalName(ctx)
		p.ITContact = r.site.ITContact(ctx)
	}
	if p.Flash == nil && r.sessions != nil {
		if msg := r.sessions.PopString(ctx, "flash_msg"); msg != "" {
			p.Flash = &Flash{Kind: r.sessions.PopString(ctx, "flash_kind"), Message: msg}
		}
	}
}

// Render merender halaman penuh. Output ditulis ke buffer dulu agar error
// template tidak menghasilkan halaman setengah jadi.
func (r *Renderer) Render(w http.ResponseWriter, req *http.Request, status int, name string, p Page) {
	r.fill(req, &p)
	t, err := r.page(name)
	if err != nil {
		r.fail(w, req, err)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, path.Base(name)+".html", p); err != nil {
		r.fail(w, req, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// Fragment merender satu blok {{define}} dari halaman tertentu (untuk HTMX).
func (r *Renderer) Fragment(w http.ResponseWriter, req *http.Request, status int, name, block string, p Page) {
	r.fill(req, &p)
	t, err := r.page(name)
	if err != nil {
		r.fail(w, req, err)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, block, p); err != nil {
		r.fail(w, req, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (r *Renderer) fail(w http.ResponseWriter, req *http.Request, err error) {
	r.log.Error("render", "err", err, "path", req.URL.Path, "req_id", reqctx.InfoFrom(req.Context()).RequestID)
	http.Error(w, "Terjadi kesalahan saat menampilkan halaman.", http.StatusInternalServerError)
}

// SetFlash menyimpan pesan untuk ditampilkan setelah redirect.
func (r *Renderer) SetFlash(ctx context.Context, kind, msg string) {
	r.sessions.Put(ctx, "flash_kind", kind)
	r.sessions.Put(ctx, "flash_msg", msg)
}

// Redirect dengan flash (pola Post/Redirect/Get). Untuk request HTMX memakai HX-Redirect.
func (r *Renderer) Redirect(w http.ResponseWriter, req *http.Request, url, kind, msg string) {
	if msg != "" {
		r.SetFlash(req.Context(), kind, msg)
	}
	if req.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, req, url, http.StatusSeeOther)
}

var errorMessages = map[int]struct{ title, msg string }{
	http.StatusBadRequest:          {"Permintaan tidak valid", "Data yang dikirim tidak dapat diproses."},
	http.StatusForbidden:           {"Akses ditolak", "Anda tidak memiliki hak untuk membuka halaman atau melakukan tindakan ini."},
	http.StatusNotFound:            {"Halaman tidak ditemukan", "Halaman yang Anda cari tidak ada atau sudah dipindahkan."},
	http.StatusTooManyRequests:     {"Terlalu banyak percobaan", "Mohon tunggu sekitar satu menit lalu coba lagi."},
	http.StatusInternalServerError: {"Terjadi kesalahan", "Sistem mengalami kendala. Silakan coba lagi; bila berulang, hubungi tim IT."},
}

// ErrorData adalah isi halaman error.
type ErrorData struct {
	Status    int
	Heading   string
	Message   string
	RequestID string
}

// Error merender halaman error ramah pengguna.
func (r *Renderer) Error(w http.ResponseWriter, req *http.Request, status int, msg string) {
	m, ok := errorMessages[status]
	if !ok {
		m = errorMessages[http.StatusInternalServerError]
	}
	if msg == "" {
		msg = m.msg
	}
	if req.Header.Get("HX-Request") == "true" {
		// HTMX: tampilkan sebagai toast lewat header, tanpa mengganti konten.
		w.Header().Set("HX-Reswap", "none")
		w.Header().Set("HX-Trigger", fmt.Sprintf(`{"sikat:error":%q}`, msg))
		w.WriteHeader(status)
		return
	}
	r.Render(w, req, status, "errors/error", Page{
		Title: m.title,
		Data:  ErrorData{Status: status, Heading: m.title, Message: msg, RequestID: reqctx.InfoFrom(req.Context()).RequestID},
	})
}

// ServerError mencatat error lalu merender halaman 500.
func (r *Renderer) ServerError(w http.ResponseWriter, req *http.Request, err error) {
	r.log.Error("server error", "err", err, "path", req.URL.Path, "req_id", reqctx.InfoFrom(req.Context()).RequestID)
	r.Error(w, req, http.StatusInternalServerError, "")
}
