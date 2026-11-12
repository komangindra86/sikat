package notifications

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// sessionKey menyimpan ID langganan push perangkat yang sedang login, agar
// langganan itu dihapus saat logout (perangkat bersama tidak menerima notifikasi
// pengguna sebelumnya).
const sessionKey = "push_subscription_id"

// Handler menyajikan inbox, pengaturan perangkat push, dan preferensi per role.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
	Sessions *scs.SessionManager
}

// Routes mendaftarkan rute di bawah /app/notifications (semua pengguna login).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.inbox)
	r.Get("/badge", h.badge)
	r.Post("/read-all", h.readAll)
	r.Get("/settings", h.settings)
	r.Post("/push/subscribe", h.subscribe)
	r.Post("/push/unsubscribe", h.unsubscribe)
	r.Post("/push/test", h.test)
	r.Post("/devices/{id}/delete", h.removeDevice)
	r.Get("/{id}", h.open)
}

// AdminRoutes: preferensi notifikasi per role (/app/settings/notifications, config.manage).
func (h *Handler) AdminRoutes(r chi.Router) {
	r.Get("/", h.preferences)
	r.Post("/", h.savePreferences)
}

type inboxData struct {
	Items      []Item
	More       bool
	UnreadOnly bool
	NextBefore int64
	Unread     int
	PushOn     bool
}

func (h *Handler) inbox(w http.ResponseWriter, r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	unreadOnly := r.URL.Query().Get("f") == "unread"
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	items, more, err := h.Service.List(r.Context(), u.ID, unreadOnly, before)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	d := inboxData{Items: items, More: more, UnreadOnly: unreadOnly, Unread: h.Service.Unread(r.Context(), u.ID), PushOn: h.Service.PushEnabled()}
	if more && len(items) > 0 {
		d.NextBefore = items[len(items)-1].ID
	}
	h.Renderer.Render(w, r, http.StatusOK, "notifications/inbox", web.Page{Title: "Notifikasi", Nav: "notifications", Data: d})
}

// badge: jumlah belum dibaca untuk app.js (polling & pesan dari service worker).
func (h *Handler) badge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]int{"unread": h.Service.Unread(r.Context(), reqctx.UserFrom(r.Context()).ID)})
}

func (h *Handler) readAll(w http.ResponseWriter, r *http.Request) {
	n, err := h.Service.ReadAll(r.Context(), reqctx.UserFrom(r.Context()).ID)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	msg := "Tidak ada notifikasi yang belum dibaca."
	if n > 0 {
		msg = strconv.FormatInt(n, 10) + " notifikasi ditandai sudah dibaca."
	}
	h.Renderer.Redirect(w, r, "/app/notifications", "success", msg)
}

func (h *Handler) open(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	target, err := h.Service.Open(r.Context(), reqctx.UserFrom(r.Context()).ID, id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "Notifikasi tidak ditemukan.")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

type settingsData struct {
	PushOn    bool
	VAPIDKey  string
	Devices   []Device
	Events    []EventInfo
	Channels  map[string]string
	RoleName  string
	CanConfig bool
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	devices, err := h.Service.Devices(r.Context(), u.ID)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	prefs, err := h.Service.RolePreferences(r.Context(), u.RoleCode)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "notifications/settings", web.Page{
		Title: "Pengaturan Notifikasi", Nav: "notifications",
		Data: settingsData{PushOn: h.Service.PushEnabled(), VAPIDKey: h.Service.VAPIDPublicKey(), Devices: devices,
			Events: Events, Channels: prefs, RoleName: u.RoleName, CanConfig: u.Can("config.manage")},
	})
}

// jsonError menjawab request fetch dari app.js.
func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	if !h.Service.PushEnabled() {
		jsonError(w, http.StatusServiceUnavailable, "Notifikasi push belum dikonfigurasi di server.")
		return
	}
	var in SubscribeInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "Data langganan tidak valid.")
		return
	}
	in.UserAgent = r.UserAgent()
	u := reqctx.UserFrom(r.Context())
	id, err := h.Service.Subscribe(r.Context(), u.ID, in)
	if errors.Is(err, ErrInvalidSubscription) {
		jsonError(w, http.StatusBadRequest, "Data langganan tidak valid.")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Sessions.Put(r.Context(), sessionKey, id)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": id})
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&in); err != nil || in.Endpoint == "" {
		jsonError(w, http.StatusBadRequest, "Data tidak valid.")
		return
	}
	if err := h.Service.Unsubscribe(r.Context(), reqctx.UserFrom(r.Context()).ID, in.Endpoint); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Sessions.Remove(r.Context(), sessionKey)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	n, err := h.Service.SendTest(r.Context(), reqctx.UserFrom(r.Context()).ID)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if n == 0 {
		h.Renderer.Redirect(w, r, "/app/notifications/settings", "error", "Belum ada perangkat aktif. Aktifkan notifikasi di perangkat ini terlebih dahulu.")
		return
	}
	h.Renderer.Redirect(w, r, "/app/notifications/settings", "success",
		"Notifikasi uji dikirim ke "+strconv.Itoa(n)+" perangkat. Biasanya tiba dalam beberapa detik.")
}

func (h *Handler) removeDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err := h.Service.RemoveDevice(r.Context(), reqctx.UserFrom(r.Context()).ID, id); err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if h.Sessions.GetInt64(r.Context(), sessionKey) == id {
		h.Sessions.Remove(r.Context(), sessionKey)
	}
	h.Renderer.Redirect(w, r, "/app/notifications/settings", "success", "Perangkat dihapus dari daftar penerima notifikasi.")
}

// BeforeLogout menghapus langganan push perangkat yang dipakai login ini.
func (h *Handler) BeforeLogout(r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	id := h.Sessions.GetInt64(r.Context(), sessionKey)
	if u == nil || id == 0 {
		return
	}
	_ = h.Service.RemoveDevice(r.Context(), u.ID, id)
}

type prefsData struct {
	Events []EventInfo
	Roles  []RolePrefs
	PushOn bool
}

func (h *Handler) preferences(w http.ResponseWriter, r *http.Request) {
	h.renderPrefs(w, r, http.StatusOK, nil)
}

func (h *Handler) renderPrefs(w http.ResponseWriter, r *http.Request, status int, errs map[string]string) {
	roles, err := h.Service.Preferences(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, status, "notifications/preferences", web.Page{
		Title: "Preferensi Notifikasi", Nav: "settings", Errors: errs,
		Data: prefsData{Events: Events, Roles: roles, PushOn: h.Service.PushEnabled()},
	})
}

func (h *Handler) savePreferences(w http.ResponseWriter, r *http.Request) {
	errs, err := h.Service.SavePreferences(r.Context(), reqctx.UserFrom(r.Context()), r.PostFormValue)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(errs) > 0 {
		h.renderPrefs(w, r, http.StatusUnprocessableEntity, errs)
		return
	}
	h.Renderer.Redirect(w, r, "/app/settings/notifications", "success", "Preferensi notifikasi disimpan.")
}
