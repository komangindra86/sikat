package sla

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"sikat/internal/clock"
)

// Counts merangkum status SLA tiket aktif.
type Counts struct {
	Active, Safe, Warning, Late, Paused int
}

// EscalationRow adalah satu kejadian eskalasi untuk ditampilkan.
type EscalationRow struct {
	TicketID    int64
	TicketNo    string
	Kind        string
	Priority    string
	Cycle       int
	UsedPct     sql.NullInt64
	Notify      bool
	TriggeredAt time.Time
}

// Overview adalah data dashboard SLA.
type Overview struct {
	Counts  Counts
	Tickets []TicketRow // tiket aktif, paling mendesak di atas
	Recent  []EscalationRow
}

func urgency(t TicketRow) int {
	m := t.Current()
	switch m.State {
	case StateLate:
		return 3000 + m.Pct
	case StateWarning:
		return 2000 + m.Pct
	case StatePaused:
		return 500 + m.Pct
	}
	return 1000 + m.Pct
}

// CountsOf menghitung jumlah per status dari tiket aktif.
func CountsOf(list []TicketRow) Counts {
	var c Counts
	for _, t := range list {
		c.Active++
		switch t.Current().State {
		case StateLate:
			c.Late++
		case StateWarning:
			c.Warning++
		case StatePaused:
			c.Paused++
		default:
			c.Safe++
		}
	}
	return c
}

// Overview memuat status SLA seluruh tiket aktif dan eskalasi terbaru.
func (s *Service) Overview(ctx context.Context, recent int) (Overview, error) {
	var o Overview
	now := s.clock.Now()
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return o, err
	}
	o.Tickets, err = loadTicketRows(ctx, s.db, cals, now, activeWhere)
	if err != nil {
		return o, err
	}
	sort.SliceStable(o.Tickets, func(i, j int) bool { return urgency(o.Tickets[i]) > urgency(o.Tickets[j]) })
	o.Counts = CountsOf(o.Tickets)
	o.Recent, err = s.RecentEscalations(ctx, recent)
	return o, err
}

// Summary hanya menghitung jumlah per status (untuk beranda).
func (s *Service) Summary(ctx context.Context) (Counts, error) {
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return Counts{}, err
	}
	list, err := loadTicketRows(ctx, s.db, cals, s.clock.Now(), activeWhere)
	if err != nil {
		return Counts{}, err
	}
	return CountsOf(list), nil
}

// RecentEscalations mengambil kejadian eskalasi terbaru.
func (s *Service) RecentEscalations(ctx context.Context, limit int) ([]EscalationRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.ticket_id, t.ticket_no, e.kind, e.priority, e.cycle, e.used_pct, e.notify, e.triggered_at
		  FROM sla_escalations e JOIN tickets t ON t.id = e.ticket_id
		 ORDER BY e.triggered_at DESC, e.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EscalationRow
	for rows.Next() {
		var e EscalationRow
		if err := rows.Scan(&e.TicketID, &e.TicketNo, &e.Kind, &e.Priority, &e.Cycle, &e.UsedPct, &e.Notify, &e.TriggeredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TicketSLA memuat evaluasi SLA beberapa tiket sekaligus (untuk antrean & detail).
func (s *Service) TicketSLA(ctx context.Context, ids []int64) (map[int64]TicketRow, error) {
	out := map[int64]TicketRow{}
	if len(ids) == 0 {
		return out, nil
	}
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return nil, err
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	list, err := loadTicketRows(ctx, s.db, cals, s.clock.Now(),
		`WHERE t.sla_policy_id IS NOT NULL AND t.id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		out[t.ID] = t
	}
	return out, nil
}

// Tally adalah rekap capaian satu target.
type Tally struct {
	Met, Missed, Running int
}

// Rate adalah persentase tercapai dari yang sudah dinilai (−1 bila belum ada).
func (t Tally) Rate() int {
	if n := t.Met + t.Missed; n > 0 {
		return t.Met * 100 / n
	}
	return -1
}

// PriorityTally adalah rekap per prioritas.
type PriorityTally struct {
	Priority   string
	Total      int
	Response   Tally
	Resolution Tally
}

// BreachRow adalah tiket yang melanggar SLA pada bulan laporan.
type BreachRow struct {
	TicketRow
	ResponseMissed   bool
	ResolutionMissed bool
}

// Report adalah laporan SLA bulanan (berdasarkan tiket yang dibuat pada bulan itu).
type Report struct {
	Month      time.Time // tanggal 1, WITA
	Total      int       // tiket dengan SLA (tidak termasuk dibatalkan)
	Response   Tally
	Resolution Tally
	ByPriority []PriorityTally
	Breaches   []BreachRow
}

// MonthlyReport menyusun laporan pelanggaran SLA untuk bulan month (WITA).
// Pelanggaran penyelesaian pada siklus mana pun (tiket dibuka ulang) ikut dihitung.
func (s *Service) MonthlyReport(ctx context.Context, month time.Time) (Report, error) {
	m := month.In(clock.Location)
	start := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, clock.Location)
	end := start.AddDate(0, 1, 0)
	r := Report{Month: start}
	cals, err := LoadCalendars(ctx, s.db)
	if err != nil {
		return r, err
	}
	list, err := loadTicketRows(ctx, s.db, cals, s.clock.Now(),
		`WHERE t.sla_policy_id IS NOT NULL AND t.status <> 'CANCELLED' AND t.created_at >= ? AND t.created_at < ?
		 ORDER BY t.created_at`, start.UTC(), end.UTC())
	if err != nil {
		return r, err
	}

	// Pelanggaran yang tercatat (termasuk siklus sebelumnya).
	breached := map[int64]map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.ticket_id, e.kind FROM sla_escalations e JOIN tickets t ON t.id = e.ticket_id
		 WHERE e.kind IN ('RESPONSE_BREACH','RESOLUTION_BREACH') AND t.created_at >= ? AND t.created_at < ?`,
		start.UTC(), end.UTC())
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var id int64
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			rows.Close()
			return r, err
		}
		if breached[id] == nil {
			breached[id] = map[string]bool{}
		}
		breached[id][kind] = true
	}
	rows.Close()

	byP := map[string]*PriorityTally{}
	for _, p := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		byP[p] = &PriorityTally{Priority: p}
		r.ByPriority = append(r.ByPriority, PriorityTally{})
	}
	tally := func(t *Tally, m Measure, recorded bool) bool {
		switch {
		case !m.Defined:
			return false
		case recorded || m.Late():
			t.Missed++
			return true
		case m.Running:
			t.Running++
		default:
			t.Met++
		}
		return false
	}
	for _, t := range list {
		r.Total++
		pt := byP[t.Priority]
		pt.Total++
		rm := tally(&r.Response, t.Resp, breached[t.ID][KindResponseBreach])
		tally(&pt.Response, t.Resp, breached[t.ID][KindResponseBreach])
		sm := tally(&r.Resolution, t.Reso, breached[t.ID][KindResolutionBreach])
		tally(&pt.Resolution, t.Reso, breached[t.ID][KindResolutionBreach])
		if rm || sm {
			r.Breaches = append(r.Breaches, BreachRow{TicketRow: t, ResponseMissed: rm, ResolutionMissed: sm})
		}
	}
	for i, p := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		r.ByPriority[i] = *byP[p]
	}
	return r, nil
}
