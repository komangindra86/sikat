# SIKAT — Rancangan Database

> Status: **sesuai implementasi (PHASE 9).** Sumber kebenaran skema adalah migrasi
> goose di folder [`migrations/`](../migrations) (00001–00013); dokumen ini
> menjelaskan maksud & aturannya. `schema-draft.sql` adalah rancangan PHASE 0
> (referensi sejarah; perbedaan dicatat di §0). Diagram relasi: [`ERD.md`](ERD.md).

## 0. Migrasi & perbedaan dari rancangan PHASE 0

| Migrasi | Isi |
|---|---|
| 00001_init | Organisasi, katalog, perangkat, sistem, audit |
| 00002_seed_config | Konfigurasi awal (bidang, jenis gangguan, alasan tunda, pengaturan) |
| 00003_qr_tickets | QR, tiket, penugasan, histori, komentar, lampiran, penomoran |
| 00004_seed_phase2 | Aturan prioritas awal, pengaturan upload |
| 00005_activities | Kegiatan internal & anggota |
| 00006_maintenance | Riwayat pemeliharaan, sparepart per entri, peringatan kerusakan berulang |
| 00007_sla | Kalender kerja, hari libur, kebijakan SLA, eskalasi, kolom siklus/revisi SLA tiket |
| 00008_notifications | Outbox, inbox, langganan & pengiriman push, preferensi per role |
| 00009_bidang_coordinator | `it_categories.coordinator_user_id` (satu koordinator per bidang) |
| 00010_triage | `it_categories.is_triage` (bidang Triase, unik), `tickets.reported_ticket_category_id` (jenis pilihan pelapor sebelum dikoreksi) |
| 00011_integration | `employees`, `integration_configs`, `integration_sync_logs`, `external_id_mappings`, `integration_imports` |
| 00012_backups | `backup_logs`, `attachments.file_deleted_at` (retensi foto), pengaturan `attachments.retention_days` |
| 00013_uniform_codes | Kode lama diseragamkan ke format otomatis (UNT/RNG/BDG/KAT/ALS/SPR/PRK); `legacy_codes` (kode sebelum diseragamkan); `pending_reasons.requires_note` |

Perbedaan penting dari rancangan: `push_deliveries` ditambahkan (antrean push per
perangkat dengan retry); `integration_configs` belum menyimpan rahasia terenkripsi
(belum ada adapter SIMRS); `employees` ditambahkan sebagai direktori pegawai
(bukan akun login); penomoran memakai tabel `number_sequences`.

---

## 1. Konvensi

| Aturan | Nilai |
|---|---|
| Engine / charset | InnoDB, `utf8mb4`, `utf8mb4_0900_ai_ci` |
| Primary key | `id BIGINT UNSIGNED AUTO_INCREMENT` (kecuali tabel relasi/kunci alami) |
| Timestamp | `DATETIME(3)` **UTC**; koneksi dipaksa `time_zone='+00:00'`; tampil sebagai Asia/Makassar |
| Tanggal bisnis | `DATE` dalam WITA (mis. `internal_activities.activity_date`, `sla_holidays.holiday_date`) |
| Uang | `DECIMAL(14,2)` (Rupiah) |
| Enum status | `ENUM` MySQL untuk nilai tetap yang dipakai logika kode; label Indonesia ada di kode |
| Master data | Tidak dihapus bila sudah dipakai; dinonaktifkan via `is_active`. FK default **RESTRICT**. |
| Perangkat | Tidak dihapus; status `DISPOSED` ("Dihapus dari inventaris") |
| Tiket | Tidak dihapus (trigger); batal = status `CANCELLED` + alasan |
| Histori | `ticket_histories`, `audit_logs`, `ticket_comments`: tanpa UPDATE/DELETE (trigger). `asset_maintenance_histories`: tanpa DELETE; koreksi biaya/catatan tercatat di `audit_logs`. |
| Data sinkronisasi | Kolom `source`, `local_modified_at`, `last_synced_at` pada departments, rooms, users, assets |
| Versi minimal | MySQL **8.0.16+** (CHECK constraint ditegakkan) |

