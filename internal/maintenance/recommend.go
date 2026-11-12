// Package maintenance mengelola riwayat pemeliharaan perangkat: entri otomatis
// dari penyelesaian tiket dan kegiatan internal, entri manual, sparepart & biaya,
// statistik perangkat, serta deteksi kerusakan berulang dengan rekomendasi
// berbasis aturan. Keputusan (mis. penggantian) tetap di tangan koordinator.
package maintenance

import (
	"strconv"
)

// Rekomendasi (bukan keputusan).
const (
	RecEvaluate = "EVALUATE"
	RecInspect  = "INSPECT"
	RecReplace  = "CONSIDER_REPLACEMENT"
)

// Config adalah ambang deteksi & rekomendasi (dari pengaturan sistem).
type Config struct {
	Threshold        int
	WindowDays       int
	InspectThreshold int
	ReplaceAgeYears  int
	ReplaceCost      int64
}

// Facts adalah fakta perangkat untuk penilaian.
type Facts struct {
	CountInWindow   int     // gangguan dalam jendela (setelah keputusan terakhir)
	MaxSameCategory int     // gangguan terbanyak dengan jenis yang sama dalam jendela
	AgeYears        float64 // 0 bila tanggal pengadaan tidak diketahui
	TotalCost       float64 // total biaya perbaikan tercatat (Rp)
}

// Recommend menerapkan aturan:
//   - gangguan ≥ ambang → Evaluasi perangkat
//   - gangguan ≥ ambang pemeriksaan ATAU jenis yang sama berulang ≥ ambang → Pemeriksaan lanjutan
//   - syarat pemeriksaan lanjutan DAN (umur ≥ batas ATAU total biaya ≥ batas) → Pertimbangan penggantian
//
// Mengembalikan "" bila belum mencapai ambang. reasons menjelaskan dasar rekomendasi.
func Recommend(c Config, f Facts) (string, []string) {
	if f.CountInWindow < c.Threshold {
		return "", nil
	}
	reasons := []string{strconv.Itoa(f.CountInWindow) + " gangguan dalam " + strconv.Itoa(c.WindowDays) + " hari"}
	inspect := false
	if f.CountInWindow >= c.InspectThreshold {
		inspect = true
	}
	if f.MaxSameCategory >= c.Threshold {
		inspect = true
		reasons = append(reasons, strconv.Itoa(f.MaxSameCategory)+" gangguan dengan jenis yang sama")
	}
	if !inspect {
		return RecEvaluate, reasons
	}
	old := c.ReplaceAgeYears > 0 && f.AgeYears >= float64(c.ReplaceAgeYears)
	costly := c.ReplaceCost > 0 && f.TotalCost >= float64(c.ReplaceCost)
	if old {
		reasons = append(reasons, "umur perangkat "+strconv.FormatFloat(f.AgeYears, 'f', 1, 64)+" tahun")
	}
	if costly {
		reasons = append(reasons, "total biaya perbaikan Rp "+strconv.FormatFloat(f.TotalCost, 'f', 0, 64))
	}
	if old || costly {
		return RecReplace, reasons
	}
	return RecInspect, reasons
}

// RecRank untuk membandingkan tingkat rekomendasi.
func RecRank(r string) int {
	switch r {
	case RecReplace:
		return 3
	case RecInspect:
		return 2
	case RecEvaluate:
		return 1
	}
	return 0
}
