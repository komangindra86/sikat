package integration

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyajikan halaman integrasi (/app/integrations, khusus integration.manage).
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
}

// Routes mendaftarkan rute.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.index)
	r.Get("/template/{entity}", h.template)
	r.Post("/imports", h.upload)
	r.Get("/imports/{id}", h.preview)
	r.Post("/imports/{id}/apply", h.apply)
	r.Post("/imports/{id}/cancel", h.cancel)
	r.Get("/logs", h.logs)
	r.Get("/logs/{id}", h.logDetail)
	r.Post("/config/{entity}", h.config)
	r.Post("/sync/{entity}", h.sync)
}

// EntityInfo menjelaskan entitas untuk halaman.
type EntityInfo struct {
	Code         string
	Columns      []string
	Required     map[string]bool
	Help         map[string]string
	APIAvailable bool
	DBAvailable  bool
}

func (h *Handler) entities() []EntityInfo {
	var out []EntityInfo
	for _, e := range Entities {
		cols, req, help := CSVColumns(e)
		rm := map[string]bool{}
		for _, c := range req {
			rm[c] = true
		}
		out = append(out, EntityInfo{Code: e, Columns: cols, Required: rm, Help: help,
			APIAvailable: h.Service.ProviderAvailable(ProviderSimrsAPI), DBAvailable: h.Service.ProviderAvailable(ProviderSimrsDB)})
	}
	return out
}

type indexData struct {
	Statuses []Status
	Entities []EntityInfo
	Logs     []LogEntry
	Form     map[string]string
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.renderIndex(w, r, http.StatusOK, nil, nil)
}

func (h *Handler) renderIndex(w http.ResponseWriter, r *http.Request, status int, form, errs map[string]string) {
	st, err := h.Service.Statuses(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	logs, err := h.Service.Logs(r.Context(), "", 8)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if form == nil {
		form = map[string]string{"entity": EntityDepartment}
	}
	h.Renderer.Render(w, r, status, "integrations/index", web.Page{Title: "Integrasi", Nav: "integrations", Errors: errs,
		Data: indexData{Statuses: st, Entities: h.entities(), Logs: logs, Form: form}})
}

func (h *Handler) template(w http.ResponseWriter, r *http.Request) {
	e := strings.TrimSuffix(strings.ToUpper(chi.URLParam(r, "entity")), ".CSV")
	if !ValidEntity(e) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sikat-template-`+strings.ToLower(e)+`.csv"`)
	_, _ = w.Write(Template(e))
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(MaxCSVBytes + 1<<20); err != nil {
		h.renderIndex(w, r, http.StatusUnprocessableEntity, nil, map[string]string{"file": "File terlalu besar atau tidak terbaca (maks. 5 MB)."})
		return
	}
	form := map[string]string{"entity": r.PostFormValue("entity"), "mark_missing": r.PostFormValue("mark_missing"), "policy": r.PostFormValue("policy")}
	errs := map[string]string{}
	if !ValidEntity(form["entity"]) {
		errs["entity"] = "Pilih jenis data."
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		errs["file"] = "Pilih file CSV."
	}
	if len(errs) > 0 {
		h.renderIndex(w, r, http.StatusUnprocessableEntity, form, errs)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxCSVBytes+1))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	u := reqctx.UserFrom(r.Context())
	id, err := h.Service.CreateImport(r.Context(), u.ID, form["entity"], hdr.Filename, data, form["mark_missing"] == "1", form["policy"])
	if err != nil {
		h.renderIndex(w, r, http.StatusUnprocessableEntity, form, map[string]string{"file": err.Error()})
		return
	}
	http.Redirect(w, r, "/app/integrations/imports/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

type previewData struct {
	Import Import
	Result Result
	Rows   []RowResult
	More   int
}

const previewMaxRows = 500

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	im, err := h.Service.GetImport(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if im.Status != "PREVIEW" {
		if im.LogID.Valid {
			http.Redirect(w, r, "/app/integrations/logs/"+strconv.FormatInt(im.LogID.Int64, 10), http.StatusSeeOther)
			return
		}
		h.Renderer.Redirect(w, r, "/app/integrations", "info", "Impor ini sudah dibatalkan.")
		return
	}
	res, err := h.Service.Preview(r.Context(), im)
	if err != nil {
		h.Renderer.Redirect(w, r, "/app/integrations", "error", err.Error())
		return
	}
	d := previewData{Import: im, Result: res, Rows: res.Rows}
	if len(d.Rows) > previewMaxRows {
		d.More, d.Rows = len(d.Rows)-previewMaxRows, d.Rows[:previewMaxRows]
	}
	h.Renderer.Render(w, r, http.StatusOK, "integrations/preview", web.Page{Title: "Pratinjau impor", Nav: "integrations", Data: d})
}

func (h *Handler) apply(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	logID, err := h.Service.ApplyImport(r.Context(), id, reqctx.UserFrom(r.Context()).ID)
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, ErrImportClosed):
		h.Renderer.Redirect(w, r, "/app/integrations", "error", err.Error())
	case err != nil && logID == 0:
		h.Renderer.ServerError(w, r, err)
	default:
		msg := "Impor diterapkan."
		kind := "success"
		if err != nil {
			msg, kind = "Impor gagal: "+err.Error(), "error"
		}
		h.Renderer.Redirect(w, r, "/app/integrations/logs/"+strconv.FormatInt(logID, 10), kind, msg)
	}
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err := h.Service.CancelImport(r.Context(), id); err != nil && !errors.Is(err, ErrImportClosed) {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Redirect(w, r, "/app/integrations", "info", "Impor dibatalkan; tidak ada data yang diubah.")
}

type logsData struct {
	Logs   []LogEntry
	Entity string
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	e := r.URL.Query().Get("entitas")
	if !ValidEntity(e) {
		e = ""
	}
	logs, err := h.Service.Logs(r.Context(), e, 100)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "integrations/logs", web.Page{Title: "Log sinkronisasi", Nav: "integrations", Data: logsData{Logs: logs, Entity: e}})
}

func (h *Handler) logDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	l, err := h.Service.Log(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "integrations/log", web.Page{Title: "Log sinkronisasi", Nav: "integrations", Data: l})
}

func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	e := chi.URLParam(r, "entity")
	sched, _ := strconv.Atoi(r.PostFormValue("schedule_minutes"))
	err := h.Service.SetProvider(r.Context(), reqctx.UserFrom(r.Context()).ID, e, r.PostFormValue("provider"), sched, r.PostFormValue("policy"))
	if errors.Is(err, ErrUnsupported) {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	if err != nil {
		h.Renderer.Redirect(w, r, "/app/integrations", "error", err.Error())
		return
	}
	h.Renderer.Redirect(w, r, "/app/integrations", "success", "Sumber data disimpan.")
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	e := chi.URLParam(r, "entity")
	if !ValidEntity(e) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	logID, err := h.Service.Sync(r.Context(), e, "MANUAL", reqctx.UserFrom(r.Context()).ID)
	if logID == 0 {
		msg := "Sinkronisasi tidak dijalankan."
		if err != nil {
			msg = err.Error()
		}
		h.Renderer.Redirect(w, r, "/app/integrations", "info", msg)
		return
	}
	kind, msg := "success", "Sinkronisasi selesai."
	if err != nil {
		kind, msg = "error", "Sinkronisasi gagal; dijadwalkan ulang otomatis. "+err.Error()
	}
	h.Renderer.Redirect(w, r, "/app/integrations/logs/"+strconv.FormatInt(logID, 10), kind, msg)
}