Kata tercadang yang perlu dihindari (ditemukan saat validasi): `last_value`
(fungsi window MySQL 8) → dipakai `current_value`.

---

## 2. Katalog Tabel

Tabel dari master prompt (✓) dan tambahan yang diperlukan (＋) beserta alasannya.

### 2.1 Organisasi & pengguna

| Tabel | | Isi | Catatan |
|---|:-:|---|---|
| `roles` | ✓ | ADMIN, COORDINATOR, TECHNICIAN | Permission di kode (matriks) |
| `users` | ✓ | Akun petugas IT | Argon2id, lockout (`failed_login_count`, `locked_until`), wajib ganti password pertama kali |
| `user_competencies` | ＋ | Petugas ↔ bidang | Filter antrean "sesuai kemampuan saya" (master prompt §10) |
| `departments` | ✓ | Unit/bagian | `is_critical_service` → input otomatis mesin prioritas |
| `employees` | ＋ | Direktori pegawai (bukan akun login) | NIP unik; saran nama pelapor saat petugas mencatat tiket; dapat diisi dari CSV/SIMRS |
| `rooms` | ✓ | Ruangan milik unit | `is_critical_service` NULL = ikut unit |

### 2.2 Katalog

| Tabel | | Isi |
|---|:-:|---|
| `it_categories` | ✓ | **Bidang** (Hardware, Software & SIMRS, Jaringan, Server, …). `coordinator_user_id`: satu koordinator per bidang; `is_triage`: bidang Triase (unik) untuk masalah di luar daftar. |
| `ticket_categories` | ✓ | **Jenis gangguan** bahasa awam ("Komputer tidak menyala", "Tidak bisa cetak"), terhubung ke bidang + `default_priority` |
| `action_templates` | ✓ | Teks tindakan siap pakai untuk petugas |
| `pending_reasons` | ＋ | Alasan tunda siap pakai + flag `pauses_sla` (penghentian SLA hanya bila dikonfigurasi) |
| `activity_types` | ＋ | Jenis kegiatan internal (Maintenance, Instalasi, Rapat, …) — bukan hardcode |

### 2.3 Perangkat, QR & pemeliharaan

| Tabel | | Isi | Integritas penting |
|---|:-:|---|---|
| `asset_categories` | ✓ | Komputer, Printer, Switch, … | – |
| `assets` | ✓ | Master perangkat | `asset_code` unik; field selain kode/nama/kategori opsional |
| `qr_codes` | ✓ | Token QR ruangan/perangkat | CHECK target sesuai tipe; UNIQUE generated → **satu QR aktif per ruangan/perangkat**; regenerate = nonaktifkan lama + buat baru (`generation`, `replaced_by_id`) |
| `asset_maintenance_histories` | ✓ | Timeline perbaikan/pemeliharaan perangkat | `repair_minutes` & `downtime_minutes` = generated column; UNIQUE (tiket, perangkat, siklus) mencegah entri ganda; CHECK sumber |
| `spare_parts` | ✓ | Katalog sparepart (tanpa stok) | – |
| `maintenance_parts` | ✓ | Sparepart per entri pemeliharaan | `line_cost` generated = qty × harga |
| `asset_alerts` | ＋ | Peringatan kerusakan berulang + keputusan koordinator | UNIQUE generated → maks satu alert terbuka per perangkat; CHECK alert ditutup wajib punya keputusan |

### 2.4 Tiket

| Tabel | | Isi | Integritas penting |
|---|:-:|---|---|
| `number_sequences` | ＋ | Penomoran TKT/KEG per tahun | Alokasi atomik `LAST_INSERT_ID(expr)` |
| `tickets` | ✓ | Tiket | CHECK: PENDING wajib alasan; RESOLVED/CLOSED wajib tindakan + hasil + `resolved_at`; status selain NEW/CANCELLED wajib `assignee_id`. Nilai SLA disalin saat dibuat. Token pelapor disimpan sebagai SHA-256. |
| `ticket_assignments` | ✓ | Riwayat penugasan (utama & pendukung) | UNIQUE generated → **maks satu PJ utama aktif per tiket**; user tidak dobel aktif |
| `ticket_histories` | ✓ | Semua kejadian tiket | Append-only (trigger); CHECK actor USER wajib `actor_user_id` |
| `ticket_comments` | ✓ | Komentar pelapor/petugas, catatan internal | Append-only |
| `attachments` | ✓ | Foto tiket/kegiatan/pemeliharaan | CHECK tepat satu pemilik; CHECK MIME jpeg/png/webp; `file_deleted_at` = file dihapus sesuai retensi (metadata tetap) |

