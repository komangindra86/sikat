package reports_test

import (
	"bytes"
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"sikat/internal/attachments"
	"sikat/internal/clock"
	"sikat/internal/priority"
	"sikat/internal/rbac"
	"sikat/internal/reports"
	"sikat/internal/testutil"
	"sikat/internal/tickets"
)

type fx struct {
	db                 *sql.DB
	clk                *clock.Fake
	tk                 *tickets.Service
	rs                 *reports.Service
	room, cat          int64
	tech, tech2, coord *rbac.User
}

func setup(t *testing.T) fx {
	t.Helper()
	db := testutil.DB(t)
	now := time.Now().UTC()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	f := fx{db: db, clk: clock.NewFake(time.Date(2026, 10, 31, 15, 30, 0, 0, time.UTC))} // 31 Okt 23:30 WITA
	dept := exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('KEU','Keuangan',?,?)`, now, now)
	f.room = exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'KSR','Kasir',?,?)`, dept, now, now)
	if err := db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Komputer tidak menyala'`).Scan(&f.cat); err != nil {
		t.Fatal(err)
	}
	f.tk = tickets.NewService(db, f.clk, attachments.Store{Dir: t.TempDir()})
	f.rs = reports.NewService(db, f.clk)
	f.tech = &rbac.User{ID: testutil.CreateUser(t, db, "tek1", "TECHNICIAN", "password-tek1", false), RoleCode: rbac.RoleTechnician, FullName: "User tek1"}
	f.tech2 = &rbac.User{ID: testutil.CreateUser(t, db, "tek2", "TECHNICIAN", "password-tek2", false), RoleCode: rbac.RoleTechnician, FullName: "User tek2"}
	f.coord = &rbac.User{ID: testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor", false), RoleCode: rbac.RoleCoordinator}
	return f
}

