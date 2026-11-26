package reports

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"sikat/internal/clock"
	"sikat/internal/web"
)

// Batas baris: HTML menampilkan sebagian, ekspor memuat semua sampai MaxRows.
const (
	MaxRows     = 20000
	HTMLMaxRows = 500
)

// Filter yang dapat dipakai sebuah laporan.
const (
	FUnit      = "unit"
	FBidang    = "bidang"
	FPetugas   = "petugas"
	FPrioritas = "prioritas"
	FStatus    = "status"
)

// Def adalah definisi satu laporan.
type Def struct {
	Slug, Title, Group, Description string
	Filters                         []string
	run                             func(ctx context.Context, s *Service, f Filter) (Table, error)
}

// Uses memeriksa apakah laporan memakai filter tertentu (template).
func (d Def) Uses(name string) bool {
	for _, f := range d.Filters {
		if f == name {
			return true
		}
	}
	return false
}

var ticketFilters = []string{FUnit, FBidang, FPetugas, FPrioritas, FStatus}

// Defs adalah daftar laporan (master prompt §18) dalam urutan tampil.
var Defs = []Def{
	{"koordinator", "Laporan Koordinator IT", "Laporan koordinator", "Pengganti spreadsheet: seluruh penanganan tiket dan kegiatan internal pada periode, terisi otomatis.", ticketFilters, runCoordinator},
	{"bulanan", "Rekap tiket bulanan", "Tiket", "Jumlah tiket masuk per bulan (per hari untuk periode ≤ 2 bulan) beserta status akhirnya.", ticketFilters, runMonthly},
	{"bidang", "Rekap per bidang", "Tiket", "Tiket per bidang IT: jumlah, selesai, waktu respons & penyelesaian.", ticketFilters, groupReport("bidang")},
	{"unit", "Rekap per unit", "Tiket", "Tiket per unit/bagian pelapor.", ticketFilters, groupReport("unit")},
	{"petugas", "Rekap per petugas", "Tiket", "Tiket per penanggung jawab, termasuk keterlibatan sebagai pendukung.", ticketFilters, runByTechnician},
	{"prioritas", "Rekap per prioritas", "Tiket", "Tiket per prioritas.", ticketFilters, groupReport("prioritas")},
	{"pemilahan", "Pemilahan jenis gangguan", "Tiket", "Tiket di luar daftar (Triase/\"Lainnya\") dan tiket yang jenisnya dikoreksi — bahan untuk menambah jenis gangguan baru.", []string{FUnit, FBidang, FPetugas}, runTriage},
	{"tertunda", "Rekap tiket tertunda", "Tiket", "Setiap kali tiket ditunda pada periode: alasan dan lamanya.", ticketFilters, runPending},
	{"sla", "Rekap SLA", "Kinerja & SLA", "Kepatuhan target respons & penyelesaian per prioritas (berdasarkan buku besar eskalasi).", []string{FUnit, FBidang, FPetugas, FPrioritas}, runSLA},
	{"respons", "Waktu respons", "Kinerja & SLA", "Waktu sejak tiket masuk sampai diambil petugas (waktu kalender).", []string{FUnit, FBidang, FPetugas, FPrioritas}, durationReport(false)},
	{"penyelesaian", "Waktu penyelesaian", "Kinerja & SLA", "Waktu sejak tiket masuk sampai diselesaikan (waktu kalender).", []string{FUnit, FBidang, FPetugas, FPrioritas}, durationReport(true)},
	{"beban-kerja", "Beban kerja petugas", "Kinerja & SLA", "Penugasan tiket, penyelesaian, dan kegiatan internal per petugas.", []string{FUnit, FBidang, FPetugas}, runWorkload},
	{"kegiatan", "Kegiatan internal", "Kegiatan", "Daftar kegiatan internal IT beserta pelaksana dan hasilnya.", []string{FUnit, FBidang, FPetugas}, runActivities},
	{"sering-rusak", "Perangkat sering rusak", "Perangkat", "Perangkat dengan jumlah gangguan mencapai ambang kerusakan berulang pada periode.", []string{FUnit}, runRepeatDamage},
	{"pemeliharaan", "Riwayat pemeliharaan", "Perangkat", "Seluruh riwayat pemeliharaan perangkat (dari tiket, kegiatan, dan input manual).", []string{FUnit, FPetugas}, runMaintenance},
	{"downtime", "Downtime perangkat", "Perangkat", "Total waktu perangkat tidak dapat dipakai, dari riwayat pemeliharaan.", []string{FUnit}, runDowntime},
}

// Find mencari definisi laporan.
func Find(slug string) (Def, bool) {
	for _, d := range Defs {
		if d.Slug == slug {
			return d, true
		}
	}
	return Def{}, false
}

// Service menjalankan laporan.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	// RepeatThreshold mengembalikan ambang kerusakan berulang (pengaturan sistem).
	RepeatThreshold func(ctx context.Context) int
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock) *Service {
	return &Service{db: db, clock: c, RepeatThreshold: func(context.Context) int { return 3 }}
}

// Now mengembalikan waktu sekarang (jam sistem/uji).
func (s *Service) Now() time.Time { return s.clock.Now() }

// Run menjalankan laporan.
func (s *Service) Run(ctx context.Context, d Def, f Filter) (Table, error) {
	t, err := d.run(ctx, s, f)
	if err != nil {
		return t, fmt.Errorf("laporan %s: %w", d.Slug, err)
	}
	t.Title = d.Title
	return t, nil
}

func priorityLabel(p string) string { return web.Label("priority", p) }

// ---------------------------------------------------------------------------
// Laporan koordinator (pengganti spreadsheet)
// ---------------------------------------------------------------------------

type coordRow struct {
	at    time.Time
	cells []Cell
}