### 2.5 Prioritas & SLA

| Tabel | | Isi | Integritas penting |
|---|:-:|---|---|
| `priority_rules` | ✓ | Aturan berurutan, kondisi opsional | CHECK rentang min ≤ max; `replacement_in` bertipe SET |
| `sla_policies` | ✓ | Target respons & penyelesaian | UNIQUE generated `match_key` → tidak ada dua kebijakan aktif untuk kombinasi sama; CHECK resolusi ≥ respons |
| `sla_calendars` | ✓ | Kalender kerja (24/7 atau jam kerja) | Maks satu kalender default |
| `sla_calendar_hours` | ＋ | Jam kerja per hari | CHECK hari 1–7, mulai < selesai (shift lewat tengah malam dipecah 2 baris) |
| `sla_holidays` | ＋ | Hari libur per kalender | UNIQUE (kalender, tanggal) |
| `sla_escalations` | ✓ | Kejadian peringatan/pelanggaran SLA | UNIQUE (tiket, jenis, siklus, level) → `INSERT IGNORE` idempoten |

### 2.6 Kegiatan internal

| Tabel | | Isi | Integritas penting |
|---|:-:|---|---|
| `internal_activities` | ✓ | Kegiatan non-tiket | `duration_minutes` generated; CHECK selesai ≥ mulai; status DONE wajib hasil |
| `activity_members` | ✓ | Pelaksana & pendukung | UNIQUE generated → satu pelaksana utama |

### 2.7 Notifikasi

| Tabel | | Isi | Integritas penting |
|---|:-:|---|---|
| `outbox_events` | ＋ | Event domain yang ditulis dalam transaksi bisnis | UNIQUE `dedupe_key`; retry via `attempts`/`next_attempt_at` |
| `notifications` | ✓ | Inbox in-app per user + status push | UNIQUE (user, event) |
| `push_subscriptions` | ✓ | Endpoint Web Push per perangkat | UNIQUE hash endpoint; hanya host push service resmi; nonaktif otomatis saat 404/410 |
| `push_deliveries` | ＋ | Pengiriman push per notifikasi × perangkat | UNIQUE (notifikasi, perangkat); retry, kedaluwarsa 1 jam |
| `notification_preferences` | ＋ | Event × role → inbox/push + cakupan | UNIQUE (role, event) |

### 2.8 Integrasi & sistem

| Tabel | | Isi |
|---|:-:|---|
| `integration_configs` | ✓ | Provider per entitas, jadwal, kebijakan konflik, percobaan ulang |
| `integration_imports` | ＋ | File CSV yang menunggu pratinjau → terapkan (isi dibuang setelah diterapkan/7 hari) |
| `integration_sync_logs` | ✓ | Hasil tiap sinkronisasi (jumlah tambah/ubah/lewati/konflik/gagal, error) |
| `external_id_mappings` | ✓ | `(source, entity, external_id) ↔ local_id`, hash payload terakhir |
| `audit_logs` | ✓ | Login, perubahan master/konfigurasi (before/after JSON) — append-only |
| `system_settings` | ✓ | Key-value bertipe (ambang kerusakan berulang, auto-close, batas upload, …) |
| `sessions` | ＋ | Session server-side (skema `scs` mysqlstore) |
| `backup_logs` | ＋ | Riwayat backup (file, ukuran, SHA-256, versi migrasi, jumlah baris tabel utama) & hasil uji restore |

---

## 3. Keputusan Desain Penting

### 3.1 Snapshot vs referensi

