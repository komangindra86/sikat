package tickets

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/settings"
	"sikat/internal/web"
	"sikat/internal/workflows"
)

// Actions menentukan tombol/form yang ditampilkan untuk pengguna saat ini.
type Actions struct {
	Claim, Start, Pend, Resume, Resolve, Release, Cancel, Close, Reopen bool
	TakeOver, Priority, Support, Comment, Photo                         bool
}

// Any bernilai true bila ada aksi alur kerja yang tersedia.
func (a Actions) Any() bool {
	return a.Start || a.Pend || a.Resume || a.Resolve || a.Release || a.Cancel || a.Close || a.Reopen || a.TakeOver
}

func computeActions(t Ticket, u *rbac.User, members []Member) Actions {
	primary := t.AssigneeID.Valid && t.AssigneeID.Int64 == u.ID
	coord := u.Has(rbac.PermTicketTakeover)
	support := false
	for _, m := range members {
		if m.UserID == u.ID && m.Role == "SUPPORT" {
			support = true
		}
	}
	can := func(a workflows.Action) bool { _, ok := workflows.Next(a, t.Status); return ok }
	work := primary && u.Has(rbac.PermTicketWork)
	teamStatus := t.Status == workflows.Claimed || t.Status == workflows.InProgress || t.Status == workflows.Pending
	return Actions{
		Claim:    u.Has(rbac.PermTicketClaim) && t.Status == workflows.New,
		Start:    work && can(workflows.Start),
		Pend:     work && can(workflows.Pend),
		Resume:   work && can(workflows.Resume),
		Resolve:  work && can(workflows.Resolve),
		Release:  work && can(workflows.Release),
		Cancel:   u.Has(rbac.PermTicketCancel) && can(workflows.Cancel),
		Close:    coord && can(workflows.Close),
		Reopen:   coord && can(workflows.Reopen),
		TakeOver: coord && teamStatus && !primary,
		Priority: u.Has(rbac.PermTicketPriority) && !workflows.IsTerminal(t.Status),
		Support:  (primary || coord) && teamStatus,
		Comment:  !workflows.IsTerminal(t.Status) || coord,
		Photo:    (primary || support || coord) && t.Status != workflows.Cancelled,
	}
}

type detailView struct {
	Ticket         Ticket
	Histories      []History
	Attachments    []Attachment
	Comments       []Comment
	Members        []Member
	Actions        Actions
	CanClaim       bool
	NewLink        string
	PendingReasons []Option
	Templates      []Template
	Staff          []Option
	SpareParts     []SparePart
	Open           string // form yang dibuka kembali setelah error validasi
	Form           map[string]string
	MaxFiles       int
	MaxMB          int
}

func (h *Handler) renderDetail(w http.ResponseWriter, r *http.Request, id int64, status int, open string, form, errs map[string]string) {
	ctx := r.Context()
	t, hist, atts, err := h.Service.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	u := reqctx.UserFrom(ctx)
	members, err := h.Service.Members(ctx, id)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	comments, err := h.Service.Comments(ctx, id, false)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	acts := computeActions(t, u, members)
	v := detailView{Ticket: t, Histories: hist, Attachments: atts, Comments: comments, Members: members, Actions: acts,
		CanClaim: acts.Claim, Open: open, Form: form,
		MaxFiles: h.Settings.Int(ctx, settings.KeyUploadMaxFiles), MaxMB: h.Settings.Int(ctx, settings.KeyUploadMaxMB)}
	if v.Form == nil {
		v.Form = map[string]string{}
	}
	if status == http.StatusOK {
		v.NewLink = h.Sessions.PopString(ctx, "ticket_link_"+strconv.FormatInt(id, 10))
	}
	if acts.Pend {
		if v.PendingReasons, err = h.Service.PendingReasons(ctx); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	if acts.Resolve {
		if v.Templates, err = h.Service.ActionTemplates(ctx, id); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
		if t.AssetID.Valid {
			if v.SpareParts, err = h.Service.SpareParts(ctx); err != nil {
				h.Renderer.ServerError(w, r, err)
				return
			}
		}
	}
	if acts.Support {
		if v.Staff, err = h.Service.StaffOptions(ctx); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	h.Renderer.Render(w, r, status, "tickets/detail", web.Page{Title: t.TicketNo, Nav: "tickets", Errors: errs, Data: v})
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if h.OnView != nil {
		if u := reqctx.UserFrom(r.Context()); u != nil {
			h.OnView(r.Context(), u.ID, id)
		}
	}
	h.renderDetail(w, r, id, http.StatusOK, "", nil, nil)
}

// act menjalankan satu aksi dan menangani hasil secara seragam.
func (h *Handler) act(w http.ResponseWriter, r *http.Request, open, success string, fn func(id int64, u *rbac.User) (map[string]string, error)) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	url := "/app/tickets/" + strconv.FormatInt(id, 10)
	errs, err := fn(id, reqctx.UserFrom(r.Context()))
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, ErrForbidden):
		h.Renderer.Error(w, r, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrTransition), errors.Is(err, ErrMemberInvalid), errors.Is(err, ErrMemberExists):
		h.Renderer.Redirect(w, r, url, "error", err.Error())
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		h.renderDetail(w, r, id, http.StatusUnprocessableEntity, open, web.FormValues(r), errs)
	default:
		h.Renderer.Redirect(w, r, url, "success", success)
	}
}