func runCoordinator(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"No", KindInt, 4}, {"Tanggal", KindDate, 8}, {"Unit/Bagian", KindText, 13}, {"Bidang", KindText, 10},
		{"Kegiatan", KindText, 26}, {"Pelaksana", KindText, 11}, {"Pendukung", KindText, 11},
		{"Status", KindText, 8}, {"Hasil/Keterangan", KindText, 20},
	}}
	c := f.ticketCond()
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.ticket_no, t.created_at, d.name, r.name, ic.name, tc.name, t.description, ua.full_name,
		       (SELECT GROUP_CONCAT(u.full_name ORDER BY u.full_name SEPARATOR ', ')
		          FROM ticket_assignments ta JOIN users u ON u.id = ta.user_id
		         WHERE ta.ticket_id = t.id AND ta.role = 'SUPPORT' AND ta.released_at IS NULL),
		       t.status, t.resolution_action, t.resolution_result, pr.label, t.cancel_reason
		  FROM tickets t
		  JOIN it_categories ic ON ic.id = t.it_category_id
		  JOIN ticket_categories tc ON tc.id = t.ticket_category_id
		  LEFT JOIN departments d ON d.id = t.department_id
		  LEFT JOIN rooms r ON r.id = t.room_id
		  LEFT JOIN users ua ON ua.id = t.assignee_id
		  LEFT JOIN pending_reasons pr ON pr.id = t.pending_reason_id
		 WHERE `+c.where()+` ORDER BY t.created_at LIMIT ?`, append(c.args, MaxRows)...)
	if err != nil {
		return t, err
	}
	var all []coordRow
	var nTickets, nDone int
	for rows.Next() {
		var id int64
		var no, cat, kind, desc, status string
		var created time.Time
		var dept, room, lead, support, action, result, reason, cancel sql.NullString
		if err := rows.Scan(&id, &no, &created, &dept, &room, &cat, &kind, &desc, &lead, &support, &status, &action, &result, &reason, &cancel); err != nil {
			rows.Close()
			return t, err
		}
		unit := dept.String
		if room.Valid && room.String != "" && room.String != dept.String {
			unit = strings.TrimPrefix(unit+" – "+room.String, " – ")
		}
		hasil := ""
		switch status {
		case "RESOLVED", "CLOSED":
			nDone++
			hasil = action.String
			if result.Valid {
				hasil = strings.TrimSpace(hasil + " (" + web.Label("resolution_result", result.String) + ")")
			}
		case "PENDING":
			hasil = "Ditunda: " + reason.String
		case "CANCELLED":
			hasil = "Dibatalkan: " + cancel.String
		}
		nTickets++
		k := text(no + " · " + kind + ": " + clipText(desc, 140))
		k.Link = fmt.Sprintf("/app/tickets/%d", id)
		all = append(all, coordRow{created, []Cell{{}, dateCell(created), text(unit), text(cat), k, nullText(lead), nullText(support),
			text(web.Label("ticket_status", status)), text(hasil)}})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return t, err
	}

	// Kegiatan internal: filter prioritas/status tiket tidak berlaku.
	nActs := 0
	if f.Priority == "" && f.Status == "" {
		ac := f.activityCond()
		arows, err := s.db.QueryContext(ctx, `
			SELECT a.id, a.activity_no, a.activity_date, d.name, r.name, ic.name, ty.name, a.title, a.status, a.result,
			       (SELECT u.full_name FROM activity_members m JOIN users u ON u.id = m.user_id WHERE m.activity_id = a.id AND m.role = 'LEAD'),
			       (SELECT GROUP_CONCAT(u.full_name ORDER BY u.full_name SEPARATOR ', ')
			          FROM activity_members m JOIN users u ON u.id = m.user_id WHERE m.activity_id = a.id AND m.role = 'SUPPORT')
			  FROM internal_activities a
			  JOIN activity_types ty ON ty.id = a.activity_type_id
			  JOIN it_categories ic ON ic.id = a.it_category_id
			  LEFT JOIN departments d ON d.id = a.department_id
			  LEFT JOIN rooms r ON r.id = a.room_id
			 WHERE `+ac.where()+` ORDER BY a.activity_date, a.id LIMIT ?`, append(ac.args, MaxRows)...)
		if err != nil {
			return t, err
		}
		for arows.Next() {
			var id int64
			var no, cat, typ, title, status string
			var date time.Time
			var dept, room, result, lead, support sql.NullString
			if err := arows.Scan(&id, &no, &date, &dept, &room, &cat, &typ, &title, &status, &result, &lead, &support); err != nil {
				arows.Close()
				return t, err
			}
			unit := dept.String
			if room.Valid && room.String != "" {
				unit = strings.TrimPrefix(unit+" – "+room.String, " – ")
			}
			if unit == "" {
				unit = "Internal IT"
			}
			nActs++
			// DATE kegiatan adalah tanggal WITA; jadikan tengah malam WITA agar urutan benar.
			at := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, clock.Location)
			k := text(no + " · " + typ + ": " + title)
			k.Link = fmt.Sprintf("/app/activities/%d", id)
			all = append(all, coordRow{at, []Cell{{}, dateCell(at), text(unit), text(cat), k, nullText(lead), nullText(support),
				text(web.Label("activity_status", status)), nullText(result)}})
		}
		arows.Close()
		if err := arows.Err(); err != nil {
			return t, err
		}
	} else {
		t.Notes = append(t.Notes, "Filter prioritas/status hanya berlaku untuk tiket; kegiatan internal tidak ditampilkan.")
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	for i, r := range all {
		r.cells[0] = intCell(i + 1)
		t.Rows = append(t.Rows, r.cells)
	}
	t.Metrics = []Metric{
		{"Penanganan tiket", formatInt(int64(nTickets)), ""},
		{"Tiket selesai", formatInt(int64(nDone)), "green"},
		{"Kegiatan internal", formatInt(int64(nActs)), ""},
		{"Total baris", formatInt(int64(len(all))), ""},
	}
	t.Notes = append(t.Notes, "Tiket berdasarkan tanggal masuk; kegiatan berdasarkan tanggal kegiatan. Data diambil otomatis dari tiket dan kegiatan internal.")
	return t, nil
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---------------------------------------------------------------------------
// Rekap bulanan
// ---------------------------------------------------------------------------

func runMonthly(ctx context.Context, s *Service, f Filter) (Table, error) {
	daily := f.End.Sub(f.Start) <= 62*24*time.Hour
	format, label := "%Y-%m", "Bulan"
	if daily {
		format, label = "%Y-%m-%d", "Tanggal"
	}
	t := Table{Columns: []Column{
		{label, KindText, 14}, {"Masuk", KindInt, 8}, {"Selesai/ditutup", KindInt, 10}, {"Masih aktif", KindInt, 9},
		{"Dibatalkan", KindInt, 9}, {"Kritis", KindInt, 8}, {"Melanggar SLA", KindInt, 10}, {"% selesai", KindPct, 9},
	}}
	c := f.ticketCond()
	// WITA = UTC+8 tanpa DST: pengelompokan memakai offset tetap.
	rows, err := s.db.QueryContext(ctx, `
		SELECT DATE_FORMAT(DATE_ADD(t.created_at, INTERVAL 8 HOUR), '`+format+`') b,
		       COUNT(*), SUM(t.status IN ('RESOLVED','CLOSED')), SUM(t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING')),
		       SUM(t.status = 'CANCELLED'), SUM(t.priority = 'CRITICAL'),
		       SUM(EXISTS (SELECT 1 FROM sla_escalations e WHERE e.ticket_id = t.id AND e.kind IN ('RESPONSE_BREACH','RESOLUTION_BREACH')))
		  FROM tickets t WHERE `+c.where()+` GROUP BY b`, c.args...)
	if err != nil {
		return t, err
	}
	type agg struct{ total, done, active, cancelled, critical, breached int }
	data := map[string]agg{}
	for rows.Next() {
		var b string
		var a agg
		if err := rows.Scan(&b, &a.total, &a.done, &a.active, &a.cancelled, &a.critical, &a.breached); err != nil {
			rows.Close()
			return t, err
		}
		data[b] = a
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return t, err
	}
	chart := &Chart{Series: []string{"Selesai/ditutup", "Masih aktif", "Dibatalkan"}}
	var tot agg
	start, end := f.Start.In(clock.Location), f.End.In(clock.Location)
	for cur := start; cur.Before(end); {
		var key, name, short string
		var next time.Time
		if daily {
			key, name, short = cur.Format("2006-01-02"), cur.Format("02/01/2006"), cur.Format("2")
			next = cur.AddDate(0, 0, 1)
		} else {
			key = cur.Format("2006-01")
			name = monthNames[cur.Month()-1] + " " + cur.Format("2006")
			short = monthNames[cur.Month()-1][:3]
			next = time.Date(cur.Year(), cur.Month()+1, 1, 0, 0, 0, 0, clock.Location)
		}
		a := data[key]
		tot.total += a.total
		tot.done += a.done
		tot.active += a.active
		tot.cancelled += a.cancelled
		tot.critical += a.critical
		tot.breached += a.breached
		t.Rows = append(t.Rows, []Cell{text(name), intCell(a.total), intCell(a.done), intCell(a.active), intCell(a.cancelled),
			intCell(a.critical), intCell(a.breached), pctCell(a.done, a.total)})
		chart.Bars = append(chart.Bars, Bar{Label: short, Values: []int{a.done, a.active, a.cancelled}})
		cur = next
	}
	t.Totals = []Cell{text("Total"), intCell(tot.total), intCell(tot.done), intCell(tot.active), intCell(tot.cancelled),
		intCell(tot.critical), intCell(tot.breached), pctCell(tot.done, tot.total)}
	t.Chart = chart
	t.Metrics = []Metric{
		{"Tiket masuk", formatInt(int64(tot.total)), ""},
		{"Selesai/ditutup", formatInt(int64(tot.done)), "green"},
		{"Masih aktif", formatInt(int64(tot.active)), "amber"},
		{"Melanggar SLA", formatInt(int64(tot.breached)), "red"},
	}
	t.Notes = []string{"Dikelompokkan menurut tanggal tiket masuk (WITA); status adalah status saat laporan dibuat."}
	return t, nil
}

// ---------------------------------------------------------------------------
// Rekap per bidang / unit / prioritas
// ---------------------------------------------------------------------------

func groupReport(by string) func(ctx context.Context, s *Service, f Filter) (Table, error) {
	return func(ctx context.Context, s *Service, f Filter) (Table, error) {
		var key, name, join, label string
		switch by {
		case "bidang":
			key, name, join, label = "t.it_category_id", "ic.name", "JOIN it_categories ic ON ic.id = t.it_category_id", "Bidang"
		case "unit":
			key, name, join, label = "t.department_id", "COALESCE(d.name, 'Tanpa unit')", "LEFT JOIN departments d ON d.id = t.department_id", "Unit/Bagian"
		default:
			key, name, join, label = "t.priority", "t.priority", "", "Prioritas"
		}
		t := Table{Columns: []Column{
			{label, KindText, 18}, {"Masuk", KindInt, 7}, {"Selesai/ditutup", KindInt, 9}, {"Masih aktif", KindInt, 8},
			{"Dibatalkan", KindInt, 8}, {"Rata-rata respons", KindDuration, 11}, {"Rata-rata penyelesaian", KindDuration, 12},
			{"Melanggar SLA", KindInt, 9}, {"% selesai", KindPct, 8},
		}}
		c := f.ticketCond()
		order := "total DESC, label"
		if by == "prioritas" {
			order = "FIELD(t.priority, 'CRITICAL', 'HIGH', 'MEDIUM', 'LOW')"
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT `+name+` label, COUNT(*) total,
			       SUM(t.status IN ('RESOLVED','CLOSED')), SUM(t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING')),
			       SUM(t.status = 'CANCELLED'),
			       AVG(TIMESTAMPDIFF(SECOND, t.created_at, t.first_response_at)) / 60,
			       AVG(TIMESTAMPDIFF(SECOND, t.created_at, t.resolved_at)) / 60,
			       SUM(EXISTS (SELECT 1 FROM sla_escalations e WHERE e.ticket_id = t.id AND e.kind IN ('RESPONSE_BREACH','RESOLUTION_BREACH')))
			  FROM tickets t `+join+` WHERE `+c.where()+`
			 GROUP BY `+key+`, label ORDER BY `+order, c.args...)
		if err != nil {
			return t, err
		}
		defer rows.Close()
		var tot struct{ total, done, active, cancelled, breached int }
		for rows.Next() {
			var lbl string
			var total, done, active, cancelled, breached int
			var resp, res sql.NullFloat64
			if err := rows.Scan(&lbl, &total, &done, &active, &cancelled, &resp, &res, &breached); err != nil {
				return t, err
			}
			if by == "prioritas" {
				lbl = priorityLabel(lbl)
			}
			tot.total += total
			tot.done += done
			tot.active += active
			tot.cancelled += cancelled
			tot.breached += breached
			t.Rows = append(t.Rows, []Cell{text(lbl), intCell(total), intCell(done), intCell(active), intCell(cancelled),
				durCell(resp), durCell(res), intCell(breached), pctCell(done, total)})
		}
		if err := rows.Err(); err != nil {
			return t, err
		}
		var resp, res sql.NullFloat64
		if err := s.db.QueryRowContext(ctx, `SELECT AVG(TIMESTAMPDIFF(SECOND, t.created_at, t.first_response_at)) / 60,
			AVG(TIMESTAMPDIFF(SECOND, t.created_at, t.resolved_at)) / 60 FROM tickets t WHERE `+c.where(), c.args...).Scan(&resp, &res); err != nil {
			return t, err
		}
		t.Totals = []Cell{text("Total"), intCell(tot.total), intCell(tot.done), intCell(tot.active), intCell(tot.cancelled),
			durCell(resp), durCell(res), intCell(tot.breached), pctCell(tot.done, tot.total)}
		t.Notes = []string{"Tiket yang masuk pada periode. Waktu respons & penyelesaian adalah waktu kalender (bukan jam kerja); kepatuhan SLA memakai jam kerja di laporan Rekap SLA."}
		return t, nil
	}
}

