package activities_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"sikat/internal/activities"
	"sikat/internal/attachments"
	"sikat/internal/clock"
	"sikat/internal/rbac"
	"sikat/internal/testutil"
)

func getAll(m map[string][]string) func(string) []string {
	return func(k string) []string { return m[k] }
}

type fx struct {
	svc                *activities.Service
	tech, other, coord *rbac.User
	typeID, itID       string
}

func setup(t *testing.T) (fx, func(q string, args ...any) int) {
	db := testutil.DB(t)
	clk := clock.NewFake(time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC)) // 14:00 WITA
	f := fx{svc: activities.NewService(db, clk, attachments.Store{Dir: t.TempDir()})}
	f.tech = &rbac.User{ID: testutil.CreateUser(t, db, "tek1", "TECHNICIAN", "password-tek1", false), RoleCode: rbac.RoleTechnician}
	f.other = &rbac.User{ID: testutil.CreateUser(t, db, "tek2", "TECHNICIAN", "password-tek2", false), RoleCode: rbac.RoleTechnician}
	f.coord = &rbac.User{ID: testutil.CreateUser(t, db, "koor", "COORDINATOR", "password-koor", false), RoleCode: rbac.RoleCoordinator}
	var tid, it int64
	_ = db.QueryRow(`SELECT id, it_category_id FROM activity_types WHERE name = 'Pengecekan server'`).Scan(&tid, &it)
	f.typeID, f.itID = strconv.FormatInt(tid, 10), strconv.FormatInt(it, 10)
	return f, func(q string, args ...any) int { return testutil.Count(t, db, q, args...) }
}

func (f fx) form(mod map[string]string) map[string]string {
	m := map[string]string{
		"activity_type_id": f.typeID, "it_category_id": f.itID, "title": "Cek server DB", "status": "DONE",
		"result": "Normal", "started_at": "2026-11-11T08:00", "ended_at": "2026-11-11T09:35",
	}
	for k, v := range mod {
		m[k] = v
	}
	return m
}

func TestCreateActivity(t *testing.T) {
	f, count := setup(t)
	ctx := context.Background()
	id, _, errs, err := f.svc.Create(ctx, f.tech, testutil.Form(f.form(nil)),
		getAll(map[string][]string{"support_ids": {strconv.FormatInt(f.other.ID, 10), strconv.FormatInt(f.tech.ID, 10)}}))
	if err != nil || id == 0 {
		t.Fatalf("%v %v", errs, err)
	}
	a, err := f.svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.No != "KEG-2026-000001" || a.DurationMinutes.Int64 != 95 || a.DurationText() != "1 j 35 mnt" {
		t.Fatalf("nomor/durasi otomatis salah: %s %d %s", a.No, a.DurationMinutes.Int64, a.DurationText())
	}
	if a.Date.Format("2006-01-02") != "2026-11-11" {
		t.Fatalf("tanggal otomatis dari waktu mulai: %s", a.Date)
	}
	if !a.LeadID.Valid || a.LeadID.Int64 != f.tech.ID || len(a.Supports()) != 1 {
		t.Fatalf("pelaksana = pembuat, pelaksana tidak dobel jadi pendukung: %+v", a.Members)
	}
	if count(`SELECT COUNT(*) FROM audit_logs WHERE action = 'activity.create'`) != 1 {
		t.Fatal("pembuatan kegiatan harus tercatat di audit")
	}
	// Petugas tidak bisa menunjuk orang lain sebagai pelaksana utama.
	id2, _, _, _ := f.svc.Create(ctx, f.tech, testutil.Form(f.form(map[string]string{"lead_id": strconv.FormatInt(f.other.ID, 10)})), getAll(nil))
	a2, _ := f.svc.Get(ctx, id2)
	if a2.LeadID.Int64 != f.tech.ID || a2.No != "KEG-2026-000002" {
		t.Fatalf("petugas selalu menjadi pelaksana kegiatannya: %+v", a2)
	}
}

