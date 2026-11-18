# SIKAT — Implementation Plan, Asumsi & Risiko

> Status (18 November 2026): **PHASE 0–8 selesai. PHASE 9 selesai, menunggu validasi.**
> Default yang dipakai karena belum dijawab: Q1 (VPS publik + HTTPS), Q3 (tutup
> otomatis 3 hari), Q4 (pencatatan pemeliharaan saja, tanpa jadwal preventif),
> Q6 (Admin IT tidak menangani tiket), **Q7 (kolom laporan koordinator sesuai master
> prompt §18 — mohon contoh spreadsheet lama bila ada kolom lain yang dibutuhkan)**.
> Diputuskan: Q5 (petugas boleh mencatat tiket atas nama pelapor), Q2 (angka SLA
> usulan disetujui; kalender "Jam kerja" Senin–Jumat 08.00–16.00 WITA, "24 jam" untuk Kritis),
> koordinator per bidang (13/11/2026: tepat satu koordinator per bidang; fokus pada
> bidangnya tetapi tetap dapat melihat semua; bidang tanpa koordinator → semua koordinator),
> triase (16/11/2026: masalah di luar daftar → bidang Triase, dipilah dengan koreksi
> jenis gangguan; laporan pemilahan untuk menambah jenis gangguan baru).
>
> Hasil PHASE 9: audit keamanan OWASP Top 10 (SECURITY.md) dengan 2 perbaikan
> (session berakhir saat password diganti/direset; allowlist host Web Push mencegah
> SSRF). Uji beban 20.000 tiket / 20 pengguna bersamaan tanpa error, p95 halaman
> utama < 1 detik. Backup harian + **uji restore otomatis** diuji end-to-end dengan
> mysqldump/mysql 8.0.30; retensi foto (Q9, default 2 tahun) diuji. Paket deploy
> Linux (build statis diverifikasi), systemd, Nginx/HTTPS, DEPLOYMENT/USER_GUIDE/
> ADMIN_GUIDE. Belum dijalankan: `govulncheck` dan `go test -race` (lihat SECURITY.md §4);
> deploy ke server Linux sungguhan menunggu server tersedia.
>
> Hasil PHASE 8: 127 automated test lulus (MySQL 8.0.30 lokal), termasuk kriteria
> rencana: **sinkron tidak menimpa perubahan lokal** (konflik dicatat; hanya ditimpa
> dengan kebijakan "Utamakan data sumber") dan **gagal → retry → tercatat** (provider
> SIMRS tiruan gagal 2×, berhasil pada percobaan ke-3; batas percobaan ulang dihormati).
> Impor CSV unggah → pratinjau → terapkan diuji lewat HTTP. Adapter SIMRS API/DB
> **belum dibuat** (spesifikasi belum tersedia; tidak ada integrasi palsu) — antarmuka,
> jadwal, dan retry siap; panduan di INTEGRATION.md. Keputusan 18/11/2026: integrasi
> memakai API SIMRS; menu Integrasi (impor CSV) disembunyikan dari navigasi.
>
> Hasil PHASE 7: 115 automated test lulus (MySQL 8.0.30 lokal), termasuk batas bulan
> WITA (tiket 00.30 WITA tanggal 1 masuk bulan baru walau UTC masih bulan lalu),
> kalkulasi waktu respons/penyelesaian, rekap per bidang/petugas/tertunda/beban kerja,
> laporan koordinator (tiket + kegiatan, kolom sesuai format), cakupan data pribadi
> petugas, file Excel yang dapat dibuka ulang (angka tetap angka) dan PDF multi-halaman.
> 15 laporan × 3 jenis periode × HTML/Excel/PDF diuji terhadap data dev (135 kombinasi);
> dashboard per role dan contoh PDF diperiksa visual.
> Penyesuaian dari rencana: ekspor dibuat langsung (sinkron) dengan batas 20.000 baris
> — volume data RS kecil, sehingga antrean worker belum diperlukan; PDF memakai
> `go-pdf/fpdf` (lihat docs/ARCHITECTURE.md).
> Belum diuji: race detector (`go test -race` butuh compiler C di Windows) dan
> build/deploy di Linux (PHASE 9).

---

## 1. Hasil Pemeriksaan Direktori (PHASE 0)

