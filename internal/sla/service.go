package sla

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"sikat/internal/clock"
	"sikat/internal/database"
)

// Service mengakses konfigurasi SLA dan data SLA tiket.
type Service struct {
	db    *sql.DB
	clock clock.Clock
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock) *Service {
	if c == nil {
		c = clock.Real{}
	}
	return &Service{db: db, clock: c}
}

// Now adalah waktu sekarang menurut clock service.
func (s *Service) Now() time.Time { return s.clock.Now() }

func location(name string) *time.Location {
	if name == "" || name == "Asia/Makassar" {
		return clock.Location
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return clock.Location
}

// parseClock mengubah "HH:MM:SS" (TIME MySQL) menjadi menit sejak tengah malam.
func parseClock(s string) (int, error) {
	var h, m, sec int
	if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err != nil {
		return 0, fmt.Errorf("format jam %q: %w", s, err)
	}
	return h*60 + m, nil
}

// LoadCalendars memuat seluruh kalender beserta jam kerja dan hari libur.
func LoadCalendars(ctx context.Context, q database.DBTX) (map[int64]*Calendar, error) {
	out := map[int64]*Calendar{}
	rows, err := q.QueryContext(ctx, `SELECT id, name, timezone, is_24x7 FROM sla_calendars`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		c := &Calendar{Holidays: map[string]string{}}
		var tz string
		if err := rows.Scan(&c.ID, &c.Name, &tz, &c.Is24x7); err != nil {
			rows.Close()
			return nil, err
		}
		c.Loc = location(tz)
		out[c.ID] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.QueryContext(ctx, `SELECT calendar_id, weekday, start_time, end_time FROM sla_calendar_hours`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var wd int
		var st, en string
		if err := rows.Scan(&id, &wd, &st, &en); err != nil {
			rows.Close()
			return nil, err
		}
		c, ok := out[id]
		if !ok || wd < 1 || wd > 7 {
			continue
		}
		a, err1 := parseClock(st)
		b, err2 := parseClock(en)
		if err1 != nil || err2 != nil {
			rows.Close()
			return nil, errors.Join(err1, err2)
		}
		c.Hours[wd] = append(c.Hours[wd], Interval{a, b})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range out {
		c.Hours = NormalizeHours(c.Hours)
	}

	rows, err = q.QueryContext(ctx, `SELECT calendar_id, holiday_date, name FROM sla_holidays`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var d time.Time
		var name string
		if err := rows.Scan(&id, &d, &name); err != nil {
			return nil, err
		}
		if c, ok := out[id]; ok {
			c.Holidays[d.Format("2006-01-02")] = name
		}
	}
	return out, rows.Err()
}

// LoadCalendar memuat satu kalender (nil bila tidak ada).
func LoadCalendar(ctx context.Context, q database.DBTX, id int64) (*Calendar, error) {
	all, err := LoadCalendars(ctx, q)
	if err != nil {
		return nil, err
	}
	return all[id], nil
}

// Match adalah atribut tiket untuk memilih kebijakan SLA.
type Match struct {
	Priority         string
	TicketCategoryID int64
	ITCategoryID     int64
	DepartmentID     int64
}

// Policy adalah kebijakan SLA terpilih.
type Policy struct {
	ID                int64
	Name              string
	CalendarID        int64
	ResponseMinutes   int
	ResolutionMinutes int
	WarnPct           int
	AllowPause        bool
}

// FindPolicy memilih kebijakan aktif yang paling spesifik untuk tiket:
// prioritas wajib sama; jenis gangguan > bidang > unit menentukan kekhususan.
// ok=false bila tidak ada kebijakan (tiket tanpa SLA).
func FindPolicy(ctx context.Context, q database.DBTX, m Match) (Policy, bool, error) {
	var p Policy
	err := q.QueryRowContext(ctx, `
		SELECT id, name, calendar_id, response_minutes, resolution_minutes, warn_pct, allow_pause
		  FROM sla_policies
		 WHERE is_active = TRUE AND priority = ?
		   AND (ticket_category_id IS NULL OR ticket_category_id = ?)
		   AND (it_category_id IS NULL OR it_category_id = ?)
		   AND (department_id IS NULL OR department_id = ?)
		 ORDER BY (ticket_category_id IS NOT NULL) * 4 + (it_category_id IS NOT NULL) * 2 + (department_id IS NOT NULL) DESC, id
		 LIMIT 1`, m.Priority, m.TicketCategoryID, m.ITCategoryID, m.DepartmentID).
		Scan(&p.ID, &p.Name, &p.CalendarID, &p.ResponseMinutes, &p.ResolutionMinutes, &p.WarnPct, &p.AllowPause)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}

// Assignment adalah nilai SLA yang disalin ke tiket.
type Assignment struct {
	Found         bool
	Policy        Policy
	ResponseDue   time.Time
	ResolutionDue time.Time
}

// Columns mengembalikan potongan SET dan argumennya untuk menyalin SLA ke tiket.
// Bila tidak ada kebijakan, kolom SLA dikosongkan.
func (a Assignment) Columns() (string, []any) {
	const sets = `sla_policy_id = ?, sla_calendar_id = ?, sla_response_minutes = ?, sla_resolution_minutes = ?,
	              sla_warn_pct = ?, sla_allow_pause = ?, response_due_at = ?, resolution_due_at = ?`
	if !a.Found {
		return sets, []any{nil, nil, nil, nil, nil, false, nil, nil}
	}
	p := a.Policy
	return sets, []any{p.ID, p.CalendarID, p.ResponseMinutes, p.ResolutionMinutes, p.WarnPct, p.AllowPause,
		a.ResponseDue, a.ResolutionDue}
}

// Assign memilih kebijakan untuk tiket dan menghitung batas waktunya.
// createdAt adalah awal target respons; cycleStart awal target penyelesaian;
// pausedMinutes jeda yang sudah terkumpul di siklus ini.
func Assign(ctx context.Context, q database.DBTX, m Match, createdAt, cycleStart time.Time, pausedMinutes int) (Assignment, error) {
	p, ok, err := FindPolicy(ctx, q, m)
	if err != nil || !ok {
		return Assignment{}, err
	}
	cal, err := LoadCalendar(ctx, q, p.CalendarID)
	if err != nil {
		return Assignment{}, err
	}
	return Assignment{
		Found: true, Policy: p,
		ResponseDue:   cal.Add(createdAt, time.Duration(p.ResponseMinutes)*time.Minute),
		ResolutionDue: cal.Add(cycleStart, time.Duration(p.ResolutionMinutes+pausedMinutes)*time.Minute),
	}, nil
}

// Unpause menghitung akumulasi jeda saat tiket keluar dari status yang menghentikan SLA,
// dan batas waktu penyelesaian yang baru.
func Unpause(ctx context.Context, q database.DBTX, calendarID int64, pausedSince time.Time, pausedMinutes int,
	cycleStart time.Time, resolutionMinutes int, now time.Time) (int, time.Time, error) {
	cal, err := LoadCalendar(ctx, q, calendarID)
	if err != nil {
		return 0, time.Time{}, err
	}
	total := pausedMinutes + int(cal.Between(pausedSince, now)/time.Minute)
	due := cal.Add(cycleStart, time.Duration(resolutionMinutes+total)*time.Minute)
	return total, due, nil
}

// DueFrom menghitung start + minutes waktu operasional pada kalender.
func DueFrom(ctx context.Context, q database.DBTX, calendarID int64, start time.Time, minutes int) (time.Time, error) {
	cal, err := LoadCalendar(ctx, q, calendarID)
	if err != nil {
		return time.Time{}, err
	}
	return cal.Add(start, time.Duration(minutes)*time.Minute), nil
}

// placeholders membuat "?, ?, ?" sebanyak n.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
