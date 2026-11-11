package assets

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sikat/internal/clock"
)

var validStatus = map[string]bool{
	StatusActive: true, StatusBroken: true, StatusInRepair: true, StatusUnused: true, StatusDisposed: true,
}

// Validate memeriksa input form. Keberadaan referensi diperiksa di service.
func Validate(get func(string) string, today time.Time) (Input, map[string]string, map[string]string) {
	errs := map[string]string{}
	form := map[string]string{}
	field := func(name string, maxLen int, upper bool, label string, required bool) string {
		v := strings.TrimSpace(get(name))
		if upper {
			v = strings.ToUpper(v)
		}
		form[name] = v
		if v == "" && required {
			errs[name] = label + " wajib diisi."
		} else if utf8.RuneCountInString(v) > maxLen {
			errs[name] = label + " terlalu panjang."
		} else if strings.ContainsAny(v, "\r\n") && name != "notes" {
			errs[name] = label + " tidak boleh berisi baris baru."
		}
		return v
	}
	id := func(name, label string, required bool) int64 {
		v := strings.TrimSpace(get(name))
		form[name] = v
		if v == "" {
			if required {
				errs[name] = label + " wajib dipilih."
			}
			return 0
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs[name] = "Pilihan " + strings.ToLower(label) + " tidak valid."
			return 0
		}
		return n
	}

	in := Input{
		AssetCode:    field("asset_code", 50, true, "Kode inventaris", true),
		Name:         field("name", 150, false, "Nama perangkat", true),
		CategoryID:   id("asset_category_id", "Kategori", true),
		Brand:        field("brand", 80, false, "Merk", false),
		Model:        field("model", 80, false, "Model", false),
		SerialNumber: field("serial_number", 100, false, "Serial number", false),
		DepartmentID: id("department_id", "Unit", false),
		RoomID:       id("room_id", "Ruangan", false),
		Notes:        field("notes", 2000, false, "Keterangan", false),
	}

	in.Status = strings.TrimSpace(get("status"))
	form["status"] = in.Status
	if in.Status == "" {
		in.Status = StatusActive
		form["status"] = StatusActive
	}
	if !validStatus[in.Status] {
		errs["status"] = "Status tidak valid."
	}

	if p := strings.TrimSpace(get("procured_at")); p != "" {
		form["procured_at"] = p
		d, err := time.ParseInLocation("2006-01-02", p, clock.Location)
		switch {
		case err != nil:
			errs["procured_at"] = "Format tanggal tidak valid."
		case d.After(today):
			errs["procured_at"] = "Tanggal pengadaan tidak boleh di masa depan."
		case d.Year() < 1980:
			errs["procured_at"] = "Tanggal pengadaan tidak masuk akal."
		default:
			in.ProcuredAt = &d
		}
	}
	return in, form, errs
}
