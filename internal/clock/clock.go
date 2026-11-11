// Package clock menyediakan sumber waktu yang bisa diganti saat pengujian.
// Seluruh waktu yang disimpan ke database dalam UTC.
package clock

import (
	"sync"
	"time"
)

// Clock mengembalikan waktu saat ini (UTC).
type Clock interface {
	Now() time.Time
}

// Real memakai jam sistem.
type Real struct{}

// Now mengembalikan waktu sistem dalam UTC, dibulatkan ke milidetik
// (sesuai presisi kolom DATETIME(3)).
func (Real) Now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// Fake adalah jam yang dapat diatur, untuk pengujian.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake membuat jam palsu pada waktu t.
func NewFake(t time.Time) *Fake { return &Fake{now: t.UTC().Truncate(time.Millisecond)} }

// Now mengembalikan waktu jam palsu.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance memajukan jam palsu.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Location adalah zona waktu tampilan (WITA).
var Location = mustLoad("Asia/Makassar")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// tzdata di-embed lewat import time/tzdata di main; fallback offset tetap.
		return time.FixedZone("WITA", 8*3600)
	}
	return loc
}