Tiket menyimpan **referensi** (`room_id`, `asset_id`, `ticket_category_id`) dan
**snapshot** nilai yang tidak boleh berubah mengikuti master:

- `it_category_id` (bidang saat itu), `critical_service` (status kritis unit saat itu)
- Nilai SLA (`sla_response_minutes`, `sla_resolution_minutes`, `sla_warn_pct`, `sla_allow_pause`, `sla_calendar_id`)
- `priority_rule_id` (aturan yang menghasilkan prioritas)

Dengan begitu laporan bulan lalu tidak berubah ketika admin mengubah kategori
atau kebijakan SLA hari ini.

### 3.2 Perangkat pindah ruangan

QR perangkat **tidak** menyimpan ruangan; ruangan diambil dari `assets.room_id`
saat dipindai. Tiket menyimpan `room_id` saat pengajuan, sehingga riwayat tetap
menunjukkan lokasi saat kejadian. Perpindahan dicatat sebagai
`asset_maintenance_histories.maintenance_type = 'RELOCATION'` + audit log.

### 3.3 Riwayat perangkat dibentuk otomatis

| Kejadian | Entri `asset_maintenance_histories` |
|---|---|
| Tiket ber-`asset_id` → RESOLVED | `CORRECTIVE`, `source=TICKET`, isi dari form penyelesaian; `ticket_cycle` = `tickets.sla_cycle` |
| Tiket dibuka ulang lalu RESOLVED lagi | Entri baru dengan siklus berikutnya (tidak menimpa) |
| Kegiatan internal ber-`asset_id` selesai | sesuai jenis kegiatan (`PREVENTIVE`/`INSPECTION`/`INSTALLATION`), `source=ACTIVITY` |
| Koordinator menutup `asset_alerts` | `ALERT_DECISION`, `source=ALERT` |
| Input manual koordinator (perbaikan di luar sistem) | `source=MANUAL` |

Statistik halaman perangkat:

| Statistik | Rumus |
|---|---|
| Jumlah kerusakan | `COUNT(tickets)` untuk perangkat (tidak termasuk CANCELLED) |
| Jumlah perbaikan | `COUNT(asset_maintenance_histories)` tipe CORRECTIVE |
| Gangguan berulang | tiket dalam jendela `repeat_damage.window_days` |
| Total downtime | `SUM(downtime_minutes)` |
| Total biaya | `SUM(service_cost) + SUM(maintenance_parts.line_cost)` |
| Perawatan terakhir | `MAX(finished_at)` |

### 3.4 Sparepart tanpa stok

Asumsi PHASE 0: `spare_parts` hanya katalog + harga acuan; tidak ada stok
masuk/keluar. Sparepart dicatat pada entri pemeliharaan perangkat. Tiket tanpa
perangkat terdaftar tidak mencatat sparepart (form penyelesaian akan mendorong
petugas menautkan perangkat).

### 3.5 Kolom `downtime_minutes`

Dihitung dari `problem_started_at` (perkiraan awal gangguan dari pelapor, atau
`tickets.created_at` bila kosong) sampai `finished_at` (= `service_restored_at`
atau `resolved_at`). Jika data awal tidak tersedia, kolom NULL — tidak dikarang.

---

## 4. Pola Query Kritis

### Antrean petugas

```sql
SELECT ... FROM tickets t
 WHERE t.status = 'NEW'
   AND (:only_mine_competency = 0 OR t.it_category_id IN (SELECT it_category_id FROM user_competencies WHERE user_id = :uid))
 ORDER BY FIELD(t.priority,'CRITICAL','HIGH','MEDIUM','LOW'), t.response_due_at, t.created_at
 LIMIT 50;
-- index: ix_tickets_queue (status, priority, created_at)
```

### Worker SLA (setiap menit)

```sql
SELECT id FROM tickets
 WHERE status IN ('NEW') AND response_due_at <= :now_plus_warning_window;      -- ix_tickets_resp_due
SELECT id FROM tickets
 WHERE status IN ('CLAIMED','IN_PROGRESS','PENDING') AND resolution_due_at <= :now_plus_warning_window; -- ix_tickets_reso_due
```

