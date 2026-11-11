package tickets

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sikat/internal/priority"
)

var phoneRe = regexp.MustCompile(`^[0-9+ ()-]{3,30}$`)

// IncidentOptions adalah pilihan "Sejak kapan?" (perkiraan, bukan input jam bebas).
var IncidentOptions = []struct{ Value, Label string }{
	{"", "Tidak tahu"},
	{"now", "Baru saja"},
	{"1h", "Sekitar 1 jam lalu"},
	{"3h", "Beberapa jam lalu"},
	{"1d", "Sejak kemarin"},
}

func incidentTime(code string, now time.Time) (*time.Time, bool) {
	var d time.Duration
	switch code {
	case "":
		return nil, true
	case "now":
		d = 0
	case "1h":
		d = time.Hour
	case "3h":
		d = 3 * time.Hour
	case "1d":
		d = 24 * time.Hour
	default:
		return nil, false
	}
	t := now.Add(-d)
	return &t, true
}

func parseID(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

// ParseCommon membaca isian yang sama untuk pengajuan publik dan oleh petugas.
// Lokasi (unit/ruangan/perangkat) diisi pemanggil.
func ParseCommon(get func(string) string, now time.Time) (Draft, map[string]string, map[string]string) {
	errs := map[string]string{}
	form := map[string]string{}
	val := func(k string) string {
		v := strings.TrimSpace(get(k))
		form[k] = v
		return v
	}
	var d Draft

	d.ReporterName = val("reporter_name")
	switch {
	case d.ReporterName == "":
		errs["reporter_name"] = "Nama pelapor wajib diisi."
	case utf8.RuneCountInString(d.ReporterName) > 120:
		errs["reporter_name"] = "Nama maksimal 120 karakter."
	}

	d.ReporterPhone = val("reporter_phone")
	if d.ReporterPhone != "" && !phoneRe.MatchString(d.ReporterPhone) {
		errs["reporter_phone"] = "Nomor HP/ekstensi hanya boleh berisi angka, spasi, +, -, ( )."
	}

	if id, ok := parseID(val("reporter_department_id")); ok {
		d.ReporterDepartmentID = id
	} else {
		errs["reporter_department_id"] = "Pilihan unit tidak valid."
	}

	if id, ok := parseID(val("ticket_category_id")); !ok || id == 0 {
		errs["ticket_category_id"] = "Pilih jenis masalah."
	} else {
		d.TicketCategoryID = id
	}

	d.Description = val("description")
	switch n := utf8.RuneCountInString(d.Description); {
	case n == 0:
		errs["description"] = "Ceritakan kendalanya secara singkat."
	case n < 5:
		errs["description"] = "Keterangan terlalu singkat."
	case n > 2000:
		errs["description"] = "Keterangan maksimal 2000 karakter."
	}

	if v := val("impact_level"); v == "" {
		errs["impact_level"] = "Pilih dampak terhadap pelayanan."
	} else if n, err := strconv.Atoi(v); err != nil || n < 0 || n > 2 {
		errs["impact_level"] = "Pilihan tidak valid."
	} else {
		d.Impact = n
	}

	if v := val("affected_scope"); v == "" {
		errs["affected_scope"] = "Pilih siapa saja yang terdampak."
	} else if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 4 {
		errs["affected_scope"] = "Pilihan tidak valid."
	} else {
		d.Scope = n
	}

	d.Replacement = val("replacement_available")
	switch d.Replacement {
	case "":
		d.Replacement = priority.ReplacementUnknown
		form["replacement_available"] = d.Replacement
	case priority.ReplacementYes, priority.ReplacementNo, priority.ReplacementUnknown:
	default:
		errs["replacement_available"] = "Pilihan tidak valid."
	}

	if t, ok := incidentTime(val("incident_started"), now); ok {
		d.IncidentStartedAt = t
	} else {
		errs["incident_started"] = "Pilihan tidak valid."
	}

	if id, ok := parseID(val("asset_id")); ok {
		d.AssetID = id
	} else {
		errs["asset_id"] = "Pilihan perangkat tidak valid."
	}
	return d, form, errs
}

// ParseStaffLocation membaca sumber & lokasi untuk tiket yang dicatat petugas.
func ParseStaffLocation(get func(string) string, d *Draft, form, errs map[string]string) {
	d.Channel = strings.TrimSpace(get("channel"))
	form["channel"] = d.Channel
	switch d.Channel {
	case ChannelPhone, ChannelMessage, ChannelWalkIn:
	default:
		errs["channel"] = "Pilih sumber laporan."
	}
	dept := strings.TrimSpace(get("department_id"))
	form["department_id"] = dept
	if id, ok := parseID(dept); !ok || id == 0 {
		errs["department_id"] = "Pilih unit lokasi gangguan."
	} else {
		d.DepartmentID = id
	}
	room := strings.TrimSpace(get("room_id"))
	form["room_id"] = room
	if id, ok := parseID(room); ok {
		d.RoomID = id
	} else {
		errs["room_id"] = "Pilihan ruangan tidak valid."
	}
}
