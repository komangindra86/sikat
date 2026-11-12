package activities

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sikat/internal/clock"
)

func dayStart(t time.Time) time.Time {
	y, m, d := t.In(clock.Location).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, clock.Location)
}

// Parse membaca form kegiatan. Keberadaan referensi diperiksa di service.
func Parse(get func(string) string, getAll func(string) []string, now time.Time) (Input, map[string]string, map[string]string) {
	errs := map[string]string{}
	form := map[string]string{}
	val := func(k string) string {
		v := strings.TrimSpace(get(k))
		form[k] = v
		return v
	}
	id := func(k, label string, required bool) int64 {
		v := val(k)
		if v == "" {
			if required {
				errs[k] = label + " wajib dipilih."
			}
			return 0
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs[k] = "Pilihan " + strings.ToLower(label) + " tidak valid."
			return 0
		}
		return n
	}
	text := func(k, label string, max int, required bool) string {
		v := val(k)
		switch n := utf8.RuneCountInString(v); {
		case n == 0 && required:
			errs[k] = label + " wajib diisi."
		case n > max:
			errs[k] = label + " maksimal " + strconv.Itoa(max) + " karakter."
		}
		return v
	}
	dt := func(k, label string) *time.Time {
		v := val(k)
		if v == "" {
			return nil
		}
		t, err := time.ParseInLocation("2006-01-02T15:04", v, clock.Location)
		if err != nil {
			errs[k] = "Format " + strings.ToLower(label) + " tidak valid."
			return nil
		}
		u := t.UTC()
		return &u
	}

	var in Input
	in.TypeID = id("activity_type_id", "Jenis kegiatan", true)
	in.ITCategoryID = id("it_category_id", "Bidang", true)
	in.DepartmentID = id("department_id", "Unit", false)
	in.RoomID = id("room_id", "Ruangan", false)
	in.AssetID = id("asset_id", "Perangkat", false)
	in.LeadID = id("lead_id", "Pelaksana", false)
	in.Title = text("title", "Kegiatan", 200, true)
	in.Description = text("description", "Uraian", 4000, false)
	in.Result = text("result", "Hasil", 4000, false)
	in.MaintenanceType = val("maintenance_type")
	if in.MaintenanceType == "" {
		in.MaintenanceType = "PREVENTIVE"
		form["maintenance_type"] = in.MaintenanceType
	}
	if !maintenanceTypes[in.MaintenanceType] {
		errs["maintenance_type"] = "Jenis riwayat perangkat tidak valid."
	}
	in.RelatedTicketNo = strings.ToUpper(val("related_ticket_no"))
	form["related_ticket_no"] = in.RelatedTicketNo

	in.Status = val("status")
	if in.Status == "" {
		in.Status = StatusDone
		form["status"] = StatusDone
	}
	if !validStatus[in.Status] {
		errs["status"] = "Status tidak valid."
	}
	in.StartedAt = dt("started_at", "Waktu mulai")
	in.EndedAt = dt("ended_at", "Waktu selesai")

	today := dayStart(now)
	if d := val("activity_date"); d != "" {
		t, err := time.ParseInLocation("2006-01-02", d, clock.Location)
		if err != nil {
			errs["activity_date"] = "Format tanggal tidak valid."
		} else {
			in.Date = t
		}
	} else if in.StartedAt != nil {
		in.Date = dayStart(*in.StartedAt) // tanggal otomatis dari waktu mulai
	} else {
		in.Date = today
	}

	if in.StartedAt != nil && in.EndedAt != nil && in.EndedAt.Before(*in.StartedAt) {
		errs["ended_at"] = "Waktu selesai tidak boleh sebelum waktu mulai."
	}
	if in.Status == StatusDone {
		if in.Result == "" {
			errs["result"] = "Hasil wajib diisi untuk kegiatan yang selesai."
		}
		if in.EndedAt != nil && in.EndedAt.After(now.Add(time.Minute)) {
			errs["ended_at"] = "Kegiatan selesai tidak boleh berakhir di masa depan."
		}
		if !in.Date.IsZero() && in.Date.After(today) {
			errs["activity_date"] = "Tanggal kegiatan yang sudah selesai tidak boleh di masa depan."
		}
	}
	if in.Status == StatusPlanned && in.EndedAt != nil {
		errs["ended_at"] = "Kegiatan yang masih direncanakan belum memiliki waktu selesai."
	}

	seen := map[int64]bool{}
	for _, v := range getAll("support_ids") {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || n <= 0 {
			errs["support_ids"] = "Pilihan pendukung tidak valid."
			continue
		}
		if !seen[n] {
			seen[n] = true
			in.SupportIDs = append(in.SupportIDs, n)
		}
	}
	return in, form, errs
}
