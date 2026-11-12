package sla

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sikat/internal/audit"
	"sikat/internal/clock"
	"sikat/internal/database"
)

// ErrNotFound dikembalikan bila kalender/hari libur tidak ada.
var ErrNotFound = errors.New("data tidak ditemukan")

// CalendarInfo adalah ringkasan kalender untuk daftar.
type CalendarInfo struct {
	ID           int64
	Name         string
	Is24x7       bool
	IsDefault    bool
	Hours        string // ringkasan jam kerja
	HolidayCount int
	PolicyCount  int
}

// DayForm adalah isian jam kerja satu hari.
type DayForm struct {
	Weekday int
	On      bool
	Start   string // "08:00"
	End     string
}

// CalendarForm adalah isian form kalender.
type CalendarForm struct {
	ID        int64
	Name      string
	Is24x7    bool
	IsDefault bool
	Days      [7]DayForm // Senin..Minggu
}

// Holiday adalah satu hari libur.
type Holiday struct {
	ID   int64
	Date time.Time
	Name string
}

func hhmm(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

func summarize(c *Calendar) string {
	if c.Is24x7 {
		return "24 jam setiap hari"
	}
	if !c.HasHours() {
		return "Belum ada jam kerja"
	}
	// Kelompokkan hari berurutan dengan jam yang sama, mis. "Sen–Jum 08:00–16:00".
	short := [...]string{"", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab", "Min"}
	key := func(d int) string {
		parts := make([]string, 0, len(c.Hours[d]))
		for _, iv := range c.Hours[d] {
			parts = append(parts, hhmm(iv.Start)+"–"+hhmm(iv.End))
		}
		return strings.Join(parts, ", ")
	}
	var out []string
	for d := 1; d <= 7; {
		k := key(d)
		e := d
		for e+1 <= 7 && key(e+1) == k {
			e++
		}
		if k != "" {
			days := short[d]
			if e > d {
				days += "–" + short[e]
			}
			out = append(out, days+" "+k)
		}
		d = e + 1
	}
	return strings.Join(out, " · ")
}

// Calendars mengembalikan daftar kalender.
func (s *Service) Calendars(ctx context.Context) ([]CalendarInfo, error) {
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.is_default,
		       (SELECT COUNT(*) FROM sla_holidays h WHERE h.calendar_id = c.id AND h.holiday_date >= ?),
		       (SELECT COUNT(*) FROM sla_policies p WHERE p.calendar_id = c.id AND p.is_active)
		  FROM sla_calendars c ORDER BY c.is_default DESC, c.name`, s.clock.Now().In(clock.Location).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarInfo
	for rows.Next() {
		var ci CalendarInfo
		if err := rows.Scan(&ci.ID, &ci.IsDefault, &ci.HolidayCount, &ci.PolicyCount); err != nil {
			return nil, err
		}
		if c := cals[ci.ID]; c != nil {
			ci.Name, ci.Is24x7, ci.Hours = c.Name, c.Is24x7, summarize(c)
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

// NewCalendarForm mengembalikan form kosong (Senin–Jumat 08:00–16:00).
func NewCalendarForm() CalendarForm {
	var f CalendarForm
	for i := range f.Days {
		f.Days[i] = DayForm{Weekday: i + 1, On: i < 5, Start: "08:00", End: "16:00"}
	}
	return f
}

// CalendarForm memuat kalender untuk diedit beserta hari liburnya.
func (s *Service) CalendarForm(ctx context.Context, id int64) (CalendarForm, []Holiday, error) {
	f := NewCalendarForm()
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return f, nil, err
	}
	c, ok := cals[id]
	if !ok {
		return f, nil, ErrNotFound
	}
	f.ID, f.Name, f.Is24x7 = c.ID, c.Name, c.Is24x7
	if err := s.db.QueryRowContext(ctx, `SELECT is_default FROM sla_calendars WHERE id = ?`, id).Scan(&f.IsDefault); err != nil {
		return f, nil, err
	}
	for d := 1; d <= 7; d++ {
		day := DayForm{Weekday: d, Start: "08:00", End: "16:00"}
		if ivs := c.Hours[d]; len(ivs) > 0 {
			// Form menampilkan satu rentang per hari (awal paling pagi – akhir paling sore).
			day.On, day.Start, day.End = true, hhmm(ivs[0].Start), hhmm(ivs[len(ivs)-1].End)
		}
		f.Days[d-1] = day
	}
	hs, err := s.Holidays(ctx, id)
	return f, hs, err
}

// Holidays mengembalikan hari libur kalender (terbaru di atas).
func (s *Service) Holidays(ctx context.Context, calendarID int64) ([]Holiday, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, holiday_date, name FROM sla_holidays WHERE calendar_id = ? ORDER BY holiday_date DESC`, calendarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Holiday
	for rows.Next() {
		var h Holiday
		if err := rows.Scan(&h.ID, &h.Date, &h.Name); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func parseHHMM(v string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// SaveCalendar menyimpan kalender baru (id=0) atau perubahan. Jam kerja diganti seluruhnya.
func (s *Service) SaveCalendar(ctx context.Context, f CalendarForm) (int64, map[string]string, error) {
	errs := map[string]string{}
	f.Name = strings.TrimSpace(f.Name)
	switch n := utf8.RuneCountInString(f.Name); {
	case n == 0:
		errs["name"] = "Nama kalender wajib diisi."
	case n > 100:
		errs["name"] = "Nama maksimal 100 karakter."
	}
	type span struct{ wd, a, b int }
	var spans []span
	for _, d := range f.Days {
		if !d.On {
			continue
		}
		a, ok1 := parseHHMM(d.Start)
		b, ok2 := parseHHMM(d.End)
		key := "day_" + strconv.Itoa(d.Weekday)
		switch {
		case !ok1 || !ok2:
			errs[key] = "Isi jam mulai dan selesai (JJ:MM)."
		case a >= b:
			errs[key] = "Jam selesai harus setelah jam mulai."
		default:
			spans = append(spans, span{d.Weekday, a, b})
		}
	}
	if !f.Is24x7 && len(spans) == 0 && len(errs) == 0 {
		errs["days"] = "Pilih minimal satu hari kerja, atau tandai kalender 24 jam."
	}
	if len(errs) > 0 {
		return f.ID, errs, nil
	}

	now := s.clock.Now()
	id := f.ID
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		before := map[string]any{}
		if id != 0 {
			var name string
			var is24, def bool
			err := tx.QueryRowContext(ctx, `SELECT name, is_24x7, is_default FROM sla_calendars WHERE id = ? FOR UPDATE`, id).Scan(&name, &is24, &def)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			before = map[string]any{"name": name, "is_24x7": is24, "is_default": def}
		}
		if f.IsDefault {
			if _, err := tx.ExecContext(ctx, `UPDATE sla_calendars SET is_default = FALSE, updated_at = ? WHERE is_default AND id <> ?`, now, id); err != nil {
				return err
			}
		}
		if id == 0 {
			res, err := tx.ExecContext(ctx, `INSERT INTO sla_calendars (name, is_24x7, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
				f.Name, f.Is24x7, f.IsDefault, now, now)
			if err != nil {
				return err
			}
			if id, err = res.LastInsertId(); err != nil {
				return err
			}
		} else if _, err := tx.ExecContext(ctx, `UPDATE sla_calendars SET name = ?, is_24x7 = ?, is_default = ?, updated_at = ? WHERE id = ?`,
			f.Name, f.Is24x7, f.IsDefault, now, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sla_calendar_hours WHERE calendar_id = ?`, id); err != nil {
			return err
		}
		hours := make([]string, 0, len(spans))
		for _, sp := range spans {
			if _, err := tx.ExecContext(ctx, `INSERT INTO sla_calendar_hours (calendar_id, weekday, start_time, end_time) VALUES (?, ?, ?, ?)`,
				id, sp.wd, hhmm(sp.a)+":00", hhmm(sp.b)+":00"); err != nil {
				return err
			}
			hours = append(hours, fmt.Sprintf("%d %s-%s", sp.wd, hhmm(sp.a), hhmm(sp.b)))
		}
		action := "sla.calendar.update"
		if f.ID == 0 {
			action = "sla.calendar.create"
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: action, EntityType: "sla_calendars", EntityID: id,
			Before: before, After: map[string]any{"name": f.Name, "is_24x7": f.Is24x7, "is_default": f.IsDefault, "hours": hours}})
	})
	if isDup(err) {
		return id, map[string]string{"name": "Nama kalender sudah dipakai."}, nil
	}
	if err != nil {
		return id, nil, err
	}
	return id, nil, s.RecomputeDue(ctx, id)
}

// AddHoliday menambah hari libur (tanggal WITA "2006-01-02").
func (s *Service) AddHoliday(ctx context.Context, calendarID int64, date, name string) (map[string]string, error) {
	errs := map[string]string{}
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), clock.Location)
	if err != nil {
		errs["holiday_date"] = "Pilih tanggal."
	}
	name = strings.TrimSpace(name)
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		errs["holiday_name"] = "Nama hari libur wajib diisi."
	case n > 120:
		errs["holiday_name"] = "Nama maksimal 120 karakter."
	}
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	err = database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO sla_holidays (calendar_id, holiday_date, name, created_at) VALUES (?, ?, ?, ?)`,
			calendarID, d.Format("2006-01-02"), name, now)
		if err != nil {
			return err
		}
		hid, _ := res.LastInsertId()
		return audit.Record(ctx, tx, now, audit.Entry{Action: "sla.holiday.create", EntityType: "sla_holidays", EntityID: hid,
			After: map[string]any{"calendar_id": calendarID, "date": d.Format("2006-01-02"), "name": name}})
	})
	switch {
	case isDup(err):
		return map[string]string{"holiday_date": "Tanggal ini sudah terdaftar sebagai hari libur."}, nil
	case database.IsForeignKeyMissing(err):
		return nil, ErrNotFound
	case err != nil:
		return nil, err
	}
	return nil, s.RecomputeDue(ctx, calendarID)
}

// DeleteHoliday menghapus hari libur dari kalender.
func (s *Service) DeleteHoliday(ctx context.Context, calendarID, holidayID int64) error {
	now := s.clock.Now()
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		var d time.Time
		var name string
		err := tx.QueryRowContext(ctx, `SELECT holiday_date, name FROM sla_holidays WHERE id = ? AND calendar_id = ? FOR UPDATE`,
			holidayID, calendarID).Scan(&d, &name)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sla_holidays WHERE id = ?`, holidayID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, now, audit.Entry{Action: "sla.holiday.delete", EntityType: "sla_holidays", EntityID: holidayID,
			Before: map[string]any{"calendar_id": calendarID, "date": d.Format("2006-01-02"), "name": name}})
	})
	if err != nil {
		return err
	}
	return s.RecomputeDue(ctx, calendarID)
}

// RecomputeDue menghitung ulang batas waktu tiket aktif yang memakai kalender
// setelah kalender/hari libur berubah, agar data tersimpan konsisten dengan tampilan.
func (s *Service) RecomputeDue(ctx context.Context, calendarID int64) error {
	now := s.clock.Now()
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return err
	}
	list, err := loadTicketRows(ctx, s.db, cals, now, activeWhere+` AND t.sla_calendar_id = ?`, calendarID)
	if err != nil {
		return err
	}
	for _, t := range list {
		var rd, sd any
		if t.Resp.Defined && t.Resp.Running {
			rd = t.Resp.DueAt
		}
		if !t.Reso.DueAt.IsZero() {
			sd = t.Reso.DueAt
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE tickets SET response_due_at = COALESCE(?, response_due_at),
			resolution_due_at = COALESCE(?, resolution_due_at) WHERE id = ?`, rd, sd, t.ID); err != nil {
			return err
		}
	}
	return nil
}

func isDup(err error) bool { _, ok := database.DuplicateKey(err); return ok }
