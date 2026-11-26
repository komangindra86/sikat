package tickets

import (
	"reflect"
	"testing"
)

func TestMatchNames(t *testing.T) {
	names := []string{"A. A. Ayu Dewi", "Dewi Lestari", "I Made Ardewi", "Kadek Dewi Sari", "Ni Luh Sari", "Putu Ani"}
	cases := []struct {
		q     string
		limit int
		want  []string
	}{
		// Diawali kata ketikan lebih dulu, lalu yang hanya memuatnya; urutan abjad dipertahankan.
		{"dewi", 8, []string{"A. A. Ayu Dewi", "Dewi Lestari", "Kadek Dewi Sari", "I Made Ardewi"}},
		{"DEWI sari", 8, []string{"Kadek Dewi Sari"}},
		{"dewi", 2, []string{"A. A. Ayu Dewi", "Dewi Lestari"}},
		{"xyz", 8, nil},
		{"  ", 8, nil},
	}
	for _, c := range cases {
		if got := MatchNames(names, c.q, c.limit); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q (maks %d) = %v, harus %v", c.q, c.limit, got, c.want)
		}
	}
}
