package web

// Option adalah pasangan nilai/label untuk pilihan form.
type Option struct {
	Value string
	Label string
}

// labelGroups memetakan kode internal ke label Bahasa Indonesia.
// Urutan di sini juga urutan tampil pada pilihan form.
var labelGroups = map[string][]Option{
	"asset_status": {
		{"ACTIVE", "Aktif"}, {"BROKEN", "Rusak"}, {"IN_REPAIR", "Dalam perbaikan"},
		{"UNUSED", "Tidak digunakan"}, {"DISPOSED", "Dihapus dari inventaris"},
	},
	"priority": {
		{"LOW", "Rendah"}, {"MEDIUM", "Sedang"}, {"HIGH", "Tinggi"}, {"CRITICAL", "Kritis"},
	},
	"source": {
		{"LOCAL", "Lokal"}, {"SIMRS", "SIMRS"}, {"CSV", "Impor CSV"},
	},
	"actor_type": {
		{"USER", "Pengguna"}, {"REPORTER", "Pelapor"}, {"SYSTEM", "Sistem"},
	},
	"ticket_status": {
		{"NEW", "Baru Masuk"}, {"CLAIMED", "Diterima"}, {"IN_PROGRESS", "Sedang Dikerjakan"},
		{"PENDING", "Tertunda"}, {"RESOLVED", "Selesai"}, {"CLOSED", "Ditutup"}, {"CANCELLED", "Dibatalkan"},
	},
	"channel": {
		{"QR", "Scan QR"}, {"PHONE", "Telepon"}, {"MESSAGE", "WA / pesan"}, {"WALK_IN", "Datang langsung"},
	},
	"impact": {
		{"0", "Tidak mengganggu"}, {"1", "Sebagian terganggu"}, {"2", "Pelayanan berhenti"},
	},
	"scope": {
		{"1", "Satu orang"}, {"2", "Beberapa orang"}, {"3", "Satu unit"}, {"4", "Lebih dari satu unit"},
	},
	"replacement": {
		{"YES", "Ada"}, {"NO", "Tidak ada"}, {"UNKNOWN", "Tidak tahu"},
	},
	"qr_type": {
		{"ROOM", "Ruangan"}, {"ASSET", "Perangkat"},
	},
	"resolution_result": {
		{"FIXED", "Berfungsi normal"}, {"WORKAROUND", "Solusi sementara"}, {"REPLACED", "Perangkat diganti"},
		{"VENDOR", "Diteruskan ke vendor"}, {"NOT_REPRODUCIBLE", "Tidak ditemukan masalah"}, {"CANNOT_FIX", "Tidak dapat diperbaiki"},
	},
	"ticket_event": {
		{"CREATED", "Tiket dibuat"}, {"CLAIMED", "Tiket diambil"}, {"STARTED", "Mulai dikerjakan"},
		{"PENDING", "Ditunda"}, {"RESUMED", "Dilanjutkan"}, {"RESOLVED", "Diselesaikan"},
		{"RELEASED", "Dikembalikan ke antrean"}, {"CANCELLED", "Dibatalkan"}, {"CLOSED", "Ditutup"},
		{"AUTO_CLOSED", "Ditutup otomatis"}, {"REOPENED", "Dibuka kembali"}, {"REPORTER_CONFIRMED", "Dikonfirmasi pelapor"},
		{"SLA_CRITICAL", "Eskalasi tiket kritis"}, {"SLA_WARNING", "Pengingat SLA"}, {"SLA_BREACH", "SLA terlewati"},
		{"TAKEN_OVER", "Diambil alih"}, {"SUPPORT_ADDED", "Anggota pendukung ditambahkan"},
		{"SUPPORT_REMOVED", "Anggota pendukung dilepas"}, {"PRIORITY_CHANGED", "Prioritas diubah"}, {"PHOTO_ADDED", "Foto ditambahkan"},
		{"CATEGORY_CHANGED", "Jenis gangguan dikoreksi"},
	},
	"maintenance_type": {
		{"CORRECTIVE", "Perbaikan"}, {"PREVENTIVE", "Pemeliharaan preventif"}, {"INSPECTION", "Pemeriksaan"},
		{"INSTALLATION", "Instalasi"}, {"RELOCATION", "Pemindahan"}, {"ALERT_DECISION", "Keputusan koordinator"},
	},
	"maintenance_source": {
		{"TICKET", "Tiket"}, {"ACTIVITY", "Kegiatan"}, {"MANUAL", "Manual"}, {"ALERT", "Peringatan"},
	},
	"maintenance_result": {
		{"FIXED", "Berfungsi normal"}, {"WORKAROUND", "Solusi sementara"}, {"REPLACED", "Perangkat diganti"},
		{"VENDOR", "Diteruskan ke vendor"}, {"NOT_REPRODUCIBLE", "Tidak ditemukan masalah"}, {"CANNOT_FIX", "Tidak dapat diperbaiki"},
		{"DONE", "Selesai"},
	},
	"sla_state": {
		{"SAFE", "Aman"}, {"WARNING", "Peringatan"}, {"LATE", "Terlambat"}, {"PAUSED", "Dijeda"},
		{"MET", "Tercapai"}, {"MISSED", "Terlewati"},
	},
	"sla_target": {{"RESPONSE", "Respons"}, {"RESOLUTION", "Penyelesaian"}},
	"notif_event": {
		{"TICKET_CREATED", "Tiket baru"}, {"TICKET_CRITICAL", "Tiket kritis"}, {"TICKET_CLAIMED", "Tiket diambil"},
		{"TICKET_STARTED", "Mulai dikerjakan"}, {"TICKET_PENDING", "Tiket tertunda"}, {"TICKET_RESOLVED", "Tiket selesai"},
		{"TICKET_REOPENED", "Dibuka kembali"}, {"TICKET_ASSIGNED", "Penugasan"}, {"TICKET_TRANSFERRED", "Dialihkan ke bidang"}, {"SLA_WARNING", "SLA hampir habis"},
		{"SLA_BREACH", "SLA terlewati"}, {"REPEAT_DAMAGE", "Perangkat sering rusak"}, {"TEST", "Notifikasi uji"},
	},
	"integration_entity": {
		{"DEPARTMENT", "Unit"}, {"ROOM", "Ruangan"}, {"EMPLOYEE", "Pegawai"}, {"ASSET", "Perangkat"},
	},
	"integration_provider": {
		{"LOCAL", "Lokal (dikelola di SIKAT)"}, {"CSV", "Impor CSV"}, {"SIMRS_API", "SIMRS (API)"}, {"SIMRS_DB", "SIMRS (database read-only)"},
	},
	"sync_action": {
		{"INSERT", "Ditambah"}, {"UPDATE", "Diperbarui"}, {"UNCHANGED", "Tidak berubah"}, {"LINK", "Ditautkan"},
		{"CONFLICT", "Konflik"}, {"ERROR", "Galat"}, {"DEACTIVATE", "Dinonaktifkan"}, {"MISSING", "Tidak ada di sumber"},
	},
	"sync_status": {
		{"SUCCESS", "Berhasil"}, {"PARTIAL", "Sebagian"}, {"FAILED", "Gagal"}, {"RUNNING", "Berjalan"},
	},
	"sync_trigger": {
		{"MANUAL", "Manual"}, {"SCHEDULE", "Terjadwal"}, {"RETRY", "Percobaan ulang"},
	},
	"conflict_policy": {
		{"KEEP_LOCAL", "Pertahankan perubahan lokal"}, {"PREFER_SOURCE", "Utamakan data sumber"},
	},
	"notif_channel": {
		{"NONE", "Tidak dikirim"}, {"INBOX", "Inbox"}, {"PUSH", "Inbox + push"},
	},
	"sla_kind": {
		{"CRITICAL_IMMEDIATE", "Tiket kritis"}, {"RESPONSE_WARNING", "Respons hampir habis"},
		{"RESPONSE_BREACH", "Respons terlewati"}, {"RESOLUTION_WARNING", "Penyelesaian hampir habis"},
		{"RESOLUTION_BREACH", "Penyelesaian terlewati"},
	},
	"weekday": {
		{"1", "Senin"}, {"2", "Selasa"}, {"3", "Rabu"}, {"4", "Kamis"}, {"5", "Jumat"}, {"6", "Sabtu"}, {"7", "Minggu"},
	},
	"recommendation": {
		{"EVALUATE", "Evaluasi perangkat"}, {"INSPECT", "Pemeriksaan lanjutan"}, {"CONSIDER_REPLACEMENT", "Pertimbangan penggantian"},
	},
	"alert_decision": {
		{"NO_ACTION", "Tidak perlu tindakan"}, {"INSPECT", "Pemeriksaan lanjutan"}, {"REPAIR", "Perbaikan menyeluruh"},
		{"REPLACEMENT_PROPOSED", "Usulkan penggantian"}, {"OTHER", "Lainnya"},
	},
	"activity_status": {
		{"PLANNED", "Direncanakan"}, {"IN_PROGRESS", "Berlangsung"}, {"DONE", "Selesai"}, {"CANCELLED", "Dibatalkan"},
	},
}

