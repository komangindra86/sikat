package reports

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"sikat/internal/clock"
)

// Jenis periode.
const (
	PeriodMonth = "bulan"
	PeriodYear  = "tahun"
	PeriodRange = "tanggal"
)

// MaxRangeDays: rentang tanggal maksimum (laporan tetap ringan tanpa worker).
const MaxRangeDays = 731

// Filter adalah kriteria laporan. Batas periode dihitung dalam WITA lalu
// dikonversi ke UTC (Start inklusif, End eksklusif).
type Filter struct {
	Period string
	Month  string // "2006-01"
	Year   int
	From   string // "2006-01-02"
	To     string

	Start, End time.Time // UTC

	DepartmentID int64
	ITCategoryID int64
	UserID       int64 // petugas (filter)
	Priority     string
	Status       string

	// FieldIDs membatasi ke beberapa bidang sekaligus (dashboard koordinator per bidang).
	FieldIDs []int64

	// ScopeUserID dipaksakan untuk pengguna yang hanya boleh melihat datanya
	// sendiri (report.view_own): tiket & kegiatan tempat ia menjadi PJ/pendukung.
	ScopeUserID int64
}

var monthNames = []string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

// ParseFilter membaca filter dari query string. Nilai tidak valid diganti default
// (bulan berjalan) dan dilaporkan lewat errs.
func ParseFilter(q url.Values, now time.Time) (Filter, map[string]string) {
	errs := map[string]string{}
	local := now.In(clock.Location)
	f := Filter{Period: q.Get("periode")}
	switch f.Period {
	case PeriodYear, PeriodRange:
	default:
		f.Period = PeriodMonth
	}
	f.Month = local.Format("2006-01")
	f.Year = local.Year()
	f.From = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.Location).Format("2006-01-02")
	f.To = local.Format("2006-01-02")

	switch f.Period {
	case PeriodMonth:
		if v := q.Get("bulan"); v != "" {
			if t, err := time.ParseInLocation("2006-01", v, clock.Location); err == nil && t.Year() >= 2000 && t.Year() <= 2100 {
				f.Month = v
			} else {
				errs["bulan"] = "Bulan tidak valid."
			}
		}
		t, _ := time.ParseInLocation("2006-01", f.Month, clock.Location)
		f.Start, f.End = t, t.AddDate(0, 1, 0)
	case PeriodYear:
		if v := q.Get("tahun"); v != "" {
			if y, err := strconv.Atoi(v); err == nil && y >= 2000 && y <= 2100 {
				f.Year = y
			} else {
				errs["tahun"] = "Tahun tidak valid."
			}
		}
		f.Start = time.Date(f.Year, 1, 1, 0, 0, 0, 0, clock.Location)
		f.End = f.Start.AddDate(1, 0, 0)
	case PeriodRange:
		from, errF := time.ParseInLocation("2006-01-02", q.Get("dari"), clock.Location)
		to, errT := time.ParseInLocation("2006-01-02", q.Get("sampai"), clock.Location)
		switch {
		case errF != nil || errT != nil:
			errs["dari"] = "Isi tanggal awal dan akhir."
		case to.Before(from):
			errs["dari"] = "Tanggal akhir harus sama atau setelah tanggal awal."
		case to.Sub(from) > MaxRangeDays*24*time.Hour:
			errs["dari"] = "Rentang maksimal 2 tahun."
		}
		if len(errs) == 0 {
			f.From, f.To = q.Get("dari"), q.Get("sampai")
			f.Start, f.End = from, to.AddDate(0, 0, 1)
		} else {
			fr, _ := time.ParseInLocation("2006-01-02", f.From, clock.Location)
			tt, _ := time.ParseInLocation("2006-01-02", f.To, clock.Location)
			f.Start, f.End = fr, tt.AddDate(0, 0, 1)
		}
	}
	f.Start, f.End = f.Start.UTC(), f.End.UTC()

	f.DepartmentID = posInt(q.Get("unit"))
	f.ITCategoryID = posInt(q.Get("bidang"))
	f.UserID = posInt(q.Get("petugas"))
	for _, v := range q["bidang_ids"] {
		if id := posInt(v); id > 0 && len(f.FieldIDs) < 50 {
			f.FieldIDs = append(f.FieldIDs, id)
		}
	}
	switch p := q.Get("prioritas"); p {
	case "LOW", "MEDIUM", "HIGH", "CRITICAL":
		f.Priority = p
	}
	switch s := q.Get("status"); s {
	case "NEW", "CLAIMED", "IN_PROGRESS", "PENDING", "RESOLVED", "CLOSED", "CANCELLED", "ACTIVE":
		f.Status = s
	}
	return f, errs
}

func posInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// PeriodLabel: "November 2026", "Tahun 2026", atau "01/11/2026 – 15/11/2026".
func (f Filter) PeriodLabel() string {
	switch f.Period {
	case PeriodYear:
		return "Tahun " + strconv.Itoa(f.Year)
	case PeriodRange:
		a, _ := time.Parse("2006-01-02", f.From)
		b, _ := time.Parse("2006-01-02", f.To)
		return a.Format("02/01/2006") + " – " + b.Format("02/01/2006")
	default:
		t, _ := time.Parse("2006-01", f.Month)
		return monthNames[t.Month()-1] + " " + strconv.Itoa(t.Year())
	}
}

// Query mengembalikan query string filter (untuk tautan ekspor & navigasi).
func (f Filter) Query() url.Values {
	v := url.Values{}
	v.Set("periode", f.Period)
	switch f.Period {
	case PeriodYear:
		v.Set("tahun", strconv.Itoa(f.Year))
	case PeriodRange:
		v.Set("dari", f.From)
		v.Set("sampai", f.To)
	default:
		v.Set("bulan", f.Month)
	}
	set := func(k string, n int64) {
		if n > 0 {
			v.Set(k, strconv.FormatInt(n, 10))
		}
	}
	set("unit", f.DepartmentID)
	set("bidang", f.ITCategoryID)
	set("petugas", f.UserID)
	for _, id := range f.FieldIDs {
		v.Add("bidang_ids", strconv.FormatInt(id, 10))
	}
	if f.Priority != "" {
		v.Set("prioritas", f.Priority)
	}
	if f.Status != "" {
		v.Set("status", f.Status)
	}
	return v
}

// QueryString untuk template.
func (f Filter) QueryString() string { return f.Query().Encode() }

// cond membangun klausa WHERE.
type cond struct {
	parts []string
	args  []any
}

func (c *cond) add(sql string, args ...any) {
	c.parts = append(c.parts, sql)
	c.args = append(c.args, args...)
}

func (c *cond) where() string {
	if len(c.parts) == 0 {
		return "TRUE"
	}
	return strings.Join(c.parts, " AND ")
}

// ticketCond: tiket yang MASUK pada periode + filter. Alias tabel tiket "t".
func (f Filter) ticketCond() *cond {
	c := &cond{}
	c.add("t.created_at >= ? AND t.created_at < ?", f.Start, f.End)
	f.ticketFilters(c)
	return c
}

// ticketFilters menambahkan filter non-periode untuk alias "t".
func (f Filter) ticketFilters(c *cond) {
	if f.DepartmentID > 0 {
		c.add("t.department_id = ?", f.DepartmentID)
	}
	if f.ITCategoryID > 0 {
		c.add("t.it_category_id = ?", f.ITCategoryID)
	}
	if in, args := inList(f.FieldIDs); in != "" {
		c.add("t.it_category_id IN "+in, args...)
	}
	if f.Priority != "" {
		c.add("t.priority = ?", f.Priority)
	}
	switch f.Status {
	case "":
	case "ACTIVE":
		c.add("t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING')")
	default:
		c.add("t.status = ?", f.Status)
	}
	for _, uid := range []int64{f.UserID, f.ScopeUserID} {
		if uid > 0 {
			c.add("(t.assignee_id = ? OR EXISTS (SELECT 1 FROM ticket_assignments ta WHERE ta.ticket_id = t.id AND ta.user_id = ? AND ta.role = 'SUPPORT'))", uid, uid)
		}
	}
}

// activityCond: kegiatan internal pada periode (tanggal kegiatan WITA). Alias "a".
func (f Filter) activityCond() *cond {
	c := &cond{}
	from := f.Start.In(clock.Location).Format("2006-01-02")
	to := f.End.In(clock.Location).Format("2006-01-02")
	c.add("a.activity_date >= ? AND a.activity_date < ?", from, to)
	if f.DepartmentID > 0 {
		c.add("a.department_id = ?", f.DepartmentID)
	}
	if f.ITCategoryID > 0 {
		c.add("a.it_category_id = ?", f.ITCategoryID)
	}
	if in, args := inList(f.FieldIDs); in != "" {
		c.add("a.it_category_id IN "+in, args...)
	}
	for _, uid := range []int64{f.UserID, f.ScopeUserID} {
		if uid > 0 {
			c.add("EXISTS (SELECT 1 FROM activity_members am WHERE am.activity_id = a.id AND am.user_id = ?)", uid)
		}
	}
	return c
}

// maintenanceCond: riwayat pemeliharaan (tidak dibatalkan) yang selesai pada periode. Alias "h", perangkat "x".
func (f Filter) maintenanceCond() *cond {
	c := &cond{}
	c.add("h.voided_at IS NULL AND h.finished_at >= ? AND h.finished_at < ?", f.Start, f.End)
	if f.DepartmentID > 0 {
		c.add("x.department_id = ?", f.DepartmentID)
	}
	for _, uid := range []int64{f.UserID, f.ScopeUserID} {
		if uid > 0 {
			c.add("h.performed_by_user_id = ?", uid)
		}
	}
	return c
}

// inList: "(?,?,?)" beserta argumennya; kosong bila daftar kosong.
func inList(ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}
