package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"

	"github.com/alexedwards/scs/v2"
)

const (
	csrfSessionKey = "csrf_token"
	// CSRFFormField adalah nama field form yang membawa token.
	CSRFFormField = "csrf_token"
	// CSRFHeader adalah header yang dikirim HTMX.
	CSRFHeader = "X-CSRF-Token"
)

// CSRF memiliki dua lapis perlindungan:
//  1. http.CrossOriginProtection (stdlib) menolak request lintas-origin
//     berdasarkan header Sec-Fetch-Site / Origin.
//  2. Token sinkron per-session yang wajib ada pada setiap request yang
//     mengubah data (POST/PUT/PATCH/DELETE).
type CSRF struct {
	sessions *scs.SessionManager
	cop      *http.CrossOriginProtection
	onFail   http.Handler
}

// NewCSRF membuat middleware CSRF. onFail dipanggil saat request ditolak.
func NewCSRF(sessions *scs.SessionManager, onFail http.Handler) *CSRF {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(onFail)
	return &CSRF{sessions: sessions, cop: cop, onFail: onFail}
}

// Token mengembalikan token CSRF session ini, membuatnya bila belum ada.
func (c *CSRF) Token(r *http.Request) string {
	tok := c.sessions.GetString(r.Context(), csrfSessionKey)
	if tok == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		tok = base64.RawURLEncoding.EncodeToString(b)
		c.sessions.Put(r.Context(), csrfSessionKey, tok)
	}
	return tok
}

// Middleware memverifikasi request yang mengubah data.
func (c *CSRF) Middleware(next http.Handler) http.Handler {
	verify := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		want := c.sessions.GetString(r.Context(), csrfSessionKey)
		got := r.Header.Get(CSRFHeader)
		if got == "" {
			got = r.PostFormValue(CSRFFormField)
		}
		if want == "" || got == "" || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
			c.onFail.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
	return c.cop.Handler(verify)
}