| Temuan | Dampak |
|---|---|
| `C:\laragon\www\Sikat` hanya berisi `Master Prompt.txt`; belum ada source code | Proyek baru; tidak ada file yang ditimpa |
| Folder berada di dalam repo git `C:\laragon\www` yang meng-ignore semua subfolder (`/*`) | SIKAT perlu **repo git sendiri** (`git init` di awal PHASE 1) |
| Go 1.27.3 terpasang | Memenuhi kebutuhan |
| MySQL 8.0.30 (Laragon) berjalan, `sql_mode` strict | Dipakai untuk development & test; skema draft sudah tervalidasi di sini |
| Node.js tersedia | Opsional; Tailwind memakai standalone CLI agar server produksi tidak butuh Node |
| `sqlc`, `goose`, `mysql` CLI tidak ada di PATH | Tidak masalah: goose dipakai sebagai library; MySQL CLI ada di folder Laragon |

### Validasi skema yang sudah dilakukan

`docs/schema-draft.sql` dijalankan ke database sementara `sikat_phase0_check`
(kemudian dihapus). Hasil:

- 42 tabel + 8 trigger terbentuk tanpa error (setelah 2 perbaikan: kata tercadang
  `last_value`, dan `ON DELETE CASCADE` yang dilarang pada kolom dasar generated column).
- 24 uji pelanggaran **ditolak** DB sesuai harapan, antara lain: dua PJ utama aktif,
  klaim kedua (0 baris), QR aktif ganda, PENDING tanpa alasan, RESOLVED tanpa
  tindakan, ubah/hapus histori, hapus tiket, lampiran dua pemilik, eskalasi duplikat,
  alert terbuka ganda, dua kebijakan SLA aktif identik.
- Kasus valid **lolos**: nomor urut 1→2, regenerate QR, ambil alih tiket, durasi
  kegiatan otomatis (95 menit), downtime/repair generated, biaya sparepart, pencocokan
  aturan prioritas dengan `SET`.

Uji konkurensi sungguhan (goroutine paralel) baru bisa dilakukan setelah kode
Go ada — dijadwalkan di PHASE 2.

---

## 2. Rencana Per Phase

Setiap phase ditutup dengan: implementasi → dijalankan → `go test ./...` hijau →
perbaikan → dokumentasi diperbarui → laporan. Phase berikutnya hanya dimulai
setelah divalidasi pemilik proyek.

### PHASE 1 — Foundation

| Deliverable | Kriteria diterima |
|---|---|
| `git init`, `go.mod`, struktur folder §22, `Makefile`/skrip build | `go build ./...` sukses |
| Config dari env + validasi saat start | Start gagal dengan pesan jelas bila variabel wajib kosong |
| Koneksi DB (UTC), helper transaksi, migrasi goose ter-embed | `sikat migrate up/down/status` berjalan di DB kosong |
| Migrasi `0001` = tabel organisasi, katalog, perangkat, QR, sistem, audit | Sesuai `schema-draft.sql` |
| Auth: login/logout, Argon2id, session MySQL, rotasi session, lockout, ganti password pertama kali | Test auth hijau |
| RBAC middleware + matriks permission | Test: tiap role hanya bisa akses rute yang diizinkan |
| CSRF, security headers, rate limit, recover, request log | Test CSRF & rate limit |
| `sikat create-admin` (interaktif) | Tidak ada password default di kode |
| Layout responsif (Tailwind + HTMX), komponen: tombol, kartu, badge status, toast, form error | Tampil benar di 360 px / 768 px / 1280 px |
| CRUD master: unit, ruangan, bidang, jenis gangguan, kategori perangkat, perangkat, alasan tunda, jenis kegiatan, template tindakan, sparepart, pengguna + kompetensi, pengaturan sistem | Tiap aksi tercatat di `audit_logs`; master terpakai tidak bisa dihapus, hanya dinonaktifkan |
| Dokumen: README, INSTALLATION, CHANGELOG awal | Langkah instalasi bisa diikuti dari nol |

### PHASE 2 — QR Ticketing

| Deliverable | Kriteria diterima |
|---|---|
| Generate QR (single & massal), preview, unduh PNG, ZIP, halaman cetak, nonaktifkan, regenerate | Token acak 22 karakter; QR lama mati setelah regenerate |
| Halaman publik `/q/{token}` + form pengajuan (kompresi foto di klien) | Lokasi/perangkat terisi otomatis; QR nonaktif → pesan ramah |
| Pembuatan tiket: nomor atomik, token pelapor (hash), lampiran tervalidasi + re-encode | Test penomoran paralel: tidak ada nomor ganda |
| Priority engine + halaman admin aturan + "Uji aturan" | Test tabel kasus contoh master prompt §9 |
| Halaman konfirmasi + pemantauan `/t/{token}` | Token salah → 404; tidak membocorkan tiket lain |
| Antrean bersama + filter kompetensi + klaim atomik | **Test kritis: 2–20 goroutine klaim tiket yang sama → tepat 1 berhasil** |
| Pencatatan tiket oleh petugas atas nama pelapor (sumber Telepon / WA / Datang langsung; channel PHONE/MESSAGE/WALK_IN) | Disetujui (Q5, 11/11/2026). Tiket tercatat `created_by_user_id`, masuk antrean, prioritas & SLA sama seperti tiket QR |
| Migrasi `0002` = tiket, penugasan, histori, komentar, lampiran, prioritas, SLA (struktur) | – |

