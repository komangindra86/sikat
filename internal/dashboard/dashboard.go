// Package dashboard menampilkan halaman beranda sesuai role. Pada PHASE 1
// isinya ringkasan master data & perangkat; ringkasan tiket, SLA, dan kinerja
// ditambahkan pada fase terkait.
package dashboard

import (
	"context"
	"database/sql"
	"net/http"

	"sikat/internal/assets"
	"sikat/internal/audit"
	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/tickets"
	"sikat/internal/web"
)

// Summary adalah angka ringkasan dari database.
type Summary struct {
	ActiveUsers   int
	Departments   int
	Rooms         int
	AssetsTotal   int
	AssetsByState []assets.StatusCount
	ITCategories  int
	TicketKinds   int
	RecentAudit   []audit.Log
	Tickets       tickets.Summary
	ShowTickets   bool
}

// Handler menampilkan beranda.
type Handler struct {
	DB       *sql.DB
	Assets   *assets.Service
	Tickets  *tickets.Service
	Renderer *web.Renderer
}

func count(ctx context.Context, db *sql.DB, q string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, q).Scan(&n)
	return n, err
}

func (h *Handler) summary(ctx context.Context, u *rbac.User) (Summary, error) {
	var s Summary
	queries := []struct {
		dst *int
		q   string
	}{
		{&s.ActiveUsers, `SELECT COUNT(*) FROM users WHERE is_active = TRUE`},
		{&s.Departments, `SELECT COUNT(*) FROM departments WHERE is_active = TRUE`},
		{&s.Rooms, `SELECT COUNT(*) FROM rooms WHERE is_active = TRUE`},
		{&s.ITCategories, `SELECT COUNT(*) FROM it_categories WHERE is_active = TRUE`},
		{&s.TicketKinds, `SELECT COUNT(*) FROM ticket_categories WHERE is_active = TRUE`},
	}
	for _, q := range queries {
		n, err := count(ctx, h.DB, q.q)
		if err != nil {
			return s, err
		}
		*q.dst = n
	}
	byState, err := h.Assets.StatusCounts(ctx)
	if err != nil {
		return s, err
	}
	s.AssetsByState = byState
	for _, b := range byState {
		if b.Status != assets.StatusDisposed {
			s.AssetsTotal += b.Count
		}
	}
	if u.Has(rbac.PermTicketQueue) && h.Tickets != nil {
		ts, err := h.Tickets.Summary(ctx, u.ID)
		if err != nil {
			return s, err
		}
		s.Tickets, s.ShowTickets = ts, true
	}
	if u.Has(rbac.PermAuditView) {
		logs, _, err := audit.List(ctx, h.DB, audit.Filter{Limit: 8})
		if err != nil {
			return s, err
		}
		s.RecentAudit = logs
	}
	return s, nil
}

// Home menampilkan beranda.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	u := reqctx.UserFrom(r.Context())
	s, err := h.summary(r.Context(), u)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "dashboard/home", web.Page{Title: "Beranda", Nav: "home", Data: s})
}

// AuditHandler menampilkan jejak audit untuk Admin IT.
type AuditHandler struct {
	DB       *sql.DB
	Renderer *web.Renderer
}

type auditData struct {
	Logs   []audit.Log
	Pager  web.Pager
	Action string
}

// List menampilkan log audit.
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	pager := web.NewPager(r, 50)
	action := r.URL.Query().Get("action")
	logs, total, err := audit.List(r.Context(), h.DB, audit.Filter{Action: action, Limit: pager.PerPage, Offset: pager.Offset()})
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager.Total = total
	h.Renderer.Render(w, r, http.StatusOK, "dashboard/audit", web.Page{
		Title: "Jejak Audit", Nav: "audit", Data: auditData{Logs: logs, Pager: pager, Action: action},
	})
}
