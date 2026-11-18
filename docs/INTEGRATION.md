# SIKAT — Integrasi Master Data & SIMRS

> **Keputusan 18/11/2026:** integrasi akan memakai **API SIMRS**. Menu
> *Administrasi › Integrasi* (impor CSV) disembunyikan dari navigasi; halamannya
> tetap ada di `/app/integrations` untuk Admin bila sewaktu-waktu dibutuhkan, dan
> mesin sinkron yang sama dipakai oleh adapter API nanti (§6).

Status: **siap integrasi (PHASE 8)**. SIKAT berjalan penuh tanpa SIMRS. Master data
(unit, ruangan, pegawai, perangkat) selalu tersimpan di database SIKAT; sumber luar
hanya mengisi atau memperbarui data lokal. Adapter SIMRS **belum dibuat** karena
spesifikasi koneksi SIMRS belum tersedia — sesuai prinsip "jangan membuat integrasi
palsu".

## 1. Prinsip

- Tidak mengubah data SIMRS, tidak ada foreign key ke database SIMRS.
- Pemetaan lewat `external_id_mappings (source, entity_type, external_id) → local_id`.
- Hasil sinkron disimpan lokal; waktu sinkron terakhir & error tercatat di
  `integration_sync_logs`.
- **Perubahan manual di SIKAT tidak ditimpa** tanpa keputusan admin (lihat §4).
- Data yang hilang dari sumber **tidak dihapus**, hanya dinonaktifkan.
- Sinkron manual oleh admin, sinkron terjadwal, dan percobaan ulang otomatis.

## 2. Sumber data per entitas

Menu **Administrasi › Integrasi** (Admin IT, permission `integration.manage`).

| Sumber | Status | Keterangan |
|---|---|---|
| Lokal | Tersedia (default) | Data dikelola di Master Data. `LocalProvider`: sinkron = tidak ada yang dilakukan, dinyatakan jujur. |
| Impor CSV | Tersedia | Unggah → pratinjau (tanpa menyimpan) → terapkan. |
| SIMRS (API) | Menunggu spesifikasi | Antarmuka siap (§6); adapter dibuat setelah dokumentasi API diterima. |
| SIMRS (database read-only) | Menunggu spesifikasi | Antarmuka siap (§6); adapter dibuat setelah akses & struktur view diterima. |

## 3. Impor CSV

1. Unduh template di halaman Integrasi (format UTF-8 + pemisah `;`, langsung rapi di Excel).
2. Isi data, simpan sebagai **CSV UTF-8**. Pemisah `,` atau `;` keduanya diterima.
3. Unggah, pilih kebijakan konflik dan apakah file berisi **data lengkap**.
4. Periksa pratinjau per baris (Ditambah / Diperbarui / Sama / Ditautkan / Konflik /
   Galat / Dinonaktifkan). Belum ada data yang berubah.
5. **Terapkan**. Baris konflik & galat dilewati; hasil tercatat di log dan jejak audit.

Urutan impor: **Unit → Ruangan → Pegawai → Perangkat** (referensi lebih dulu).
Maksimal 5 MB / 20.000 baris per file. File yang tidak diterapkan dibuang setelah 7 hari.

### Kolom

| Entitas | Kolom (wajib **tebal**) | Catatan |
|---|---|---|
| Unit | **external_id**, **kode**, **nama**, layanan_kritis, aktif | `layanan_kritis`/`aktif`: ya/tidak |
| Ruangan | **external_id**, **kode**, **nama**, **unit**, lokasi, aktif | `unit` = external_id unit di sumber yang sama, atau kode unit SIKAT |
| Pegawai | **nip**, **nama**, unit, jabatan, telepon, aktif | NIP dipakai sebagai external_id. Direktori, bukan akun login. |
| Perangkat | **external_id**, **kode_aset**, **nama**, **kategori**, unit, ruangan, merek, model, nomor_seri | `kategori` = kode/nama kategori perangkat yang sudah ada; status perangkat tidak disinkronkan |

## 4. Aturan sinkronisasi

