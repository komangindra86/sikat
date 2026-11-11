// Package worker menjalankan pekerjaan latar belakang terjadwal (tutup otomatis
// tiket, dan pada fase berikutnya: SLA, notifikasi, sinkronisasi, backup).
//
// Agar aman bila suatu saat ada lebih dari satu instance aplikasi, hanya
// pemegang MySQL GET_LOCK yang menjalankan job. Lock terikat pada satu koneksi
// khusus; bila koneksi putus, lock lepas otomatis dan instance lain dapat mengambilnya.
package worker

import (
	"context"
	"database/sql"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Job adalah satu pekerjaan berkala.
type Job struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context) error
}

// Runner menjalankan job bila menjadi leader.
type Runner struct {
	DB       *sql.DB
	Log      *slog.Logger
	LockName string
	Jobs     []Job
	// RetryLeader adalah jeda mencoba ulang menjadi leader.
	RetryLeader time.Duration

	mu     sync.Mutex
	leader bool
}

// IsLeader bernilai true bila runner sedang memegang lock.
func (r *Runner) IsLeader() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.leader
}

func (r *Runner) setLeader(v bool) {
	r.mu.Lock()
	r.leader = v
	r.mu.Unlock()
}

// TryLock mencoba mengambil lock pada koneksi khusus. Mengembalikan koneksi
// pemegang lock (nil bila lock dipegang pihak lain).
func (r *Runner) TryLock(ctx context.Context) (*sql.Conn, error) {
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, r.LockName).Scan(&got); err != nil {
		conn.Close()
		return nil, err
	}
	if !got.Valid || got.Int64 != 1 {
		conn.Close()
		return nil, nil
	}
	return conn, nil
}

// Run memblokir sampai ctx selesai.
func (r *Runner) Run(ctx context.Context) {
	if r.RetryLeader == 0 {
		r.RetryLeader = time.Minute
	}
	for {
		conn, err := r.TryLock(ctx)
		if err != nil && ctx.Err() == nil {
			r.Log.Error("worker: gagal mengambil lock", "err", err)
		}
		if conn != nil {
			r.setLeader(true)
			r.Log.Info("worker: menjadi leader", "lock", r.LockName)
			r.runJobs(ctx, conn)
			r.setLeader(false)
			_, _ = conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK(?)`, r.LockName)
			conn.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.RetryLeader):
		}
	}
}

// runJobs menjalankan semua job sampai ctx selesai atau koneksi lock putus.
func (r *Runner) runJobs(ctx context.Context, conn *sql.Conn) {
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for _, j := range r.Jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			r.loop(jobCtx, j)
		}(j)
	}
	// Pantau koneksi lock; bila putus, hentikan job agar tidak ada dua leader.
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			wg.Wait()
			return
		case <-t.C:
			if err := conn.PingContext(ctx); err != nil {
				r.Log.Error("worker: koneksi lock putus, berhenti menjadi leader", "err", err)
				cancel()
				wg.Wait()
				return
			}
		}
	}
}

func (r *Runner) loop(ctx context.Context, j Job) {
	r.RunOnce(ctx, j)
	t := time.NewTicker(j.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.RunOnce(ctx, j)
		}
	}
}

// RunOnce menjalankan job sekali dengan pencatatan & pemulihan panic.
func (r *Runner) RunOnce(ctx context.Context, j Job) {
	start := time.Now()
	defer func() {
		if p := recover(); p != nil {
			r.Log.Error("worker: panic", "job", j.Name, "err", p, "stack", string(debug.Stack()))
		}
	}()
	if err := j.Run(ctx); err != nil && ctx.Err() == nil {
		r.Log.Error("worker: job gagal", "job", j.Name, "err", err, "dur_ms", time.Since(start).Milliseconds())
		return
	}
	r.Log.Debug("worker: job selesai", "job", j.Name, "dur_ms", time.Since(start).Milliseconds())
}
