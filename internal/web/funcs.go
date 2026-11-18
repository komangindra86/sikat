package web

import (
	"database/sql"
	"errors"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sikat/internal/clock"
)

func (r *Renderer) funcs() template.FuncMap {
	return template.FuncMap{
		"static":    r.staticURL,
		"wita":      formatWITA,
		"witaDate":  formatWITADate,
		"label":     Label,
		"labels":    Labels,
		"tone":      Tone,
		"money":     FormatMoney,
		"dict":      dict,
		"add":       func(a, b int) int { return a + b },
		"join":      strings.Join,
		"queryWith": queryWith,
		"nullStr":   func(s sql.NullString) string { return s.String },
		"since":     since,
		"minutes":   minutesText,
		"dur":       durText,
		// monthName: "November 2026" (zona waktu nilai dipertahankan).
		"monthName": func(t time.Time) string { return bulanID[t.Month()-1] + " " + strconv.Itoa(t.Year()) },
		// dateID: tanggal kalender (DATE) seperti "Jumat, 25 Desember 2026".
		"dateID": func(t time.Time) string {
			return hariID[t.Weekday()] + ", " + strconv.Itoa(t.Day()) + " " + bulanID[t.Month()-1] + " " + strconv.Itoa(t.Year())
		},
		"cycleSuffix": func(c int) string {
			if c > 1 {
				return " (siklus " + strconv.Itoa(c) + ")"
			}
			return ""
		},
		"today": todayWITA,
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i
			}
			return out
		},
		"truncate": func(n int, s string) string {
			r := []rune(s)
			if len(r) <= n {
				return s
			}
			return string(r[:n]) + "…"
		},
		"deref": func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		},
	}
}

func (r *Renderer) staticURL(p string) string {
	v := r.staticHashes[p]
	// Mode development: versi diambil dari waktu ubah file di disk, sehingga
	// CSS/JS hasil build ulang langsung dimuat browser tanpa restart server.
	if r.reloadFromOS {
		if st, err := os.Stat(filepath.Join(r.reloadDir, "static", filepath.FromSlash(p))); err == nil {
			v = strconv.FormatInt(st.ModTime().UnixNano(), 36)
		}
	}
	if v == "" {
		return "/static/" + p
	}
	return "/static/" + p + "?v=" + v
}

// formatWITA menerima time.Time, *time.Time, atau sql.NullTime dan menampilkan
// dalam WITA. Nilai kosong menjadi "–".
func formatWITA(v any) string {
	t, ok := asTime(v)
	if !ok {
		return "–"
	}
	return t.In(clock.Location).Format("02/01/2006 15:04")
}

func formatWITADate(v any) string {
	t, ok := asTime(v)
	if !ok {
		return "–"
	}
	return t.In(clock.Location).Format("02/01/2006")
}

func asTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, !t.IsZero()
	case *time.Time:
		if t == nil {
			return time.Time{}, false
		}
		return *t, !t.IsZero()
	case sql.NullTime:
		return t.Time, t.Valid
	}
	return time.Time{}, false
}

// since menampilkan lama waktu sejak t dalam bentuk singkat ("5 mnt", "2 j 10 mnt", "3 hari").
func since(v any) string {
	t, ok := asTime(v)
	if !ok {
		return "–"
	}
	return humanDuration(time.Since(t))
}

// durText menampilkan time.Duration (nilai negatif ditampilkan sebagai besarnya).
func durText(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		return "< 1 mnt"
	}
	mins := int(d.Minutes())
	days, h, m := mins/(24*60), mins%(24*60)/60, mins%60
	out := ""
	if days > 0 {
		out = strconv.Itoa(days) + " hari "
	}
	if h > 0 {
		out += strconv.Itoa(h) + " j "
	}
	if m > 0 && days == 0 {
		out += strconv.Itoa(m) + " mnt"
	}
	return strings.TrimSpace(out)
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "< 1 mnt"
	}
	mins := int(d.Minutes())
	switch {
	case mins < 60:
		return strconv.Itoa(mins) + " mnt"
	case mins < 24*60:
		h, m := mins/60, mins%60
		if m == 0 {
			return strconv.Itoa(h) + " j"
		}
		return strconv.Itoa(h) + " j " + strconv.Itoa(m) + " mnt"
	default:
		return strconv.Itoa(mins/(24*60)) + " hari"
	}
}

var hariID = [...]string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
var bulanID = [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

// todayWITA menampilkan tanggal hari ini, mis. "Kamis, 12 November 2026".
func todayWITA() string {
	t := time.Now().In(clock.Location)
	return hariID[t.Weekday()] + ", " + strconv.Itoa(t.Day()) + " " + bulanID[t.Month()-1] + " " + strconv.Itoa(t.Year())
}

// minutesText menampilkan menit (int atau sql.NullInt64) sebagai teks durasi.
func minutesText(v any) string {
	var m int64
	switch x := v.(type) {
	case int:
		m = int64(x)
	case int64:
		m = x
	case sql.NullInt64:
		if !x.Valid {
			return "–"
		}
		m = x.Int64
	default:
		return "–"
	}
	if m < 0 {
		return "–"
	}
	if m == 0 {
		return "0 mnt"
	}
	if m >= 24*60 {
		d, h := m/(24*60), (m%(24*60))/60
		if h == 0 {
			return strconv.FormatInt(d, 10) + " hari"
		}
		return strconv.FormatInt(d, 10) + " hari " + strconv.FormatInt(h, 10) + " j"
	}
	return humanDuration(time.Duration(m) * time.Minute)
}

// FormatMoney memformat angka desimal ("175000.00") menjadi "Rp 175.000".
func FormatMoney(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "–"
	}
	intPart, frac, _ := strings.Cut(v, ".")
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := "Rp " + b.String()
	if frac = strings.TrimRight(frac, "0"); frac != "" {
		out += "," + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, errors.New("dict membutuhkan pasangan kunci/nilai")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, errors.New("kunci dict harus string")
		}
		m[k] = kv[i+1]
	}
	return m, nil
}

// queryWith menyalin query string dan mengganti satu parameter (untuk paginasi/filter).
func queryWith(q url.Values, key string, val any) string {
	c := url.Values{}
	for k, v := range q {
		c[k] = append([]string(nil), v...)
	}
	switch x := val.(type) {
	case int:
		c.Set(key, strconv.Itoa(x))
	case string:
		c.Set(key, x)
	}
	return "?" + c.Encode()
}
