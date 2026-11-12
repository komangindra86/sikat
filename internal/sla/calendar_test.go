package sla

import (
	"database/sql"
	"testing"
	"time"
)

var wita = time.FixedZone("WITA", 8*3600)

// officeCal: Senin–Jumat 08.00–16.00, Selasa 17 Nov 2026 libur.
func officeCal() *Calendar {
	c := &Calendar{Name: "Jam kerja", Loc: wita, Holidays: map[string]string{"2026-11-17": "Libur uji"}}
	for d := 1; d <= 5; d++ {
		c.Hours[d] = []Interval{{8 * 60, 16 * 60}}
	}
	return c
}

// at: 16 Nov 2026 adalah Senin.
func at(day, hour, min int) time.Time { return time.Date(2026, 11, day, hour, min, 0, 0, wita) }

func TestCalendarAdd(t *testing.T) {
	c := officeCal()
	cases := []struct {
		name  string
		start time.Time
		d     time.Duration
		want  time.Time
	}{
		{"di dalam jam kerja", at(16, 9, 0), 2 * time.Hour, at(16, 11, 0)},
		{"tepat sampai jam tutup", at(16, 8, 0), 8 * time.Hour, at(16, 16, 0)},
		{"lintas hari + melewati hari libur Selasa", at(16, 15, 0), 2 * time.Hour, at(18, 9, 0)},
		{"mulai sebelum jam buka", at(18, 6, 0), 30 * time.Minute, at(18, 8, 30)},
		{"mulai setelah jam tutup", at(18, 18, 0), 30 * time.Minute, at(19, 8, 30)},
		{"lintas akhir pekan", at(20, 15, 0), 2 * time.Hour, at(23, 9, 0)},
		{"mulai hari Sabtu", at(21, 10, 0), time.Hour, at(23, 9, 0)},
		{"mulai di hari libur", at(17, 10, 0), time.Hour, at(18, 9, 0)},
		{"40 jam kerja = seminggu (dengan 1 hari libur)", at(16, 8, 0), 40 * time.Hour, at(23, 16, 0)},
		{"durasi nol", at(21, 10, 0), 0, at(21, 10, 0)},
	}
	for _, tc := range cases {
		if got := c.Add(tc.start, tc.d); !got.Equal(tc.want) {
			t.Errorf("%s: Add(%s, %s) = %s, mau %s", tc.name, tc.start.Format("Mon 02 15:04"), tc.d,
				got.In(wita).Format("Mon 02 15:04"), tc.want.Format("Mon 02 15:04"))
		}
	}
}

func TestCalendarBetween(t *testing.T) {
	c := officeCal()
	cases := []struct {
		name string
		a, b time.Time
		want time.Duration
	}{
		{"satu hari", at(16, 9, 0), at(16, 11, 30), 150 * time.Minute},
		{"lintas akhir pekan", at(20, 15, 0), at(23, 9, 0), 2 * time.Hour},
		{"di luar jam kerja", at(16, 17, 0), at(16, 23, 0), 0},
		{"melewati hari libur", at(16, 15, 0), at(18, 9, 0), 2 * time.Hour},
		{"terbalik", at(16, 11, 0), at(16, 9, 0), 0},
	}
	for _, tc := range cases {
		if got := c.Between(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: Between = %s, mau %s", tc.name, got, tc.want)
		}
	}
}

// Add dan Between harus konsisten: Between(s, Add(s, d)) == d untuk awal apa pun.
func TestCalendarAddBetweenConsistent(t *testing.T) {
	c := officeCal()
	c.Hours[6] = []Interval{{8 * 60, 12 * 60}, {13 * 60, 15 * 60}} // Sabtu dua sesi
	start := at(14, 0, 0)
	for i := 0; i < 400; i++ {
		s := start.Add(time.Duration(i*47) * time.Minute)
		d := time.Duration(1+i*13%2900) * time.Minute
		end := c.Add(s, d)
		if got := c.Between(s, end); got != d {
			t.Fatalf("awal %s, d=%s: Between(Add) = %s", s.Format("Mon 02 15:04"), d, got)
		}
	}
}

func TestCalendar24x7AndEmpty(t *testing.T) {
	c := &Calendar{Is24x7: true, Loc: wita, Holidays: map[string]string{"2026-11-17": "x"}}
	if got := c.Add(at(17, 23, 0), 2*time.Hour); !got.Equal(at(18, 1, 0)) {
		t.Errorf("24/7 mengabaikan jam & libur: %s", got)
	}
	empty := &Calendar{Loc: wita} // tanpa jam kerja → tidak boleh berhenti selamanya
	if got := empty.Add(at(21, 10, 0), time.Hour); !got.Equal(at(21, 11, 0)) {
		t.Errorf("kalender kosong diperlakukan 24/7: %s", got)
	}
}

