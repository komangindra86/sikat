package web

import (
	"database/sql"
	"errors"
	"html/template"
	"net/url"
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
		"queryWith": queryWith,
		"nullStr":   func(s sql.NullString) string { return s.String },
		"since":     since,
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
