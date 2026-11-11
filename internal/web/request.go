package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Pager menyimpan informasi paginasi.
type Pager struct {
	Page    int
	PerPage int
	Total   int
	Query   url.Values
}

// NewPager membaca ?page= dari request.
func NewPager(r *http.Request, perPage int) Pager {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return Pager{Page: page, PerPage: perPage, Query: r.URL.Query()}
}

// Offset untuk query SQL.
func (p Pager) Offset() int { return (p.Page - 1) * p.PerPage }

// Pages adalah jumlah halaman.
func (p Pager) Pages() int {
	if p.Total == 0 {
		return 1
	}
	return (p.Total + p.PerPage - 1) / p.PerPage
}

// HasPrev / HasNext untuk tombol navigasi.
func (p Pager) HasPrev() bool { return p.Page > 1 }

// HasNext bernilai true bila masih ada halaman berikutnya.
func (p Pager) HasNext() bool { return p.Page < p.Pages() }

// From/To untuk teks "Menampilkan x–y dari z".
func (p Pager) From() int {
	if p.Total == 0 {
		return 0
	}
	return p.Offset() + 1
}

// To adalah nomor baris terakhir di halaman ini.
func (p Pager) To() int {
	to := p.Offset() + p.PerPage
	if to > p.Total {
		return p.Total
	}
	return to
}

// IDParam membaca parameter rute {name} sebagai ID positif.
func IDParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id, err == nil && id > 0
}

// FormValues mengubah form menjadi map nilai string ter-trim (untuk diisi ulang ke form).
func FormValues(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, v := range r.PostForm {
		if len(v) > 0 {
			out[k] = strings.TrimSpace(v[0])
		}
	}
	return out
}