func TestNormalizeHours(t *testing.T) {
	var h [8][]Interval
	h[1] = []Interval{{13 * 60, 16 * 60}, {8 * 60, 12 * 60}, {11 * 60, 14 * 60}, {20, 10}}
	h = NormalizeHours(h)
	if len(h[1]) != 1 || h[1][0] != (Interval{8 * 60, 16 * 60}) {
		t.Errorf("rentang tumpang tindih digabung & yang tidak valid dibuang: %v", h[1])
	}
}

func nullMin(n int64) sql.NullInt64  { return sql.NullInt64{Int64: n, Valid: true} }
func nullT(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }

func TestEvaluateStates(t *testing.T) {
	c := officeCal()
	base := Snapshot{Status: "NEW", CreatedAt: at(16, 9, 0), ResponseMinutes: nullMin(60), ResolutionMinutes: nullMin(480), WarnPct: 75}

	resp, reso := Evaluate(base, c, at(16, 9, 30))
	if resp.State != StateSafe || resp.Pct != 50 || !resp.Running || resp.Remaining != 30*time.Minute {
		t.Errorf("respons aman 50%%: %+v", resp)
	}
	if !resp.DueAt.Equal(at(16, 10, 0)) || !reso.DueAt.Equal(at(18, 9, 0)) {
		t.Errorf("batas waktu: respons %s, selesai %s", resp.DueAt, reso.DueAt)
	}
	if resp, _ = Evaluate(base, c, at(16, 9, 50)); resp.State != StateWarning {
		t.Errorf("≥75%% harus peringatan: %+v", resp)
	}
	if resp, _ = Evaluate(base, c, at(16, 10, 30)); resp.State != StateLate || resp.Over() != 30*time.Minute {
		t.Errorf("lewat target harus terlambat: %+v", resp)
	}
	// Di luar jam kerja waktu tidak berjalan.
	night := base
	night.CreatedAt = at(16, 15, 50)
	if resp, _ = Evaluate(night, c, at(18, 8, 0)); resp.State != StateSafe || resp.Used != 10*time.Minute {
		t.Errorf("malam & hari libur tidak dihitung: %+v", resp)
	}

	// Diambil tepat waktu, selesai terlambat.
	done := base
	done.Status = "RESOLVED"
	done.FirstResponseAt = nullT(at(16, 9, 20))
	done.ResolvedAt = nullT(at(18, 10, 0))
	resp, reso = Evaluate(done, c, at(19, 12, 0))
	if resp.State != StateMet || resp.Running || reso.State != StateMissed || reso.Used != 9*time.Hour {
		t.Errorf("respons tercapai, penyelesaian terlewati: resp %+v reso %+v", resp, reso)
	}

	if resp, reso = Evaluate(Snapshot{Status: "CANCELLED", ResponseMinutes: nullMin(60)}, c, at(16, 9, 0)); resp.Defined || reso.Defined {
		t.Error("tiket dibatalkan tidak dinilai")
	}
}

func TestEvaluatePause(t *testing.T) {
	c := officeCal()
	s := Snapshot{Status: "PENDING", CreatedAt: at(16, 8, 0), FirstResponseAt: nullT(at(16, 8, 5)),
		ResolutionMinutes: nullMin(240), WarnPct: 75, PausedMinutes: 60, PausedSince: nullT(at(16, 11, 0))}
	_, reso := Evaluate(s, c, at(16, 13, 0))
	// 5 jam berlalu − 1 jam jeda lama − 2 jam jeda berjalan = 2 jam terpakai.
	if reso.State != StatePaused || reso.Used != 2*time.Hour || !reso.DueAt.IsZero() {
		t.Errorf("dijeda: %+v", reso)
	}
	s.Status, s.PausedSince = "IN_PROGRESS", sql.NullTime{}
	s.PausedMinutes = 180
	_, reso = Evaluate(s, c, at(16, 13, 0))
	// 5 jam − 3 jam jeda = 2 jam; batas = 08:00 + 4 jam + 3 jam jeda = 15:00 Senin.
	if reso.State != StateSafe || reso.Used != 2*time.Hour || !reso.DueAt.Equal(at(16, 15, 0)) {
		t.Errorf("setelah dilanjutkan, batas waktu bergeser sebesar jeda: %+v", reso)
	}
	// Siklus baru setelah dibuka ulang dihitung dari awal siklus.
	r := Snapshot{Status: "IN_PROGRESS", CreatedAt: at(16, 8, 0), CycleStartedAt: nullT(at(19, 10, 0)),
		FirstResponseAt: nullT(at(16, 8, 1)), ResolutionMinutes: nullMin(120), WarnPct: 75}
	if _, reso = Evaluate(r, c, at(19, 11, 0)); reso.Used != time.Hour || reso.State != StateSafe {
		t.Errorf("siklus baru: %+v", reso)
	}
}
