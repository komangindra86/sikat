// Package middleware berisi middleware HTTP: request info, log, recover,
// security headers, CSRF, rate limit, autentikasi, dan RBAC.
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"sikat/internal/reqctx"
)

// RequestInfo menyimpan request ID, IP klien, dan user agent ke context.
// Header X-Forwarded-For / X-Real-IP hanya dipercaya bila trustProxy aktif
// DAN koneksi berasal dari loopback (reverse proxy di host yang sama).
func RequestInfo(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := newRequestID()
			w.Header().Set("X-Request-ID", id)
			info := reqctx.Info{
				RequestID: id,
				IP:        ClientIP(r, trustProxy),
				UserAgent: r.UserAgent(),
			}
			next.ServeHTTP(w, r.WithContext(reqctx.WithInfo(r.Context(), info)))
		})
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ClientIP menentukan alamat IP klien.
func ClientIP(r *http.Request, trustProxy bool) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !trustProxy {
		return host
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return host
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" && net.ParseIP(xr) != nil {
		return xr
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Ambil entri paling kanan yang bukan loopback: entri yang ditambahkan proxy kita.
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			p := strings.TrimSpace(parts[i])
			if ip := net.ParseIP(p); ip != nil && !ip.IsLoopback() {
				return p
			}
		}
	}
	return host
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logger mencatat setiap request (tanpa query string agar token tidak tercatat).
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if strings.HasPrefix(r.URL.Path, "/static/") && rec.status < 400 {
				return
			}
			info := reqctx.InfoFrom(r.Context())
			attrs := []any{
				"method", r.Method, "path", RedactPath(r.URL.Path), "status", rec.status,
				"dur_ms", time.Since(start).Milliseconds(), "ip", info.IP, "req_id", info.RequestID,
			}
			if u := reqctx.UserFrom(r.Context()); u != nil {
				attrs = append(attrs, "user_id", u.ID)
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "http", attrs...)
		})
	}
}

// RedactPath menyamarkan token akses publik (/t/{token}, /q/{token}) agar tidak
// tersimpan di log; siapa pun yang membaca log tidak boleh bisa membuka tiket.
func RedactPath(p string) string {
	for _, prefix := range []string{"/t/", "/q/"} {
		if strings.HasPrefix(p, prefix) {
			rest := strings.TrimPrefix(p, prefix)
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				return prefix + "[token]" + rest[i:]
			}
			return prefix + "[token]"
		}
	}
	return p
}

// BodyLimit membatasi ukuran body request: maxMultipart untuk unggahan file,
// maxOther untuk form biasa. Melebihi batas → error saat body dibaca.
func BodyLimit(maxMultipart, maxOther int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				limit := maxOther
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
					limit = maxMultipart
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recoverer menangkap panic, mencatatnya, dan memanggil onPanic untuk merender halaman error.
func Recoverer(log *slog.Logger, onPanic func(w http.ResponseWriter, r *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler {
						panic(p)
					}
					log.Error("panic", "err", p, "path", r.URL.Path,
						"req_id", reqctx.InfoFrom(r.Context()).RequestID, "stack", string(debug.Stack()))
					onPanic(w, r)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders menambahkan header keamanan standar. CSP tidak mengizinkan
// script/style inline; seluruh JS dan CSS berasal dari /static.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; " +
		"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NoStore mencegah halaman berisi data internal disimpan cache browser/proxy.
func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
