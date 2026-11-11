package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"sikat/internal/reqctx"
)

// RateLimiter membatasi laju request per kunci (biasanya IP) di memori.
// Cukup untuk satu instance aplikasi; lihat risiko R11 di IMPLEMENTATION_PLAN.
type RateLimiter struct {
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
	ttl      time.Duration
	visitors map[string]*visitor
}

type visitor struct {
	lim  *rate.Limiter
	seen time.Time
}

// NewRateLimiter mengizinkan rata-rata `per` request setiap `every` dengan burst `burst`.
func NewRateLimiter(per int, every time.Duration, burst int) *RateLimiter {
	return &RateLimiter{
		limit:    rate.Limit(float64(per) / every.Seconds()),
		burst:    burst,
		ttl:      10 * time.Minute,
		visitors: map[string]*visitor{},
	}
}

// Allow mengonsumsi satu token untuk key.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.visitors[key]
	if !ok {
		v = &visitor{lim: rate.NewLimiter(l.limit, l.burst)}
		l.visitors[key] = v
	}
	v.seen = time.Now()
	return v.lim.Allow()
}

// Cleanup menghapus kunci yang lama tidak aktif, berjalan sampai ctx selesai.
func (l *RateLimiter) Cleanup(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.mu.Lock()
			for k, v := range l.visitors {
				if time.Since(v.seen) > l.ttl {
					delete(l.visitors, k)
				}
			}
			l.mu.Unlock()
		}
	}
}

// Middleware membatasi request per IP klien. onLimit dipanggil saat melewati batas.
// Bila onlyMutating true, hanya POST/PUT/PATCH/DELETE yang dihitung.
func (l *RateLimiter) Middleware(onlyMutating bool, onLimit http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if onlyMutating && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
				next.ServeHTTP(w, r)
				return
			}
			if !l.Allow(reqctx.InfoFrom(r.Context()).IP) {
				w.Header().Set("Retry-After", "60")
				onLimit.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
