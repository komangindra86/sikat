package tickets

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"sikat/internal/priority"
	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/settings"
	"sikat/internal/web"
)

// Handler melayani halaman tiket untuk petugas IT.
type Handler struct {
	Service  *Service
	Settings *settings.Service
	Renderer *web.Renderer
	Sessions *scs.SessionManager
	BaseURL  string
	// Require membuat middleware permission (dari auth.Guard).
	Require func(rbac.Permission) func(http.Handler) http.Handler
	// OnView (opsional) dipanggil saat detail tiket dibuka (menandai notifikasi tiket dibaca).
	OnView func(ctx context.Context, userID, ticketID int64)
	// Employees (opsional): nama pegawai SIMRS untuk saran nama pelapor.
	Employees EmployeeSource
}

// Routes mendaftarkan rute di bawah /app/tickets.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.queue)
	r.Get("/assets", h.assetOptions)
	r.Get("/attachments/{id}", h.attachment)
	r.With(h.Require(rbac.PermTicketCreate)).Get("/new", h.newForm)
	r.With(h.Require(rbac.PermTicketCreate)).Post("/", h.create)
	r.With(h.Require(rbac.PermTicketClaim)).Post("/{id}/claim", h.claim)
	r.Get("/{id}", h.detail)
	r.Post("/{id}/start", h.start)
	r.Post("/{id}/pend", h.pend)
	r.Post("/{id}/resume", h.resume)
	r.Post("/{id}/resolve", h.resolve)
	r.Post("/{id}/release", h.release)
	r.Post("/{id}/cancel", h.cancel)
	r.Post("/{id}/close", h.closeTicket)
	r.Post("/{id}/reopen", h.reopen)
	r.Post("/{id}/takeover", h.takeOver)
	r.Post("/{id}/priority", h.changePriority)
	r.Post("/{id}/category", h.changeCategory)
	r.Post("/{id}/support", h.addSupport)
	r.Post("/{id}/support/{uid}/remove", h.removeSupport)
	r.Post("/{id}/link", h.renewLink)
	r.Post("/{id}/comments", h.addComment)
	r.Post("/{id}/photos", h.addPhotos)
}

type queueData struct {
	Mode            string
	Tickets         []Ticket
	Pager           web.Pager
	Filter          QueueFilter
	HasCompetencies bool
	Workload        []Workload
	Departments     []Option
	CanClaim        bool
	CanCreate       bool
	ShowAll         bool
	UserID          int64
	SelfURL         string
	// CoordinatedFields: bidang yang dikoordinasi (koordinator).
	CoordinatedFields []string
}

type cardData struct {
	T        Ticket
	CanClaim bool
	Result   *ClaimResult
	UserID   int64
	CSRF     string
}

type newData struct {
	Form        map[string]string
	Categories  []Category
	Departments []Option
	Rooms       []Option
	Assets      []Option
	MaxFiles    int
	MaxMB       int
	Incident    []struct{ Value, Label string }
	CanClaim    bool
	// Employees: saran nama pelapor dari daftar pegawai SIMRS.
	Employees []string
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := reqctx.UserFrom(ctx)
	q := r.URL.Query()
	mode := q.Get("mode")
	if mode != "mine" && mode != "active" {
		mode = "available"
	}
	hasComp, err := h.Service.HasCompetencies(ctx, u.ID)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager := web.NewPager(r, 30)
	dept, _ := strconv.ParseInt(q.Get("department_id"), 10, 64)
	f := QueueFilter{Mode: mode, UserID: u.ID, DepartmentID: dept, Limit: pager.PerPage, Offset: pager.Offset()}
	if p := q.Get("priority"); priority.ValidPriority(p) {
		f.Priority = p
	}
	showAll := q.Get("semua") == "1"
	// Koordinator: antrean baru & aktif default ke bidang yang dikoordinasinya.
	var fields []string
	if u.RoleCode == rbac.RoleCoordinator {
		if fields, err = h.Service.CoordinatedFields(ctx, u.ID); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	if field, _ := strconv.ParseInt(q.Get("bidang"), 10, 64); field > 0 {
		f.ITCategoryID = field // bidang tertentu (mis. tautan "perlu dipilah")
	} else if len(fields) > 0 && mode != "mine" {
		f.OnlyCoordinated = !showAll
	} else {
		f.OnlyCompetency = mode == "available" && hasComp && !showAll
	}
	list, total, err := h.Service.Queue(ctx, f)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	pager.Total = total
	wl, err := h.Service.Workload(ctx)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	depts, err := h.Service.Departments(ctx)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "tickets/queue", web.Page{
		Title: "Antrean Tiket", Nav: "tickets",
		Data: queueData{Mode: mode, Tickets: list, Pager: pager, Filter: f, HasCompetencies: hasComp, Workload: wl,
			Departments: depts, CanClaim: u.Has(rbac.PermTicketClaim), CanCreate: u.Has(rbac.PermTicketCreate),
			ShowAll: showAll, UserID: u.ID, SelfURL: r.URL.RequestURI(), CoordinatedFields: fields},
	})
}

