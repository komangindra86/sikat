package sla

import (
	"database/sql"
	"time"
)

// Status SLA untuk tampilan dan eskalasi.
const (
	StateSafe    = "SAFE"    // berjalan, di bawah ambang peringatan
	StateWarning = "WARNING" // berjalan, ≥ ambang peringatan
	StateLate    = "LATE"    // berjalan, target terlewati
	StatePaused  = "PAUSED"  // dihentikan sementara (alasan tunda yang dikonfigurasi)
	StateMet     = "MET"     // selesai dalam target
	StateMissed  = "MISSED"  // selesai melewati target
)

// Snapshot adalah data SLA satu tiket (disalin dari kolom tickets).
type Snapshot struct {
	Status            string
	CreatedAt         time.Time
	CycleStartedAt    sql.NullTime // awal siklus (dibuat / dibuka ulang); kosong = CreatedAt
	FirstResponseAt   sql.NullTime
	ResolvedAt        sql.NullTime
	ResponseMinutes   sql.NullInt64
	ResolutionMinutes sql.NullInt64
	WarnPct           int
	PausedSince       sql.NullTime
	PausedMinutes     int
}

// Measure adalah hasil evaluasi satu target (respons atau penyelesaian).
type Measure struct {
	Defined   bool
	Running   bool
	Target    time.Duration
	Used      time.Duration // waktu operasional terpakai (dikurangi jeda)
	Remaining time.Duration // negatif bila terlambat
	Pct       int
	State     string
	DueAt     time.Time // nol selama dijeda
	WarnPct   int
}

// Late melaporkan apakah target terlewati (berjalan atau sudah selesai).
func (m Measure) Late() bool { return m.State == StateLate || m.State == StateMissed }

// Over mengembalikan lama keterlambatan (0 bila belum terlambat).
func (m Measure) Over() time.Duration {
	if m.Remaining < 0 {
		return -m.Remaining
	}
	return 0
}

// BarPct adalah persentase untuk progress bar (0–100).
func (m Measure) BarPct() int {
	if m.Pct > 100 {
		return 100
	}
	return m.Pct
}

func isActive(status string) bool {
	switch status {
	case "NEW", "CLAIMED", "IN_PROGRESS", "PENDING":
		return true
	}
	return false
}

func pct(used, target time.Duration) int {
	if target <= 0 {
		return 0
	}
	p := int(used * 100 / target)
	if p > 999 {
		p = 999
	}
	return p
}

func stateOf(m *Measure, paused bool) {
	m.Remaining = m.Target - m.Used
	m.Pct = pct(m.Used, m.Target)
	switch {
	case !m.Running && m.Used <= m.Target:
		m.State = StateMet
	case !m.Running:
		m.State = StateMissed
	case m.Used > m.Target:
		m.State = StateLate
	case paused:
		m.State = StatePaused
	case m.Pct >= m.WarnPct:
		m.State = StateWarning
	default:
		m.State = StateSafe
	}
}

// Evaluate menghitung target respons dan penyelesaian pada saat now.
//
// Respons: dari tiket dibuat sampai pertama kali diambil petugas (hanya siklus pertama).
// Penyelesaian: dari awal siklus sampai RESOLVED, dikurangi waktu jeda yang
// dikonfigurasi. Tiket dibatalkan tidak dinilai.
func Evaluate(s Snapshot, cal *Calendar, now time.Time) (resp, reso Measure) {
	if s.Status == "CANCELLED" {
		return
	}
	warn := s.WarnPct
	if warn <= 0 || warn >= 100 {
		warn = 75
	}

	if s.ResponseMinutes.Valid && s.ResponseMinutes.Int64 > 0 {
		resp = Measure{Defined: true, WarnPct: warn, Target: time.Duration(s.ResponseMinutes.Int64) * time.Minute}
		end := now
		switch {
		case s.FirstResponseAt.Valid:
			end = s.FirstResponseAt.Time
		case s.Status == "NEW":
			resp.Running = true
		default:
			resp.Defined = false // tidak pernah diambil (data lama) — tidak dinilai
		}
		if resp.Defined {
			resp.Used = cal.Between(s.CreatedAt, end)
			resp.DueAt = cal.Add(s.CreatedAt, resp.Target)
			stateOf(&resp, false)
		}
	}

	if s.ResolutionMinutes.Valid && s.ResolutionMinutes.Int64 > 0 {
		reso = Measure{Defined: true, WarnPct: warn, Target: time.Duration(s.ResolutionMinutes.Int64) * time.Minute}
		start := s.CreatedAt
		if s.CycleStartedAt.Valid {
			start = s.CycleStartedAt.Time
		}
		end := now
		switch {
		case isActive(s.Status):
			reso.Running = true
		case s.ResolvedAt.Valid:
			end = s.ResolvedAt.Time
		default:
			reso.Defined = false
		}
		if reso.Defined {
			paused := time.Duration(s.PausedMinutes) * time.Minute
			pausedNow := reso.Running && s.PausedSince.Valid
			if pausedNow {
				paused += cal.Between(s.PausedSince.Time, now)
			}
			if used := cal.Between(start, end) - paused; used > 0 {
				reso.Used = used
			}
			if !pausedNow {
				reso.DueAt = cal.Add(start, reso.Target+time.Duration(s.PausedMinutes)*time.Minute)
			}
			stateOf(&reso, pausedNow)
		}
	}
	return
}

// Current mengembalikan target yang sedang relevan untuk ditampilkan ringkas:
// respons selama tiket belum diambil, selain itu penyelesaian.
func Current(resp, reso Measure) (Measure, string) {
	if resp.Defined && resp.Running {
		return resp, "RESPONSE"
	}
	if reso.Defined {
		return reso, "RESOLUTION"
	}
	return Measure{}, ""
}
