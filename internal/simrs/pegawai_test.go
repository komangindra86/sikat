package simrs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseNames(t *testing.T) {
	cases := map[string]string{
		"response": `{"response":[{"id":2,"namalengkap":"  Wayan  Sari "},{"id":1,"namalengkap":"ani lestari","noidentitas":"5101"}]}`,
		"data":     `{"data":[{"id":"2","label":"Wayan Sari"},{"id":"1","nama":"ani lestari"}]}`,
		"list":     `[{"id":1,"namalengkap":"Wayan Sari"},{"id":3,"namalengkap":"WAYAN SARI"},{"id":4},{"id":5,"namalengkap":"ani lestari"}]`,
	}
	for name, body := range cases {
		got, err := ParseNames([]byte(body))
		if err != nil || !reflect.DeepEqual(got, []string{"ani lestari", "Wayan Sari"}) {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
	for _, body := range []string{`{"response":[]}`, `{"message":"token salah"}`, `<html>`} {
		if _, err := ParseNames([]byte(body)); err == nil {
			t.Errorf("respons %s harus ditolak", body)
		}
	}
}

func TestPegawaiCacheAndFallback(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("token") != "rahasia" || r.Header.Get("Cookie") != "sesi=1" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"response":[{"id":1,"namalengkap":"Ani"}]}`))
	}))
	defer srv.Close()

	if NewPegawai(PegawaiConfig{URL: srv.URL}, nil) != nil {
		t.Fatal("tanpa token fitur nonaktif")
	}
	var nilP *Pegawai
	if nilP.Names(context.Background()) != nil {
		t.Fatal("pegawai nonaktif mengembalikan daftar kosong")
	}

	now := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	p := NewPegawai(PegawaiConfig{URL: srv.URL, Token: "token: rahasia", Cookie: "Cookie: sesi=1"}, nil)
	p.now = func() time.Time { return now }
	ctx := context.Background()

	if got := p.Names(ctx); !reflect.DeepEqual(got, []string{"Ani"}) {
		t.Fatalf("muat pertama: %v", got)
	}
	p.Names(ctx)
	if calls.Load() != 1 {
		t.Fatalf("daftar segar diambil dari cache, panggilan %d", calls.Load())
	}

	// Kedaluwarsa + SIMRS gangguan: daftar lama tetap dipakai, lalu jeda sebelum mencoba lagi.
	fail.Store(true)
	now = now.Add(CacheTTL + time.Second)
	if got := p.Names(ctx); !reflect.DeepEqual(got, []string{"Ani"}) {
		t.Fatalf("cadangan saat gangguan: %v", got)
	}
	waitIdle(t, p)
	if calls.Load() != 2 {
		t.Fatalf("pembaruan latar belakang dijalankan sekali, panggilan %d", calls.Load())
	}
	p.Names(ctx)
	waitIdle(t, p)
	if calls.Load() != 2 {
		t.Fatal("setelah gagal, tunggu RetryAfter sebelum mencoba lagi")
	}

	// Belum pernah berhasil + gagal: kosong, bukan error.
	q := NewPegawai(PegawaiConfig{URL: srv.URL, Token: "salah"}, nil)
	if got := q.Names(ctx); got != nil {
		t.Fatalf("gagal tanpa cadangan harus kosong: %v", got)
	}
}

func waitIdle(t *testing.T, p *Pegawai) {
	t.Helper()
	for i := 0; i < 200; i++ {
		p.mu.Lock()
		busy := p.refreshing
		p.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pembaruan latar belakang tidak selesai")
}
