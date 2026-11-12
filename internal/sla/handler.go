package sla

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"sikat/internal/clock"
	"sikat/internal/rbac"
	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyajikan dashboard SLA, laporan bulanan, dan pengelolaan kalender kerja.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
	Require  func(rbac.Permission) func(http.Handler) http.Handler
}

// Routes mendaftarkan rute di bawah /app/sla.
func (h *Handler) Routes(r chi.Router) {
	r.With(h.Require(rbac.PermSLAView)).Get("/", h.overview)
	r.With(h.Require(rbac.PermSLAView)).Get("/report", h.report)
	r.Group(func(r chi.Router) {
		r.Use(h.Require(rbac.PermConfigManage))
		r.Get("/calendars", h.calendars)
		r.Get("/calendars/new", h.newCalendar)
		r.Post("/calendars/new", h.saveCalendar)
		r.Get("/calendars/{id}", h.editCalendar)
		r.Post("/calendars/{id}", h.saveCalendar)
		r.Post("/calendars/{id}/holidays", h.addHoliday)
		r.Post("/calendars/{id}/holidays/{hid}/delete", h.deleteHoliday)
	})
}

type overviewData struct {
	Overview
	CanConfig bool
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	o, err := h.Service.Overview(r.Context(), 20)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "sla/overview", web.Page{
		Title: "SLA", Nav: "sla",
		Data: overviewData{Overview: o, CanConfig: reqctx.UserFrom(r.Context()).Has(rbac.PermConfigManage)},
	})
}

type reportData struct {
	Report
	MonthValue string // "2006-01" untuk input type=month
	Prev, Next string
	IsCurrent  bool
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	now := h.Service.Now().In(clock.Location)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.Location)
	if v := r.URL.Query().Get("bulan"); v != "" {
		if t, err := time.ParseInLocation("2006-01", v, clock.Location); err == nil && t.Year() >= 2000 && t.Year() <= 2100 {
			month = t
		}
	}
	rep, err := h.Service.MonthlyReport(r.Context(), month)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.Location)
	h.Renderer.Render(w, r, http.StatusOK, "sla/report", web.Page{
		Title: "Laporan SLA", Nav: "sla",
		Data: reportData{Report: rep, MonthValue: month.Format("2006-01"),
			Prev: month.AddDate(0, -1, 0).Format("2006-01"), Next: month.AddDate(0, 1, 0).Format("2006-01"),
			IsCurrent: !month.Before(cur)},
	})
}

func (h *Handler) calendars(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.Calendars(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "sla/calendars", web.Page{Title: "Kalender Kerja", Nav: "master", Data: list})
}

type calendarData struct {
	Form     CalendarForm
	Holidays []Holiday
	Errors   map[string]string
	HForm    map[string]string
	Today    string
}

func (h *Handler) renderCalendar(w http.ResponseWriter, r *http.Request, status int, d calendarData) {
	title := "Kalender baru"
	if d.Form.ID != 0 {
		title = d.Form.Name
	}
	d.Today = h.Service.Now().In(clock.Location).Format("2006-01-02")
	h.Renderer.Render(w, r, status, "sla/calendar_form", web.Page{Title: title, Nav: "master", Data: d, Errors: d.Errors})
}

func (h *Handler) newCalendar(w http.ResponseWriter, r *http.Request) {
	h.renderCalendar(w, r, http.StatusOK, calendarData{Form: NewCalendarForm()})
}

func (h *Handler) editCalendar(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	f, hs, err := h.Service.CalendarForm(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.renderCalendar(w, r, http.StatusOK, calendarData{Form: f, Holidays: hs})
}

func (h *Handler) saveCalendar(w http.ResponseWriter, r *http.Request) {
	var id int64
	if chi.URLParam(r, "id") != "" {
		var ok bool
		if id, ok = web.IDParam(r, "id"); !ok {
			h.Renderer.Error(w, r, http.StatusNotFound, "")
			return
		}
	}
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	f := CalendarForm{ID: id, Name: r.PostFormValue("name"), Is24x7: r.PostFormValue("is_24x7") == "1",
		IsDefault: r.PostFormValue("is_default") == "1"}
	for i := range f.Days {
		wd := strconv.Itoa(i + 1)
		f.Days[i] = DayForm{Weekday: i + 1, On: r.PostFormValue("day_"+wd) == "1",
			Start: r.PostFormValue("start_" + wd), End: r.PostFormValue("end_" + wd)}
	}
	newID, errs, err := h.Service.SaveCalendar(r.Context(), f)
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		var hs []Holiday
		if id != 0 {
			if hs, err = h.Service.Holidays(r.Context(), id); err != nil {
				h.Renderer.ServerError(w, r, err)
				return
			}
		}
		h.renderCalendar(w, r, http.StatusUnprocessableEntity, calendarData{Form: f, Holidays: hs, Errors: errs})
	default:
		h.Renderer.Redirect(w, r, "/app/sla/calendars/"+strconv.FormatInt(newID, 10), "success", "Kalender disimpan.")
	}
}

func (h *Handler) addHoliday(w http.ResponseWriter, r *http.Request) {
	id, ok := web.IDParam(r, "id")
	if !ok {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	errs, err := h.Service.AddHoliday(r.Context(), id, r.PostFormValue("holiday_date"), r.PostFormValue("holiday_name"))
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	case len(errs) > 0:
		f, hs, err := h.Service.CalendarForm(r.Context(), id)
		if err != nil {
			h.Renderer.ServerError(w, r, err)
			return
		}
		h.renderCalendar(w, r, http.StatusUnprocessableEntity, calendarData{Form: f, Holidays: hs, Errors: errs,
			HForm: map[string]string{"holiday_date": r.PostFormValue("holiday_date"), "holiday_name": r.PostFormValue("holiday_name")}})
	default:
		h.Renderer.Redirect(w, r, "/app/sla/calendars/"+strconv.FormatInt(id, 10)+"#libur", "success", "Hari libur ditambahkan.")
	}
}

func (h *Handler) deleteHoliday(w http.ResponseWriter, r *http.Request) {
	id, ok1 := web.IDParam(r, "id")
	hid, ok2 := web.IDParam(r, "hid")
	if !ok1 || !ok2 {
		h.Renderer.Error(w, r, http.StatusNotFound, "")
		return
	}
	err := h.Service.DeleteHoliday(r.Context(), id, hid)
	switch {
	case errors.Is(err, ErrNotFound):
		h.Renderer.Error(w, r, http.StatusNotFound, "")
	case err != nil:
		h.Renderer.ServerError(w, r, err)
	default:
		h.Renderer.Redirect(w, r, "/app/sla/calendars/"+strconv.FormatInt(id, 10)+"#libur", "success", "Hari libur dihapus.")
	}
}