func noErrs(err error) (map[string]string, error) { return nil, err }

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "", "Tiket mulai dikerjakan.", func(id int64, u *rbac.User) (map[string]string, error) {
		return noErrs(h.Service.Start(r.Context(), id, u))
	})
}

func (h *Handler) pend(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "pend", "Tiket ditunda.", func(id int64, u *rbac.User) (map[string]string, error) {
		reason, _ := strconv.ParseInt(r.PostFormValue("pending_reason_id"), 10, 64)
		return h.Service.Pend(r.Context(), id, u, reason, r.PostFormValue("note"))
	})
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "", "Tiket dilanjutkan.", func(id int64, u *rbac.User) (map[string]string, error) {
		return noErrs(h.Service.Resume(r.Context(), id, u))
	})
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "resolve", "Tiket diselesaikan. Pelapor dapat mengonfirmasi lewat link pemantauan.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.Resolve(r.Context(), id, u, ResolveInput{
			Action: r.PostFormValue("resolution_action"), Result: r.PostFormValue("resolution_result"),
			Note: r.PostFormValue("resolution_note"), RestoredAt: r.PostFormValue("service_restored_at"),
			Parts: PartLinesFrom(r), ServiceCost: r.PostFormValue("service_cost"), AssetStatusAfter: r.PostFormValue("asset_status_after"),
		})
	})
}

func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "release", "Tiket dikembalikan ke antrean.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.Release(r.Context(), id, u, r.PostFormValue("reason"))
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "cancel", "Tiket dibatalkan.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.Cancel(r.Context(), id, u, r.PostFormValue("reason"))
	})
}

func (h *Handler) closeTicket(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "close", "Tiket ditutup.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.Close(r.Context(), id, u, r.PostFormValue("note"))
	})
}

func (h *Handler) reopen(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "reopen", "Tiket dibuka kembali.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.Reopen(r.Context(), id, u, r.PostFormValue("reason"))
	})
}

func (h *Handler) takeOver(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "takeover", "Tiket diambil alih. Anda penanggung jawab utama.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.TakeOver(r.Context(), id, u, r.PostFormValue("reason"))
	})
}

func (h *Handler) changePriority(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "priority", "Prioritas diubah.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.ChangePriority(r.Context(), id, u, r.PostFormValue("priority"), r.PostFormValue("reason"))
	})
}

func (h *Handler) addSupport(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "", "Anggota pendukung ditambahkan.", func(id int64, u *rbac.User) (map[string]string, error) {
		uid, _ := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
		return noErrs(h.Service.AddSupport(r.Context(), id, u, uid))
	})
}

func (h *Handler) removeSupport(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "", "Anggota pendukung dilepas.", func(id int64, u *rbac.User) (map[string]string, error) {
		uid, _ := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
		return noErrs(h.Service.RemoveSupport(r.Context(), id, u, uid))
	})
}

func (h *Handler) addComment(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "comment", "Catatan ditambahkan.", func(id int64, u *rbac.User) (map[string]string, error) {
		return h.Service.AddComment(r.Context(), id, u, r.PostFormValue("body"), r.PostFormValue("visibility") != "public")
	})
}

func (h *Handler) addPhotos(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "photo", "Foto ditambahkan.", func(id int64, u *rbac.User) (map[string]string, error) {
		ctx := r.Context()
		maxFiles := h.Settings.Int(ctx, settings.KeyUploadMaxFiles)
		photos, msg, err := parsePhotos(r, maxFiles, int64(h.Settings.Int(ctx, settings.KeyUploadMaxMB))<<20)
		if err != nil {
			return nil, err
		}
		if msg != "" {
			return map[string]string{"photos": msg}, nil
		}
		if len(photos) == 0 {
			return map[string]string{"photos": "Pilih minimal satu foto."}, nil
		}
		return noErrs(h.Service.AddPhotos(ctx, id, u, photos, r.PostFormValue("visible_to_reporter") == "1"))
	})
}

// PartLinesFrom membaca baris sparepart dari form (part_id[], part_qty[], part_cost[]).
func PartLinesFrom(r *http.Request) []PartLine {
	ids, qtys, costs := r.PostForm["part_id"], r.PostForm["part_qty"], r.PostForm["part_cost"]
	var out []PartLine
	for i, id := range ids {
		l := PartLine{PartID: id}
		if i < len(qtys) {
			l.Qty = qtys[i]
		}
		if i < len(costs) {
			l.UnitCost = costs[i]
		}
		out = append(out, l)
	}
	return out
}
