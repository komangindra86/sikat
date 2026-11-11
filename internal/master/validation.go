package master

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Validated adalah hasil validasi: nilai siap simpan (nil = NULL), nilai untuk
// diisi ulang ke form, dan pesan error per kolom.
type Validated struct {
	Values map[string]*string
	Form   map[string]string
	Errors map[string]string
}

// OK bernilai true bila tidak ada error.
func (v Validated) OK() bool { return len(v.Errors) == 0 }

// Validate memeriksa input form terhadap definisi entitas. Validasi keberadaan
// referensi (foreign key) dilakukan di service karena membutuhkan database.
func Validate(e *Entity, get func(string) string) Validated {
	out := Validated{Values: map[string]*string{}, Form: map[string]string{}, Errors: map[string]string{}}
	for _, f := range e.Fields {
		raw := strings.TrimSpace(get(f.Column))
		if f.Upper {
			raw = strings.ToUpper(raw)
		}
		out.Form[f.Column] = raw

		switch f.Kind {
		case KindBool:
			v := "0"
			if raw == "1" || raw == "on" || raw == "true" {
				v = "1"
			}
			out.Form[f.Column] = v
			out.Values[f.Column] = &v
			continue
		}

		if raw == "" {
			if f.Required {
				out.Errors[f.Column] = f.Label + " wajib diisi."
				continue
			}
			if f.Kind == KindEnum && !choiceAllowed(f.Choices, "") {
				out.Errors[f.Column] = "Pilihan tidak valid."
				continue
			}
			out.Values[f.Column] = nil
			continue
		}

		switch f.Kind {
		case KindText, KindTextarea:
			if f.MaxLen > 0 && utf8.RuneCountInString(raw) > f.MaxLen {
				out.Errors[f.Column] = fmt.Sprintf("%s maksimal %d karakter.", f.Label, f.MaxLen)
				continue
			}
			if f.Kind == KindText && strings.ContainsAny(raw, "\r\n") {
				out.Errors[f.Column] = f.Label + " tidak boleh berisi baris baru."
				continue
			}
		case KindInt:
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				out.Errors[f.Column] = f.Label + " harus berupa angka bulat."
				continue
			}
			if n < f.Min || (f.Max != 0 && n > f.Max) {
				out.Errors[f.Column] = fmt.Sprintf("%s harus antara %d dan %d.", f.Label, f.Min, f.Max)
				continue
			}
			raw = strconv.FormatInt(n, 10)
		case KindSelect:
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n <= 0 {
				out.Errors[f.Column] = "Pilihan " + strings.ToLower(f.Label) + " tidak valid."
				continue
			}
			raw = strconv.FormatInt(n, 10)
		case KindEnum:
			if !choiceAllowed(f.Choices, raw) {
				out.Errors[f.Column] = "Pilihan tidak valid."
				continue
			}
		case KindMoney:
			m, err := ParseMoney(raw)
			if err != nil {
				out.Errors[f.Column] = f.Label + ": " + err.Error()
				continue
			}
			raw = m
		}
		v := raw
		out.Values[f.Column] = &v
	}
	return out
}

func choiceAllowed(cs []Choice, v string) bool {
	for _, c := range cs {
		if c.Value == v {
			return true
		}
	}
	return false
}

// maxMoney sesuai DECIMAL(14,2).
var maxMoney = new(big.Rat).SetInt64(999_999_999_999)

// ParseMoney menerima format "175000", "175.000", "175.000,50", "Rp 175.000"
// dan mengembalikan string desimal "175000.50" untuk DECIMAL(14,2).
func ParseMoney(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "Rp"), "rp")
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if strings.HasPrefix(s, "-") {
		return "", fmt.Errorf("tidak boleh negatif")
	}
	// Format Indonesia: titik = pemisah ribuan, koma = desimal.
	s = strings.ReplaceAll(s, ".", "")
	s = strings.Replace(s, ",", ".", 1)
	if s == "" || strings.ContainsAny(s, ",") {
		return "", fmt.Errorf("format angka tidak valid")
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return "", fmt.Errorf("format angka tidak valid")
	}
	if r.Cmp(maxMoney) > 0 {
		return "", fmt.Errorf("nilai terlalu besar")
	}
	if _, frac, has := strings.Cut(s, "."); has && len(frac) > 2 {
		return "", fmt.Errorf("maksimal 2 angka di belakang koma")
	}
	return r.FloatString(2), nil
}
