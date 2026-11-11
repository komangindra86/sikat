package qrcodes

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyediakan halaman pengelolaan QR.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
}

// Routes mendaftarkan rute di bawah /app/qr.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/generate", h.generate)
	r.Post("/generate-missing", h.generateMissing)
	r.Get("/print", h.print)
	r.Get("/zip", h.zip)
	r.Get("/{id}", h.detail)
	r.Get("/{id}/png", h.png)
	r.Post("/{id}/deactivate", h.deactivate)
	r.Post("/{id}/regenerate", h.regenerate)
}

type listData struct {
	QRs          []QR
	Pager        web.Pager
	Filter       Filter
	MissingRooms int
	MissingAsset int
	Rooms        []RoomOption
}

type detailData struct {
	QR  QR
	URL string
}

type printData struct {
	QRs []QR
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pager := web.NewPager(r, 50)
	f := Filter{Type: q.Get("type"), Search: q.Get("q"), IncludeInactive: q.Get("inactive") == "1", Limit: pager.PerPage, Offset: pager.Offset()}
	list, total, err := h.Service.List(r.Context(), f)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager.Total = total
	mr, ma, err := h.Service.MissingCounts(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	rooms, err := h.Service.RoomsWithoutQR(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "qr/list", web.Page{
		Title: "QR Code", Nav: "master",
		Data: listData{QRs: list, Pager: pager, Filter: f, MissingRooms: mr, MissingAsset: ma, Rooms: rooms},
	})
}

func actor(r *http.Request) int64 { return reqctx.UserFrom(r.Context()).ID }

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	typ := r.PostFormValue("type")
	target, _ := strconv.ParseInt(r.PostFormValue("target_id"), 10, 64)
	id, err := h.Service.Generate(r.Context(), actor(r), typ, target)
	switch {
	case errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrTargetInvalid):
		h.Renderer.Redirect(w, r, backURL(r), "error", err.Error())
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	default:
		h.Renderer.Redirect(w, r, "/app/qr/"+strconv.FormatInt(id, 10), "success", "QR berhasil dibuat.")
	}
}

// backURL hanya menerima path lokal agar tidak bisa dipakai untuk open redirect.
func backURL(r *http.Request) string {
	b := r.PostFormValue("return")
	if strings.HasPrefix(b, "/app/") && !strings.Contains(b, "//") {
		return b
	}
	return "/app/qr"
}

func (h *Handler) generateMissing(w http.ResponseWriter, r *http.Request) {
	typ := r.PostFormValue("type")
	n, err := h.Service.GenerateMissing(r.Context(), actor(r), typ)
	if errors.Is(err, ErrTargetInvalid) {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	what := "ruangan"
	if typ == TypeAsset {
		what = "perangkat"
	}
	if n == 0 {
		h.Renderer.Redirect(w, r, "/app/qr", "info", "Semua "+what+" sudah memiliki QR aktif.")
		return
	}
	h.Renderer.Redirect(w, r, "/app/qr?type="+typ, "success", fmt.Sprintf("%d QR %s berhasil dibuat.", n, what))
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) (QR, bool) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return QR{}, false
	}
	q, err := h.Service.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return QR{}, false
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return QR{}, false
	}
	return q, true
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	q, ok := h.load(w, r)
	if !ok {
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "qr/detail", web.Page{
		Title: "QR " + q.TargetCode, Nav: "master", Data: detailData{QR: q, URL: h.Service.URL(q.Token)},
	})
}

var unsafeFile = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func fileName(q QR) string {
	return "QR-" + strings.Trim(unsafeFile.ReplaceAllString(q.TargetCode, "-"), "-") + ".png"
}

func (h *Handler) png(w http.ResponseWriter, r *http.Request) {
	q, ok := h.load(w, r)
	if !ok {
		return
	}
	img, err := h.Service.PNG(q.Token, 512)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+fileName(q)+`"`)
	}
	_, _ = w.Write(img)
}

func parseIDs(raw string) []int64 {
	var out []int64
	for _, p := range strings.Split(raw, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
		if len(out) >= 500 {
			break
		}
	}
	return out
}

// selection mengambil QR aktif dari ?ids=1,2,3 atau semua QR aktif sesuai ?type=.
func (h *Handler) selection(r *http.Request) ([]QR, error) {
	q := r.URL.Query()
	f := Filter{Type: q.Get("type"), Limit: 500}
	if ids := parseIDs(q.Get("ids")); len(ids) > 0 {
		f.IDs = ids
	}
	list, _, err := h.Service.List(r.Context(), f)
	return list, err
}

func (h *Handler) print(w http.ResponseWriter, r *http.Request) {
	list, err := h.selection(r)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "qr/print", web.Page{Title: "Cetak QR", Data: printData{QRs: list}})
}

func (h *Handler) zip(w http.ResponseWriter, r *http.Request) {
	list, err := h.selection(r)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(list) == 0 {
		h.Renderer.Redirect(w, r, "/app/qr", "info", "Tidak ada QR aktif untuk diunduh.")
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	used := map[string]int{}
	for _, q := range list {
		img, err := h.Service.PNG(q.Token, 512)
		if err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
		name := fileName(q)
		if n := used[name]; n > 0 {
			name = strings.TrimSuffix(name, ".png") + "-" + strconv.Itoa(n+1) + ".png"
		}
		used[fileName(q)]++
		f, err := zw.Create(name)
		if err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
		if _, err := f.Write(img); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="qr-sikat.zip"`)
	_, _ = w.Write(buf.Bytes())
}

func (h *Handler) deactivate(w http.ResponseWriter, r *http.Request) {
	q, ok := h.load(w, r)
	if !ok {
		return
	}
	if err := h.Service.Deactivate(r.Context(), actor(r), q.ID); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Redirect(w, r, "/app/qr/"+strconv.FormatInt(q.ID, 10), "success", "QR dinonaktifkan. Stiker lama tidak dapat dipakai lagi.")
}

func (h *Handler) regenerate(w http.ResponseWriter, r *http.Request) {
	q, ok := h.load(w, r)
	if !ok {
		return
	}
	id, err := h.Service.Regenerate(r.Context(), actor(r), q.ID)
	switch {
	case errors.Is(err, ErrTargetInvalid), errors.Is(err, ErrAlreadyExists):
		h.Renderer.Redirect(w, r, "/app/qr/"+strconv.FormatInt(q.ID, 10), "error", err.Error())
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	default:
		h.Renderer.Redirect(w, r, "/app/qr/"+strconv.FormatInt(id, 10), "success", "QR baru dibuat. Cetak dan ganti stiker lama.")
	}
}
