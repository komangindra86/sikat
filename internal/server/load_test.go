package server_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"sikat/internal/server"
	"sikat/internal/testutil"
)

// Uji beban ringan (PHASE 9). Tidak berjalan pada test biasa; jalankan dengan:
//
//	SIKAT_LOAD_TEST=1 SIKAT_TEST_DSN="root@tcp(127.0.0.1:3306)/" go test ./internal/server/ -run TestLoad -v -timeout 20m
//
// Mengisi ~20.000 tiket (± 2–3 tahun volume RS menengah) beserta histori, lalu
// mengukur latensi halaman utama dengan 20 pengguna bersamaan.
func TestLoad(t *testing.T) {
	if os.Getenv("SIKAT_LOAD_TEST") == "" {
		t.Skip("set SIKAT_LOAD_TEST=1 untuk uji beban")
	}
	srv, newClient, db := newAppDB(t, server.Deps{LoginRate: 10000})
	_ = srv
	koorID := testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor-123", false)
	techID := testutil.Count(t, db, `SELECT id FROM users WHERE username = 'budi'`)
	ctx := context.Background()
	now := time.Now().UTC()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO departments (code, name, created_at, updated_at) VALUES ('IGD','IGD',?,?), ('ICU','ICU',?,?), ('KEU','Keuangan',?,?)`, now, now, now, now, now, now)
	exec(`INSERT INTO rooms (department_id, code, name, created_at, updated_at) SELECT id, CONCAT(code,'-1'), CONCAT('Ruang ', name), ?, ? FROM departments`, now, now)

	const total = 20000
	start := time.Now()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET SESSION cte_max_recursion_depth = 100000`); err != nil {
		t.Fatal(err)
	}
	// Status: 70% ditutup, 10% selesai, 5% dibatalkan, sisanya aktif.
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO tickets (ticket_no, public_token_hash, channel, department_id, room_id, reporter_name, ticket_category_id, it_category_id,
		       description, impact_level, affected_scope, priority, status, assignee_id, pending_reason_id, first_response_at, resolved_at, closed_at,
		       cancelled_at, cancel_reason, resolution_action, resolution_result, created_at, updated_at)
		WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < ?)
		SELECT CONCAT('TKT-L-', LPAD(n, 7, '0')), UNHEX(SHA2(CONCAT('load', n), 256)), 'QR',
		       d.id, r.id, CONCAT('Pelapor ', n), tc.id, tc.it_category_id, 'Uji beban: perangkat bermasalah',
		       n % 3, 1 + n % 4, ELT(1 + n % 4, 'LOW','MEDIUM','HIGH','CRITICAL'),
		       CASE WHEN n % 20 < 14 THEN 'CLOSED' WHEN n % 20 < 16 THEN 'RESOLVED' WHEN n % 20 = 16 THEN 'CANCELLED'
		            WHEN n % 20 = 17 THEN 'NEW' WHEN n % 20 = 18 THEN 'IN_PROGRESS' ELSE 'PENDING' END,
		       IF(n % 20 IN (16, 17), NULL, ?), IF(n % 20 = 19, (SELECT MIN(id) FROM pending_reasons), NULL),
		       IF(n % 20 IN (16, 17), NULL, ts + INTERVAL 20 MINUTE),
		       IF(n % 20 < 16, ts + INTERVAL 3 HOUR, NULL), IF(n % 20 < 14, ts + INTERVAL 2 DAY, NULL),
		       IF(n % 20 = 16, ts + INTERVAL 1 HOUR, NULL), IF(n % 20 = 16, 'Duplikat', NULL),
		       IF(n % 20 < 16, 'Perbaikan uji beban', NULL), IF(n % 20 < 16, 'FIXED', NULL), ts, ts
		  FROM (SELECT n, ? - INTERVAL (n % 900) DAY - INTERVAL (n % 1440) MINUTE AS ts FROM seq) s
		  JOIN departments d ON d.id = (SELECT MIN(id) FROM departments) + s.n % 3
		  JOIN rooms r ON r.department_id = d.id
		  JOIN ticket_categories tc ON tc.id = (SELECT MIN(id) FROM ticket_categories WHERE is_active) + s.n % 8`, total, techID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO ticket_histories (ticket_id, event_type, to_status, actor_type, actor_user_id, created_at)
		SELECT id, 'CREATED', 'NEW', 'REPORTER', NULL, created_at FROM tickets
		UNION ALL SELECT id, 'CLAIMED', 'CLAIMED', 'USER', assignee_id, first_response_at FROM tickets WHERE first_response_at IS NOT NULL
		UNION ALL SELECT id, 'RESOLVED', 'RESOLVED', 'USER', assignee_id, resolved_at FROM tickets WHERE resolved_at IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `ANALYZE TABLE tickets, ticket_histories`); err != nil {
		t.Fatal(err)
	}
	t.Logf("data uji: %d tiket, %d histori (%s)", testutil.Count(t, db, `SELECT COUNT(*) FROM tickets`),
		testutil.Count(t, db, `SELECT COUNT(*) FROM ticket_histories`), time.Since(start).Round(time.Second))
	_ = koorID

	type page struct{ user, pass, path string }
	month := now.Format("2006-01")
	pages := []page{
		{"koor", "password-koor-123", "/app"},
		{"koor", "password-koor-123", "/app/tickets?mode=active"},
		{"budi", "password-budi-123", "/app"},
		{"budi", "password-budi-123", "/app/tickets"},
		{"koor", "password-koor-123", "/app/sla"},
		{"koor", "password-koor-123", "/app/reports/koordinator?periode=bulan&bulan=" + month},
		{"koor", "password-koor-123", "/app/reports/bulanan?periode=tahun&tahun=" + now.Format("2006")},
		{"koor", "password-koor-123", "/app/reports/petugas?periode=tahun&tahun=" + now.Format("2006")},
		{"koor", "password-koor-123", "/app/notifications/badge"},
	}
	const workers, perWorker = 20, 15
	for _, p := range pages {
		clients := make([]*client, workers)
		for i := range clients {
			clients[i] = newClient()
			clients[i].login(p.user, p.pass)
		}
		var mu sync.Mutex
		var lat []time.Duration
		errs := 0
		var wg sync.WaitGroup
		for _, c := range clients {
			wg.Add(1)
			go func(c *client) {
				defer wg.Done()
				for i := 0; i < perWorker; i++ {
					t0 := time.Now()
					resp, err := c.http.Get(c.base + p.path)
					d := time.Since(t0)
					mu.Lock()
					if err != nil || resp.StatusCode != http.StatusOK {
						errs++
					} else {
						lat = append(lat, d)
					}
					mu.Unlock()
					if resp != nil {
						resp.Body.Close()
					}
				}
			}(c)
		}
		wg.Wait()
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		pct := func(q float64) time.Duration {
			if len(lat) == 0 {
				return 0
			}
			return lat[int(q*float64(len(lat)-1))]
		}
		line := fmt.Sprintf("%-70s n=%d galat=%d p50=%v p95=%v maks=%v", p.path, len(lat), errs,
			pct(0.5).Round(time.Millisecond), pct(0.95).Round(time.Millisecond), pct(1).Round(time.Millisecond))
		t.Log(line)
		if errs > 0 {
			t.Errorf("galat pada %s: %d", p.path, errs)
		}
		if pct(0.95) > 2*time.Second {
			t.Errorf("p95 %s terlalu lambat: %v", p.path, pct(0.95))
		}
	}
}