func (h *Handler) claim(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	u := reqctx.UserFrom(r.Context())
	res, err := h.Service.Claim(r.Context(), id, u.ID)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		t, _, _, err := h.Service.Get(r.Context(), id)
		if err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
		h.Renderer.Fragment(w, r, http.StatusOK, "tickets/queue", "ticket-card-page", web.Page{
			Data: cardData{T: t, Result: &res, UserID: u.ID},
		})
		return
	}
	url := "/app/tickets/" + strconv.FormatInt(id, 10)
	if res.OK {
		h.Renderer.Redirect(w, r, url, "success", "Tiket berhasil diambil. Anda penanggung jawab tiket ini.")
		return
	}
	msg := "Tiket ini sudah tidak tersedia di antrean."
	if res.ClaimedBy != "" {
		msg = "Tiket ini sudah diambil oleh " + res.ClaimedBy + "."
	}
	h.Renderer.Redirect(w, r, url, "error", msg)
}

func (h *Handler) attachment(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	key, err := h.Service.AttachmentKey(r.Context(), id)
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

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

func (h *Handler) renderNew(w http.ResponseWriter, r *http.Request, status int, form, errs map[string]string) {
	ctx := r.Context()
	cats, err := h.Service.Categories(ctx, 0)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	depts, err := h.Service.Departments(ctx)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	dept, room := atoi(form["department_id"]), atoi(form["room_id"])
	var rooms []Option
	if dept != 0 {
		if rooms, err = h.Service.DepartmentRooms(ctx, dept); err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
	}
	assets, err := h.Service.LocationAssets(ctx, dept, room)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	var employees []string
	if h.Employees != nil {
		employees = h.Employees.Names(ctx)
	}
	h.Renderer.Render(w, r, status, "tickets/new", web.Page{
		Title: "Catat tiket", Nav: "tickets", Errors: errs,
		Data: newData{Form: form, Categories: cats, Departments: depts, Rooms: rooms, Assets: assets, Employees: employees,
			MaxFiles: h.Settings.Int(ctx, settings.KeyUploadMaxFiles), MaxMB: h.Settings.Int(ctx, settings.KeyUploadMaxMB),
			Incident: IncidentOptions, CanClaim: reqctx.UserFrom(ctx).Has(rbac.PermTicketClaim)},
	})
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	h.renderNew(w, r, http.StatusOK, map[string]string{"channel": ChannelPhone, "replacement_available": priority.ReplacementUnknown}, nil)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := reqctx.UserFrom(ctx)
	d, form, errs := ParseCommon(r.PostFormValue, h.Service.clock.Now())
	ParseStaffLocation(r.PostFormValue, &d, form, errs)
	d.CreatedBy = u.ID
	claimNow := r.PostFormValue("claim_now") == "1" && u.Has(rbac.PermTicketClaim)
	if claimNow {
		form["claim_now"] = "1"
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
		h.renderNew(w, r, http.StatusUnprocessableEntity, form, errs)
		return
	}
	created, fieldErrs, err := h.Service.Create(ctx, d, photos)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(fieldErrs) > 0 {
		h.renderNew(w, r, http.StatusUnprocessableEntity, form, fieldErrs)
		return
	}
	msg := "Tiket " + created.Number + " dicatat dengan prioritas " + web.Label("priority", created.Priority) + "."
	if claimNow {
		res, err := h.Service.Claim(ctx, created.ID, u.ID)
		if err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
		if res.OK {
			msg += " Anda penanggung jawab tiket ini."
		}
	}
	id := strconv.FormatInt(created.ID, 10)
	// Link pemantauan hanya bisa dibentuk sekarang (token asli tidak disimpan).
	h.Sessions.Put(ctx, "ticket_link_"+id, h.BaseURL+"/t/"+created.Token)
	h.Renderer.Redirect(w, r, "/app/tickets/"+id, "success", msg)
}

// assetOptions mengembalikan <option> perangkat untuk unit/ruangan terpilih (HTMX).
func (h *Handler) assetOptions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := h.Service.LocationAssets(r.Context(), atoi(q.Get("department_id")), atoi(q.Get("room_id")))
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "tickets/new", "asset-options", web.Page{Data: newData{Assets: list, Form: map[string]string{}}})
}