Persentase pemakaian dihitung di Go dengan kalender (bukan di SQL), lalu
`INSERT IGNORE INTO sla_escalations`.

### Kerusakan berulang

```sql
SELECT COUNT(*) FROM tickets
 WHERE asset_id = ? AND status <> 'CANCELLED' AND created_at >= ?;   -- ix_tickets_asset
```

### Beban kerja petugas

```sql
SELECT a.user_id, a.role, COUNT(*) FROM ticket_assignments a
  JOIN tickets t ON t.id = a.ticket_id
 WHERE a.released_at IS NULL AND t.status IN ('CLAIMED','IN_PROGRESS','PENDING')
 GROUP BY a.user_id, a.role;                                          -- ix_tassign_user
```

### Laporan koordinator (pengganti spreadsheet)

`UNION ALL` dua sumber dengan kolom identitas jenis pekerjaan:

```sql
SELECT 'TIKET' AS jenis, t.ticket_no AS nomor, DATE(CONVERT_TZ(t.resolved_at,'+00:00','+08:00')) AS tanggal, ...
  FROM tickets t WHERE t.resolved_at BETWEEN :from_utc AND :to_utc AND t.status IN ('RESOLVED','CLOSED')
UNION ALL
SELECT 'KEGIATAN', a.activity_no, a.activity_date, ...
  FROM internal_activities a WHERE a.activity_date BETWEEN :from_date AND :to_date AND a.status = 'DONE'
ORDER BY tanggal, nomor;
```

> `CONVERT_TZ` dengan offset numerik tidak butuh tabel zona waktu MySQL. WITA
> tidak memakai DST, sehingga `+08:00` aman. Rentang `:from_utc/:to_utc` tetap
> dihitung di Go dari tanggal WITA.

---

## 5. Konfigurasi Awal (seed, bukan data produksi palsu)

Hanya data **konfigurasi** yang dibutuhkan agar sistem dapat berjalan; semuanya
dapat diubah admin. Master data rumah sakit (unit, ruangan, perangkat, pegawai)
**tidak** di-seed.

| Data | Isi awal |
|---|---|
| `roles` | ADMIN, COORDINATOR, TECHNICIAN |
| Akun admin | Dibuat lewat `sikat create-admin` (interaktif), tidak ada password default di kode |
| `it_categories`, `ticket_categories`, `activity_types`, `pending_reasons` | Usulan daftar umum (dapat diedit sebelum go-live) — lihat pertanyaan terbuka |
| `sla_calendars` | "24 jam" (default) dan "Jam kerja" (Senin–Jumat 08:00–16:00, Sabtu 08:00–12:00) |
| `sla_policies` | Satu kebijakan per prioritas (angka awal — **perlu persetujuan**) |
| `priority_rules` | 5 aturan contoh di `ARCHITECTURE.md §6.2` |
| `notification_preferences` | Matriks default per role |
| `system_settings` | `repeat_damage.threshold=3`, `repeat_damage.window_days=90`, `ticket.auto_close_days=3`, `upload.max_mb=5`, `upload.max_files=3`, `backup.retention_days=14`, nama RS |

---

## 6. Retensi & Ukuran

Perkiraan beban RS tipe menengah: ratusan s.d. ~2.000 tiket/bulan → < 25 ribu
tiket/tahun; histori ~10 baris/tiket. Ini kecil untuk MySQL; index di atas
sudah cukup. Yang tumbuh paling cepat adalah **file foto** — dikompres di klien
(maks ~1600 px) dan di server. Kebijakan retensi foto (mis. hapus foto tiket
CLOSED > 2 tahun, metadata tetap) diputuskan bersama pihak RS.

**Implementasi (PHASE 9):** pengaturan `attachments.retention_days` (default 730,
0 = selamanya). Worker `retensi-lampiran` menghapus file foto tiket yang sudah
ditutup/dibatalkan lewat masa simpan dan mengisi `file_deleted_at`; foto
kegiatan & pemeliharaan tidak tersentuh. Uji beban PHASE 9: 20.000 tiket dan
54.000 histori — halaman utama p95 < 1 detik dengan 20 pengguna bersamaan.