// ---------------------------------------------------------------------------
// Rekap per petugas
// ---------------------------------------------------------------------------

func runByTechnician(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"Petugas", KindText, 18}, {"Penanggung jawab", KindInt, 10}, {"Pendukung", KindInt, 8}, {"Selesai/ditutup", KindInt, 9},
		{"Masih aktif", KindInt, 8}, {"Rata-rata respons", KindDuration, 11}, {"Rata-rata penyelesaian", KindDuration, 12}, {"Melanggar SLA", KindInt, 9},
	}}
	c := f.ticketCond()
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(t.assignee_id, 0), COALESCE(u.full_name, 'Belum diambil'), COUNT(*),
		       SUM(t.status IN ('RESOLVED','CLOSED')), SUM(t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING')),
		       AVG(TIMESTAMPDIFF(SECOND, t.created_at, t.first_response_at)) / 60,
		       AVG(TIMESTAMPDIFF(SECOND, t.created_at, t.resolved_at)) / 60,
		       SUM(EXISTS (SELECT 1 FROM sla_escalations e WHERE e.ticket_id = t.id AND e.kind IN ('RESPONSE_BREACH','RESOLUTION_BREACH')))
		  FROM tickets t LEFT JOIN users u ON u.id = t.assignee_id
		 WHERE `+c.where()+` GROUP BY t.assignee_id, u.full_name ORDER BY t.assignee_id IS NULL, COUNT(*) DESC`, c.args...)
	if err != nil {
		return t, err
	}
	type rowT struct {
		id                         int64
		name                       string
		primary, done, active, brk int
		resp, res                  sql.NullFloat64
		support                    int
	}
	var list []*rowT
	byID := map[int64]*rowT{}
	for rows.Next() {
		r := &rowT{}
		if err := rows.Scan(&r.id, &r.name, &r.primary, &r.done, &r.active, &r.resp, &r.res, &r.brk); err != nil {
			rows.Close()
			return t, err
		}
		list = append(list, r)
		byID[r.id] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return t, err
	}
	// Keterlibatan sebagai pendukung pada tiket periode yang sama.
	srows, err := s.db.QueryContext(ctx, `
		SELECT ta.user_id, u.full_name, COUNT(DISTINCT ta.ticket_id)
		  FROM ticket_assignments ta JOIN tickets t ON t.id = ta.ticket_id JOIN users u ON u.id = ta.user_id
		 WHERE ta.role = 'SUPPORT' AND `+c.where()+` GROUP BY ta.user_id, u.full_name`, c.args...)
	if err != nil {
		return t, err
	}
	for srows.Next() {
		var id int64
		var name string
		var n int
		if err := srows.Scan(&id, &name, &n); err != nil {
			srows.Close()
			return t, err
		}
		if f.UserID > 0 && id != f.UserID || f.ScopeUserID > 0 && id != f.ScopeUserID {
			continue
		}
		r, ok := byID[id]
		if !ok {
			r = &rowT{id: id, name: name}
			list = append(list, r)
			byID[id] = r
		}
		r.support = n
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return t, err
	}
	for _, r := range list {
		if (f.UserID > 0 && r.id != f.UserID) || (f.ScopeUserID > 0 && r.id != f.ScopeUserID) {
			continue // tiket yang hanya didukung pengguna terfilter: baris PJ lain disembunyikan
		}
		t.Rows = append(t.Rows, []Cell{text(r.name), intCell(r.primary), intCell(r.support), intCell(r.done), intCell(r.active),
			durCell(r.resp), durCell(r.res), intCell(r.brk)})
	}
	t.Notes = []string{"Tiket yang masuk pada periode, dikelompokkan menurut penanggung jawab saat ini. Pendukung = jumlah tiket tempat petugas menjadi anggota pendukung."}
	return t, nil
}

// ---------------------------------------------------------------------------
// Tiket tertunda
// ---------------------------------------------------------------------------

func runPending(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"No. tiket", KindText, 11}, {"Mulai ditunda", KindDate, 11}, {"Unit/Bagian", KindText, 13}, {"Bidang", KindText, 10},
		{"Alasan", KindText, 14}, {"Keterangan", KindText, 18}, {"Petugas", KindText, 12}, {"Lama tertunda", KindDuration, 10}, {"Status sekarang", KindText, 10},
	}}
	c := &cond{}
	c.add("h.event_type = 'PENDING' AND h.created_at >= ? AND h.created_at < ?", f.Start, f.End)
	f.ticketFilters(c)
	now := s.clock.Now()
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.ticket_no, h.created_at, d.name, ic.name,
		       JSON_UNQUOTE(JSON_EXTRACT(h.meta, '$.pending_reason')), h.note, u.full_name, t.status,
		       (SELECT MIN(h2.created_at) FROM ticket_histories h2
		         WHERE h2.ticket_id = h.ticket_id AND h2.id > h.id AND h2.from_status = 'PENDING')
		  FROM ticket_histories h
		  JOIN tickets t ON t.id = h.ticket_id
		  JOIN it_categories ic ON ic.id = t.it_category_id
		  LEFT JOIN departments d ON d.id = t.department_id
		  LEFT JOIN users u ON u.id = h.actor_user_id
		 WHERE `+c.where()+` ORDER BY h.created_at LIMIT ?`, append(c.args, MaxRows)...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	reasons := map[string]int{}
	var still int
	var totalMin float64
	for rows.Next() {
		var id int64
		var no, cat, status string
		var started time.Time
		var dept, reason, note, actor sql.NullString
		var ended sql.NullTime
		if err := rows.Scan(&id, &no, &started, &dept, &cat, &reason, &note, &actor, &status, &ended); err != nil {
			return t, err
		}
		end := now
		if ended.Valid {
			end = ended.Time
		} else {
			still++
		}
		mins := end.Sub(started).Minutes()
		totalMin += mins
		reasons[reason.String]++
		noCell := text(no)
		noCell.Link = fmt.Sprintf("/app/tickets/%d", id)
		lama := durCell(sql.NullFloat64{Float64: mins, Valid: true})
		if !ended.Valid {
			lama.Text += " (masih)"
		}
		t.Rows = append(t.Rows, []Cell{noCell, timeCell(sql.NullTime{Time: started, Valid: true}), nullText(dept), text(cat),
			nullText(reason), nullText(note), nullText(actor), lama, text(web.Label("ticket_status", status))})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	top, topN := "–", 0
	for r, n := range reasons {
		if n > topN || (n == topN && r < top) {
			top, topN = r, n
		}
	}
	avg := "–"
	if len(t.Rows) > 0 {
		avg = Duration(int64(math.Round(totalMin / float64(len(t.Rows)))))
	}
	t.Metrics = []Metric{
		{"Kali ditunda", formatInt(int64(len(t.Rows))), ""},
		{"Masih tertunda", formatInt(int64(still)), "amber"},
		{"Rata-rata lama", avg, ""},
		{"Alasan terbanyak", top, ""},
	}
	t.Notes = []string{"Satu baris per kejadian tunda yang dimulai pada periode. Lama tertunda dihitung sampai tiket dilanjutkan/diselesaikan, atau sampai sekarang bila masih tertunda."}
	return t, nil
}

// ---------------------------------------------------------------------------
// Rekap SLA
// ---------------------------------------------------------------------------

func runSLA(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"Prioritas", KindText, 12}, {"Tiket ber-SLA", KindInt, 9},
		{"Respons dinilai", KindInt, 9}, {"Respons terlewati", KindInt, 9}, {"Kepatuhan respons", KindPct, 10},
		{"Penyelesaian dinilai", KindInt, 10}, {"Penyelesaian terlewati", KindInt, 10}, {"Kepatuhan penyelesaian", KindPct, 11},
	}}
	g := f
	g.Status = ""
	c := g.ticketCond()
	c.add("t.sla_policy_id IS NOT NULL")
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.priority, COUNT(*),
		       SUM(rb.x IS NOT NULL OR t.first_response_at IS NOT NULL), SUM(rb.x IS NOT NULL),
		       SUM(sb.x IS NOT NULL OR t.resolved_at IS NOT NULL), SUM(sb.x IS NOT NULL)
		  FROM tickets t
		  LEFT JOIN (SELECT DISTINCT ticket_id, 1 x FROM sla_escalations WHERE kind = 'RESPONSE_BREACH') rb ON rb.ticket_id = t.id
		  LEFT JOIN (SELECT DISTINCT ticket_id, 1 x FROM sla_escalations WHERE kind = 'RESOLUTION_BREACH') sb ON sb.ticket_id = t.id
		 WHERE `+c.where()+` GROUP BY t.priority ORDER BY FIELD(t.priority, 'CRITICAL', 'HIGH', 'MEDIUM', 'LOW')`, c.args...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	var tot [5]int
	for rows.Next() {
		var p string
		var n, rd, rb, sd, sb int
		if err := rows.Scan(&p, &n, &rd, &rb, &sd, &sb); err != nil {
			return t, err
		}
		for i, v := range []int{n, rd, rb, sd, sb} {
			tot[i] += v
		}
		t.Rows = append(t.Rows, []Cell{text(priorityLabel(p)), intCell(n), intCell(rd), intCell(rb), pctCell(rd-rb, rd),
			intCell(sd), intCell(sb), pctCell(sd-sb, sd)})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	t.Totals = []Cell{text("Total"), intCell(tot[0]), intCell(tot[1]), intCell(tot[2]), pctCell(tot[1]-tot[2], tot[1]),
		intCell(tot[3]), intCell(tot[4]), pctCell(tot[3]-tot[4], tot[3])}
	rp, sp := pctCell(tot[1]-tot[2], tot[1]), pctCell(tot[3]-tot[4], tot[3])
	t.Metrics = []Metric{
		{"Tiket ber-SLA", formatInt(int64(tot[0])), ""},
		{"Kepatuhan respons", rp.Text, toneFor(rp)},
		{"Kepatuhan penyelesaian", sp.Text, toneFor(sp)},
		{"Pelanggaran", formatInt(int64(tot[2] + tot[4])), "red"},
	}
	t.Notes = []string{
		"Dinilai = sudah direspons/diselesaikan, atau sudah melewati batas. Tiket yang masih berjalan dan belum lewat batas belum dinilai.",
		"Pelanggaran diambil dari buku besar eskalasi SLA (jam kerja & hari libur sesuai kalender kebijakan), termasuk siklus setelah dibuka kembali.",
	}
	return t, nil
}

func toneFor(c Cell) string {
	switch {
	case !c.Valid:
		return ""
	case c.Num >= 90:
		return "green"
	case c.Num >= 75:
		return "amber"
	default:
		return "red"
	}
}

// ---------------------------------------------------------------------------
// Waktu respons / penyelesaian
// ---------------------------------------------------------------------------

func durationReport(resolution bool) func(ctx context.Context, s *Service, f Filter) (Table, error) {
	return func(ctx context.Context, s *Service, f Filter) (Table, error) {
		t := Table{Columns: []Column{
			{"Prioritas", KindText, 12}, {"Jumlah tiket", KindInt, 9}, {"Rata-rata", KindDuration, 11}, {"Median", KindDuration, 11},
			{"90% tiket ≤", KindDuration, 11}, {"Tercepat", KindDuration, 10}, {"Terlama", KindDuration, 10},
		}}
		col := "t.first_response_at"
		if resolution {
			col = "t.resolved_at"
		}
		g := f
		g.Status = ""
		c := g.ticketCond()
		c.add(col + " IS NOT NULL")
		rows, err := s.db.QueryContext(ctx, `SELECT t.priority, TIMESTAMPDIFF(SECOND, t.created_at, `+col+`) / 60
			FROM tickets t WHERE `+c.where(), c.args...)
		if err != nil {
			return t, err
		}
		defer rows.Close()
		by := map[string][]float64{}
		var all []float64
		for rows.Next() {
			var p string
			var m float64
			if err := rows.Scan(&p, &m); err != nil {
				return t, err
			}
			by[p] = append(by[p], m)
			all = append(all, m)
		}
		if err := rows.Err(); err != nil {
			return t, err
		}
		line := func(label string, v []float64) []Cell {
			st := stats(v)
			return []Cell{text(label), intCell(len(v)), durCell(st.avg), durCell(st.median), durCell(st.p90), durCell(st.min), durCell(st.max)}
		}
		for _, p := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
			if len(by[p]) > 0 {
				t.Rows = append(t.Rows, line(priorityLabel(p), by[p]))
			}
		}
		t.Totals = line("Semua", all)
		st := stats(all)
		t.Metrics = []Metric{
			{"Tiket dihitung", formatInt(int64(len(all))), ""},
			{"Rata-rata", durCell(st.avg).Text, ""},
			{"Median", durCell(st.median).Text, ""},
			{"90% tiket ≤", durCell(st.p90).Text, ""},
		}
		what := "diambil petugas (respons pertama)"
		if resolution {
			what = "diselesaikan (status Selesai terakhir)"
		}
		t.Notes = []string{"Tiket yang masuk pada periode dan sudah " + what + ". Waktu kalender, termasuk malam & hari libur."}
		return t, nil
	}
}

type durStats struct{ avg, median, p90, min, max sql.NullFloat64 }

func stats(v []float64) durStats {
	if len(v) == 0 {
		return durStats{}
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	sum := 0.0
	for _, x := range s {
		sum += x
	}
	q := func(p float64) float64 { // nearest-rank
		i := int(math.Ceil(p*float64(len(s)))) - 1
		if i < 0 {
			i = 0
		}
		return s[i]
	}
	median := s[len(s)/2]
	if len(s)%2 == 0 {
		median = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	nf := func(x float64) sql.NullFloat64 { return sql.NullFloat64{Float64: x, Valid: true} }
	return durStats{avg: nf(sum / float64(len(s))), median: nf(median), p90: nf(q(0.9)), min: nf(s[0]), max: nf(s[len(s)-1])}
}

// ---------------------------------------------------------------------------
// Beban kerja petugas
// ---------------------------------------------------------------------------

func runWorkload(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"Petugas", KindText, 18}, {"Role", KindText, 11}, {"Tiket sebagai PJ", KindInt, 9}, {"Tiket sebagai pendukung", KindInt, 10},
		{"Tiket diselesaikan", KindInt, 9}, {"Tiket aktif sekarang", KindInt, 9}, {"Kegiatan (PJ)", KindInt, 8},
		{"Kegiatan (pendukung)", KindInt, 9}, {"Durasi kegiatan", KindDuration, 10},
	}}
	// Filter unit/bidang membatasi tiket & kegiatan yang dihitung.
	// Baris = petugas; filter petugas memilih baris, bukan membatasi tiketnya.
	g := f
	g.UserID, g.ScopeUserID = 0, 0
	tf := &cond{}
	g.ticketFilters(tf)
	tw := tf.where()
	af := &cond{}
	if f.DepartmentID > 0 {
		af.add("a.department_id = ?", f.DepartmentID)
	}
	if f.ITCategoryID > 0 {
		af.add("a.it_category_id = ?", f.ITCategoryID)
	}
	aw := af.where()
	from := f.Start.In(clock.Location).Format("2006-01-02")
	to := f.End.In(clock.Location).Format("2006-01-02")

	uc := &cond{}
	uc.add("u.is_active = TRUE AND r.code IN ('TECHNICIAN','COORDINATOR')")
	for _, uid := range []int64{f.UserID, f.ScopeUserID} {
		if uid > 0 {
			uc.add("u.id = ?", uid)
		}
	}
	q := `
		SELECT u.full_name, r.name,
		  (SELECT COUNT(DISTINCT ta.ticket_id) FROM ticket_assignments ta JOIN tickets t ON t.id = ta.ticket_id
		    WHERE ta.user_id = u.id AND ta.role = 'PRIMARY' AND ta.assigned_at >= ? AND ta.assigned_at < ? AND ` + tw + `),
		  (SELECT COUNT(DISTINCT ta.ticket_id) FROM ticket_assignments ta JOIN tickets t ON t.id = ta.ticket_id
		    WHERE ta.user_id = u.id AND ta.role = 'SUPPORT' AND ta.assigned_at >= ? AND ta.assigned_at < ? AND ` + tw + `),
		  (SELECT COUNT(*) FROM ticket_histories h JOIN tickets t ON t.id = h.ticket_id
		    WHERE h.actor_user_id = u.id AND h.event_type = 'RESOLVED' AND h.created_at >= ? AND h.created_at < ? AND ` + tw + `),
		  (SELECT COUNT(*) FROM tickets t WHERE t.assignee_id = u.id AND t.status IN ('CLAIMED','IN_PROGRESS','PENDING') AND ` + tw + `),
		  (SELECT COUNT(*) FROM activity_members m JOIN internal_activities a ON a.id = m.activity_id
		    WHERE m.user_id = u.id AND m.role = 'LEAD' AND a.status <> 'CANCELLED' AND a.activity_date >= ? AND a.activity_date < ? AND ` + aw + `),
		  (SELECT COUNT(*) FROM activity_members m JOIN internal_activities a ON a.id = m.activity_id
		    WHERE m.user_id = u.id AND m.role = 'SUPPORT' AND a.status <> 'CANCELLED' AND a.activity_date >= ? AND a.activity_date < ? AND ` + aw + `),
		  (SELECT SUM(a.duration_minutes) FROM activity_members m JOIN internal_activities a ON a.id = m.activity_id
		    WHERE m.user_id = u.id AND a.status <> 'CANCELLED' AND a.activity_date >= ? AND a.activity_date < ? AND ` + aw + `)
		FROM users u JOIN roles r ON r.id = u.role_id
		WHERE ` + uc.where() + ` ORDER BY u.full_name`
	var args []any
	args = append(append(args, f.Start, f.End), tf.args...)
	args = append(append(args, f.Start, f.End), tf.args...)
	args = append(append(args, f.Start, f.End), tf.args...)
	args = append(args, tf.args...)
	args = append(append(args, from, to), af.args...)
	args = append(append(args, from, to), af.args...)
	args = append(append(args, from, to), af.args...)
	args = append(args, uc.args...)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	var sums [6]int
	for rows.Next() {
		var name, role string
		var pj, sup, done, active, al, as int
		var dur sql.NullFloat64
		if err := rows.Scan(&name, &role, &pj, &sup, &done, &active, &al, &as, &dur); err != nil {
			return t, err
		}
		for i, v := range []int{pj, sup, done, active, al, as} {
			sums[i] += v
		}
		t.Rows = append(t.Rows, []Cell{text(name), text(role), intCell(pj), intCell(sup), intCell(done), intCell(active), intCell(al), intCell(as), durCell(dur)})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	t.Totals = []Cell{text("Total"), text(""), intCell(sums[0]), intCell(sums[1]), intCell(sums[2]), intCell(sums[3]), intCell(sums[4]), intCell(sums[5]), {Text: ""}}
	t.Notes = []string{"Penugasan dihitung saat petugas ditetapkan pada periode (termasuk pengambilalihan). Tiket aktif adalah kondisi saat ini. Kegiatan yang dibatalkan tidak dihitung."}
	return t, nil
}

// ---------------------------------------------------------------------------
// Kegiatan internal
// ---------------------------------------------------------------------------

func runActivities(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"No. kegiatan", KindText, 10}, {"Tanggal", KindDate, 8}, {"Jenis", KindText, 11}, {"Bidang", KindText, 10}, {"Unit/Lokasi", KindText, 12},
		{"Kegiatan", KindText, 20}, {"Pelaksana", KindText, 11}, {"Pendukung", KindText, 11}, {"Durasi", KindDuration, 8}, {"Status", KindText, 8}, {"Hasil", KindText, 18},
	}}
	c := f.activityCond()
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.activity_no, a.activity_date, ty.name, ic.name, d.name, a.title, a.status, a.result, a.duration_minutes,
		       (SELECT u.full_name FROM activity_members m JOIN users u ON u.id = m.user_id WHERE m.activity_id = a.id AND m.role = 'LEAD'),
		       (SELECT GROUP_CONCAT(u.full_name ORDER BY u.full_name SEPARATOR ', ')
		          FROM activity_members m JOIN users u ON u.id = m.user_id WHERE m.activity_id = a.id AND m.role = 'SUPPORT')
		  FROM internal_activities a
		  JOIN activity_types ty ON ty.id = a.activity_type_id
		  JOIN it_categories ic ON ic.id = a.it_category_id
		  LEFT JOIN departments d ON d.id = a.department_id
		 WHERE `+c.where()+` ORDER BY a.activity_date, a.id LIMIT ?`, append(c.args, MaxRows)...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	var done int
	var totalDur float64
	types := map[string]int{}
	for rows.Next() {
		var id int64
		var no, typ, cat, title, status string
		var date time.Time
		var dept, result, lead, support sql.NullString
		var dur sql.NullFloat64
		if err := rows.Scan(&id, &no, &date, &typ, &cat, &dept, &title, &status, &result, &dur, &lead, &support); err != nil {
			return t, err
		}
		if status == "DONE" {
			done++
		}
		if dur.Valid && status != "CANCELLED" {
			totalDur += dur.Float64
		}
		types[typ]++
		noCell := text(no)
		noCell.Link = fmt.Sprintf("/app/activities/%d", id)
		day := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, clock.Location)
		t.Rows = append(t.Rows, []Cell{noCell, dateCell(day), text(typ), text(cat), nullText(dept), text(title), nullText(lead), nullText(support),
			durCell(dur), text(web.Label("activity_status", status)), nullText(result)})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	top, topN := "–", 0
	for k, n := range types {
		if n > topN || (n == topN && k < top) {
			top, topN = k, n
		}
	}
	t.Metrics = []Metric{
		{"Kegiatan", formatInt(int64(len(t.Rows))), ""},
		{"Selesai", formatInt(int64(done)), "green"},
		{"Total durasi", Duration(int64(totalDur)), ""},
		{"Jenis terbanyak", top, ""},
	}
	return t, nil
}

