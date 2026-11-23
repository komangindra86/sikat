package master

var priorityChoices = []Choice{
	{"LOW", "Rendah"}, {"MEDIUM", "Sedang"}, {"HIGH", "Tinggi"}, {"CRITICAL", "Kritis"},
}

var (
	refDepartments    = &Ref{Table: "departments", LabelCol: "name", OrderBy: "name"}
	refITCategories   = &Ref{Table: "it_categories", LabelCol: "name", OrderBy: "sort_order, name"}
	refTicketCategory = &Ref{Table: "ticket_categories", LabelCol: "name", OrderBy: "sort_order, name"}
	// refCoordinators: hanya akun ber-role Koordinator IT.
	refCoordinators = &Ref{Table: "users", LabelCol: "full_name", OrderBy: "full_name",
		Where: "role_id IN (SELECT id FROM roles WHERE code = 'COORDINATOR')"}
)

// codeField adalah kode otomatis Prefix-NNN; data lama/impor tetap memakai kodenya sendiri.
func codeField(prefix string, width int) Field {
	return Field{Column: "code", Label: "Kode", Kind: KindText, InList: true, Auto: &AutoCode{Prefix: prefix, Width: width}}
}

func sortField() Field {
	return Field{Column: "sort_order", Label: "Urutan tampil", Kind: KindInt, Min: 0, Max: 9999, Default: "0",
		Help: "Angka kecil tampil lebih dulu."}
}

