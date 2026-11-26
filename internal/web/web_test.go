package web

import (
	"database/sql"
	"net/url"
	"testing"
	"time"
)

func TestFormatMoney(t *testing.T) {
	cases := map[string]string{
		"175000.00": "Rp 175.000",
		"1250.50":   "Rp 1.250,5",
		"999":       "Rp 999",
		"1000":      "Rp 1.000",
		"":          "–",
	}
	for in, want := range cases {
		if got := FormatMoney(in); got != want {
			t.Errorf("FormatMoney(%q) = %q, ingin %q", in, got, want)
		}
	}
}

func TestFormatQty(t *testing.T) {
	cases := map[string]string{"12.00": "12", "2.50": "2,5", "0.05": "0,05", "0.00": "0", "100": "100", "": "–"}
	for in, want := range cases {
		if got := FormatQty(in); got != want {
			t.Errorf("FormatQty(%q) = %q, ingin %q", in, got, want)
		}
	}
}

func TestFormatWITA(t *testing.T) {
	utc := time.Date(2026, 11, 11, 16, 30, 0, 0, time.UTC)
	if got := formatWITA(utc); got != "12/11/2026 00:30" {
		t.Fatalf("UTC 16:30 harus tampil 00:30 WITA esok hari, dapat %s", got)
	}
	if formatWITA(sql.NullTime{}) != "–" || formatWITA(time.Time{}) != "–" {
		t.Fatal("waktu kosong harus tampil –")
	}
}

func TestPager(t *testing.T) {
	p := Pager{Page: 2, PerPage: 25, Total: 51, Query: url.Values{}}
	if p.Pages() != 3 || p.From() != 26 || p.To() != 50 || !p.HasPrev() || !p.HasNext() {
		t.Fatalf("pager salah: pages=%d from=%d to=%d", p.Pages(), p.From(), p.To())
	}
	if got := queryWith(url.Values{"q": {"a b"}, "page": {"2"}}, "page", 3); got != "?page=3&q=a+b" {
		t.Fatalf("queryWith = %s", got)
	}
}

func TestLabels(t *testing.T) {
	if Label("asset_status", "IN_REPAIR") != "Dalam perbaikan" || Tone("priority", "CRITICAL") != "alert" {
		t.Fatal("label/tone tidak sesuai")
	}
	if Label("asset_status", "X") != "X" || !ValidLabel("priority", "LOW") || ValidLabel("priority", "X") {
		t.Fatal("fallback label salah")
	}
}
