package maintenance

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/tickets"
	"sikat/internal/web"
)

// Handler menyediakan aksi pemeliharaan & halaman peringatan kerusakan berulang.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
	Require  func(rbac.Permission) func(http.Handler) http.Handler
	// RenderAsset merender ulang detail perangkat dengan isian & error form entri manual.
	RenderAsset func(w http.ResponseWriter, r *http.Request, assetID int64, status int, form, errs map[string]string)
}

// Routes mendaftarkan rute di bawah /app/maintenance.
func (h *Handler) Routes(r chi.Router) {
	r.With(h.Require(rbac.PermAssetView)).Get("/alerts", h.alerts)
	r.With(h.Require(rbac.PermMaintenanceRecord)).Post("/assets/{id}", h.addManual)
	r.With(h.Require(rbac.PermMaintenanceReview)).Post("/entries/{id}/void", h.void)
	r.With(h.Require(rbac.PermMaintenanceReview)).Post("/alerts/{id}/decide", h.decide)
}

type alertsData struct {
	Open      []Alert
	Closed    []Alert
	CanReview bool
}

func (h *Handler) alerts(w http.ResponseWriter, r *http.Request) {
	open, err := h.Service.Alerts(r.Context(), true, 200)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	closed, err := h.Service.Alerts(r.Context(), false, 30)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "maintenance/alerts", web.Page{
		Title: "Perangkat Sering Rusak", Nav: "alerts",
		Data: alertsData{Open: open, Closed: closed, CanReview: reqctx.UserFrom(r.Context()).Has(rbac.PermMaintenanceReview)},
	})
}

func (h *Handler) addManual(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	errs, err := h.Service.AddManual(r.Context(), reqctx.UserFrom(r.Context()), id, ManualInput{
		Type: r.PostFormValue("m_type"), Action: r.PostFormValue("m_action"), Result: r.PostFormValue("m_result"),
		StartedAt: r.PostFormValue("m_started_at"), FinishedAt: r.PostFormValue("m_finished_at"),
		ServiceCost: r.PostFormValue("m_service_cost"), StatusAfter: r.PostFormValue("m_status_after"),
		Notes: r.PostFormValue("m_notes"), Parts: tickets.PartLinesFrom(r),
	})
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, ErrForbidden):
		h.Renderer.Error(w, r, http.StatusForbidden, "")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		h.RenderAsset(w, r, id, http.StatusUnprocessableEntity, web.FormValues(r), errs)
	default:
		h.Renderer.Redirect(w, r, "/app/assets/"+strconv.FormatInt(id, 10)+"#riwayat", "success", "Riwayat pemeliharaan dicatat.")
	}
}

func (h *Handler) void(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	assetID, errs, err := h.Service.Void(r.Context(), reqctx.UserFrom(r.Context()), id, r.PostFormValue("void_reason"))
	back := "/app/assets/" + strconv.FormatInt(assetID, 10) + "#riwayat"
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		h.Renderer.Redirect(w, r, backOr(r, back), "error", errs["void_reason"])
	default:
		h.Renderer.Redirect(w, r, back, "success", "Entri riwayat dibatalkan dan tidak dihitung lagi di statistik.")
	}
}

func backOr(r *http.Request, fallback string) string {
	if ret := r.PostFormValue("return"); len(ret) > 5 && ret[:5] == "/app/" {
		return ret
	}
	return fallback
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	assetID, errs, err := h.Service.Decide(r.Context(), reqctx.UserFrom(r.Context()), id, r.PostFormValue("decision"), r.PostFormValue("decision_note"))
	back := backOr(r, "/app/assets/"+strconv.FormatInt(assetID, 10))
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		msg := errs["decision"]
		if msg == "" {
			msg = errs["decision_note"]
		}
		h.Renderer.Redirect(w, r, back, "error", msg)
	default:
		h.Renderer.Redirect(w, r, back, "success", "Keputusan dicatat di riwayat perangkat.")
	}
}