// Entities adalah seluruh master data yang dikelola engine ini, sesuai urutan menu.
var Entities = []*Entity{
	{
		Slug: "departments", Table: "departments", Title: "Unit", Singular: "unit",
		Description: "Unit/bagian rumah sakit. Unit layanan klinis kritis menaikkan prioritas tiket otomatis.",
		Fields: []Field{
			codeField("UNT", 3),
			{Column: "name", Label: "Nama unit", Kind: KindText, Required: true, MaxLen: 150, InList: true},
			{Column: "is_critical_service", Label: "Layanan klinis kritis", Kind: KindBool, InList: true,
				Help: "Contoh: IGD, ICU, kamar operasi. Dipakai mesin prioritas otomatis."},
		},
		OrderBy: "name", Search: []string{"code", "name"}, HasActive: true, TracksSync: true,
		UniqueFields: map[string]UniqueRule{
			"uq_departments_code": {"code", "Kode unit sudah dipakai."},
			"uq_departments_name": {"name", "Nama unit sudah ada."},
		},
	},
	{
		Slug: "rooms", Table: "rooms", Title: "Ruangan", Singular: "ruangan",
		Description: "Ruangan milik setiap unit. QR ruangan (fase 2) mengisi lokasi otomatis.",
		Fields: []Field{
			{Column: "department_id", Label: "Unit", Kind: KindSelect, Required: true, Ref: refDepartments, InList: true},
			codeField("RNG", 4),
			{Column: "name", Label: "Nama ruangan", Kind: KindText, Required: true, MaxLen: 150, InList: true},
			{Column: "location_note", Label: "Gedung / lantai", Kind: KindText, MaxLen: 150, InList: true},
			{Column: "is_critical_service", Label: "Layanan klinis kritis", Kind: KindEnum, InList: true,
				Choices: []Choice{{"", "Ikuti pengaturan unit"}, {"1", "Ya"}, {"0", "Tidak"}},
				Help:    "Biarkan mengikuti unit kecuali ruangan ini berbeda dari unitnya."},
		},
		OrderBy: "name", Search: []string{"code", "name", "location_note"}, HasActive: true, TracksSync: true,
		UniqueFields: map[string]UniqueRule{
			"uq_rooms_code":      {"code", "Kode ruangan sudah dipakai."},
			"uq_rooms_dept_name": {"name", "Nama ruangan sudah ada di unit ini."},
		},
	},
	{
		Slug: "employees", Table: "employees", Title: "Pegawai", Singular: "pegawai",
		Description: "Direktori pegawai rumah sakit (bukan akun login). Dipakai sebagai saran nama pelapor saat petugas mencatat tiket.",
		Fields: []Field{
			{Column: "nip", Label: "NIP / NIK", Kind: KindText, Required: true, MaxLen: 30, InList: true},
			{Column: "full_name", Label: "Nama lengkap", Kind: KindText, Required: true, MaxLen: 150, InList: true},
			{Column: "department_id", Label: "Unit", Kind: KindSelect, Ref: refDepartments, InList: true},
			{Column: "position", Label: "Jabatan", Kind: KindText, MaxLen: 100, InList: true},
			{Column: "phone", Label: "Telepon / ekstensi", Kind: KindText, MaxLen: 30},
		},
		OrderBy: "full_name", Search: []string{"nip", "full_name", "position"}, HasActive: true, TracksSync: true,
		UniqueFields: map[string]UniqueRule{
			"uq_employees_nip": {"nip", "NIP sudah terdaftar."},
		},
	},
	{
		Slug: "it-categories", Table: "it_categories", Title: "Bidang", Singular: "bidang",
		Description: "Bidang pekerjaan IT dan koordinatornya. Dipakai untuk kompetensi petugas, notifikasi koordinator, dan laporan per bidang.",
		Fields: []Field{
			codeField("BDG", 2),
			{Column: "name", Label: "Nama bidang", Kind: KindText, Required: true, MaxLen: 100, InList: true},
			{Column: "coordinator_user_id", Label: "Koordinator", Kind: KindSelect, Ref: refCoordinators, InList: true,
				Help: "Satu koordinator per bidang (satu koordinator boleh memegang beberapa bidang). Notifikasi & eskalasi bidang ini dikirim kepadanya; bila kosong, ke semua koordinator."},
			{Column: "description", Label: "Keterangan", Kind: KindText, MaxLen: 255, InList: true},
			sortField(),
		},
		OrderBy: "sort_order, name", Search: []string{"code", "name"}, HasActive: true,
		UniqueFields: map[string]UniqueRule{
			"uq_it_categories_code": {"code", "Kode bidang sudah dipakai."},
			"uq_it_categories_name": {"name", "Nama bidang sudah ada."},
		},
	},
	{
		Slug: "ticket-categories", Table: "ticket_categories", Title: "Jenis Gangguan", Singular: "jenis gangguan",
		Description: "Pilihan masalah yang dilihat pelapor. Gunakan bahasa sehari-hari, bukan istilah teknis.",
		Fields: []Field{
			{Column: "name", Label: "Nama (bahasa awam)", Kind: KindText, Required: true, MaxLen: 120, InList: true,
				Help: "Contoh: Tidak bisa mencetak"},
			{Column: "it_category_id", Label: "Bidang", Kind: KindSelect, Required: true, Ref: refITCategories, InList: true},
			{Column: "help_text", Label: "Penjelasan singkat", Kind: KindText, MaxLen: 255,
				Help: "Tampil di bawah pilihan untuk membantu pelapor."},
			{Column: "default_priority", Label: "Prioritas bawaan", Kind: KindEnum, Required: true, Choices: priorityChoices,
				Default: "MEDIUM", InList: true, Help: "Dipakai bila tidak ada aturan prioritas yang cocok."},
			sortField(),
		},
		OrderBy: "sort_order, name", Search: []string{"name", "help_text"}, HasActive: true,
		UniqueFields: map[string]UniqueRule{
			"uq_ticket_categories_name": {"name", "Jenis gangguan dengan nama ini sudah ada."},
		},
	},
	{
		Slug: "asset-categories", Table: "asset_categories", Title: "Kategori Perangkat", Singular: "kategori perangkat",
		Fields: []Field{
			codeField("KAT", 2),
			{Column: "name", Label: "Nama kategori", Kind: KindText, Required: true, MaxLen: 100, InList: true},
			{Column: "it_category_id", Label: "Bidang", Kind: KindSelect, Ref: refITCategories, InList: true},
		},
		OrderBy: "name", Search: []string{"code", "name"}, HasActive: true,
		UniqueFields: map[string]UniqueRule{
			"uq_asset_categories_code": {"code", "Kode kategori sudah dipakai."},
			"uq_asset_categories_name": {"name", "Nama kategori sudah ada."},
		},
	},
	{
		Slug: "pending-reasons", Table: "pending_reasons", Title: "Alasan Tunda", Singular: "alasan tunda",
		Description: "Pilihan alasan saat petugas menunda tiket.",
		Fields: []Field{
			codeField("ALS", 2),
			{Column: "label", Label: "Alasan", Kind: KindText, Required: true, MaxLen: 120, InList: true},
			{Column: "pauses_sla", Label: "Menghentikan SLA", Kind: KindBool, InList: true,
				Help: "Hanya berlaku bila kebijakan SLA tiket mengizinkan penghentian (diatur di modul SLA)."},
			sortField(),
		},
		OrderBy: "sort_order, label", Search: []string{"code", "label"}, HasActive: true,
		UniqueFields: map[string]UniqueRule{
			"uq_pending_reasons_code": {"code", "Kode alasan sudah dipakai."},
		},
	},
	{
		Slug: "activity-types", Table: "activity_types", Title: "Jenis Kegiatan", Singular: "jenis kegiatan",
		Description: "Jenis kegiatan internal IT di luar tiket.",
		Fields: []Field{
			{Column: "name", Label: "Nama kegiatan", Kind: KindText, Required: true, MaxLen: 100, InList: true},
			{Column: "it_category_id", Label: "Bidang bawaan", Kind: KindSelect, Ref: refITCategories, InList: true},
			sortField(),
		},
		OrderBy: "sort_order, name", Search: []string{"name"}, HasActive: true,
		UniqueFields: map[string]UniqueRule{
			"uq_activity_types_name": {"name", "Jenis kegiatan dengan nama ini sudah ada."},
		},
	},
	{
		Slug: "action-templates", Table: "action_templates", Title: "Template Tindakan", Singular: "template tindakan",
		Description: "Teks tindakan siap pakai agar petugas tidak mengetik ulang.",
		Fields: []Field{
			{Column: "title", Label: "Judul", Kind: KindText, Required: true, MaxLen: 120, InList: true},
			{Column: "it_category_id", Label: "Bidang", Kind: KindSelect, Ref: refITCategories, InList: true},
			{Column: "ticket_category_id", Label: "Jenis gangguan", Kind: KindSelect, Ref: refTicketCategory, InList: true},
			{Column: "body", Label: "Isi tindakan", Kind: KindTextarea, Required: true, MaxLen: 2000},
			sortField(),
		},
		OrderBy: "sort_order, title", Search: []string{"title", "body"}, HasActive: true,
	},
	{
		Slug: "spare-parts", Table: "spare_parts", Title: "Sparepart", Singular: "sparepart",
		Description: "Katalog sparepart dan harga acuan (tanpa stok).",
		Fields: []Field{
			codeField("SPR", 4),
			{Column: "name", Label: "Nama sparepart", Kind: KindText, Required: true, MaxLen: 150, InList: true},
			{Column: "unit", Label: "Satuan", Kind: KindText, Required: true, MaxLen: 20, Default: "pcs", InList: true},
			{Column: "default_unit_cost", Label: "Harga acuan (Rp)", Kind: KindMoney, InList: true,
				Help: "Opsional. Contoh: 175000"},
		},
		OrderBy: "name", Search: []string{"code", "name"}, HasActive: true,
		UniqueFields: map[string]UniqueRule{
			"uq_spare_parts_code": {"code", "Kode sparepart sudah dipakai."},
		},
	},
}

