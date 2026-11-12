// Package sla menghitung target waktu layanan (SLA) tiket berdasarkan kalender
// kerja, mencatat eskalasi secara idempoten, dan menyajikan data dashboard SLA.
//
// Kalkulator (Calendar, Evaluate) murni: tidak menyentuh database dan tidak
// membaca jam sistem, sehingga dapat diuji dengan waktu apa pun.
package sla

import (
	"sort"
	"time"
)

// Interval adalah rentang jam operasional dalam satu hari, dalam menit sejak
// tengah malam waktu setempat: [Start, End).
type Interval struct {
	Start, End int
}

// Calendar adalah kalender kerja. Hari kerja 1=Senin .. 7=Minggu.
type Calendar struct {
	ID       int64
	Name     string
	Is24x7   bool
	Loc      *time.Location
	Hours    [8][]Interval     // indeks 1..7; indeks 0 tidak dipakai
	Holidays map[string]string // "2006-01-02" → nama hari libur
}

// maxDays membatasi pencarian hari kerja (mis. kalender tanpa jam kerja).
const maxDays = 3660

// isoWeekday mengubah time.Weekday (Minggu=0) menjadi 1=Senin .. 7=Minggu.
func isoWeekday(w time.Weekday) int {
	if w == time.Sunday {
		return 7
	}
	return int(w)
}

// HasHours melaporkan apakah kalender memiliki setidaknya satu jam operasional.
func (c *Calendar) HasHours() bool {
	for d := 1; d <= 7; d++ {
		if len(c.Hours[d]) > 0 {
			return true
		}
	}
	return false
}

// alwaysOn: 24/7, atau kalender rusak tanpa jam kerja (diperlakukan 24/7 agar
// SLA tidak pernah "berhenti selamanya").
func (c *Calendar) alwaysOn() bool { return c == nil || c.Is24x7 || !c.HasHours() }

func (c *Calendar) loc() *time.Location {
	if c.Loc == nil {
		return time.UTC
	}
	return c.Loc
}

// IsHoliday melaporkan apakah tanggal lokal t adalah hari libur.
func (c *Calendar) IsHoliday(t time.Time) bool {
	if len(c.Holidays) == 0 {
		return false
	}
	_, ok := c.Holidays[t.In(c.loc()).Format("2006-01-02")]
	return ok
}

// dayIntervals mengembalikan rentang absolut jam operasional pada tanggal lokal day
// (00:00 setempat), terurut.
func (c *Calendar) dayIntervals(day time.Time) [][2]time.Time {
	if c.IsHoliday(day) {
		return nil
	}
	ivs := c.Hours[isoWeekday(day.Weekday())]
	out := make([][2]time.Time, 0, len(ivs))
	for _, iv := range ivs {
		out = append(out, [2]time.Time{
			day.Add(time.Duration(iv.Start) * time.Minute),
			day.Add(time.Duration(iv.End) * time.Minute),
		})
	}
	return out
}

func midnight(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func nextDay(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, day.Location())
}

// Add mengembalikan saat ketika d waktu operasional telah berlalu sejak start.
// Waktu di luar jam kerja dan hari libur dilewati.
func (c *Calendar) Add(start time.Time, d time.Duration) time.Time {
	if d <= 0 {
		return start
	}
	if c.alwaysOn() {
		return start.Add(d)
	}
	loc := c.loc()
	t := start.In(loc)
	day := midnight(t, loc)
	for i := 0; i < maxDays; i++ {
		for _, iv := range c.dayIntervals(day) {
			if !t.Before(iv[1]) {
				continue
			}
			if t.Before(iv[0]) {
				t = iv[0]
			}
			avail := iv[1].Sub(t)
			if d <= avail {
				return t.Add(d)
			}
			d -= avail
			t = iv[1]
		}
		day = nextDay(day)
		if t.Before(day) {
			t = day
		}
	}
	return t.Add(d) // tidak tercapai pada kalender yang valid
}

// Between mengembalikan lama waktu operasional di antara a dan b (0 bila b ≤ a).
func (c *Calendar) Between(a, b time.Time) time.Duration {
	if !b.After(a) {
		return 0
	}
	if c.alwaysOn() {
		return b.Sub(a)
	}
	loc := c.loc()
	var total time.Duration
	day := midnight(a, loc)
	end := b.In(loc)
	for i := 0; i < maxDays && day.Before(end); i++ {
		for _, iv := range c.dayIntervals(day) {
			s, e := iv[0], iv[1]
			if s.Before(a) {
				s = a
			}
			if e.After(end) {
				e = end
			}
			if e.After(s) {
				total += e.Sub(s)
			}
		}
		day = nextDay(day)
	}
	return total
}

// NormalizeHours mengurutkan rentang per hari dan membuang yang tidak valid.
func NormalizeHours(h [8][]Interval) [8][]Interval {
	for d := 1; d <= 7; d++ {
		ivs := h[d][:0:0]
		for _, iv := range h[d] {
			if iv.Start >= 0 && iv.End <= 24*60 && iv.Start < iv.End {
				ivs = append(ivs, iv)
			}
		}
		sort.Slice(ivs, func(i, j int) bool { return ivs[i].Start < ivs[j].Start })
		merged := ivs[:0:0]
		for _, iv := range ivs { // gabungkan rentang yang tumpang tindih
			if n := len(merged); n > 0 && iv.Start <= merged[n-1].End {
				if iv.End > merged[n-1].End {
					merged[n-1].End = iv.End
				}
				continue
			}
			merged = append(merged, iv)
		}
		h[d] = merged
	}
	return h
}
