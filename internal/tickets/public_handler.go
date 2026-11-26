package tickets

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"sikat/internal/middleware"
	"sikat/internal/qrcodes"
	"sikat/internal/reqctx"
	"sikat/internal/settings"
	"sikat/internal/web"
	"sikat/internal/workflows"
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
	// Employees (opsional): nama pegawai SIMRS untuk saran nama pelapor. Form publik
	// tidak memuat seluruh daftar; nama dicari per ketikan (min. 3 huruf, maks. 8 hasil).
	Employees      EmployeeSource
	SuggestLimiter *middleware.RateLimiter // batas pencarian nama per IP
}

// Routes mendaftarkan rute publik.
func (h *PublicHandler) Routes(r chi.Router) {
	r.Get("/q/{token}", h.form)
	r.Post("/q/{token}", h.submit)
	r.Get("/q/{token}/ruangan", h.rooms)
	r.Get("/q/{token}/pegawai", h.employees)
	r.Get("/t/{token}", h.track)
	r.Get("/t/{token}/foto/{id}", h.photo)
	r.Post("/t/{token}/konfirmasi", h.feedback)
	r.Post("/t/{token}/keterangan", h.comment)
}

type reportData struct {
	Token       string
	Target      qrcodes.Target
	Groups      []CategoryGroup // jenis gangguan per bidang, Triase terakhir
	OpenField   int64           // bidang yang terbuka saat form tampil
	Departments []Option
	Rooms       []Option // QR global: ruangan unit lokasi terpilih
	Assets      []Option
	Form        map[string]string
	MaxFiles    int
	MaxMB       int
	Incident    []struct{ Value, Label string }
	Suggest     bool     // saran nama pegawai tersedia
	Names       []string // hasil pencarian nama (fragmen HTMX)
}

type invalidData struct {
	Inactive bool
}

type trackData struct {
	Ticket      Ticket
	Histories   []History
	Attachments []Attachment
	Comments    []Comment
	Token       string
	TrackURL    string
	New         bool
	Form        map[string]string
	Open        string
}

// Polling: halaman pemantauan masih memeriksa perubahan status. Berhenti saat tiket
// Selesai (kartu konfirmasi tampil) atau berakhir, agar isian pelapor tidak terhapus.
func (d trackData) Polling() bool {
	return d.Ticket.Status != workflows.Resolved && !workflows.IsTerminal(d.Ticket.Status)
}

