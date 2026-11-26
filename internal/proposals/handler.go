package proposals

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"sikat/internal/middleware"
	"sikat/internal/qrcodes"
	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler melayani halaman Kajian untuk Admin IT.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
	// OnView (opsional) dipanggil saat admin membuka kajian (menandai notifikasinya dibaca).
	OnView func(ctx context.Context, userID int64, url string)
}

// Routes mendaftarkan rute /app/kajian.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/{id}", h.detail)
	r.Post("/{id}/terima", h.receive)
	r.Get("/{id}/dokumen", h.download)
}

type listData struct {
	Items  []Proposal
	Filter Filter
	Pager  web.Pager
	Open   int
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := listData{Filter: Filter{Status: q.Get("status"), Kind: q.Get("kind"), Search: q.Get("q")}, Pager: web.NewPager(r, 25)}
	var err error
	if d.Items, err = h.Service.List(r.Context(), d.Filter, &d.Pager); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if d.Open, err = h.Service.CountSubmitted(r.Context()); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "kajian/list", web.Page{Title: "Kajian kebutuhan", Nav: "kajian", Data: d})
}

type detailData struct {
	P    Proposal
	Form map[string]string
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) (Proposal, bool) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return Proposal{}, false
	}
	p, err := h.Service.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "Kajian tidak ditemukan.")
		return p, false
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return p, false
	}
	return p, true
}

func (h *Handler) renderDetail(w http.ResponseWriter, r *http.Request, status int, p Proposal, form, errs map[string]string) {
	if form == nil {
		form = map[string]string{}
	}
	h.Renderer.Render(w, r, status, "kajian/detail", web.Page{Title: p.No(), Nav: "kajian", Errors: errs, Data: detailData{P: p, Form: form}})
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	p, ok := h.load(w, r)
	if !ok {
		return
	}
	if h.OnView != nil {
		if u := reqctx.UserFrom(r.Context()); u != nil {
			h.OnView(r.Context(), u.ID, "/app/kajian/"+strconv.FormatInt(p.ID, 10))
		}
	}
	h.renderDetail(w, r, http.StatusOK, p, nil, nil)
}

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	p, ok := h.load(w, r)
	if !ok {
		return
	}
	back := "/app/kajian/" + strconv.FormatInt(p.ID, 10)
	errs, err := h.Service.Receive(r.Context(), p.ID, reqctx.UserFrom(r.Context()), r.PostFormValue("received_note"))
	switch {
	case errors.Is(err, ErrReceived):
		h.Renderer.Redirect(w, r, back, "error", "Kajian ini sudah diterima sebelumnya.")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		h.renderDetail(w, r, http.StatusUnprocessableEntity, p, web.FormValues(r), errs)
	default:
		h.Renderer.Redirect(w, r, back, "success", "Kajian diterima dan tersimpan sebagai arsip. Pemohon melihat status Diterima di link pemantauannya.")
	}
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	p, ok := h.load(w, r)
	if !ok {
		return
	}
	f, err := h.Service.Store().Open(p.FileKey)
	if errors.Is(err, os.ErrNotExist) {
		h.Renderer.Error(w, r, http.StatusNotFound, "File dokumen kajian tidak ditemukan di penyimpanan.")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", p.FileMime)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": p.FileName}))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", st.ModTime().Truncate(time.Second), f)
}

// PublicHandler melayani pemohon tanpa login: form kajian dari QR lapor dan halaman
// status kajian berbasis token.
type PublicHandler struct {
	Service   *Service
	QR        *qrcodes.Service
	Renderer  *web.Renderer
	BaseURL   string
	IPLimiter *middleware.RateLimiter // batas kiriman per IP
	QRLimiter *middleware.RateLimiter // batas kiriman per QR
	// Suggest: saran nama pegawai SIMRS tersedia (memakai /q/{token}/pegawai form lapor).
	Suggest bool
}