func TestActivityValidation(t *testing.T) {
	f, count := setup(t)
	ctx := context.Background()
	cases := map[string]struct {
		mod   map[string]string
		field string
	}{
		"selesai sebelum mulai":       {map[string]string{"ended_at": "2026-11-11T07:00"}, "ended_at"},
		"selesai tanpa hasil":         {map[string]string{"result": ""}, "result"},
		"selesai di masa depan":       {map[string]string{"ended_at": "2026-11-12T09:00"}, "ended_at"},
		"judul wajib":                 {map[string]string{"title": ""}, "title"},
		"jenis tidak ada":             {map[string]string{"activity_type_id": "9999"}, "activity_type_id"},
		"tiket terkait tidak ada":     {map[string]string{"related_ticket_no": "TKT-2026-999999"}, "related_ticket_no"},
		"rencana tidak punya selesai": {map[string]string{"status": "PLANNED", "result": ""}, "ended_at"},
	}
	for name, c := range cases {
		id, _, errs, err := f.svc.Create(ctx, f.tech, testutil.Form(f.form(c.mod)), getAll(nil))
		if err != nil || id != 0 || errs[c.field] == "" {
			t.Errorf("%s: harus error di %s, dapat id=%d %v %v", name, c.field, id, errs, err)
		}
	}
	if count(`SELECT COUNT(*) FROM internal_activities`) != 0 {
		t.Fatal("kegiatan tidak valid tidak boleh tersimpan")
	}
	// Kegiatan terencana di masa depan boleh.
	id, _, errs, _ := f.svc.Create(ctx, f.tech, testutil.Form(f.form(map[string]string{
		"status": "PLANNED", "result": "", "activity_date": "2026-11-20", "started_at": "", "ended_at": "",
	})), getAll(nil))
	if id == 0 {
		t.Fatalf("rencana kegiatan masa depan harus boleh: %v", errs)
	}
}

func TestActivityAccess(t *testing.T) {
	f, _ := setup(t)
	ctx := context.Background()
	id, _, _, _ := f.svc.Create(ctx, f.tech, testutil.Form(f.form(nil)), getAll(nil))
	a, _ := f.svc.Get(ctx, id)
	if !activities.CanView(a, f.tech) || !activities.CanView(a, f.coord) || activities.CanView(a, f.other) {
		t.Fatal("hak lihat: pembuat & koordinator boleh, petugas lain tidak")
	}
	if _, _, err := f.svc.Update(ctx, f.other, id, testutil.Form(f.form(nil)), getAll(nil)); err != activities.ErrForbidden {
		t.Fatalf("petugas lain tidak boleh mengubah, dapat %v", err)
	}
	// Koordinator boleh mengubah dan mengganti pelaksana.
	_, errs, err := f.svc.Update(ctx, f.coord, id, testutil.Form(f.form(map[string]string{
		"lead_id": strconv.FormatInt(f.other.ID, 10), "result": "Disk 62%",
	})), getAll(nil))
	if err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	a, _ = f.svc.Get(ctx, id)
	if a.LeadID.Int64 != f.other.ID || a.Result.String != "Disk 62%" {
		t.Fatalf("perubahan koordinator: %+v", a)
	}

	mine, total, _ := f.svc.List(ctx, activities.Filter{MemberID: f.other.ID})
	if total != 1 || mine[0].ID != id {
		t.Fatalf("filter anggota: %d", total)
	}
	_, total, _ = f.svc.List(ctx, activities.Filter{From: time.Date(2026, 12, 1, 0, 0, 0, 0, clock.Location)})
	if total != 0 {
		t.Fatal("filter tanggal harus mengecualikan kegiatan November")
	}
	if n, _ := f.svc.MonthCount(ctx, f.other.ID); n != 1 {
		t.Fatalf("jumlah kegiatan selesai bulan ini: %d", n)
	}
}