### PHASE 3 — Work Management

| Deliverable | Kriteria diterima |
|---|---|
| State machine status + tombol sesuai transisi yang sah | Test semua transisi sah & tidak sah |
| Tunda (alasan wajib), selesaikan (tindakan + hasil wajib), lepas, buka ulang, batal | CHECK DB + validasi service |
| Catatan/komentar (internal & publik), foto | Pelapor tidak melihat catatan internal |
| Anggota pendukung, ambil alih koordinator, ubah prioritas beralasan | Semua tercatat di histori |
| Konfirmasi pelapor + auto-close N hari (worker sederhana) | Test auto-close idempoten |
| Kegiatan internal + anggota + lampiran + nomor KEG | Durasi otomatis; masuk data laporan |

### PHASE 4 — Asset Maintenance

| Deliverable | Kriteria diterima |
|---|---|
| Detail perangkat: identitas, riwayat gabungan (tiket + kegiatan + manual), statistik | Angka cocok dengan data uji |
| Entri riwayat otomatis saat tiket RESOLVED / kegiatan selesai | Tidak ada entri ganda saat retry/buka ulang |
| Sparepart & biaya pada penyelesaian | Total biaya benar |
| Deteksi kerusakan berulang + rekomendasi berbasis aturan + keputusan koordinator | Test ambang & jendela waktu; satu alert terbuka per perangkat |
| (Opsional) jadwal preventive maintenance | Bila disetujui |

### PHASE 5 — SLA & Eskalasi

| Deliverable | Kriteria diterima |
|---|---|
| Admin: kalender, jam kerja, hari libur, kebijakan SLA | Tidak ada angka SLA di kode selain seed konfigurasi |
| `sla.Calculator` murni | **Test: jam operasional, lintas akhir pekan, hari libur, mulai di luar jam kerja, 24/7, pause** |
| Penetapan due date saat tiket dibuat / prioritas berubah / tunda-lanjut | – |
| Worker SLA + `sla_escalations` idempoten + leader lock | **Test: worker dijalankan berulang → notifikasi tidak dobel** |
| Tampilan SLA: sisa waktu, persentase, aman/peringatan/terlambat; dashboard SLA | – |

### PHASE 6 — PWA & Notifikasi

| Deliverable | Kriteria diterima |
|---|---|
| Manifest, ikon, service worker (cache aset statis, halaman offline) | Lighthouse "installable" |
| Outbox → inbox → Web Push (VAPID), retry, nonaktifkan endpoint 404/410 | Test dedupe & retry |
| Kelola subscription per perangkat, preferensi notifikasi per role | – |
| Payload lock screen generik | Test: payload tidak memuat lokasi/deskripsi/nama |

### PHASE 7 — Dashboard & Reporting

| Deliverable | Kriteria diterima |
|---|---|
| Dashboard petugas/koordinator/admin dari query DB | Angka cocok dengan data uji |
| 14 laporan master prompt §18 + filter | Test kalkulasi laporan (batas bulan WITA) |
| Laporan koordinator (pengganti spreadsheet) | Kolom sesuai format lama |
| Export Excel & PDF (export besar via worker) | File terbuka benar di Excel / PDF reader |

### PHASE 8 — SIMRS Integration Ready

| Deliverable | Kriteria diterima |
|---|---|
| Interface provider + `LocalProvider` + `CsvImportProvider` (pratinjau → terapkan) | Aplikasi jalan tanpa SIMRS |
| `external_id_mappings`, sync log, retry, sync manual, halaman konfigurasi | **Test: sync tidak menimpa perubahan lokal yang belum diselesaikan; gagal → retry → tercatat** |
| Adapter API/DB SIMRS | Hanya bila spesifikasi SIMRS tersedia (tidak membuat integrasi palsu) |
| INTEGRATION.md | – |

