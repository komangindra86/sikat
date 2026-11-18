package tickets_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"sikat/internal/attachments"
	"sikat/internal/clock"
	"sikat/internal/priority"
	"sikat/internal/testutil"
	"sikat/internal/tickets"
)

type fx struct {
	db                        *sql.DB
	svc                       *tickets.Service
	store                     attachments.Store
	dept, critDept            int64
	room, critRoom, otherRoom int64
	asset, cat, catInactive   int64
}

func setup(t *testing.T) fx {
	db := testutil.DB(t)
	now := time.Now().UTC()
	ins := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	f := fx{db: db, store: attachments.Store{Dir: t.TempDir()}}
	f.svc = tickets.NewService(db, clock.NewFake(time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)), f.store)
	f.dept = ins(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('KEU','Keuangan',?,?)`, now, now)
	f.critDept = ins(`INSERT INTO departments (code, name, is_critical_service, created_at, updated_at) VALUES ('ICU','ICU',TRUE,?,?)`, now, now)
	f.room = ins(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'KSR','Kasir',?,?)`, f.dept, now, now)
	f.otherRoom = ins(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'ADM','Admin',?,?)`, f.dept, now, now)
	f.critRoom = ins(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (?, 'ICU1','Nurse station',?,?)`, f.critDept, now, now)
	acat := ins(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('PC','Komputer',?,?)`, now, now)
	f.asset = ins(`INSERT INTO assets (asset_code, name, asset_category_id, department_id, room_id, created_at, updated_at) VALUES ('KSR-01','PC Kasir',?,?,?,?,?)`, acat, f.dept, f.room, now, now)
	if err := db.QueryRow(`SELECT id FROM ticket_categories WHERE name = 'Komputer tidak menyala'`).Scan(&f.cat); err != nil {
		t.Fatal(err)
	}
	f.catInactive = ins(`INSERT INTO ticket_categories (it_category_id, name, is_active, created_at, updated_at) VALUES (1, 'Lama', FALSE, ?, ?)`, now, now)
	return f
}

func (f fx) draft(mod func(*tickets.Draft)) tickets.Draft {
	d := tickets.Draft{
		Channel: tickets.ChannelQR, RoomID: f.room, RoomFromQR: true, ReporterName: "Ani",
		TicketCategoryID: f.cat, Description: "Komputer mati total", Impact: 2, Scope: 1, Replacement: priority.ReplacementYes,
	}
	if mod != nil {
		mod(&d)
	}
	return d
}

func TestCreateTicketAutomation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	c, errs, err := f.svc.Create(ctx, f.draft(func(d *tickets.Draft) { d.AssetID = f.asset }), nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("create gagal: %v %v", errs, err)
	}
	if c.Number != "TKT-2026-000001" {
		t.Fatalf("nomor tiket = %s", c.Number)
	}
	if c.Priority != priority.Medium {
		t.Fatalf("satu komputer, ada pengganti → Sedang; dapat %s", c.Priority)
	}
	tk, hist, _, err := f.svc.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != tickets.StatusNew || !tk.DepartmentID.Valid || tk.DepartmentID.Int64 != f.dept || tk.ReporterDeptName.String != "Keuangan" {
		t.Fatalf("lokasi/unit pelapor harus terisi otomatis dari ruangan: %+v", tk)
	}
	if !tk.PriorityRuleName.Valid || len(hist) != 1 || hist[0].EventType != tickets.EventCreated || hist[0].ActorType != "REPORTER" {
		t.Fatalf("histori/aturan prioritas tidak tercatat: %+v %+v", tk.PriorityRuleName, hist)
	}

	// Token pelapor: hanya hash yang tersimpan, token asli membuka tiket, token lain tidak.
	if n := testutil.Count(t, f.db, `SELECT COUNT(*) FROM tickets WHERE public_token_hash = ?`, c.Token); n != 0 {
		t.Fatal("token asli tidak boleh tersimpan")
	}
	pub, _, _, err := f.svc.GetPublic(ctx, c.Token)
	if err != nil || pub.ID != c.ID {
		t.Fatalf("token pelapor harus membuka tiketnya: %v", err)
	}
	other, _ := tickets.NewToken()
	if _, _, _, err := f.svc.GetPublic(ctx, other); err != tickets.ErrNotFound {
		t.Fatalf("token lain harus ErrNotFound, dapat %v", err)
	}
	if _, _, _, err := f.svc.GetPublic(ctx, "pendek"); err != tickets.ErrNotFound {
		t.Fatal("format token salah harus ErrNotFound")
	}

	// Layanan kritis dihitung dari ruangan/unit, bukan dari pelapor.
	c2, errs, err := f.svc.Create(ctx, f.draft(func(d *tickets.Draft) {
		d.RoomID, d.Scope, d.Replacement = f.critRoom, 3, priority.ReplacementNo
	}), nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if c2.Priority != priority.Critical || c2.Number != "TKT-2026-000002" {
		t.Fatalf("ICU berhenti satu unit → Kritis nomor 2; dapat %s %s", c2.Priority, c2.Number)
	}
}

func TestCreateValidation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	otherAsset := func(d *tickets.Draft) { d.RoomID = f.otherRoom; d.AssetID = f.asset }
	cases := map[string]struct {
		mod   func(*tickets.Draft)
		field string
	}{
		"perangkat bukan di ruangan QR": {otherAsset, "asset_id"},
		"jenis gangguan nonaktif":       {func(d *tickets.Draft) { d.TicketCategoryID = f.catInactive }, "ticket_category_id"},
		"ruangan bukan milik unit":      {func(d *tickets.Draft) { d.RoomFromQR = false; d.DepartmentID = f.critDept }, "room_id"},
		"perangkat tidak ada":           {func(d *tickets.Draft) { d.AssetID = 99999 }, "asset_id"},
	}
	for name, c := range cases {
		created, errs, err := f.svc.Create(ctx, f.draft(c.mod), nil)
		if err != nil || created.ID != 0 || errs[c.field] == "" {
			t.Errorf("%s: harus error di %s; dapat %v %v", name, c.field, errs, err)
		}
	}
	if n := testutil.Count(t, f.db, `SELECT COUNT(*) FROM tickets`); n != 0 {
		t.Fatalf("tiket tidak valid tidak boleh tersimpan, ada %d", n)
	}
	// Nomor urut tidak "terpakai" oleh percobaan yang gagal.
	c, _, _ := f.svc.Create(ctx, f.draft(nil), nil)
	if c.Number != "TKT-2026-000001" {
		t.Fatalf("nomor setelah percobaan gagal = %s", c.Number)
	}
}

func TestParseCommon(t *testing.T) {
	now := time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)
	d, _, errs := tickets.ParseCommon(testutil.Form(map[string]string{
		"reporter_name": " Ani ", "ticket_category_id": "3", "description": "Printer macet",
		"impact_level": "1", "affected_scope": "2", "incident_started": "1h",
	}), now)
	if len(errs) != 0 || d.ReporterName != "Ani" || d.Replacement != priority.ReplacementUnknown ||
		d.IncidentStartedAt == nil || !d.IncidentStartedAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("parse salah: %+v %v", d, errs)
	}
	_, _, errs = tickets.ParseCommon(testutil.Form(map[string]string{
		"impact_level": "9", "affected_scope": "0", "description": "ab", "replacement_available": "MUNGKIN",
		"incident_started": "kemarin lusa", "reporter_phone": "<script>",
	}), now)
	for _, k := range []string{"reporter_name", "ticket_category_id", "impact_level", "affected_scope", "description", "replacement_available", "incident_started", "reporter_phone"} {
		if errs[k] == "" {
			t.Errorf("field %s harus error", k)
		}
	}
}

func TestTicketNumberingConcurrent(t *testing.T) {
	f := setup(t)
	const n = 20
	var wg sync.WaitGroup
	numbers := make([]string, n)
	errsCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, fe, err := f.svc.Create(context.Background(), f.draft(nil), nil)
			if err != nil || len(fe) > 0 {
				errsCh <- fmt.Errorf("%v %v", fe, err)
				return
			}
			numbers[i] = c.Number
		}(i)
	}
	wg.Wait()
	close(errsCh)
	for err := range errsCh {
		t.Fatal(err)
	}
	sort.Strings(numbers)
	for i, num := range numbers {
		if want := fmt.Sprintf("TKT-2026-%06d", i+1); num != want {
			t.Fatalf("nomor ke-%d = %s, ingin %s (harus unik & berurutan)", i, num, want)
		}
	}
}

// TEST KRITIS (master prompt §23): petugas mengambil tiket yang sama bersamaan → hanya satu berhasil.
func TestConcurrentClaimOnlyOneWins(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	c, _, err := f.svc.Create(ctx, f.draft(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	const n = 10
	users := make([]int64, n)
	for i := range users {
		users[i] = testutil.CreateUser(t, f.db, fmt.Sprintf("tek%02d", i), "TECHNICIAN", "password-teknisi", false)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]tickets.ClaimResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.svc.Claim(ctx, c.ID, users[i])
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	var winner int64
	for i, r := range results {
		if errs[i] != nil {
			t.Fatalf("klaim %d error: %v", i, errs[i])
		}
		if r.OK {
			winners++
			winner = users[i]
		} else if r.ClaimedBy == "" || r.Status != tickets.StatusClaimed {
			t.Errorf("yang kalah harus diberi tahu siapa pengambilnya: %+v", r)
		}
	}
	if winners != 1 {
		t.Fatalf("harus tepat 1 pemenang, dapat %d", winners)
	}
	if n := testutil.Count(t, f.db, `SELECT COUNT(*) FROM ticket_assignments WHERE ticket_id = ? AND role = 'PRIMARY'`, c.ID); n != 1 {
		t.Fatalf("assignment PRIMARY harus 1, dapat %d", n)
	}
	if n := testutil.Count(t, f.db, `SELECT COUNT(*) FROM ticket_histories WHERE ticket_id = ? AND event_type = 'CLAIMED'`, c.ID); n != 1 {
		t.Fatalf("histori CLAIMED harus 1, dapat %d", n)
	}
	var assignee int64
	var status string
	if err := f.db.QueryRow(`SELECT assignee_id, status FROM tickets WHERE id = ?`, c.ID).Scan(&assignee, &status); err != nil {
		t.Fatal(err)
	}
	if assignee != winner || status != tickets.StatusClaimed {
		t.Fatalf("tiket harus milik pemenang: assignee=%d winner=%d status=%s", assignee, winner, status)
	}
	if _, err := f.svc.Claim(ctx, 999999, users[0]); err != tickets.ErrNotFound {
		t.Fatalf("tiket tidak ada harus ErrNotFound, dapat %v", err)
	}
}

func pngPhoto(t *testing.T) tickets.Photo {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	p, err := attachments.Process(&b, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return tickets.Photo{Processed: p, OriginalName: "foto.png"}
}

func countFiles(t *testing.T, dir string) int {
	n := 0
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".jpg") {
			n++
		}
		return nil
	})
	return n
}

func TestPhotosStoredAndCleanedUp(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	c, errs, err := f.svc.Create(ctx, f.draft(nil), []tickets.Photo{pngPhoto(t), pngPhoto(t)})
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	_, _, atts, _ := f.svc.Get(ctx, c.ID)
	if len(atts) != 2 || countFiles(t, f.store.Dir) != 2 {
		t.Fatalf("2 foto harus tersimpan: rows=%d files=%d", len(atts), countFiles(t, f.store.Dir))
	}
	key, err := f.svc.PublicAttachmentKey(ctx, c.Token, atts[0].ID)
	if err != nil || key == "" {
		t.Fatalf("pelapor harus bisa membuka fotonya: %v", err)
	}
	other, _ := tickets.NewToken()
	if _, err := f.svc.PublicAttachmentKey(ctx, other, atts[0].ID); err != tickets.ErrNotFound {
		t.Fatal("token lain tidak boleh membuka foto")
	}
	// Validasi gagal → file yang sempat ditulis harus dihapus.
	_, errs, _ = f.svc.Create(ctx, f.draft(func(d *tickets.Draft) { d.TicketCategoryID = f.catInactive }), []tickets.Photo{pngPhoto(t)})
	if len(errs) == 0 || countFiles(t, f.store.Dir) != 2 {
		t.Fatalf("file tiket gagal harus dibersihkan: files=%d", countFiles(t, f.store.Dir))
	}
}

func TestQueueAndWorkload(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	tech := testutil.CreateUser(t, f.db, "budi", "TECHNICIAN", "password-budi-1", false)
	// Kompetensi budi: Jaringan saja (id bidang NET).
	if _, err := f.db.Exec(`INSERT INTO user_competencies (user_id, it_category_id, created_at) SELECT ?, id, UTC_TIMESTAMP() FROM it_categories WHERE code = 'NET'`, tech); err != nil {
		t.Fatal(err)
	}
	low, _, _ := f.svc.Create(ctx, f.draft(func(d *tickets.Draft) { d.Impact = 0 }), nil)
	crit, _, _ := f.svc.Create(ctx, f.draft(func(d *tickets.Draft) { d.RoomID, d.Scope, d.Replacement = f.critRoom, 3, priority.ReplacementNo }), nil)

	list, total, err := f.svc.Queue(ctx, tickets.QueueFilter{Mode: "available", UserID: tech})
	if err != nil || total != 2 || list[0].ID != crit.ID || list[1].ID != low.ID {
		t.Fatalf("antrean harus diurutkan prioritas (kritis dulu): total=%d %v", total, err)
	}
	_, total, _ = f.svc.Queue(ctx, tickets.QueueFilter{Mode: "available", UserID: tech, OnlyCompetency: true})
	if total != 0 {
		t.Fatalf("filter kompetensi Jaringan harus menyembunyikan tiket Hardware, dapat %d", total)
	}
	if _, err := f.svc.Claim(ctx, low.ID, tech); err != nil {
		t.Fatal(err)
	}
	_, total, _ = f.svc.Queue(ctx, tickets.QueueFilter{Mode: "mine", UserID: tech})
	if total != 1 {
		t.Fatalf("tiket saya harus 1, dapat %d", total)
	}
	wl, err := f.svc.Workload(ctx)
	if err != nil || len(wl) != 1 || wl[0].Primary != 1 {
		t.Fatalf("beban kerja: %+v %v", wl, err)
	}
	sum, err := f.svc.Summary(ctx, tech)
	if err != nil || sum.CriticalNew != 1 || sum.MineActive != 1 || sum.ByStatus["NEW"] != 1 {
		t.Fatalf("ringkasan: %+v %v", sum, err)
	}
}

func TestStaffCreatedTicket(t *testing.T) {
	f := setup(t)
	tech := testutil.CreateUser(t, f.db, "sari", "TECHNICIAN", "password-sari-1", false)
	c, errs, err := f.svc.Create(context.Background(), tickets.Draft{
		Channel: tickets.ChannelPhone, CreatedBy: tech, DepartmentID: f.dept, ReporterName: "Pak Kasir",
		TicketCategoryID: f.cat, Description: "Telepon: PC kasir mati", Impact: 2, Scope: 1, Replacement: priority.ReplacementNo,
	}, nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	tk, hist, _, _ := f.svc.Get(context.Background(), c.ID)
	if tk.Channel != tickets.ChannelPhone || tk.CreatedByName.String != "User sari" || hist[0].ActorType != "USER" {
		t.Fatalf("tiket petugas harus mencatat sumber & pembuat: %+v %+v", tk, hist[0])
	}
	// Database menolak tiket non-QR tanpa pembuat.
	_, err = f.db.Exec(`INSERT INTO tickets (ticket_no, public_token_hash, channel, reporter_name, ticket_category_id, it_category_id,
		description, impact_level, affected_scope, priority, created_at, updated_at)
		VALUES ('X', UNHEX(SHA2('x',256)), 'PHONE', 'x', ?, 1, 'x', 0, 1, 'LOW', UTC_TIMESTAMP(), UTC_TIMESTAMP())`, f.cat)
	if err == nil {
		t.Fatal("CHECK chk_tickets_channel harus menolak tiket telepon tanpa pembuat")
	}
}

// Retensi foto (Q9): file foto tiket yang sudah ditutup/dibatalkan lewat masa
// simpan dihapus; baris metadata & data tiket tetap.
func TestAttachmentRetention(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old, _, err := f.svc.Create(ctx, f.draft(nil), []tickets.Photo{pngPhoto(t)})
	if err != nil {
		t.Fatal(err)
	}
	active, _, _ := f.svc.Create(ctx, f.draft(nil), []tickets.Photo{pngPhoto(t)})
	now := time.Now().UTC()
	if _, err := f.db.Exec(`UPDATE tickets SET status = 'CANCELLED', cancelled_at = ?, cancel_reason = 'uji' WHERE id = ?`, now.AddDate(0, 0, -800), old.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := attachments.PurgeExpired(ctx, f.db, f.store, now, 0); err != nil || n != 0 {
		t.Fatalf("retensi 0 = simpan selamanya: %d %v", n, err)
	}
	n, err := attachments.PurgeExpired(ctx, f.db, f.store, now, 730)
	if err != nil || n != 1 {
		t.Fatalf("hanya foto tiket lama yang dihapus: %d %v", n, err)
	}
	if countFiles(t, f.store.Dir) != 1 {
		t.Fatal("file foto tiket lama terhapus, foto tiket aktif tetap")
	}
	if testutil.Count(t, f.db, `SELECT COUNT(*) FROM attachments WHERE ticket_id = ? AND file_deleted_at IS NOT NULL`, old.ID) != 1 {
		t.Fatal("metadata foto tetap dengan tanda file dihapus")
	}
	if _, _, atts, _ := f.svc.Get(ctx, old.ID); len(atts) != 0 {
		t.Fatal("foto yang dihapus tidak ditampilkan")
	}
	if _, _, atts, _ := f.svc.Get(ctx, active.ID); len(atts) != 1 {
		t.Fatal("foto tiket aktif tetap tampil")
	}
	if n, _ := attachments.PurgeExpired(ctx, f.db, f.store, now, 730); n != 0 {
		t.Fatal("pemrosesan ulang idempoten")
	}
}
