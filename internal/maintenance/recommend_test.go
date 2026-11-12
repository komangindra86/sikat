package maintenance

import "testing"

func TestRecommend(t *testing.T) {
	c := Config{Threshold: 3, WindowDays: 90, InspectThreshold: 5, ReplaceAgeYears: 5, ReplaceCost: 3_000_000}
	cases := []struct {
		name string
		f    Facts
		want string
	}{
		{"di bawah ambang", Facts{CountInWindow: 2, MaxSameCategory: 2}, ""},
		{"tepat ambang → evaluasi", Facts{CountInWindow: 3, MaxSameCategory: 1}, RecEvaluate},
		{"jenis sama berulang → pemeriksaan", Facts{CountInWindow: 3, MaxSameCategory: 3}, RecInspect},
		{"banyak gangguan → pemeriksaan", Facts{CountInWindow: 5, MaxSameCategory: 2}, RecInspect},
		{"banyak + tua → pertimbangan ganti", Facts{CountInWindow: 5, AgeYears: 6}, RecReplace},
		{"banyak + mahal → pertimbangan ganti", Facts{CountInWindow: 6, TotalCost: 3_500_000}, RecReplace},
		{"tua tapi baru 3 gangguan beda jenis → evaluasi saja", Facts{CountInWindow: 3, MaxSameCategory: 1, AgeYears: 9}, RecEvaluate},
		{"umur tidak diketahui tidak dianggap tua", Facts{CountInWindow: 5, AgeYears: 0}, RecInspect},
	}
	for _, tc := range cases {
		got, reasons := Recommend(c, tc.f)
		if got != tc.want {
			t.Errorf("%s: dapat %q, ingin %q", tc.name, got, tc.want)
		}
		if got != "" && len(reasons) == 0 {
			t.Errorf("%s: rekomendasi harus punya alasan", tc.name)
		}
	}
	if !(RecRank(RecReplace) > RecRank(RecInspect) && RecRank(RecInspect) > RecRank(RecEvaluate) && RecRank("") == 0) {
		t.Fatal("urutan rekomendasi salah")
	}
}
