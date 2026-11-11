package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if ip := ClientIP(r, true); ip != "203.0.113.9" {
		t.Fatalf("header dari non-loopback tidak boleh dipercaya, dapat %s", ip)
	}

	r.RemoteAddr = "127.0.0.1:5555"
	if ip := ClientIP(r, false); ip != "127.0.0.1" {
		t.Fatalf("trustProxy=false harus pakai RemoteAddr, dapat %s", ip)
	}
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.7")
	if ip := ClientIP(r, true); ip != "198.51.100.7" {
		t.Fatalf("harus ambil entri paling kanan dari proxy, dapat %s", ip)
	}
	r.Header.Set("X-Real-IP", "198.51.100.8")
	if ip := ClientIP(r, true); ip != "198.51.100.8" {
		t.Fatalf("X-Real-IP dari proxy lokal harus dipakai, dapat %s", ip)
	}
}

func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter(3, time.Minute, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("1.1.1.1") {
			t.Fatalf("request ke-%d harus diizinkan", i+1)
		}
	}
	if l.Allow("1.1.1.1") {
		t.Fatal("request ke-4 harus ditolak")
	}
	if !l.Allow("2.2.2.2") {
		t.Fatal("IP lain tidak boleh terpengaruh")
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	l := NewRateLimiter(1, time.Minute, 1)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) })
	h := RequestInfo(false)(l.Middleware(true, limited)(ok))

	do := func(method string) int {
		r := httptest.NewRequest(method, "/login", nil)
		r.RemoteAddr = "10.0.0.1:1"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if do("GET") != 200 || do("GET") != 200 {
		t.Fatal("GET tidak boleh dibatasi bila onlyMutating")
	}
	if do("POST") != 200 {
		t.Fatal("POST pertama harus lolos")
	}
	if do("POST") != 429 {
		t.Fatal("POST kedua harus dibatasi")
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	for _, k := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Strict-Transport-Security", "Referrer-Policy"} {
		if w.Header().Get(k) == "" {
			t.Errorf("header %s tidak ada", k)
		}
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp == "" || contains(csp, "unsafe-inline") {
		t.Errorf("CSP tidak boleh mengizinkan inline: %s", csp)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