// tones memetakan kode ke kelas warna badge yang konsisten di seluruh aplikasi.
var tones = map[string]map[string]string{
	"asset_status": {
		"ACTIVE": "green", "BROKEN": "red", "IN_REPAIR": "amber", "UNUSED": "gray", "DISPOSED": "gray",
	},
	"priority": {
		"LOW": "gray", "MEDIUM": "blue", "HIGH": "orange", "CRITICAL": "red",
	},
	"ticket_status": {
		"NEW": "blue", "CLAIMED": "indigo", "IN_PROGRESS": "amber", "PENDING": "orange",
		"RESOLVED": "green", "CLOSED": "gray", "CANCELLED": "gray",
	},
	"sla_state": {
		"SAFE": "green", "WARNING": "amber", "LATE": "red", "PAUSED": "gray", "MET": "green", "MISSED": "red",
	},
	"sla_kind": {
		"CRITICAL_IMMEDIATE": "red", "RESPONSE_WARNING": "amber", "RESPONSE_BREACH": "red",
		"RESOLUTION_WARNING": "amber", "RESOLUTION_BREACH": "red",
	},
	"notif_event": {
		"TICKET_CREATED": "blue", "TICKET_CRITICAL": "red", "TICKET_CLAIMED": "indigo", "TICKET_STARTED": "amber",
		"TICKET_PENDING": "orange", "TICKET_RESOLVED": "green", "TICKET_REOPENED": "orange", "TICKET_ASSIGNED": "indigo", "TICKET_TRANSFERRED": "indigo",
		"SLA_WARNING": "amber", "SLA_BREACH": "red", "REPEAT_DAMAGE": "orange", "TEST": "gray",
	},
	"sync_action": {
		"INSERT": "green", "UPDATE": "blue", "UNCHANGED": "gray", "LINK": "indigo", "CONFLICT": "amber",
		"ERROR": "red", "DEACTIVATE": "orange", "MISSING": "orange",
	},
	"sync_status": {
		"SUCCESS": "green", "PARTIAL": "amber", "FAILED": "red", "RUNNING": "blue",
	},
	"notif_channel": {
		"NONE": "gray", "INBOX": "blue", "PUSH": "green",
	},
	"recommendation": {
		"EVALUATE": "amber", "INSPECT": "orange", "CONSIDER_REPLACEMENT": "red",
	},
	"activity_status": {
		"PLANNED": "blue", "IN_PROGRESS": "amber", "DONE": "green", "CANCELLED": "gray",
	},
}

// Label mengembalikan label tampilan untuk kode.
func Label(group, code string) string {
	for _, o := range labelGroups[group] {
		if o.Value == code {
			return o.Label
		}
	}
	return code
}

// Labels mengembalikan seluruh pilihan untuk grup.
func Labels(group string) []Option {
	return labelGroups[group]
}

// Tone mengembalikan nama warna badge untuk kode.
func Tone(group, code string) string {
	if t, ok := tones[group][code]; ok {
		return t
	}
	return "gray"
}

// ValidLabel memeriksa apakah kode ada di grup.
func ValidLabel(group, code string) bool {
	for _, o := range labelGroups[group] {
		if o.Value == code {
			return true
		}
	}
	return false
}
