package backup

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"sikat/internal/audit"
	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyajikan riwayat backup dan tombol backup/uji restore (Admin).
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
}

// Routes di bawah /app/backups (config.manage).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/run", h.run)
	r.Post("/restore-test", h.restoreTest)
}

type listData struct {
	Logs   []Log
	Config Config
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	logs, err := h.Service.Logs(r.Context(), 60)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "backups/list", web.Page{Title: "Backup", Nav: "backups",
		Data: listData{Logs: logs, Config: h.Service.Config()}})
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	id, err := h.Service.Run(r.Context(), "MANUAL", u.ID)
	_ = audit.Record(r.Context(), h.Service.db, h.Service.clock.Now(), audit.Entry{Action: "backup.run", EntityType: "backup_logs", EntityID: id,
		After: map[string]any{"ok": err == nil}})
	if err != nil {
		h.Renderer.Redirect(w, r, "/app/backups", "error", "Backup gagal: "+err.Error())
		return
	}
	h.Renderer.Redirect(w, r, "/app/backups", "success", "Backup selesai dibuat.")
}

func (h *Handler) restoreTest(w http.ResponseWriter, r *http.Request) {
	msg, err := h.Service.RestoreTest(r.Context(), 0)
	_ = audit.Record(r.Context(), h.Service.db, h.Service.clock.Now(), audit.Entry{Action: "backup.restore_test", EntityType: "backup_logs",
		After: map[string]any{"ok": err == nil}})
	switch {
	case errors.Is(err, ErrNoBackup):
		h.Renderer.Redirect(w, r, "/app/backups", "error", "Belum ada backup berhasil untuk diuji.")
	case err != nil:
		h.Renderer.Redirect(w, r, "/app/backups", "error", "Uji restore gagal: "+err.Error())
	default:
		h.Renderer.Redirect(w, r, "/app/backups", "success", msg)
	}
}
