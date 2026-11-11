package tickets

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"sikat/internal/middleware"
	"sikat/internal/qrcodes"
	"sikat/internal/reqctx"
	"sikat/internal/settings"
	"sikat/internal/web"
)

// PublicHandler melayani pelapor tanpa login: form dari scan QR dan halaman
// pemantauan tiket berbasis token. Tidak ada tautan ke halaman internal.
type PublicHandler struct {
	Service   *Service
	QR        *qrcodes.Service
	Settings  *settings.Service
	Renderer  *web.Renderer
	BaseURL   string
	IPLimiter *middleware.RateLimiter // batas kiriman per IP
	QRLimiter *middleware.RateLimiter // batas kiriman per QR (lokasi)
}

// Routes mendaftarkan rute publik.
func (h *PublicHandler) Routes(r chi.Router) {
	r.Get("/q/{token}", h.form)
	r.Post("/q/{token}", h.submit)
	r.Get("/t/{token}", h.track)
	r.Get("/t/{token}/foto/{id}", h.photo)
}

type reportData struct {
	Token       string
	Target      qrcodes.Target
	Categories  []Category
	Departments []Option
	Assets      []Option
	Form        map[string]string
	MaxFiles    int
	MaxMB       int
	Incident    []struct{ Value, Label string }
}

type invalidData struct {
	Inactive bool
}

type trackData struct {
	Ticket      Ticket
	Histories   []History
	Attachments []Attachment
	Token       string
	TrackURL    string
	New         bool
}

func (h *PublicHandler) target(w http.ResponseWriter, r *http.Request) (qrcodes.Target, string, bool) {
	token := chi.URLParam(r, "token")
	t, err := h.QR.Resolve(r.Context(), token)
	switch {
	case errors.Is(err, qrcodes.ErrNotFound):
		h.Renderer.Render(w, r, http.StatusNotFound, "public/qr_invalid", web.Page{Title: "QR tidak dikenal", Data: invalidData{}})
		return t, token, false
	case errors.Is(err, qrcodes.ErrInactive):
		h.Renderer.Render(w, r, http.StatusGone, "public/qr_invalid", web.Page{Title: "QR tidak berlaku", Data: invalidData{Inactive: true}})
		return t, token, false
	case err != nil:
		h.Renderer.ServerError(w, r, err)
		return t, token, false
	}
	return t, token, true
}

func (h *PublicHandler) renderForm(w http.ResponseWriter, r *http.Request, status int, t qrcodes.Target, token string, form, errs map[string]string) {
	ctx := r.Context()
	cats, err := h.Service.Categories(ctx, t.AssetITCategoryID.Int64)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	depts, err := h.Service.Departments(ctx)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	var assets []Option
	if t.Type == qrcodes.TypeRoom && t.RoomID.Valid {
		if assets, err = h.Service.RoomAssets(ctx, t.RoomID.Int64); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	if form == nil {
		form = map[string]string{}
	}
	if _, ok := form["reporter_department_id"]; !ok && t.DepartmentID.Valid {
		form["reporter_department_id"] = strconv.FormatInt(t.DepartmentID.Int64, 10)
	}
	h.Renderer.Render(w, r, status, "public/report", web.Page{
		Title: "Lapor Gangguan IT", Errors: errs,
		Data: reportData{
			Token: token, Target: t, Categories: cats, Departments: depts, Assets: assets, Form: form,
			MaxFiles: h.Settings.Int(ctx, settings.KeyUploadMaxFiles), MaxMB: h.Settings.Int(ctx, settings.KeyUploadMaxMB),
			Incident: IncidentOptions,
		},
	})
}

func (h *PublicHandler) form(w http.ResponseWriter, r *http.Request) {
	t, token, ok := h.target(w, r)
	if !ok {
		return
	}
	_ = h.QR.RecordScan(r.Context(), t.QRID) // statistik, tidak kritis
	h.renderForm(w, r, http.StatusOK, t, token, nil, nil)
}

func (h *PublicHandler) submit(w http.ResponseWriter, r *http.Request) {
	t, token, ok := h.target(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if !h.IPLimiter.Allow(reqctx.InfoFrom(ctx).IP) {
		h.Renderer.Error(w, r, http.StatusTooManyRequests, "")
		return
	}
	if !h.QRLimiter.Allow("qr:" + token) {
		h.Renderer.Error(w, r, http.StatusTooManyRequests,
			"Sudah banyak laporan dari lokasi ini dalam beberapa menit terakhir. Petugas IT sudah menerima laporan tersebut; mohon tunggu sebentar.")
		return
	}
	// Honeypot: field tersembunyi yang hanya diisi bot.
	if r.PostFormValue("website") != "" {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}

	d, form, errs := ParseCommon(r.PostFormValue, h.Service.clock.Now())
	d.Channel = ChannelQR
	d.QRCodeID = t.QRID
	d.DepartmentID = t.DepartmentID.Int64
	d.RoomID = t.RoomID.Int64
	if t.Type == qrcodes.TypeAsset {
		d.AssetID = t.AssetID.Int64 // perangkat dari QR, bukan dari form
	} else {
		d.RoomFromQR = true
	}

	maxFiles := h.Settings.Int(ctx, settings.KeyUploadMaxFiles)
	maxBytes := int64(h.Settings.Int(ctx, settings.KeyUploadMaxMB)) << 20
	photos, photoErr, err := parsePhotos(r, maxFiles, maxBytes)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if photoErr != "" {
		errs["photos"] = photoErr
	}
	if len(errs) > 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, t, token, form, errs)
		return
	}
	created, fieldErrs, err := h.Service.Create(ctx, d, photos)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(fieldErrs) > 0 {
		h.renderForm(w, r, http.StatusUnprocessableEntity, t, token, form, fieldErrs)
		return
	}
	http.Redirect(w, r, "/t/"+created.Token+"?baru=1", http.StatusSeeOther)
}

func (h *PublicHandler) track(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	t, hist, atts, err := h.Service.GetPublic(r.Context(), token)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "Tiket tidak ditemukan. Pastikan link yang Anda buka lengkap.")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.Renderer.Render(w, r, http.StatusOK, "public/track", web.Page{
		Title: t.TicketNo,
		Data: trackData{Ticket: t, Histories: hist, Attachments: atts, Token: token,
			TrackURL: h.BaseURL + "/t/" + token, New: r.URL.Query().Get("baru") == "1"},
	})
}

func (h *PublicHandler) photo(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	key, err := h.Service.PublicAttachmentKey(r.Context(), chi.URLParam(r, "token"), id)
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if err := serveAttachment(w, r, h.Service.Store(), key); err != nil {
		h.Renderer.ServerError(w, r, err)
	}
}
