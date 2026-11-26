// Package licenses memeriksa lisensi (Master Data › Lisensi) yang mendekati atau
// melewati tanggal berakhir dan mengirim notifikasi "Lisensi segera habis".
package licenses

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"sikat/internal/clock"
	"sikat/internal/master"
	"sikat/internal/notifications"
)

// RemindFrom: pengingat pertama dikirim saat sisa hari ≤ RemindFrom.
const RemindFrom = 30

// Stage menentukan tahap pengingat untuk sisa hari: "30", "14", "7", "1", "0" (hari
// berakhir), lalu "x1", "x2", … tiap 7 hari setelah berakhir. ok=false bila belum waktunya.
// Satu notifikasi per tahap; tahap yang terlewat (mis. lisensi baru dicatat 5 hari
// sebelum berakhir) langsung masuk tahap saat ini.
func Stage(days int) (stage string, ok bool) {
	switch {
	case days > RemindFrom:
		return "", false
	case days > 14:
		return "30", true
	case days > 7:
		return "14", true
	case days > 1:
		return "7", true
	case days == 1:
		return "1", true
	case days > -7:
		return "0", true
	}
	return "x" + strconv.Itoa(-days/7), true
}

// Checker memeriksa lisensi aktif yang bertanggal berakhir.
type Checker struct {
	DB    *sql.DB
	Clock clock.Clock
}

// Check menulis event notifikasi untuk tahap pengingat yang belum dikirim dan
// mengembalikan jumlah event baru. Kunci dedupe memuat tanggal berakhir, sehingga
// memperbarui tanggal (perpanjangan) memulai pengingat dari awal.
func (c Checker) Check(ctx context.Context) (int, error) {
	now := c.Clock.Now()
	today := now.In(clock.Location)
	limit := time.Date(today.Year(), today.Month(), today.Day()+RemindFrom, 0, 0, 0, 0, time.UTC)
	rows, err := c.DB.QueryContext(ctx, `
		SELECT id, name, vendor, expires_at FROM licenses
		 WHERE is_active = TRUE AND expires_at IS NOT NULL AND expires_at <= ?
		 ORDER BY expires_at, id`, limit.Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	type lic struct {
		id      int64
		name    string
		vendor  sql.NullString
		expires time.Time
	}
	var due []lic
	for rows.Next() {
		var l lic
		if err := rows.Scan(&l.id, &l.name, &l.vendor, &l.expires); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	created := 0
	for _, l := range due {
		days := master.DaysUntil(today, l.expires)
		stage, ok := Stage(days)
		if !ok {
			continue
		}
		exp := l.expires.Format("2006-01-02")
		key := fmt.Sprintf("license:%d:%s:%s", l.id, exp, stage)
		var exists int
		if err := c.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE dedupe_key = ?`, key).Scan(&exists); err != nil {
			return created, err
		}
		if exists > 0 {
			continue
		}
		if _, err := notifications.Publish(ctx, c.DB, notifications.Event{
			Type: notifications.LicenseExpiry, DedupeKey: key,
			Payload: map[string]any{"license_id": l.id, "name": l.name, "vendor": l.vendor.String, "expires_at": exp, "days": days},
		}, now); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}