var impactChoices = []Choice{{"", "Apa saja"}, {"0", "Tidak mengganggu"}, {"1", "Sebagian terganggu"}, {"2", "Pelayanan berhenti"}}
var scopeChoices = []Choice{{"", "Apa saja"}, {"1", "Satu orang"}, {"2", "Beberapa orang"}, {"3", "Satu unit"}, {"4", "Lebih dari satu unit"}}

func init() {
	Entities = append(Entities, &Entity{
		Slug: "priority-rules", Table: "priority_rules", Title: "Aturan Prioritas", Singular: "aturan prioritas",
		Group: "Konfigurasi",
		Description: "Dievaluasi dari urutan terkecil; aturan pertama yang cocok menentukan prioritas tiket. " +
			"Bila tidak ada yang cocok, dipakai prioritas bawaan jenis gangguan.",
		Links: []Link{{Label: "Uji aturan", URL: "/app/priority/test"}},
		Fields: []Field{
			{Column: "sort_order", Label: "Urutan", Kind: KindInt, Required: true, Min: 0, Max: 9999, Default: "100", InList: true,
				Help: "Angka kecil dievaluasi lebih dulu."},
			{Column: "name", Label: "Nama aturan", Kind: KindText, Required: true, MaxLen: 120, InList: true},
			{Column: "description", Label: "Keterangan", Kind: KindText, MaxLen: 255},
			{Column: "result_priority", Label: "Hasil prioritas", Kind: KindEnum, Required: true, Choices: priorityChoices, Default: "MEDIUM", InList: true},
			{Column: "critical_service", Label: "Layanan klinis kritis", Kind: KindEnum, InList: true,
				Choices: []Choice{{"", "Apa saja"}, {"1", "Ya"}, {"0", "Tidak"}},
				Help:    "Dihitung otomatis dari pengaturan unit/ruangan lokasi gangguan."},
			{Column: "min_impact", Label: "Dampak minimal", Kind: KindEnum, Choices: impactChoices},
			{Column: "max_impact", Label: "Dampak maksimal", Kind: KindEnum, Choices: impactChoices},
			{Column: "min_scope", Label: "Terdampak minimal", Kind: KindEnum, Choices: scopeChoices},
			{Column: "max_scope", Label: "Terdampak maksimal", Kind: KindEnum, Choices: scopeChoices},
			{Column: "replacement_in", Label: "Perangkat pengganti", Kind: KindEnum, Choices: []Choice{
				{"", "Apa saja"}, {"YES", "Ada"}, {"NO", "Tidak ada"}, {"UNKNOWN", "Tidak tahu"},
				{"NO,UNKNOWN", "Tidak ada atau tidak tahu"}, {"YES,UNKNOWN", "Ada atau tidak tahu"},
			}},
			{Column: "ticket_category_id", Label: "Hanya jenis gangguan", Kind: KindSelect, Ref: refTicketCategory},
			{Column: "it_category_id", Label: "Hanya bidang", Kind: KindSelect, Ref: refITCategories},
			{Column: "department_id", Label: "Hanya unit", Kind: KindSelect, Ref: refDepartments},
		},
		OrderBy: "sort_order", Search: []string{"name", "description"}, HasActive: true,
		CheckFields: map[string]UniqueRule{
			"chk_prule_impact": {"max_impact", "Dampak maksimal tidak boleh lebih kecil dari dampak minimal."},
			"chk_prule_scope":  {"max_scope", "Terdampak maksimal tidak boleh lebih kecil dari terdampak minimal."},
		},
	})
}

