package database_test

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"sikat"
	"sikat/internal/database"
	"sikat/internal/testutil"
)

func TestPrepareDSN(t *testing.T) {
	dsn, err := database.PrepareDSN("u:p@tcp(127.0.0.1:3306)/sikat")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"parseTime=true", "time_zone=%27%2B00%3A00%27", "sql_mode="} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN harus memuat %s: %s", want, dsn)
		}
	}
	if _, err := database.PrepareDSN("::bukan dsn::"); err == nil {
		t.Fatal("DSN rusak harus error")
	}
}

func TestMigrationsDownAndUp(t *testing.T) {
	db := testutil.DB(t) // sudah Up
	mfs, _ := fs.Sub(sikat.MigrationsFS, "migrations")
	m, err := database.NewMigrator(db, mfs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Reset(ctx); err != nil {
		t.Fatalf("down semua migrasi gagal: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'users'`); n != 0 {
		t.Fatal("tabel users harus terhapus setelah reset")
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up ulang gagal: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM roles`); n != 3 {
		t.Fatalf("seed role harus 3, dapat %d", n)
	}
}

func TestSessionTimeZoneIsUTC(t *testing.T) {
	db := testutil.DB(t)
	var tz string
	if err := db.QueryRow(`SELECT @@session.time_zone`).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "+00:00" {
		t.Fatalf("time_zone sesi harus +00:00, dapat %s", tz)
	}
	var now time.Time
	if err := db.QueryRow(`SELECT UTC_TIMESTAMP(3)`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	if now.Location() != time.UTC {
		t.Fatalf("waktu harus diparse sebagai UTC, dapat %v", now.Location())
	}
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	db := testutil.DB(t)
	if _, err := db.Exec(`INSERT INTO audit_logs (actor_type, action, created_at) VALUES ('SYSTEM','test', UTC_TIMESTAMP(3))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE audit_logs SET action = 'ubah'`); err == nil {
		t.Fatal("UPDATE audit_logs harus ditolak trigger")
	}
	if _, err := db.Exec(`DELETE FROM audit_logs`); err == nil {
		t.Fatal("DELETE audit_logs harus ditolak trigger")
	}
}

func TestErrorHelpers(t *testing.T) {
	db := testutil.DB(t)
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('A','A',?,?), ('A','B',?,?)`, now, now, now, now)
	if key, ok := database.DuplicateKey(err); !ok || key != "uq_departments_code" {
		t.Fatalf("DuplicateKey = %q,%v (%v)", key, ok, err)
	}
	_, err = db.Exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) VALUES (9999,'R','R',?,?)`, now, now)
	if !database.IsForeignKeyMissing(err) {
		t.Fatalf("harus terdeteksi FK tidak ada: %v", err)
	}
	_, err = db.Exec(`INSERT INTO spare_parts (code, name, default_unit_cost, created_at, updated_at) VALUES ('X','X',-1,?,?)`, now, now)
	if name, ok := database.CheckConstraint(err); !ok || name != "chk_spare_parts_cost" {
		t.Fatalf("CheckConstraint = %q,%v (%v)", name, ok, err)
	}
}

func TestUniformCodesMigration(t *testing.T) {
	db := testutil.DB(t) // sudah Up, termasuk 00015 dan 00016
	ctx := context.Background()
	code := func(q string, args ...any) string {
		t.Helper()
		var s string
		if err := db.QueryRow(q, args...).Scan(&s); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return s
	}

	// Instalasi baru: kode seed ikut seragam, urut sesuai urutan tampil.
	// Hardware diganti nama menjadi Pelayanan & Support oleh 00016; kodenya tetap.
	if c := code(`SELECT code FROM it_categories WHERE name = 'Pelayanan & Support'`); c != "BDG-01" {
		t.Fatalf("Pelayanan & Support (dulu Hardware) = %q, harus BDG-01", c)
	}
	if c := code(`SELECT code FROM it_categories WHERE is_triage`); c != "BDG-07" {
		t.Fatalf("Triase = %q, harus BDG-07", c)
	}
	if c := code(`SELECT code FROM pending_reasons WHERE label = 'Lainnya'`); c != "ALS-05" {
		t.Fatalf("alasan Lainnya = %q, harus ALS-05", c)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM pending_reasons WHERE requires_note`); n != 1 ||
		code(`SELECT label FROM pending_reasons WHERE requires_note`) != "Lainnya" {
		t.Fatal(`hanya alasan "Lainnya" (dulu kode OTHER) yang wajib keterangan`)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE NOT REGEXP_LIKE(code, '^BDG-[0-9]{2}$')`); n != 0 {
		t.Fatalf("%d bidang belum berformat BDG-NN", n)
	}

	// Down 00017, 00016, lalu 00015 → nama dan kode lama kembali.
	mfs, _ := fs.Sub(sikat.MigrationsFS, "migrations")
	m, err := database.NewMigrator(db, mfs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"00017", "00016", "00015"} {
		if _, err := m.Down(ctx); err != nil {
			t.Fatalf("down %s: %v", v, err)
		}
	}
	if c := code(`SELECT code FROM it_categories WHERE name = 'Hardware'`); c != "HW" {
		t.Fatalf("setelah down Hardware = %q, harus HW", c)
	}

	// Data lama: kode bebas, kode yang sudah berformat baru, data SIMRS, nomor teralokasi.
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	now := time.Now().UTC()
	igd := exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('IGD','IGD',?,?)`, now, now)
	exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('UNT-002','Farmasi',?,?)`, now, now)
	exec(`INSERT INTO departments (code, name, source, created_at, updated_at) VALUES ('SIM-01','Radiologi','SIMRS',?,?)`, now, now)
	exec(`INSERT INTO number_sequences (name, year, current_value) VALUES ('UNT', 0, 5)`)
	// Pindahan dari aplikasi lama (tercatat CSV) berkode LEGACY-: ikut diseragamkan.
	legacyDept := exec(`INSERT INTO departments (code, name, source, created_at, updated_at) VALUES ('LEGACY-U3','Penunjang Medis','CSV',?,?)`, now, now)
	legacyRoom := exec(`INSERT INTO rooms (department_id, code, name, source, created_at, updated_at) VALUES (?, 'LEGACY-R16','BEDAH SENTRAL IBS','CSV',?,?)`, igd, now, now)
	exec(`INSERT INTO rooms (department_id, code, name, source, created_at, updated_at) VALUES (?, 'SIM-R1','Ruang SIMRS','SIMRS',?,?)`, igd, now, now)
	cat := exec(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('PC','Komputer',?,?)`, now, now)
	// 31/12/2025 17.00 UTC = 01/01/2026 WITA
	a2026 := exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('IGD-PC-01','PC',?,'2025-12-31 17:00:00','2025-12-31 17:00:00')`, cat)
	a2025 := exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('KSR-01','PC',?,'2025-06-01 02:00:00','2025-06-01 02:00:00')`, cat)
	exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('PRK-2026-0001','PC',?,?,?)`, cat, now, now)

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00015 dengan data lama: %v", err)
	}
	checks := []struct{ q, want string }{
		{`SELECT code FROM departments WHERE id = ` + itoa(igd), "UNT-006"},        // lanjut setelah nomor teralokasi (5)
		{`SELECT code FROM departments WHERE name = 'Farmasi'`, "UNT-002"},         // sudah berformat baru: tetap
		{`SELECT code FROM departments WHERE name = 'Radiologi'`, "SIM-01"},        // data SIMRS: tetap
		{`SELECT code FROM departments WHERE id = ` + itoa(legacyDept), "UNT-007"}, // LEGACY- dari CSV: ikut
		{`SELECT code FROM rooms WHERE id = ` + itoa(legacyRoom), "RNG-0001"},
		{`SELECT code FROM rooms WHERE name = 'Ruang SIMRS'`, "SIM-R1"},
		{`SELECT code FROM asset_categories WHERE id = ` + itoa(cat), "KAT-01"},
		{`SELECT asset_code FROM assets WHERE id = ` + itoa(a2026), "PRK-2026-0002"}, // tahun WITA
		{`SELECT asset_code FROM assets WHERE id = ` + itoa(a2025), "PRK-2025-0001"},
		{`SELECT old_code FROM legacy_codes WHERE table_name = 'departments' AND row_id = ` + itoa(igd), "IGD"},
		{`SELECT current_value FROM number_sequences WHERE name = 'PRK' AND year = 2026`, "2"},
	}
	for _, c := range checks {
		if got := code(c.q); got != c.want {
			t.Errorf("%s = %q, harus %q", c.q, got, c.want)
		}
	}
	// Relasi & pemetaan memakai ID: tidak berubah. Baris CSV yang kodenya diganti
	// ditandai lokal agar impor ulang tidak mengembalikan kode LEGACY- diam-diam.
	if testutil.Count(t, db, `SELECT COUNT(*) FROM departments WHERE id = ? AND local_modified_at IS NOT NULL`, legacyDept) != 1 {
		t.Error("baris CSV berkode LEGACY- harus ditandai local_modified_at")
	}
	if testutil.Count(t, db, `SELECT COUNT(*) FROM departments WHERE name = 'Radiologi' AND local_modified_at IS NULL`) != 1 {
		t.Error("baris SIMRS yang kodenya tidak diganti tidak boleh ditandai")
	}
	// Kode baru melanjutkan nomor setelah migrasi.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if c, err := database.NextCode(ctx, tx, "departments", "code", "UNT", 0, 3); err != nil || c != "UNT-008" {
		t.Fatalf("kode berikutnya = %q (%v), harus UNT-008", c, err)
	}
	// 00016 menambah 3 bidang (BDG-08 sampai BDG-10).
	if c, err := database.NextCode(ctx, tx, "it_categories", "code", "BDG", 0, 2); err != nil || c != "BDG-11" {
		t.Fatalf("kode bidang berikutnya = %q (%v), harus BDG-11", c, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestBidangFungsiMigration(t *testing.T) {
	db := testutil.DB(t) // sudah Up, termasuk 00016
	ctx := context.Background()
	str := func(q string, args ...any) string {
		t.Helper()
		var s string
		if err := db.QueryRow(q, args...).Scan(&s); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return s
	}
	bidangJenis := func(jenis string) string {
		return str(`SELECT c.name FROM ticket_categories t JOIN it_categories c ON c.id = t.it_category_id WHERE t.name = ?`, jenis)
	}

	// Bidang aktif: 6 fungsi koordinator + Triase, urut tampil.
	rows, err := db.Query(`SELECT name FROM it_categories WHERE is_active ORDER BY sort_order`)
	if err != nil {
		t.Fatal(err)
	}
	var aktif []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		aktif = append(aktif, n)
	}
	rows.Close()
	want := "Pelayanan & Support,Administrasi & Inventaris,Mutu,ERM,System Developer,Keamanan Sistem,Triase / Belum terklasifikasi"
	if got := strings.Join(aktif, ","); got != want {
		t.Fatalf("bidang aktif = %s\nharus %s", got, want)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE name IN ('Printer','Jaringan','Server & Infrastruktur') AND NOT is_active`); n != 3 {
		t.Fatalf("bidang yang dilebur harus nonaktif, dapat %d", n)
	}
	for jenis, bidang := range map[string]string{
		"Tidak bisa mencetak":                "Pelayanan & Support",
		"Internet atau jaringan mati":        "Pelayanan & Support",
		"Komputer tidak menyala":             "Pelayanan & Support",
		"SIMRS error atau tidak bisa dibuka": "ERM",
		"Lupa password atau akun terkunci":   "Keamanan Sistem",
		"Permintaan perangkat baru":          "Administrasi & Inventaris",
		"Usulan sistem atau fitur baru":      "System Developer",
		"Perbaikan sistem":                   "System Developer",
	} {
		if got := bidangJenis(jenis); got != bidang {
			t.Errorf("jenis gangguan %q di bidang %q, harus %q", jenis, got, bidang)
		}
	}
	if got := str(`SELECT c.name FROM activity_types a JOIN it_categories c ON c.id = a.it_category_id WHERE a.name = 'Inventarisasi'`); got != "Administrasi & Inventaris" {
		t.Errorf("kegiatan Inventarisasi di bidang %q", got)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM activity_types a JOIN it_categories c ON c.id = a.it_category_id WHERE c.name = 'Mutu'`); n != 7 {
		t.Errorf("jenis kegiatan Mutu = %d, harus 7 (6 baru + Audit dan evaluasi)", n)
	}

	// Down → bidang teknis kembali; lalu kompetensi lama dipakai untuk menguji perpindahan.
	mfs, _ := fs.Sub(sikat.MigrationsFS, "migrations")
	m, err := database.NewMigrator(db, mfs)
	if err != nil {
		t.Fatal(err)
	}
	// down00016 membatalkan 00017 lebih dulu (migrasi terbaru) lalu 00016.
	down00016 := func() error {
		for _, want := range []string{"00017", "00016"} {
			got, err := m.Down(ctx)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(got, want) {
				t.Fatalf("down %s, harus %s", got, want)
			}
		}
		return nil
	}
	if err := down00016(); err != nil {
		t.Fatalf("down 00016: %v", err)
	}
	if got := bidangJenis("Tidak bisa mencetak"); got != "Printer" {
		t.Fatalf("setelah down, Tidak bisa mencetak di %q, harus Printer", got)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE name IN ('Mutu','Administrasi & Inventaris','System Developer')`); n != 0 {
		t.Fatalf("bidang baru harus terhapus setelah down, sisa %d", n)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories WHERE name = 'Permintaan perangkat baru'`); n != 0 {
		t.Fatal("jenis gangguan baru harus terhapus setelah down")
	}
	uid := testutil.CreateUser(t, db, "tek.bidang", "TECHNICIAN", "password-uji-12345", false)
	if _, err := db.Exec(`INSERT INTO user_competencies (user_id, it_category_id, created_at)
		SELECT ?, id, UTC_TIMESTAMP(3) FROM it_categories WHERE name IN ('Printer','Jaringan','Software & SIMRS')`, uid); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00016: %v", err)
	}
	komp := func() string {
		r, err := db.Query(`SELECT c.name FROM user_competencies u JOIN it_categories c ON c.id = u.it_category_id WHERE u.user_id = ? ORDER BY c.name`, uid)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		var out []string
		for r.Next() {
			var n string
			_ = r.Scan(&n)
			out = append(out, n)
		}
		return strings.Join(out, ",")
	}
	if got := komp(); got != "ERM,Pelayanan & Support" {
		t.Fatalf("kompetensi setelah up = %s, harus ERM,Pelayanan & Support", got)
	}
	if err := down00016(); err != nil {
		t.Fatalf("down 00016 kedua: %v", err)
	}
	if got := komp(); got != "Jaringan,Printer,Software & SIMRS" {
		t.Fatalf("kompetensi setelah down = %s, harus kembali Jaringan,Printer,Software & SIMRS", got)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00016 kedua: %v", err)
	}

	// Jenis gangguan baru yang sudah dirujuk (jenis pilihan pelapor di tiket, riwayat pemeliharaan)
	// tidak boleh menggagalkan Down: dibiarkan nonaktif, lalu Up berikutnya mengaktifkannya lagi.
	catID := func(name string) int64 {
		var id int64
		if err := db.QueryRow(`SELECT id FROM ticket_categories WHERE name = ?`, name).Scan(&id); err != nil {
			t.Fatalf("jenis %s: %v", name, err)
		}
		return id
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO tickets (ticket_no, public_token_hash, reporter_name, ticket_category_id, reported_ticket_category_id,
		it_category_id, description, impact_level, affected_scope, priority, created_at, updated_at)
		SELECT 'TKT-UJI-1', UNHEX(SHA2('uji', 256)), 'Pelapor', ?, ?, it_category_id, 'uji', 1, 1, 'LOW', ?, ? FROM ticket_categories WHERE id = ?`,
		catID("Komputer tidak menyala"), catID("Permintaan perangkat baru"), now, now, catID("Komputer tidak menyala")); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO asset_categories (code, name, created_at, updated_at) VALUES ('KAT-UJI', 'Uji', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	katID, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO assets (asset_code, name, asset_category_id, created_at, updated_at) VALUES ('PRK-UJI', 'PC', ?, ?, ?)`, katID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	assetID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO asset_maintenance_histories (asset_id, maintenance_type, source, ticket_category_id, action_taken, result, finished_at, created_at, updated_at)
		VALUES (?, 'INSPECTION', 'MANUAL', ?, 'uji', 'DONE', ?, ?, ?)`, assetID, catID("Usulan sistem atau fitur baru"), now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := down00016(); err != nil {
		t.Fatalf("down 00016 dengan jenis baru yang dirujuk: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories WHERE name IN ('Permintaan perangkat baru','Usulan sistem atau fitur baru') AND NOT is_active`); n != 2 {
		t.Fatalf("jenis yang dirujuk harus tetap ada dan nonaktif, dapat %d", n)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories WHERE name = 'Perbaikan sistem'`); n != 0 {
		t.Fatal("jenis baru yang tidak dirujuk tetap dihapus")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE name IN ('Administrasi & Inventaris','System Developer') AND NOT is_active`); n != 2 {
		t.Fatalf("bidang yang masih dirujuk harus tetap ada dan nonaktif, dapat %d", n)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00016 ketiga: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories t JOIN it_categories c ON c.id = t.it_category_id
		WHERE t.name IN ('Permintaan perangkat baru','Usulan sistem atau fitur baru','Perbaikan sistem') AND t.is_active AND c.is_active`); n != 3 {
		t.Fatalf("setelah up lagi, jenis & bidangnya harus aktif kembali, dapat %d", n)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE is_active`); n != 7 {
		t.Fatalf("bidang aktif setelah up lagi = %d, harus 7", n)
	}
	if err := down00016(); err != nil {
		t.Fatalf("down 00016 keempat: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE name IN ('Administrasi & Inventaris','System Developer') AND NOT is_active`); n != 2 {
		t.Fatalf("down kembali menonaktifkan bidang yang diaktifkan ulang, dapat %d", n)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00016 terakhir: %v", err)
	}
}

func TestLaporPerBidangMigration(t *testing.T) {
	db := testutil.DB(t) // sudah Up, termasuk 00017
	ctx := context.Background()
	now := time.Now().UTC()

	// Tiap bidang aktif punya penjelasan untuk pelapor dan minimal satu jenis gangguan aktif.
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories WHERE is_active AND (reporter_help IS NULL OR reporter_help = '')`); n != 0 {
		t.Fatalf("%d bidang aktif belum punya penjelasan untuk pelapor", n)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM it_categories c WHERE c.is_active
		AND NOT EXISTS (SELECT 1 FROM ticket_categories t WHERE t.it_category_id = c.id AND t.is_active)`); n != 0 {
		t.Fatalf("%d bidang aktif tanpa jenis gangguan", n)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories t JOIN it_categories c ON c.id = t.it_category_id
		WHERE c.name = 'Mutu' AND t.is_active`); n != 3 {
		t.Fatalf("jenis gangguan Mutu = %d, harus 3", n)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories WHERE name LIKE 'Lainnya (%' AND is_active`); n != 6 {
		t.Fatalf("jenis Lainnya per bidang = %d, harus 6", n)
	}
	var help string
	if err := db.QueryRow(`SELECT help_text FROM ticket_categories WHERE name = 'Lainnya'`).Scan(&help); err != nil || strings.ContainsRune(help, '—') {
		t.Fatalf("bantuan Lainnya = %q (%v)", help, err)
	}

	// Hanya satu QR global aktif.
	if _, err := db.Exec(`INSERT INTO qr_codes (token, type, created_at, updated_at) VALUES ('GLOBAL0000000000000001', 'GLOBAL', ?, ?)`, now, now); err != nil {
		t.Fatalf("QR global: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO qr_codes (token, type, created_at, updated_at) VALUES ('GLOBAL0000000000000002', 'GLOBAL', ?, ?)`, now, now); err == nil {
		t.Fatal("QR global aktif kedua harus ditolak")
	}
	var qrID int64
	_ = db.QueryRow(`SELECT id FROM qr_codes WHERE type = 'GLOBAL'`).Scan(&qrID)

	// Tiket dari QR global dengan jenis baru tidak boleh menggagalkan Down.
	if _, err := db.Exec(`INSERT INTO tickets (ticket_no, public_token_hash, qr_code_id, reporter_name, ticket_category_id,
		it_category_id, description, impact_level, affected_scope, priority, created_at, updated_at)
		SELECT 'TKT-UJI-17', UNHEX(SHA2('uji17', 256)), ?, 'Pelapor', id, it_category_id, 'uji', 1, 1, 'LOW', ?, ?
		  FROM ticket_categories WHERE name = 'Lainnya (ERM)'`, qrID, now, now); err != nil {
		t.Fatal(err)
	}

	mfs, _ := fs.Sub(sikat.MigrationsFS, "migrations")
	m, err := database.NewMigrator(db, mfs)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.Down(ctx); err != nil || !strings.HasPrefix(got, "00017") {
		t.Fatalf("down 00017: %s %v", got, err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM qr_codes WHERE token LIKE 'GLOBAL%'`); n != 0 {
		t.Fatal("QR global harus terhapus setelah down")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM tickets WHERE ticket_no = 'TKT-UJI-17' AND qr_code_id IS NULL`); n != 1 {
		t.Fatal("tiket dari QR global tetap ada tanpa rujukan QR")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories WHERE name = 'Lainnya (ERM)' AND NOT is_active`); n != 1 {
		t.Fatal("jenis yang dirujuk tiket harus tetap ada dan nonaktif")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories WHERE name = 'Lainnya (Mutu)'`); n != 0 {
		t.Fatal("jenis baru yang tidak dirujuk harus terhapus")
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00017 lagi: %v", err)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_categories WHERE name LIKE 'Lainnya (%' AND is_active`); n != 6 {
		t.Fatalf("setelah up lagi, jenis Lainnya per bidang = %d, harus 6", n)
	}
	if _, err := m.Down(ctx); err != nil {
		t.Fatalf("down 00017 kedua: %v", err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 00017 terakhir: %v", err)
	}
}
