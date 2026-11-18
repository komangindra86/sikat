package reports

import (
	"fmt"
	"math"
)

// Tata letak grafik batang bertumpuk sebagai SVG yang dirender server (tanpa
// JavaScript, sesuai CSP). Warna lewat kelas CSS .chart-s0..2 (palet tervalidasi:
// brand-600, amber-600, violet-500). Hover memakai <title> bawaan SVG; tabel di
// bawah grafik adalah tampilan data lengkapnya.

// ChartLayout adalah koordinat siap render.
type ChartLayout struct {
	Width, Height float64
	PlotTop       float64
	PlotH         float64
	Baseline      float64
	Grid          []GridLine
	Bars          []BarLayout
	Series        []string
	LabelEvery    int
}

// GridLine adalah garis bantu horizontal beserta labelnya.
type GridLine struct {
	Y     float64
	Label string
}

// BarLayout adalah satu batang.
type BarLayout struct {
	X, W, LabelX float64
	Label        string
	ShowLabel    bool
	Total        int
	Segs         []SegLayout
}

// SegLayout adalah satu segmen batang.
type SegLayout struct {
	Y, H  float64
	Class string
	Title string
}

// Layout menghitung koordinat grafik.
func (c *Chart) Layout() ChartLayout {
	const top, bottom, left, slot = 8.0, 22.0, 34.0, 56.0
	n := len(c.Bars)
	l := ChartLayout{Height: 200, PlotTop: top, Series: c.Series}
	l.Width = left + float64(n)*slot + 8
	if l.Width < 480 {
		l.Width = 480
	}
	l.Baseline = l.Height - bottom
	plotH := l.Baseline - top
	l.PlotH = plotH
	max := niceMax(c.Max())
	for i := 0; i <= 2; i++ {
		v := max * i / 2
		l.Grid = append(l.Grid, GridLine{Y: l.Baseline - plotH*float64(v)/float64(max), Label: fmt.Sprint(v)})
	}
	slotW := (l.Width - left - 8) / float64(maxInt(n, 1))
	barW := math.Min(slotW*0.6, 36)
	l.LabelEvery = 1
	if n > 16 {
		l.LabelEvery = int(math.Ceil(float64(n) / 12))
	}
	for i, b := range c.Bars {
		x := left + slotW*float64(i) + (slotW-barW)/2
		bl := BarLayout{X: x, W: barW, LabelX: x + barW/2, Label: b.Label, ShowLabel: i%l.LabelEvery == 0}
		y := l.Baseline
		for si, v := range b.Values {
			bl.Total += v
			if v == 0 {
				continue
			}
			h := plotH * float64(v) / float64(max)
			gap := 0.0
			if y < l.Baseline {
				gap = 2 // celah 2px antarsegmen
			}
			seg := SegLayout{Y: y - h, H: math.Max(h-gap, 1), Class: fmt.Sprintf("chart-s%d", si),
				Title: fmt.Sprintf("%s · %s: %d", b.Label, c.Series[si], v)}
			bl.Segs = append(bl.Segs, seg)
			y -= h
		}
		l.Bars = append(l.Bars, bl)
	}
	return l
}

// niceMax membulatkan ke atas ke angka genap yang mudah dibaca.
func niceMax(v int) int {
	if v <= 4 {
		return 4
	}
	mag := int(math.Pow(10, math.Floor(math.Log10(float64(v)))))
	for _, m := range []int{1, 2, 5, 10} {
		if c := m * mag; c >= v && c%2 == 0 {
			return c
		}
	}
	return 10 * mag
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