func (f fx) create(t *testing.T) int64 {
	t.Helper()
	c, errs, err := f.tk.Create(context.Background(), tickets.Draft{
		Channel: tickets.ChannelQR, RoomID: f.room, RoomFromQR: true, ReporterName: "Ani",
		TicketCategoryID: f.cat, Description: "Komputer mati total – tidak bisa login", Impact: 1, Scope: 1, Replacement: priority.ReplacementYes,
	}, nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	return c.ID
}

func (f fx) run(t *testing.T, slug, query string, scope int64) reports.Table {
	t.Helper()
	d, ok := reports.Find(slug)
	if !ok {
		t.Fatalf("laporan %s tidak ada", slug)
	}
	q, _ := url.ParseQuery(query)
	flt, errs := reports.ParseFilter(q, f.clk.Now())
	if len(errs) > 0 {
		t.Fatalf("filter %s: %v", query, errs)
	}
	flt.ScopeUserID = scope
	tb, err := f.rs.Run(context.Background(), d, flt)
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func cellNum(t *testing.T, tb reports.Table, row int, col string) float64 {
	t.Helper()
	for i, c := range tb.Columns {
		if c.Label == col {
			cells := tb.Totals
			if row >= 0 {
				cells = tb.Rows[row]
			}
			return cells[i].Num
		}
	}
	t.Fatalf("kolom %q tidak ada di %s", col, tb.Title)
	return 0
}

func TestMonthBoundaryUsesWITA(t *testing.T) {
	f := setup(t)
	f.create(t)              // 31 Okt 23:30 WITA (15:30 UTC)
	f.clk.Advance(time.Hour) // 1 Nov 00:30 WITA (16:30 UTC, masih 31 Okt di UTC)
	f.create(t)

	nov := f.run(t, "bulanan", "periode=bulan&bulan=2026-11", 0)
	if got := cellNum(t, nov, -1, "Masuk"); got != 1 {
		t.Fatalf("November (WITA) harus 1 tiket, dapat %v", got)
	}
	if len(nov.Rows) != 30 || nov.Rows[0][0].Text != "01/11/2026" {
		t.Fatalf("periode satu bulan dikelompokkan per hari: %d baris, pertama %q", len(nov.Rows), nov.Rows[0][0].Text)
	}
	if got := cellNum(t, nov, 0, "Masuk"); got != 1 {
		t.Fatalf("tiket 00:30 WITA masuk tanggal 1 November, dapat %v", got)
	}
	okt := f.run(t, "bulanan", "periode=bulan&bulan=2026-10", 0)
	if got := cellNum(t, okt, -1, "Masuk"); got != 1 {
		t.Fatalf("Oktober (WITA) harus 1 tiket, dapat %v", got)
	}
	year := f.run(t, "bulanan", "periode=tahun&tahun=2026", 0)
	if len(year.Rows) != 12 || year.Rows[9][0].Text != "Oktober 2026" || cellNum(t, year, 9, "Masuk") != 1 || cellNum(t, year, 10, "Masuk") != 1 {
		t.Fatalf("rekap tahunan per bulan salah: %d baris", len(year.Rows))
	}
	if year.Chart == nil || len(year.Chart.Bars) != 12 {
		t.Fatal("rekap bulanan menyertakan grafik 12 batang")
	}
	rng := f.run(t, "bulanan", "periode=tanggal&dari=2026-11-01&sampai=2026-11-01", 0)
	if cellNum(t, rng, -1, "Masuk") != 1 {
		t.Fatal("rentang tanggal 1 November (WITA) inklusif")
	}
}

func TestReportCalculations(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.clk.Advance(time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC).Sub(f.clk.Now())) // 10 Nov 09:00 WITA
	a := f.create(t)
	f.clk.Advance(30 * time.Minute)
	if r, err := f.tk.Claim(ctx, a, f.tech.ID); err != nil || !r.OK {
		t.Fatal(err)
	}
	if err := f.tk.Start(ctx, a, f.tech); err != nil {
		t.Fatal(err)
	}
	if err := f.tk.AddSupport(ctx, a, f.tech, f.tech2.ID); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(90 * time.Minute) // selesai 2 jam setelah masuk
	if errs, err := f.tk.Resolve(ctx, a, f.tech, tickets.ResolveInput{Action: "Ganti PSU", Result: "FIXED"}); err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	b := f.create(t) // tiket kedua: diambil tek2 lalu ditunda
	f.clk.Advance(10 * time.Minute)
	if r, err := f.tk.Claim(ctx, b, f.tech2.ID); err != nil || !r.OK {
		t.Fatal(err)
	}
	if err := f.tk.Start(ctx, b, f.tech2); err != nil {
		t.Fatal(err)
	}
	var reason int64
	_ = f.db.QueryRow(`SELECT id FROM pending_reasons WHERE label = 'Menunggu sparepart'`).Scan(&reason)
	if errs, err := f.tk.Pend(ctx, b, f.tech2, reason, "Menunggu PSU"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	f.clk.Advance(3 * time.Hour)

	// Kegiatan internal: tek1 PJ, tek2 pendukung, 45 menit.
	var typeID, itCat int64
	_ = f.db.QueryRow(`SELECT MIN(id) FROM activity_types`).Scan(&typeID)
	_ = f.db.QueryRow(`SELECT it_category_id FROM ticket_categories WHERE id = ?`, f.cat).Scan(&itCat)
	now := f.clk.Now()
	res, err := f.db.Exec(`INSERT INTO internal_activities (activity_no, activity_date, activity_type_id, it_category_id, title, result, status,
		started_at, ended_at, created_by, created_at, updated_at) VALUES ('KGT-2026-000001', '2026-11-10', ?, ?, 'Backup server SIMRS', 'Berhasil', 'DONE', ?, ?, ?, ?, ?)`,
		typeID, itCat, now.Add(-45*time.Minute), now, f.tech.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	actID, _ := res.LastInsertId()
	if _, err := f.db.Exec(`INSERT INTO activity_members (activity_id, user_id, role, created_at) VALUES (?, ?, 'LEAD', ?), (?, ?, 'SUPPORT', ?)`,
		actID, f.tech.ID, now, actID, f.tech2.ID, now); err != nil {
		t.Fatal(err)
	}

	q := "periode=bulan&bulan=2026-11"
	resp := f.run(t, "respons", q, 0)
	if got := cellNum(t, resp, -1, "Jumlah tiket"); got != 2 {
		t.Fatalf("respons: 2 tiket, dapat %v", got)
	}
	if got := cellNum(t, resp, -1, "Rata-rata"); got != 20 { // (30 + 10) / 2
		t.Fatalf("rata-rata respons 20 menit, dapat %v", got)
	}
	reso := f.run(t, "penyelesaian", q, 0)
	if cellNum(t, reso, -1, "Jumlah tiket") != 1 || cellNum(t, reso, -1, "Median") != 120 {
		t.Fatalf("penyelesaian: 1 tiket, 120 menit; dapat %+v", reso.Totals)
	}

	bid := f.run(t, "bidang", q, 0)
	if cellNum(t, bid, -1, "Masuk") != 2 || cellNum(t, bid, -1, "Selesai/ditutup") != 1 || cellNum(t, bid, -1, "Masih aktif") != 1 {
		t.Fatalf("rekap bidang: %+v", bid.Totals)
	}
	if cellNum(t, bid, -1, "% selesai") != 50 {
		t.Fatal("persentase selesai 50%")
	}

	pet := f.run(t, "petugas", q, 0)
	found := map[string][2]float64{}
	for _, r := range pet.Rows {
		found[r[0].Text] = [2]float64{r[1].Num, r[2].Num}
	}
	if found["User tek1"] != [2]float64{1, 0} || found["User tek2"] != [2]float64{1, 1} {
		t.Fatalf("rekap petugas (PJ, pendukung): %v", found)
	}

	pend := f.run(t, "tertunda", q, 0)
	if len(pend.Rows) != 1 || pend.Rows[0][4].Text != "Menunggu sparepart" || pend.Rows[0][7].Num != 180 || !strings.Contains(pend.Rows[0][7].Text, "masih") {
		t.Fatalf("rekap tertunda: %+v", pend.Rows)
	}

	wl := f.run(t, "beban-kerja", q, 0)
	for _, r := range wl.Rows {
		if r[0].Text == "User tek1" && (r[2].Num != 1 || r[4].Num != 1 || r[6].Num != 1 || r[8].Num != 45) {
			t.Fatalf("beban kerja tek1 (PJ 1, selesai 1, kegiatan PJ 1, 45 mnt): %+v", r)
		}
		if r[0].Text == "User tek2" && (r[2].Num != 1 || r[3].Num != 1 || r[5].Num != 1 || r[7].Num != 1) {
			t.Fatalf("beban kerja tek2 (PJ 1, pendukung 1, aktif 1, kegiatan pendukung 1): %+v", r)
		}
	}

	co := f.run(t, "koordinator", q, 0)
	wantCols := []string{"No", "Tanggal", "Unit/Bagian", "Bidang", "Kegiatan", "Pelaksana", "Pendukung", "Status", "Hasil/Keterangan"}
	for i, c := range co.Columns {
		if c.Label != wantCols[i] {
			t.Fatalf("kolom laporan koordinator %d = %q, mau %q", i, c.Label, wantCols[i])
		}
	}
	if len(co.Rows) != 3 {
		t.Fatalf("laporan koordinator: 2 tiket + 1 kegiatan, dapat %d", len(co.Rows))
	}
	var resolvedRow, actRow []reports.Cell
	for _, r := range co.Rows {
		if strings.Contains(r[4].Text, "Backup server SIMRS") {
			actRow = r
		}
		if r[7].Text == "Selesai" {
			resolvedRow = r
		}
	}
	if resolvedRow == nil || resolvedRow[5].Text != "User tek1" || resolvedRow[6].Text != "User tek2" || !strings.Contains(resolvedRow[8].Text, "Ganti PSU") {
		t.Fatalf("baris tiket selesai: %+v", resolvedRow)
	}
	if actRow == nil || actRow[5].Text != "User tek1" || actRow[6].Text != "User tek2" || actRow[8].Text != "Berhasil" {
		t.Fatalf("baris kegiatan: %+v", actRow)
	}

	// Data pribadi: tek1 hanya melihat tiket & kegiatan tempat ia terlibat.
	own := f.run(t, "koordinator", q, f.tech.ID)
	if len(own.Rows) != 2 {
		t.Fatalf("laporan pribadi tek1: 1 tiket + 1 kegiatan, dapat %d", len(own.Rows))
	}
	ownBid := f.run(t, "bidang", q, f.tech2.ID)
	if cellNum(t, ownBid, -1, "Masuk") != 2 {
		t.Fatal("tek2 terlibat di kedua tiket (PJ & pendukung)")
	}

	// Filter prioritas pada laporan koordinator mengecualikan kegiatan.
	if hi := f.run(t, "koordinator", q+"&status=PENDING", 0); len(hi.Rows) != 1 || len(hi.Notes) < 2 {
		t.Fatalf("filter status: hanya tiket tertunda, kegiatan dikecualikan: %d baris", len(hi.Rows))
	}

	// Semua laporan berjalan untuk tiap jenis periode tanpa error.
	for _, d := range reports.Defs {
		for _, qq := range []string{q, "periode=tahun&tahun=2026", "periode=tanggal&dari=2026-11-01&sampai=2026-11-30&unit=1&bidang=1&petugas=1&prioritas=LOW&status=ACTIVE"} {
			f.run(t, d.Slug, qq, 0)
		}
	}
}

func TestParseFilterRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC)
	for _, q := range []string{"periode=bulan&bulan=2026-13", "periode=tanggal&dari=2026-11-10&sampai=2026-11-01", "periode=tanggal&dari=2020-01-01&sampai=2026-11-01", "periode=tahun&tahun=1999"} {
		v, _ := url.ParseQuery(q)
		f, errs := reports.ParseFilter(v, now)
		if len(errs) == 0 {
			t.Errorf("%s harus ditolak", q)
		}
		if f.Start.IsZero() || !f.End.After(f.Start) {
			t.Errorf("%s: periode default tetap valid", q)
		}
	}
	v, _ := url.ParseQuery("periode=bulan&bulan=2026-02&prioritas=SEMUA&status=XYZ&unit=-3")
	f, _ := reports.ParseFilter(v, now)
	if f.Priority != "" || f.Status != "" || f.DepartmentID != 0 {
		t.Fatalf("nilai filter asing diabaikan: %+v", f)
	}
	wantStart := time.Date(2026, 1, 31, 16, 0, 0, 0, time.UTC) // 1 Feb 00:00 WITA
	if !f.Start.Equal(wantStart) || !f.End.Equal(time.Date(2026, 2, 28, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("batas Februari WITA: %s – %s", f.Start, f.End)
	}
}

func TestExportsOpenCorrectly(t *testing.T) {
	tb := reports.Table{
		Title: "Laporan Koordinator IT", Subtitle: "Periode November 2026 · Unit: IGD",
		Columns: []reports.Column{{Label: "No", Kind: reports.KindInt, Width: 4}, {Label: "Kegiatan", Kind: reports.KindText, Width: 30},
			{Label: "Durasi", Kind: reports.KindDuration}, {Label: "% selesai", Kind: reports.KindPct}, {Label: "Biaya", Kind: reports.KindMoney}},
		Metrics: []reports.Metric{{Label: "Ambang", Value: "≥ 3 gangguan"}},
		Notes:   []string{"Catatan – dengan karakter khusus: é, ≤, …"},
	}
	for i := 1; i <= 120; i++ { // cukup untuk beberapa halaman PDF
		tb.Rows = append(tb.Rows, []reports.Cell{{Text: "1", Num: float64(i), Valid: true},
			{Text: "TKT-2026-000123 · Printer macet – kertas tersangkut di IGD lantai 1, perlu dicek teknisi · René", Valid: true},
			{Text: "2 j 15 mnt", Num: 135, Valid: true}, {Text: "50,0%", Num: 50, Valid: true}, {Text: "–"}})
	}
	tb.Totals = []reports.Cell{{Text: "Total"}, {Text: ""}, {Text: ""}, {Text: "50,0%", Num: 50, Valid: true}, {Text: "Rp 150.000", Num: 150000, Valid: true}}
	meta := reports.Meta{Hospital: "RSUD Contoh", GeneratedAt: time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC), GeneratedBy: "Koordinator"}

	var xb bytes.Buffer
	if err := reports.WriteXLSX(&xb, tb, meta); err != nil {
		t.Fatal(err)
	}
	x, err := excelize.OpenReader(&xb)
	if err != nil {
		t.Fatalf("file Excel tidak dapat dibuka: %v", err)
	}
	defer x.Close()
	if v, _ := x.GetCellValue("Laporan", "A1"); v != tb.Title {
		t.Fatalf("judul Excel = %q", v)
	}
	if v, _ := x.GetCellValue("Laporan", "B6"); v != "Kegiatan" {
		t.Fatalf("header Excel B6 = %q", v)
	}
	if typ, _ := x.GetCellType("Laporan", "A7"); typ != excelize.CellTypeNumber && typ != excelize.CellTypeUnset {
		t.Fatalf("kolom angka harus tersimpan sebagai angka, tipe %v", typ)
	}
	if v, _ := x.GetCellValue("Laporan", "A7", excelize.Options{RawCellValue: true}); v != "1" {
		t.Fatalf("nilai angka mentah = %q", v)
	}

	var pb bytes.Buffer
	if err := reports.WritePDF(&pb, tb, meta); err != nil {
		t.Fatal(err)
	}
	out := pb.Bytes()
	if !bytes.HasPrefix(out, []byte("%PDF-")) || !bytes.Contains(out, []byte("%%EOF")) {
		t.Fatal("output bukan PDF yang lengkap")
	}
	if n := bytes.Count(out, []byte("/Type /Page\n")) + bytes.Count(out, []byte("/Type /Page ")); n < 2 {
		t.Fatalf("120 baris harus lebih dari satu halaman, dapat %d", n)
	}

	// Tabel kosong tetap menghasilkan file.
	empty := reports.Table{Title: "Kosong", Columns: tb.Columns}
	if err := reports.WritePDF(&bytes.Buffer{}, empty, meta); err != nil {
		t.Fatal(err)
	}
	if err := reports.WriteXLSX(&bytes.Buffer{}, empty, meta); err != nil {
		t.Fatal(err)
	}
	if got := reports.Filename("koordinator", reports.Filter{Period: reports.PeriodMonth, Month: "2026-11"}, "xlsx"); got != "sikat-koordinator-2026-11.xlsx" {
		t.Fatalf("nama file = %s", got)
	}
}

func TestTriageReport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var lainnya, network int64
	_ = f.db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Lainnya'`).Scan(&lainnya)
	_ = f.db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Internet atau jaringan mati'`).Scan(&network)
	f.clk.Advance(time.Hour) // 1 Nov WITA
	c, errs, err := f.tk.Create(ctx, tickets.Draft{Channel: tickets.ChannelQR, RoomID: f.room, RoomFromQR: true, ReporterName: "Ani",
		TicketCategoryID: lainnya, Description: "Mesin antrian tidak cetak nomor", Impact: 1, Scope: 1, Replacement: priority.ReplacementYes}, nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	f.create(t) // tiket biasa: tidak masuk laporan pemilahan

	tb := f.run(t, "pemilahan", "periode=bulan&bulan=2026-11", 0)
	if len(tb.Rows) != 1 || tb.Rows[0][3].Text != "Lainnya" || tb.Rows[0][4].Text != "Belum dipilah" || tb.Metrics[1].Value != "1" {
		t.Fatalf("tiket triase belum dipilah: %+v %+v", tb.Rows, tb.Metrics)
	}
	if errs, err := f.tk.ChangeCategory(ctx, c.ID, f.coord, network, ""); err != nil || errs != nil {
		t.Fatalf("pilah: %v %v", errs, err)
	}
	tb = f.run(t, "pemilahan", "periode=bulan&bulan=2026-11", 0)
	r := tb.Rows[0]
	if len(tb.Rows) != 1 || r[3].Text != "Lainnya" || r[4].Text != "Internet atau jaringan mati" || r[5].Text != "Jaringan" ||
		r[7].Text != "User koor" || tb.Metrics[1].Value != "0" || tb.Metrics[3].Value != "Internet atau jaringan mati" {
		t.Fatalf("setelah dipilah: %+v %+v", r, tb.Metrics)
	}
}