| Situasi | Hasil |
|---|---|
| `external_id` baru, tidak ada data lokal berkode sama | Ditambah (sumber = CSV/SIMRS) + pemetaan |
| `external_id` baru, ada data lokal berkode sama dan isinya sama | **Ditautkan** |
| `external_id` baru, ada data lokal berkode sama tetapi isinya berbeda | **Konflik** (data lokal tidak diubah, tidak ditautkan) — kecuali kebijakan "Utamakan data sumber" |
| Sudah dipetakan, sumber tidak berubah sejak sinkron terakhir | Sama (walau data lokal diedit) |
| Sudah dipetakan, sumber berubah, data lokal tidak diedit | Diperbarui |
| Sudah dipetakan, sumber berubah, **data lokal diedit manual setelah sinkron terakhir** (`local_modified_at > last_synced_at`) | **Konflik** — data lokal dipertahankan, kecuali "Utamakan data sumber" |
| Data lengkap: ada di pemetaan tetapi tidak ada di sumber | Dinonaktifkan (`missing_since`); perangkat hanya ditandai. Bila data lokal diedit → konflik |
| Muncul kembali di sumber | Diperbarui & aktif kembali |

Perubahan manual (edit atau aktif/nonaktif di Master Data/Perangkat) pada data yang
berasal dari sumber luar otomatis menandai `local_modified_at`.

**Menyelesaikan konflik:** bila data sumber yang benar, impor/sinkron ulang dengan
kebijakan "Utamakan data sumber"; bila data SIKAT yang benar, tidak perlu tindakan
(konflik akan muncul lagi di log selama sumber berbeda).

## 5. Sinkron terjadwal & percobaan ulang

Worker `sinkron-master` (tiap menit) menjalankan entitas bersumber SIMRS yang
aktif bila jadwalnya (`schedule_minutes`) jatuh tempo. Gagal mengambil data →
log `FAILED`, dicoba lagi otomatis dengan jeda 5, 10, 20, … menit sampai
`max_retries` (default 3); setelah itu menunggu jadwal berikutnya atau sinkron
manual. Berhasil → `retry_count` direset. Selama semua entitas bersumber lokal,
worker tidak melakukan apa pun.

## 6. Menambahkan adapter SIMRS

Antarmuka (`internal/integration/provider.go`):

```go
type Provider interface {
	Kind() string   // integration.ProviderSimrsAPI atau ProviderSimrsDB
	Source() string // label pemetaan, mis. "SIMRS"
}
type OrganizationProvider interface { Departments(ctx context.Context) ([]Department, error) }
type RoomProvider         interface { Rooms(ctx context.Context) ([]Room, error) }
type EmployeeProvider     interface { Employees(ctx context.Context) ([]Employee, error) }
type AssetProvider        interface { Assets(ctx context.Context) ([]Asset, error) }
```

Langkah:

1. Buat `SimrsApiProvider` / `SimrsDatabaseProvider` yang mengimplementasikan
   `Provider` dan sebagian/semua antarmuka entitas (kembalikan data **lengkap**
   setiap kali; penonaktifan data hilang ditangani SIKAT).
2. Daftarkan di `internal/server/server.go`: `integSvc.RegisterProvider(p)`.
3. Admin memilih sumber SIMRS & jadwal di halaman Integrasi; uji dengan
   **Sinkron sekarang** dan periksa log.
4. Tambahkan test dengan server API tiruan / database uji (lihat
   `TestScheduledSyncRetriesAndLogs` untuk pola retry).

Ketentuan:

- **Database:** user read-only, akses ke view khusus (bukan tabel transaksi SIMRS),
  query ber-paging dengan `LIMIT`, timeout pendek, dijadwalkan di luar jam sibuk.
- **API:** HTTPS, timeout pendek, paging, token khusus SIKAT dengan hak baca saja.
- Kredensial dari environment (`.env` permission 600), tidak pernah di repository
  atau log. Bila kelak disimpan di database: dienkripsi AES-GCM dengan kunci dari env.

### Informasi yang dibutuhkan dari tim SIMRS

- Metode akses (REST API atau view database) dan lingkungan uji (staging).
- Untuk API: URL, autentikasi, format respons, paging, batas laju.
- Untuk database: jenis DBMS & versi, host/jaringan, user read-only, nama view.
- Untuk tiap entitas: kolom ID tetap (external_id), kode, nama, relasi unit/ruangan,
  status aktif, dan contoh data.
- Kebijakan: frekuensi sinkron yang diizinkan, jam tenang, kontak teknis.