// CanComment: pelapor masih dapat menambah keterangan.
func (d trackData) CanComment() bool { return !workflows.IsTerminal(d.Ticket.Status) }

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
	cats, err := h.Service.Categories(ctx, 0)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if form == nil {
		form = map[string]string{}
	}
	open := t.AssetITCategoryID.Int64 // QR perangkat: bidang perangkat terbuka lebih dulu
	if sel, _ := strconv.ParseInt(form["ticket_category_id"], 10, 64); sel != 0 {
		for _, c := range cats {
			if c.ID == sel {
				open = c.ITCategoryID
			}
		}
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
	var rooms []Option
	if t.Type == qrcodes.TypeGlobal {
		dept, _ := strconv.ParseInt(form["department_id"], 10, 64)
		if rooms, err = h.Service.DepartmentRooms(ctx, dept); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	if _, ok := form["reporter_department_id"]; !ok && t.DepartmentID.Valid {
		form["reporter_department_id"] = strconv.FormatInt(t.DepartmentID.Int64, 10)
	}
	h.Renderer.Render(w, r, status, "public/report", web.Page{
		Title: "Lapor Gangguan IT", Errors: errs,
		Data: reportData{
			Token: token, Target: t, Groups: groupCategories(cats), OpenField: open, Departments: depts, Rooms: rooms, Assets: assets, Form: form,
			MaxFiles: h.Settings.Int(ctx, settings.KeyUploadMaxFiles), MaxMB: h.Settings.Int(ctx, settings.KeyUploadMaxMB),
			Incident: IncidentOptions, Suggest: h.Employees != nil,
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
	limitKey := "qr:" + token
	if t.Type == qrcodes.TypeGlobal {
		limitKey += ":" + reqctx.InfoFrom(ctx).IP // QR global dipakai seluruh rumah sakit
	}
	if !h.QRLimiter.Allow(limitKey) {
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
	switch t.Type {
	case qrcodes.TypeAsset:
		d.AssetID = t.AssetID.Int64 // perangkat dari QR, bukan dari form
	case qrcodes.TypeGlobal:
		parseReporterLocation(r.PostFormValue, &d, form, errs) // pelapor memilih lokasi sendiri
	default:
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

// rooms mengembalikan <option> ruangan untuk unit terpilih di form QR global (HTMX).
func (h *PublicHandler) rooms(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.target(w, r)
	if !ok {
		return
	}
	if t.Type != qrcodes.TypeGlobal {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	dept, _ := strconv.ParseInt(r.URL.Query().Get("department_id"), 10, 64)
	rooms, err := h.Service.DepartmentRooms(r.Context(), dept)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "public/report", "room-options", web.Page{
		Data: reportData{Rooms: rooms, Form: map[string]string{}},
	})
}

// Batas pencarian nama pegawai di form publik.
const (
	SuggestMinChars = 3
	SuggestMax      = 8
)

// employees mengembalikan <option> nama pegawai SIMRS yang cocok dengan ketikan
// pelapor (HTMX). Hanya lewat QR aktif; di bawah 3 huruf atau melewati batas
// per IP, hasilnya kosong (form tetap bisa diisi bebas).
func (h *PublicHandler) employees(w http.ResponseWriter, r *http.Request) {
	if h.Employees == nil {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if _, _, ok := h.target(w, r); !ok {
		return
	}
	var names []string
	q := strings.TrimSpace(r.URL.Query().Get("reporter_name"))
	if utf8.RuneCountInString(q) >= SuggestMinChars && utf8.RuneCountInString(q) <= 120 &&
		(h.SuggestLimiter == nil || h.SuggestLimiter.Allow(reqctx.InfoFrom(r.Context()).IP)) {
		names = MatchNames(h.Employees.Names(r.Context()), q, SuggestMax)
	}
	w.Header().Set("Cache-Control", "no-store")
	h.Renderer.Fragment(w, r, http.StatusOK, "public/report", "employee-options", web.Page{
		Data: reportData{Names: names, Form: map[string]string{}},
	})
}

func (h *PublicHandler) renderTrack(w http.ResponseWriter, r *http.Request, status int, open string, form, errs map[string]string) {
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
	comments, err := h.Service.Comments(r.Context(), t.ID, true)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if form == nil {
		form = map[string]string{}
	}
	w.Header().Set("Cache-Control", "no-store")
	h.Renderer.Render(w, r, status, "public/track", web.Page{
		Title: t.TicketNo, Errors: errs,
		Data: trackData{Ticket: t, Histories: hist, Attachments: atts, Comments: comments, Token: token,
			TrackURL: h.BaseURL + "/t/" + token, New: r.URL.Query().Get("baru") == "1", Form: form, Open: open},
	})
}

func (h *PublicHandler) track(w http.ResponseWriter, r *http.Request) {
	h.renderTrack(w, r, http.StatusOK, "", nil, nil)
}

// publicAct menjalankan aksi pelapor dengan pembatasan laju & penanganan error seragam.
func (h *PublicHandler) publicAct(w http.ResponseWriter, r *http.Request, open, success string, fn func(token string) (map[string]string, error)) {
	if !h.IPLimiter.Allow(reqctx.InfoFrom(r.Context()).IP) {
		h.Renderer.Error(w, r, http.StatusTooManyRequests, "")
		return
	}
	token := chi.URLParam(r, "token")
	errs, err := fn(token)
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "Tiket tidak ditemukan.")
	case errors.Is(err, ErrTransition):
		h.Renderer.Redirect(w, r, "/t/"+token, "error", "Status tiket sudah berubah. Halaman telah diperbarui.")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		h.renderTrack(w, r, http.StatusUnprocessableEntity, open, web.FormValues(r), errs)
	default:
		h.Renderer.Redirect(w, r, "/t/"+token, "success", success)
	}
}

func (h *PublicHandler) feedback(w http.ResponseWriter, r *http.Request) {
	ok := r.PostFormValue("ok") == "1"
	msg := "Terima kasih. Tiket ditutup."
	if !ok {
		msg = "Terima kasih. Tiket dibuka kembali dan petugas akan menindaklanjuti."
	}
	h.publicAct(w, r, "feedback", msg, func(token string) (map[string]string, error) {
		return h.Service.ReporterFeedback(r.Context(), token, ok, r.PostFormValue("feedback_note"))
	})
}

func (h *PublicHandler) comment(w http.ResponseWriter, r *http.Request) {
	h.publicAct(w, r, "comment", "Keterangan terkirim ke petugas IT.", func(token string) (map[string]string, error) {
		return h.Service.AddReporterComment(r.Context(), token, r.PostFormValue("comment"))
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
