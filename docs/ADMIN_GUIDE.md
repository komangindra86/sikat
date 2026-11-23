# SIKAT — Panduan Admin IT

Admin IT mengelola akun, master data, konfigurasi, backup, dan keamanan. Admin
tidak menangani tiket (Q6); gunakan akun terpisah bila perlu ikut menangani.
Instalasi server: DEPLOYMENT.md.

## 1. Langkah awal (urutan disarankan)

1. **Pengaturan** → nama rumah sakit, kontak IT, batas login, upload, tutup
   otomatis, ambang kerusakan berulang, masa simpan foto.
2. **Master Data** (urutan): Unit → Ruangan → Bidang (tetapkan **koordinator** tiap
   bidang) → Jenis Gangguan → Kategori Perangkat → Perangkat (menu Daftar
   Perangkat) → Pegawai (opsional, saran nama pelapor). Kode (mis. `UNT-001`,
   `RNG-0001`, kode inventaris `PRK-2026-0001`) dibuat otomatis saat disimpan dan
   tidak dapat diubah; cukup isi nama dan data lainnya.
3. **Pengguna** → buat akun petugas & koordinator (password sementara, wajib
   diganti saat login pertama), centang **kompetensi** (bidang) petugas.
4. **QR Code** → *Buat semua QR ruangan* / QR perangkat → cetak → tempel.
   Pastikan alamat aplikasi (`SIKAT_BASE_URL`) sudah final sebelum mencetak.
5. **Backup** → *Backup sekarang* lalu *Uji restore*; pastikan salinan off-site berjalan.

## 2. Pengguna & hak akses

| Role | Dapat |
|---|---|
| Admin IT | Akun, master data, QR, konfigurasi (SLA, prioritas, notifikasi), backup, jejak audit, laporan semua |
| Koordinator IT | Semua tiket (fokus bidangnya), ambil alih/prioritas/batal/tutup tiket bidangnya, SLA, perangkat sering rusak, laporan semua |
| Petugas IT | Antrean & tiket sendiri, kegiatan internal, laporan pribadi |

- **Reset password**: Pengguna → ubah → *Reset password*. Password sementara wajib
  diganti; semua session lama pengguna tersebut langsung berakhir.
- **Nonaktifkan** akun pegawai yang keluar (bukan dihapus) — session-nya berakhir
  otomatis dan histori tetap utuh.
- Akun terkunci karena gagal login terbuka otomatis setelah lama kunci (Pengaturan).

## 3. Konfigurasi tiket

| Menu | Isi |
|---|---|
| Master Data › Jenis Gangguan | Pilihan bahasa awam untuk pelapor + bidang + prioritas bawaan. "Lainnya" menuju bidang **Triase**. |
| Master Data › Aturan Prioritas | Aturan berurutan (dampak, cakupan, perangkat pengganti, layanan kritis). Uji di *Uji aturan*. |
| Master Data › Alasan Tunda | Termasuk tanda "Menghentikan SLA" |
| Master Data › Template Tindakan | Teks tindakan siap pakai petugas |
| Master Data › Kebijakan SLA & Kalender Kerja | Target respons/penyelesaian per prioritas (bisa khusus bidang/jenis/unit), jam kerja, hari libur |
| Pengaturan › Preferensi notifikasi | Kejadian per role: tidak dikirim / inbox / inbox + push |

Gunakan laporan **Pemilahan jenis gangguan** secara berkala: keterangan pelapor
yang memilih "Lainnya" menunjukkan jenis gangguan baru yang perlu ditambahkan.

## 4. Backup & pemulihan

- Menu **Backup**: riwayat backup harian (waktu, ukuran, SHA-256), *Backup
  sekarang*, dan *Uji restore* (memulihkan ke database sementara lalu
  membandingkan isinya). Lakukan uji restore minimal sebulan sekali dan setelah
  update versi.
- Dashboard Admin menandai bila backup terakhir gagal atau lebih dari sehari lalu.
- Foto lampiran tidak termasuk backup database — pastikan folder foto ikut disalin
  off-site (DEPLOYMENT.md §5).

## 5. Pemantauan sistem

Dashboard Admin → **Status sistem**: database & versi migrasi, worker latar
belakang, antrean notifikasi, Web Push, koordinator bidang, integrasi SIMRS,
backup. Kuning/merah = perlu perhatian.

**Jejak Audit**: login, perubahan master data/konfigurasi/pengguna, ekspor laporan,
impor, backup — tidak dapat diubah atau dihapus.

## 6. Integrasi SIMRS

Direncanakan lewat **API SIMRS** (menunggu spesifikasi). Master data dikelola di
SIKAT sampai adapter tersedia. Rincian, termasuk informasi yang perlu diminta dari
tim SIMRS: INTEGRATION.md. Halaman impor CSV tersedia di `/app/integrations`
(tidak tampil di menu) bila sewaktu-waktu dibutuhkan untuk data massal.

## 7. Keamanan & privasi

- Jangan menyimpan data pasien/rekam medis di SIKAT; formulir sudah memperingatkan
  pelapor. Hapus foto yang terlanjur memuat data pasien (Pengguna dengan hak
  terkait dapat menyembunyikan dari pelapor; laporkan ke Admin untuk penghapusan).
- Foto tiket dihapus otomatis setelah masa simpan (default 2 tahun setelah tiket
  ditutup); data tiket tetap.
- Rahasia (DSN, kunci VAPID) hanya di file `.env` server (izin 600).
- Ringkasan audit keamanan: SECURITY.md.
