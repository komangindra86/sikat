package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sikat/internal/testutil"
	"sikat/internal/worker"
)

func TestOnlyOneLeader(t *testing.T) {
	db := testutil.DB(t)
	lock := "sikat_test_" + strings.ReplaceAll(t.Name(), "/", "_")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &worker.Runner{DB: db, Log: log, LockName: lock}
	b := &worker.Runner{DB: db, Log: log, LockName: lock}
	ctx := context.Background()

	ca, err := a.TryLock(ctx)
	if err != nil || ca == nil {
		t.Fatalf("runner A harus mendapat lock: %v", err)
	}
	cb, err := b.TryLock(ctx)
	if err != nil || cb != nil {
		t.Fatal("runner B tidak boleh mendapat lock selama A memegangnya")
	}
	ca.Close() // koneksi putus → lock lepas
	cb, err = b.TryLock(ctx)
	if err != nil || cb == nil {
		t.Fatalf("setelah A lepas, B harus mendapat lock: %v", err)
	}
	cb.Close()
}

func TestRunJobsAndRecover(t *testing.T) {
	db := testutil.DB(t)
	var runs, panics atomic.Int32
	r := &worker.Runner{
		DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), LockName: "sikat_test_run_jobs",
		Jobs: []worker.Job{
			{Name: "hitung", Interval: 20 * time.Millisecond, Run: func(context.Context) error { runs.Add(1); return nil }},
			{Name: "panik", Interval: 20 * time.Millisecond, Run: func(context.Context) error { panics.Add(1); panic("uji") }},
			{Name: "gagal", Interval: 20 * time.Millisecond, Run: func(context.Context) error { return errors.New("uji") }},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	<-done
	if runs.Load() < 3 || panics.Load() < 3 {
		t.Fatalf("job harus berjalan berulang walau ada yang panic: runs=%d panics=%d", runs.Load(), panics.Load())
	}
	if r.IsLeader() {
		t.Fatal("setelah berhenti, runner tidak boleh tetap leader")
	}
}