// ---------------------------------------------------------------------------
// Perangkat: sering rusak, pemeliharaan, downtime
// ---------------------------------------------------------------------------

func runRepeatDamage(ctx context.Context, s *Service, f Filter) (Table, error) {
	threshold := s.RepeatThreshold(ctx)
	if threshold < 2 {
		threshold = 2
	}
	t := Table{Columns: []Column{
		{"Perangkat", KindText, 18}, {"Kode", KindText, 10}, {"Unit", KindText, 13}, {"Jumlah gangguan", KindInt, 9},
		{"Gangguan terakhir", KindDate, 11}, {"Total downtime", KindDuration, 10}, {"Biaya perbaikan", KindMoney, 11}, {"Peringatan terbuka", KindText, 13},
	}}
	c := &cond{}
	c.add("t.created_at >= ? AND t.created_at < ?", f.Start, f.End)
	if f.DepartmentID > 0 {
		c.add("x.department_id = ?", f.DepartmentID)
	}
	if f.ScopeUserID > 0 {
		c.add("(t.assignee_id = ? OR EXISTS (SELECT 1 FROM ticket_assignments ta WHERE ta.ticket_id = t.id AND ta.user_id = ?))", f.ScopeUserID, f.ScopeUserID)
	}
	args := append([]any{f.Start, f.End, f.Start, f.End}, c.args...)
	args = append(args, threshold, MaxRows)
	rows, err := s.db.QueryContext(ctx, `
		SELECT x.id, x.name, x.asset_code, d.name, COUNT(*) n, MAX(t.created_at),
		       (SELECT SUM(h.downtime_minutes) FROM asset_maintenance_histories h
		         WHERE h.asset_id = x.id AND h.voided_at IS NULL AND h.finished_at >= ? AND h.finished_at < ?),
		       (SELECT SUM(h.service_cost) FROM asset_maintenance_histories h
		         WHERE h.asset_id = x.id AND h.voided_at IS NULL AND h.finished_at >= ? AND h.finished_at < ?),
		       (SELECT al.recommendation FROM asset_alerts al WHERE al.asset_id = x.id AND al.status <> 'CLOSED' LIMIT 1)
		  FROM tickets t JOIN assets x ON x.id = t.asset_id LEFT JOIN departments d ON d.id = x.department_id
		 WHERE `+c.where()+` GROUP BY x.id, x.name, x.asset_code, d.name HAVING n >= ? ORDER BY n DESC, x.name LIMIT ?`, args...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, code string
		var dept, rec sql.NullString
		var n int
		var last time.Time
		var down, cost sql.NullFloat64
		if err := rows.Scan(&id, &name, &code, &dept, &n, &last, &down, &cost, &rec); err != nil {
			return t, err
		}
		nc := text(name)
		nc.Link = fmt.Sprintf("/app/assets/%d", id)
		recCell := text("")
		if rec.Valid {
			recCell = text(web.Label("recommendation", rec.String))
		}
		t.Rows = append(t.Rows, []Cell{nc, text(code), nullText(dept), intCell(n), dateCell(last), durCell(down), moneyCell(cost), recCell})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	t.Metrics = []Metric{{"Perangkat", formatInt(int64(len(t.Rows))), "orange"}, {"Ambang", fmt.Sprintf("≥ %d gangguan", threshold), ""}}
	t.Notes = []string{fmt.Sprintf("Perangkat dengan ≥ %d tiket gangguan yang masuk pada periode (ambang dari Pengaturan sistem). Rekomendasi bukan keputusan; keputusan tetap pada koordinator.", threshold)}
	return t, nil
}

func runMaintenance(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"Tanggal", KindDate, 8}, {"Perangkat", KindText, 15}, {"Unit", KindText, 11}, {"Jenis", KindText, 10}, {"Sumber", KindText, 10},
		{"Tindakan", KindText, 22}, {"Hasil", KindText, 10}, {"Pelaksana", KindText, 11}, {"Lama perbaikan", KindDuration, 9}, {"Biaya", KindMoney, 10},
	}}
	c := f.maintenanceCond()
	rows, err := s.db.QueryContext(ctx, `
		SELECT x.id, h.finished_at, x.name, x.asset_code, d.name, h.maintenance_type, h.source, tk.ticket_no, h.action_taken, h.result,
		       u.full_name, h.repair_minutes, h.service_cost + 0
		  FROM asset_maintenance_histories h
		  JOIN assets x ON x.id = h.asset_id
		  LEFT JOIN departments d ON d.id = x.department_id
		  LEFT JOIN users u ON u.id = h.performed_by_user_id
		  LEFT JOIN tickets tk ON tk.id = h.ticket_id
		 WHERE `+c.where()+` ORDER BY h.finished_at LIMIT ?`, append(c.args, MaxRows)...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	var totalCost float64
	assets := map[int64]bool{}
	for rows.Next() {
		var assetID int64
		var finished time.Time
		var name, code, typ, src, action, result string
		var dept, ticketNo, perf sql.NullString
		var repair, cost sql.NullFloat64
		if err := rows.Scan(&assetID, &finished, &name, &code, &dept, &typ, &src, &ticketNo, &action, &result, &perf, &repair, &cost); err != nil {
			return t, err
		}
		assets[assetID] = true
		totalCost += cost.Float64
		srcText := web.Label("maintenance_source", src)
		if ticketNo.Valid {
			srcText += " " + ticketNo.String
		}
		nc := text(name + " (" + code + ")")
		nc.Link = fmt.Sprintf("/app/assets/%d", assetID)
		resText := web.Label("resolution_result", result)
		if result == "DONE" {
			resText = "Selesai"
		}
		t.Rows = append(t.Rows, []Cell{dateCell(finished), nc, nullText(dept), text(web.Label("maintenance_type", typ)), text(srcText),
			text(clipText(action, 300)), text(resText), nullText(perf), durCell(repair), moneyCell(cost)})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	t.Metrics = []Metric{
		{"Catatan pemeliharaan", formatInt(int64(len(t.Rows))), ""},
		{"Perangkat", formatInt(int64(len(assets))), ""},
		{"Total biaya", moneyCell(sql.NullFloat64{Float64: totalCost, Valid: true}).Text, ""},
	}
	t.Notes = []string{"Catatan yang dibatalkan tidak ditampilkan. Biaya = biaya jasa/vendor yang dicatat."}
	return t, nil
}

func runDowntime(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"Perangkat", KindText, 18}, {"Kode", KindText, 10}, {"Unit", KindText, 14}, {"Kejadian", KindInt, 8},
		{"Total downtime", KindDuration, 11}, {"Rata-rata", KindDuration, 10}, {"Terlama", KindDuration, 10},
	}}
	c := f.maintenanceCond()
	c.add("h.downtime_minutes IS NOT NULL")
	rows, err := s.db.QueryContext(ctx, `
		SELECT x.id, x.name, x.asset_code, d.name, COUNT(*), SUM(h.downtime_minutes), AVG(h.downtime_minutes), MAX(h.downtime_minutes)
		  FROM asset_maintenance_histories h JOIN assets x ON x.id = h.asset_id LEFT JOIN departments d ON d.id = x.department_id
		 WHERE `+c.where()+` GROUP BY x.id, x.name, x.asset_code, d.name ORDER BY SUM(h.downtime_minutes) DESC LIMIT ?`, append(c.args, MaxRows)...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	var events int
	var total float64
	for rows.Next() {
		var id int64
		var name, code string
		var dept sql.NullString
		var n int
		var sum, avg, max sql.NullFloat64
		if err := rows.Scan(&id, &name, &code, &dept, &n, &sum, &avg, &max); err != nil {
			return t, err
		}
		events += n
		total += sum.Float64
		nc := text(name)
		nc.Link = fmt.Sprintf("/app/assets/%d", id)
		t.Rows = append(t.Rows, []Cell{nc, text(code), nullText(dept), intCell(n), durCell(sum), durCell(avg), durCell(max)})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	t.Totals = []Cell{text("Total"), text(""), text(""), intCell(events), durCell(sql.NullFloat64{Float64: total, Valid: events > 0}), {Text: ""}, {Text: ""}}
	t.Metrics = []Metric{{"Perangkat terdampak", formatInt(int64(len(t.Rows))), ""}, {"Kejadian", formatInt(int64(events)), ""},
		{"Total downtime", Duration(int64(total)), "orange"}}
	t.Notes = []string{"Downtime = sejak gangguan mulai dirasakan sampai perbaikan selesai, dari riwayat pemeliharaan yang mencatat waktu awal gangguan."}
	return t, nil
}

// ---------------------------------------------------------------------------
// Pemilahan jenis gangguan (Triase & koreksi)
// ---------------------------------------------------------------------------

func runTriage(ctx context.Context, s *Service, f Filter) (Table, error) {
	t := Table{Columns: []Column{
		{"No. tiket", KindText, 11}, {"Tanggal", KindDate, 8}, {"Unit/Bagian", KindText, 12}, {"Dipilih pelapor", KindText, 13},
		{"Jenis akhir", KindText, 13}, {"Bidang akhir", KindText, 11}, {"Keterangan pelapor", KindText, 26},
		{"Dikoreksi oleh", KindText, 11}, {"Status", KindText, 9},
	}}
	c := f.ticketCond()
	c.add("(t.reported_ticket_category_id IS NOT NULL OR ic.is_triage)")
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.ticket_no, t.created_at, d.name, COALESCE(rtc.name, tc.name), tc.name, ic.name, ic.is_triage,
		       t.description, t.status, t.reported_ticket_category_id IS NOT NULL,
		       (SELECT u.full_name FROM ticket_histories h JOIN users u ON u.id = h.actor_user_id
		         WHERE h.ticket_id = t.id AND h.event_type = 'CATEGORY_CHANGED' ORDER BY h.id DESC LIMIT 1)
		  FROM tickets t
		  JOIN ticket_categories tc ON tc.id = t.ticket_category_id
		  JOIN it_categories ic ON ic.id = t.it_category_id
		  LEFT JOIN ticket_categories rtc ON rtc.id = t.reported_ticket_category_id
		  LEFT JOIN departments d ON d.id = t.department_id
		 WHERE `+c.where()+` ORDER BY t.created_at LIMIT ?`, append(c.args, MaxRows)...)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	var waiting, corrected int
	finals := map[string]int{}
	for rows.Next() {
		var id int64
		var no, reported, final, field, desc, status string
		var created time.Time
		var triage, wasCorrected bool
		var dept, by sql.NullString
		if err := rows.Scan(&id, &no, &created, &dept, &reported, &final, &field, &triage, &desc, &status, &wasCorrected, &by); err != nil {
			return t, err
		}
		if triage && (status == "NEW" || status == "CLAIMED" || status == "IN_PROGRESS" || status == "PENDING") {
			waiting++
		}
		finalCell, fieldCell := text(final), text(field)
		if triage {
			finalCell, fieldCell = text("Belum dipilah"), text(field)
		}
		if wasCorrected {
			corrected++
			if !triage {
				finals[final]++
			}
		}
		nc := text(no)
		nc.Link = fmt.Sprintf("/app/tickets/%d", id)
		t.Rows = append(t.Rows, []Cell{nc, dateCell(created), nullText(dept), text(reported), finalCell, fieldCell,
			text(clipText(desc, 300)), nullText(by), text(web.Label("ticket_status", status))})
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	top, topN := "–", 0
	for k, n := range finals {
		if n > topN || (n == topN && k < top) {
			top, topN = k, n
		}
	}
	t.Metrics = []Metric{
		{"Tiket", formatInt(int64(len(t.Rows))), ""},
		{"Masih perlu dipilah", formatInt(int64(waiting)), "amber"},
		{"Dikoreksi jenisnya", formatInt(int64(corrected)), ""},
		{"Hasil koreksi terbanyak", top, ""},
	}
	t.Notes = []string{"Baca kolom Keterangan pelapor untuk menemukan masalah yang sering muncul, lalu tambahkan sebagai jenis gangguan baru di Master Data › Jenis Gangguan agar pelapor berikutnya bisa langsung memilihnya."}
	return t, nil
}