// Routes mendaftarkan rute publik kajian.
func (h *PublicHandler) Routes(r chi.Router) {
	r.Get("/q/{token}/kajian", h.form)
	r.Post("/q/{token}/kajian", h.submit)
	r.Get("/k/{token}", h.track)
}

type formData struct {
	Token       string
	Target      qrcodes.Target
	Departments []Option
	Form        map[string]string
	MaxMB       int
	Accept      string
	Suggest     bool
}

func (h *PublicHandler) target(w http.ResponseWriter, r *http.Request) (qrcodes.Target, string, bool) {
	token := chi.URLParam(r, "token")
	t, err := h.QR.Resolve(r.Context(), token)
	switch {
	case errors.Is(err, qrcodes.ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "QR tidak dikenal. Pindai ulang QR lapor gangguan IT.")
		return t, token, false
	case errors.Is(err, qrcodes.ErrInactive):
		h.Renderer.Error(w, r, http.StatusGone, "QR ini sudah tidak berlaku.")
		return t, token, false
	case err != nil:
		h.Renderer.ServerError(w, r, err)
		return t, token, false
	}
	return t, token, true
}

func (h *PublicHandler) renderForm(w http.ResponseWriter, r *http.Request, status int, t qrcodes.Target, token string, form, errs map[string]string) {
	depts, err := h.Service.Departments(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if form == nil {
		form = map[string]string{}
	}
	if _, ok := form["department_id"]; !ok && t.DepartmentID.Valid {
		form["department_id"] = strconv.FormatInt(t.DepartmentID.Int64, 10)
	}
	h.Renderer.Render(w, r, status, "public/kajian", web.Page{
		Title: "Kirim kajian kebutuhan", Errors: errs,
		Data: formData{Token: token, Target: t, Departments: depts, Form: form, MaxMB: MaxFileBytes >> 20, Accept: Accept, Suggest: h.Suggest},
	})
}

func (h *PublicHandler) form(w http.ResponseWriter, r *http.Request) {
	t, token, ok := h.target(w, r)
	if !ok {
		return
	}
	// Dari jenis gangguan jalur kajian di form lapor: jenis kebutuhan sudah terpilih.
	form := map[string]string{"kind": r.URL.Query().Get("kind")}
	h.renderForm(w, r, http.StatusOK, t, token, form, nil)
}

func (h *PublicHandler) submit(w http.ResponseWriter, r *http.Request) {
	t, token, ok := h.target(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	ip := reqctx.InfoFrom(ctx).IP
	if !h.IPLimiter.Allow(ip) || !h.QRLimiter.Allow("kajian:"+token+":"+ip) {
		h.Renderer.Error(w, r, http.StatusTooManyRequests, "")
		return
	}
	if r.PostFormValue("website") != "" { // honeypot
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	d, form, errs := ParseDraft(r.PostFormValue)
	d.QRCodeID = t.QRID
	_, fh, err := r.FormFile("document")
	if err != nil && !errors.Is(err, http.ErrMissingFile) {
		h.Renderer.ServerError(w, r, err)
		return
	}
	file, fileErr, err := ReadFile(fh)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if fileErr != "" {
		errs["document"] = fileErr
	}
	if len(errs) > 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, t, token, form, errs)
		return
	}
	res, fieldErrs, err := h.Service.Submit(ctx, d, file)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(fieldErrs) > 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, t, token, form, fieldErrs)
		return
	}
	http.Redirect(w, r, "/k/"+res.Token+"?baru=1", http.StatusSeeOther)
}

type trackData struct {
	P        Proposal
	TrackURL string
	New      bool
}

func (h *PublicHandler) track(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	p, err := h.Service.GetByToken(r.Context(), token)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "Kajian tidak ditemukan. Pastikan link yang Anda buka lengkap.")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.Renderer.Render(w, r, http.StatusOK, "public/kajian_track", web.Page{
		Title: p.No(),
		Data:  trackData{P: p, TrackURL: h.BaseURL + "/k/" + token, New: r.URL.Query().Get("baru") == "1"},
	})
}