### PHASE 9 — Production Ready

Security audit (OWASP checklist, cek dependensi `govulncheck`), uji beban
ringan, backup terjadwal + **uji restore**, deployment (systemd + Nginx + HTTPS),
dokumentasi lengkap (§26), USER_GUIDE & ADMIN_GUIDE.

---

## 3. Strategi Pengujian

| Lapisan | Cara |
|---|---|
| Logika murni (prioritas, SLA calculator, state machine, rekomendasi) | Unit test tabel kasus, tanpa DB |
| Repository & transaksi | Integration test ke MySQL test DB (`SIKAT_TEST_DSN`), skema dibuat dari migrasi, data dibersihkan per test |
| Konkurensi (klaim, nomor tiket, eskalasi) | Goroutine paralel ke MySQL nyata |
| HTTP (auth, RBAC, CSRF, token publik) | `httptest` terhadap router lengkap |
| Worker | Jalankan job berulang dengan jam palsu (`Clock` interface) |
| UI | Pemeriksaan manual di browser pada 3 ukuran layar tiap phase |

Waktu di seluruh service memakai interface `Clock` agar test SLA/eskalasi tidak
bergantung jam sistem.

---

## 4. Asumsi

Bila salah satu asumsi keliru, mohon dikoreksi sebelum phase terkait.

1. Satu rumah sakit, satu instance aplikasi, satu database (bukan multi-tenant).
2. Satu user = satu role.
3. Petugas login dengan username + password lokal (belum SSO/LDAP).
4. Pelapor tidak punya akun; identitas = nama + unit (+ kontak opsional).
5. Sparepart hanya katalog + harga acuan, **tanpa** manajemen stok.
6. Seed hanya konfigurasi (role, kalender, SLA awal, aturan prioritas awal, daftar kategori usulan). Unit, ruangan, perangkat, pegawai diinput admin / impor CSV.
7. Bahasa antarmuka hanya Bahasa Indonesia.
8. Zona waktu tampilan Asia/Makassar (WITA, tanpa DST).
9. Volume: < 2.000 tiket/bulan, < 50 petugas IT, < 5.000 perangkat.
10. Server memiliki akses internet keluar (untuk Web Push & salinan backup off-site).
11. Lampiran hanya foto (JPEG/PNG/WebP), maks 5 MB, maks 3 per aksi.
12. Pelapor tidak dapat membatalkan tiket sendiri; pembatalan oleh koordinator/admin dengan alasan.

---

## 5. Risiko & Mitigasi

| # | Risiko | Dampak | Mitigasi |
|---|---|---|---|
| R1 | Server tidak terjangkau dari HP pegawai (intranet-only / firewall RS) | QR tidak bisa dipakai di luar Wi-Fi RS | Putuskan topologi jaringan sejak awal (pertanyaan Q1); domain + HTTPS valid |
| R2 | Web Push di iOS hanya jalan bila PWA di-install (iOS 16.4+); sebagian HP memblokir notifikasi | Petugas melewatkan notifikasi | Inbox in-app + badge + polling dashboard; push hanya pelengkap; panduan install di USER_GUIDE |
| R3 | Data pasien ikut terunggah (foto layar SIMRS, nama pasien di deskripsi) | Pelanggaran privasi | Peringatan di form, tidak ada field data pasien, strip EXIF, akses foto dibatasi, kebijakan retensi |
| R4 | Spam/penyalahgunaan form publik | Antrean kotor | Wajib QR token valid, rate limit per IP & per QR, honeypot field, koordinator bisa batalkan + nonaktifkan QR |
| R5 | Kesalahan kalkulasi SLA (libur, lintas hari, pause) | Laporan SLA tidak dipercaya | Calculator murni + test tabel luas + snapshot nilai SLA di tiket |
| R6 | Petugas tetap menerima permintaan via telepon/WA tanpa tiket | Laporan tidak lengkap | Fitur catat tiket atas nama pelapor (channel PHONE/MESSAGE/WALK_IN) — disetujui, dibangun di PHASE 2 |
| R7 | Laporan tidak sesuai format spreadsheet lama | Koordinator tetap mengetik manual | Minta contoh spreadsheet sebelum PHASE 7 |
| R8 | Spesifikasi SIMRS belum diketahui | Integrasi tertunda | Integrasi bukan ketergantungan; CSV import sebagai jalur awal |
| R9 | Single VPS = single point of failure | Aplikasi mati saat VPS bermasalah | Backup harian + off-site + uji restore terdokumentasi; RTO/RPO disepakati |
| R10 | Library PDF Go terbatas untuk tabel kompleks | Export PDF kurang rapi | Dievaluasi awal PHASE 7; fallback library lain |
| R11 | Rate limit in-memory tidak berlaku lintas instance | Bila kelak di-scale horizontal | Didokumentasikan; untuk 1 instance sudah cukup |
| R12 | Dev di Windows, produksi Linux (path, `mysqldump`, izin file) | Bug hanya muncul di server | Path via `filepath`, konfigurasi path eksplisit, uji deploy di VM Linux sebelum go-live |
| R13 | Aturan prioritas salah dikonfigurasi admin | Tiket kritis dinilai rendah | Halaman "Uji aturan", audit log perubahan aturan, koordinator dapat override |

