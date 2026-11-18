// Package reports menghasilkan laporan & analitik dari tabel transaksi (tiket,
// kegiatan internal, riwayat pemeliharaan, eskalasi SLA) lalu menyajikannya
// sebagai HTML, Excel, atau PDF. Tidak ada data laporan yang diketik ulang.
//
// Setiap laporan menghasilkan Table generik, sehingga satu template HTML dan
// satu penulis Excel/PDF melayani semua laporan.
package reports

import (
	"database/sql"
	"math"
	"strconv"
	"strings"
	"time"

	"sikat/internal/clock"
)

// Jenis kolom menentukan perataan dan format di HTML/Excel/PDF.
const (
	KindText     = "text"
	KindInt      = "int"
	KindDuration = "duration" // nilai = menit
	KindPct      = "pct"      // nilai = persen 0..100
	KindMoney    = "money"
	KindDate     = "date"
)

// Column adalah satu kolom tabel.
type Column struct {
	Label string
	Kind  string
	// Width relatif untuk PDF/Excel (0 = default sesuai jenis).
	Width float64
}

// Numeric bernilai true untuk kolom angka (rata kanan).
func (c Column) Numeric() bool {
	return c.Kind == KindInt || c.Kind == KindDuration || c.Kind == KindPct || c.Kind == KindMoney
}

// Cell adalah satu sel. Text selalu terisi (tampilan); Num diisi untuk angka.
type Cell struct {
	Text  string
	Num   float64
	Valid bool // false = kosong ("–")
	Link  string
}

// Metric adalah angka ringkasan di atas tabel.
type Metric struct {
	Label, Value, Tone string
}

// Bar adalah satu batang grafik (rekap bulanan).
type Bar struct {
	Label  string
	Values []int // sesuai urutan Chart.Series
}

// Chart adalah grafik batang bertumpuk sederhana (dirender sebagai SVG di server).
type Chart struct {
	Series []string
	Bars   []Bar
}

// Table adalah hasil satu laporan.
type Table struct {
	Title    string
	Subtitle string // periode & filter, untuk judul ekspor
	Columns  []Column
	Rows     [][]Cell
	Totals   []Cell // opsional
	Metrics  []Metric
	Chart    *Chart
	Notes    []string
	// Truncated: baris dipotong pada batas tampilan HTML.
	Truncated bool
}

func text(s string) Cell {
	if strings.TrimSpace(s) == "" {
		return Cell{Text: "–"}
	}
	return Cell{Text: s, Valid: true}
}

func nullText(s sql.NullString) Cell {
	if !s.Valid {
		return Cell{Text: "–"}
	}
	return text(s.String)
}

func intCell(n int) Cell {
	return Cell{Text: formatInt(int64(n)), Num: float64(n), Valid: true}
}

func pctCell(part, total int) Cell {
	if total == 0 {
		return Cell{Text: "–"}
	}
	p := float64(part) * 100 / float64(total)
	return Cell{Text: strconv.FormatFloat(math.Round(p*10)/10, 'f', 1, 64) + "%", Num: math.Round(p*10) / 10, Valid: true}
}

func durCell(minutes sql.NullFloat64) Cell {
	if !minutes.Valid {
		return Cell{Text: "–"}
	}
	m := math.Round(minutes.Float64)
	return Cell{Text: Duration(int64(m)), Num: m, Valid: true}
}

func moneyCell(v sql.NullFloat64) Cell {
	if !v.Valid {
		return Cell{Text: "–"}
	}
	return Cell{Text: "Rp " + formatInt(int64(math.Round(v.Float64))), Num: v.Float64, Valid: true}
}

func dateCell(t time.Time) Cell {
	if t.IsZero() {
		return Cell{Text: "–"}
	}
	return Cell{Text: t.In(clock.Location).Format("02/01/2006"), Valid: true}
}

func timeCell(t sql.NullTime) Cell {
	if !t.Valid {
		return Cell{Text: "–"}
	}
	return Cell{Text: t.Time.In(clock.Location).Format("02/01/2006 15:04"), Valid: true}
}

// Duration menampilkan menit sebagai "2 hari 3 j", "4 j 15 mnt", atau "35 mnt".
func Duration(m int64) string {
	if m < 0 {
		m = 0
	}
	d, h, mm := m/(24*60), (m/60)%24, m%60
	switch {
	case d > 0 && h > 0:
		return strconv.FormatInt(d, 10) + " hari " + strconv.FormatInt(h, 10) + " j"
	case d > 0:
		return strconv.FormatInt(d, 10) + " hari"
	case h > 0 && mm > 0:
		return strconv.FormatInt(h, 10) + " j " + strconv.FormatInt(mm, 10) + " mnt"
	case h > 0:
		return strconv.FormatInt(h, 10) + " j"
	default:
		return strconv.FormatInt(mm, 10) + " mnt"
	}
}

// formatInt: 1234567 → "1.234.567".
func formatInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// ChartMax mengembalikan total batang tertinggi (skala grafik).
func (c *Chart) Max() int {
	m := 0
	for _, b := range c.Bars {
		t := 0
		for _, v := range b.Values {
			t += v
		}
		if t > m {
			m = t
		}
	}
	return m
}