// EntityBySlug mencari entitas berdasarkan segmen URL.
func EntityBySlug(slug string) (*Entity, bool) {
	for _, e := range Entities {
		if e.Slug == slug {
			return e, true
		}
	}
	return nil, false
}

var refSLACalendars = &Ref{Table: "sla_calendars", LabelCol: "name", OrderBy: "name", NoActive: true}

func init() {
	Entities = append(Entities, &Entity{
		Slug: "sla-policies", Table: "sla_policies", Title: "Kebijakan SLA", Singular: "kebijakan SLA",
		Group: "Konfigurasi",
		Description: "Target waktu respons dan penyelesaian per prioritas. Kebijakan yang paling spesifik " +
			"(jenis gangguan > bidang > unit) dipakai; nilainya disalin ke tiket saat dibuat sehingga perubahan " +
			"di sini hanya berlaku untuk tiket baru atau saat prioritas tiket diubah.",
		Links: []Link{{Label: "Kalender kerja", URL: "/app/sla/calendars"}, {Label: "Dashboard SLA", URL: "/app/sla"}},
		Fields: []Field{
			{Column: "name", Label: "Nama kebijakan", Kind: KindText, Required: true, MaxLen: 120, InList: true},
			{Column: "priority", Label: "Prioritas", Kind: KindEnum, Required: true, Choices: priorityChoices, Default: "MEDIUM", InList: true},
			{Column: "calendar_id", Label: "Kalender", Kind: KindSelect, Required: true, Ref: refSLACalendars, InList: true,
				Help: "Waktu di luar jam kerja dan hari libur kalender tidak dihitung."},
			{Column: "response_minutes", Label: "Target respons (menit)", Kind: KindInt, Required: true, Min: 1, Max: 100000, InList: true,
				Help: "Dari tiket masuk sampai diambil petugas. 60 = 1 jam."},
			{Column: "resolution_minutes", Label: "Target penyelesaian (menit)", Kind: KindInt, Required: true, Min: 1, Max: 100000, InList: true,
				Help: "Dari tiket masuk (atau dibuka ulang) sampai selesai. 480 = 8 jam."},
			{Column: "warn_pct", Label: "Ambang peringatan (%)", Kind: KindInt, Required: true, Min: 1, Max: 99, Default: "75",
				Help: "Pengingat dikirim saat pemakaian waktu mencapai persentase ini."},
			{Column: "allow_pause", Label: "Boleh dihentikan saat tertunda", Kind: KindBool,
				Help: "SLA berhenti hanya bila opsi ini aktif DAN alasan tunda bertanda \"Menghentikan SLA\"."},
			{Column: "ticket_category_id", Label: "Hanya jenis gangguan", Kind: KindSelect, Ref: refTicketCategory},
			{Column: "it_category_id", Label: "Hanya bidang", Kind: KindSelect, Ref: refITCategories},
			{Column: "department_id", Label: "Hanya unit", Kind: KindSelect, Ref: refDepartments},
		},
		OrderBy: "FIELD(priority,'CRITICAL','HIGH','MEDIUM','LOW'), name", Search: []string{"name"}, HasActive: true,
		UniqueFields: map[string]UniqueRule{
			"uq_sla_policy_match": {"priority", "Sudah ada kebijakan aktif untuk kombinasi prioritas, jenis gangguan, bidang, dan unit yang sama."},
		},
		CheckFields: map[string]UniqueRule{
			"chk_sla_policy_minutes": {"resolution_minutes", "Target penyelesaian tidak boleh lebih kecil dari target respons."},
			"chk_sla_policy_warn":    {"warn_pct", "Ambang peringatan harus 1–99%."},
		},
	})
}
