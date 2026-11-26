package licenses_test

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"sikat/internal/clock"
	"sikat/internal/licenses"
	"sikat/internal/notifications"
	"sikat/internal/testutil"
)

func TestStage(t *testing.T) {
	cases := map[int]string{60: "", 31: "", 30: "30", 15: "30", 14: "14", 8: "14", 7: "7", 2: "7", 1: "1", 0: "0", -6: "0", -7: "x1", -13: "x1", -14: "x2", -40: "x5"}
	for days, want := range cases {
		got, ok := licenses.Stage(days)
		if got != want || ok != (want != "") {
			t.Errorf("Stage(%d) = %q,%v; ingin %q", days, got, ok, want)
		}
	}
}

func TestCheckRemindsAdmin(t *testing.T) {
	db := testutil.DB(t)
	ctx := context.Background()
	// 26 Nov 2026 10.00 WITA.
	clk := clock.NewFake(time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC))
	admin := testutil.CreateUser(t, db, "admin", "ADMIN", "password-admin-123", false)
	testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)
	notif := notifications.NewService(db, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, "")
	chk := licenses.Checker{DB: db, Clock: clk}

	now := clk.Now()
	ins := func(code, name, expires string, active bool) int64 {
		var exp any
		if expires != "" {
			exp = expires
		}
		res, err := db.Exec(`INSERT INTO licenses (code, name, vendor, expires_at, is_active, created_at, updated_at) VALUES (?, ?, 'Vendor A', ?, ?, ?, ?)`,
			code, name, exp, active, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	office := ins("LIS-001", "Office 365", "2026-12-06", true) // 10 hari lagi
	ins("LIS-002", "Antivirus", "2027-06-01", true)            // masih lama
	ins("LIS-003", "Lama", "2026-11-01", false)                // nonaktif: tidak diingatkan
	ins("LIS-004", "Tanpa batas", "", true)
	ins("LIS-005", "SSL", "2026-11-18", true) // lewat 8 hari → tahap mingguan x1

	check := func(want int) {
		t.Helper()
		if n, err := chk.Check(ctx); err != nil || n != want {
			t.Fatalf("Check = %d, %v; ingin %d", n, err, want)
		}
	}
	check(2)
	check(0) // tahap yang sama tidak dikirim ulang

	if _, err := notif.Process(ctx); err != nil {
		t.Fatal(err)
	}
	var title, body, url, channel string
	if err := db.QueryRow(`SELECT title, body, url, channel FROM notifications WHERE user_id = ? AND title LIKE '%Office%'`, admin).
		Scan(&title, &body, &url, &channel); err != nil {
		t.Fatal(err)
	}
	if title != "Lisensi Office 365 berakhir 10 hari lagi" || body != "Berakhir 6 Desember 2026 · Vendor A · Setelah diperpanjang, perbarui tanggal berakhir di Master Data › Lisensi." ||
		channel != "PUSH" || url == "" {
		t.Fatalf("notifikasi admin: %q | %q | %q | %s", title, body, url, channel)
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.user_id WHERE u.username = 'koor'`); n != 0 {
		t.Fatal("koordinator tidak menerima pengingat lisensi secara bawaan")
	}
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM notifications WHERE title = 'Lisensi SSL sudah kedaluwarsa'`); n != 1 {
		t.Fatal("lisensi yang sudah lewat tetap diingatkan")
	}

	// 4 hari kemudian (6 hari lagi) → tahap 7 hari; hari berakhir → tahap 0.
	clk.Advance(4 * 24 * time.Hour)
	check(1)
	clk.Advance(6 * 24 * time.Hour)
	check(2) // Office: hari berakhir; SSL: minggu kedua setelah berakhir
	if n := testutil.Count(t, db, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'LICENSE_EXPIRY' AND JSON_EXTRACT(payload, '$.days') = 0`); n != 1 {
		t.Fatal("pengingat pada hari berakhir")
	}

	// Diperpanjang setahun → tidak ada pengingat sampai 30 hari sebelum tanggal baru.
	if _, err := db.Exec(`UPDATE licenses SET expires_at = '2027-12-06' WHERE id = ?`, office); err != nil {
		t.Fatal(err)
	}
	check(0)
	clk.Advance(340 * 24 * time.Hour) // 11 Nov 2027: 25 hari lagi
	n, err := chk.Check(ctx)
	if err != nil || n < 1 || testutil.Count(t, db, `SELECT COUNT(*) FROM outbox_events WHERE dedupe_key = ?`, "license:"+strconv.FormatInt(office, 10)+":2027-12-06:30") != 1 {
		t.Fatalf("pengingat dimulai lagi untuk tanggal berakhir baru: %d %v", n, err)
	}
}