---

## 6. Pertanyaan Terbuka (keputusan bisnis)

Menurut aturan master prompt, keputusan bisnis yang ambigu ditanyakan sebelum
diimplementasikan. **PHASE 1 tidak bergantung pada pertanyaan ini**, kecuali Q1
yang memengaruhi konfigurasi deployment.

| # | Pertanyaan | Dibutuhkan sebelum | Usulan default |
|---|---|---|---|
| Q1 | Aplikasi di VPS internet publik (bisa diakses dari data seluler) atau server intranet RS? | PHASE 1 (config) / PHASE 6 | VPS publik + HTTPS, sesuai master prompt |
| Q2 | Apakah layanan IT 24 jam (on-call) atau jam kerja saja? Angka SLA awal disetujui? | PHASE 5 | **Diputuskan: disetujui (12/11/2026)** sebagai konfigurasi awal, dapat diubah admin. Kritis: respons 15 mnt / selesai 4 jam (24 jam) · Tinggi: 30 mnt / 8 jam kerja · Sedang: 2 jam / 16 jam kerja · Rendah: 8 jam kerja / 40 jam kerja |
| Q3 | Konfirmasi pelapor wajib, atau tiket otomatis ditutup N hari setelah Selesai? | PHASE 3 | Otomatis ditutup 3 hari bila pelapor tidak merespons |
| Q4 | Perlu **jadwal** preventive maintenance (jadwal berkala + pengingat), atau cukup pencatatan? | PHASE 4 | Pencatatan dulu; jadwal sebagai tambahan setelah PHASE 7 |
| Q5 | Bolehkah petugas mencatat tiket atas nama penelepon (telepon/WA/datang langsung)? | PHASE 2 | **Diputuskan: boleh** (11/11/2026). Ditandai sumber "Telepon/WA/Langsung" |
| Q6 | Apakah Admin IT juga menangani tiket? (memengaruhi model 1 user = 1 role) | PHASE 1 | Tidak; bila ya, gunakan akun terpisah |
| Q7 | Mohon contoh spreadsheet laporan koordinator yang dipakai sekarang | PHASE 7 | **Dipakai default (13/11/2026):** kolom sesuai master prompt §18; kolom tambahan dapat ditambahkan setelah contoh diterima |
| Q8 | Daftar awal bidang & jenis gangguan yang biasa dipakai tim IT | PHASE 1 (seed) | Hardware, Software & SIMRS, Jaringan, Server & Infrastruktur, Printer, Akun & Akses |
| Q9 | Berapa lama foto lampiran disimpan? | PHASE 9 | **Dipakai default (18/11/2026):** 2 tahun setelah tiket ditutup, metadata tetap; dapat diubah di Pengaturan › Lampiran |

---

## 7. Dokumen PHASE 0

| Dokumen | Isi |
|---|---|
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | Arsitektur, stack, struktur proyek, keamanan, RBAC, domain inti, worker, integrasi |
| [`ERD.md`](ERD.md) | Diagram relasi per domain |
| [`DATABASE.md`](DATABASE.md) | Konvensi, katalog tabel, keputusan desain, pola query, seed |
| [`schema-draft.sql`](schema-draft.sql) | DDL tervalidasi (bahan migrasi PHASE 1) |
| [`UI_WIREFRAMES.md`](UI_WIREFRAMES.md) | Wireframe pelapor, petugas, koordinator, admin |
| `IMPLEMENTATION_PLAN.md` | Dokumen ini |

Dokumen §26 lainnya (INSTALLATION, USER_GUIDE, ADMIN_GUIDE, DEPLOYMENT,
INTEGRATION, CHANGELOG) dibuat mulai PHASE 1 seiring implementasi, supaya isinya
sesuai kode yang benar-benar ada.
