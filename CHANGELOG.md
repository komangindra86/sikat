# Changelog

Format mengikuti [Keep a Changelog](https://keepachangelog.com/id/1.1.0/).

## Belum dirilis

### Ditambahkan
- **Buat link pemantauan baru** di detail tiket (kartu Pelapor), untuk pelapor yang kehilangan link.
  - Dapat dilakukan oleh penanggung jawab, petugas pendukung, petugas yang mencatat tiket, atau koordinator bidangnya.
  - Link lama langsung tidak berlaku. Link baru ditampilkan sekali beserta tombol Salin dan Bagikan.
  - Tercatat di histori tiket (pelapor melihat "Link pemantauan diperbarui") dan jejak audit (`ticket.link_renew`).

### Diubah
- **Bidang mengikuti struktur 6 koordinator** (migrasi `00016_bidang_fungsi`):
  - Pelayanan & Support (dulu Hardware, Printer, Jaringan; juga memilah Triase), ERM (dulu Software & SIMRS), Keamanan Sistem (dulu Akun & Akses, Server & Infrastruktur).
  - Bidang baru: Administrasi & Inventaris, Mutu, System Developer.
  - Tiket, kegiatan, kompetensi petugas, jenis gangguan, aturan prioritas, kebijakan SLA, template, dan kategori perangkat pindah ke bidang barunya. Bidang lama dinonaktifkan, kodenya tetap.
  - Jenis gangguan baru: permintaan & pemindahan perangkat, usulan sistem, penambahan form, perbaikan sistem. Jenis kegiatan baru untuk Administrasi & Inventaris, Mutu, dan System Developer.
  - Down tetap berhasil bila jenis gangguan baru sudah dirujuk tiket (termasuk jenis pilihan pelapor) atau riwayat pemeliharaan. Jenis dan bidangnya dibiarkan nonaktif, lalu Up berikutnya mengaktifkannya lagi (temuan uji tim server).
  - Saat diuji pada salinan produksi, checksum bidang di `deploy/verify-uniform-codes.sql` bagian 1 memang berubah (perpindahan rujukan ini).
- **Tampilan standar Casemix** (`tampilan/tampilan-casemix`):
  - Token warna dengan tiga tema (Daun, Samudra, Mawar), mode gelap, dan kepadatan Biasa/Padat, dipilih per browser lewat bilah atas.
  - Font Geist dan ikon Phosphor.
  - Badge status berupa pil dengan ikon berbentuk.
  - Halaman masuk dua kolom.
  - Sidebar tetap di kiri dan terbuka bawaan. Bisa ditutup (diingat per browser); di HP menjadi laci.
- **Kontrol form gaya shadcn**: select, tanggal/bulan/tanggal-jam (kalender), pilih file, centang, dan radio tidak lagi memakai tampilan bawaan browser.
  - Elemen asli tetap dikirim bersama form (`static/js/kontrol.js`).
  - `confirm()`/`alert()` diganti dialog dan notifikasi.
- **Kode otomatis**: kode Unit (`UNT-001`), Ruangan (`RNG-0001`), Bidang (`BDG-01`),
  Kategori Perangkat (`KAT-01`), Alasan Tunda (`ALS-01`), Sparepart (`SPR-0001`), dan
  kode inventaris Perangkat (`PRK-<tahun>-0001`, urutan diulang tiap tahun) kini dibuat
  sistem saat data disimpan dan tidak dapat diubah. Kode lama tetap; kode yang
  kebetulan sudah terpakai dilewati. Impor CSV/SIMRS tetap memakai kode dari sumber.
- Migrasi `00015_uniform_codes` (00013–00014 dipakai tim server di produksi): **kode lama diseragamkan** ke format di atas
  (mis. HW → BDG-01, IGD → UNT-001, IGD-PC-01 → PRK-2026-0001), termasuk data bawaan
  instalasi baru. Bidang & alasan tunda diberi nomor sesuai urutan tampil, perangkat
  sesuai tahun dibuat (WITA). Data dari SIMRS/CSV tidak diubah, kecuali kode berawalan
  `LEGACY-` (pindahan sekali jalan dari aplikasi lama) yang ikut diseragamkan. Kode lama tersimpan di
  tabel `legacy_codes` (migrasi dapat dibatalkan). **Cetak ulang label QR** yang sudah
  ditempel bila ingin kode baru tampil (QR tetap berfungsi tanpa dicetak ulang).
  Relasi tiket & pemetaan SIMRS memakai ID sehingga utuh; baris CSV/SIMRS yang kodenya
  diganti ditandai "diubah lokal" agar impor ulang tidak mengembalikan kode lama.
- Alasan tunda yang wajib diberi keterangan kini diatur lewat kolom **Wajib keterangan**
  (`pending_reasons.requires_note`), bukan lagi dikenali dari kode `OTHER`.

## PHASE 9: Production Ready (2026-11-18)

### Ditambahkan
- **Backup database terjadwal**: `mysqldump` harian (jam `SIKAT_BACKUP_HOUR` WITA) ke
  file `.sql.gz`, SHA-256, versi migrasi & jumlah baris tabel utama dicatat di
  `backup_logs` (migrasi `00012_backups`); retensi `SIKAT_BACKUP_KEEP_DAYS` (3 file
  terbaru selalu disimpan); gagal → dicoba lagi tiap jam.
- **Uji restore**: backup terbaru dipulihkan ke database sementara, dibandingkan,
  lalu dihapus. Menu **Backup** (Admin) dengan *Backup sekarang* & *Uji restore*,
  perintah CLI `sikat backup` dan `sikat restore-test`, status backup di dashboard Admin.
- **Retensi foto lampiran** (Q9): file foto tiket yang ditutup/dibatalkan lebih dari
  `attachments.retention_days` (default 730 hari) dihapus otomatis; metadata tetap.
- Paket deploy: `deploy/build-release.sh` (binary Linux statis + tarball + sha256),
  unit systemd dengan hardening, konfigurasi Nginx (HTTPS, HTTP/2, gzip).
- Uji beban (`SIKAT_LOAD_TEST=1`): 20.000 tiket, 20 pengguna bersamaan.
- Dokumen: `DEPLOYMENT.md`, `USER_GUIDE.md`, `ADMIN_GUIDE.md`, `SECURITY.md`;
  `DATABASE.md` & `ERD.md` diselaraskan dengan migrasi aktual.

### Keamanan
- Mengganti atau mereset password kini mengakhiri semua session lain pengguna tersebut.
- Langganan Web Push hanya menerima endpoint push service resmi (mencegah SSRF).

## PHASE 8: SIMRS Integration Ready (2026-11-18)

### Ditambahkan
- Migrasi `00011_integration`: `employees` (direktori pegawai), `integration_configs`
  (sumber data per entitas, jadwal, kebijakan konflik, retry), `integration_sync_logs`,
  `external_id_mappings`, `integration_imports`.
- Paket `internal/integration`: antarmuka `OrganizationProvider`, `RoomProvider`,
  `EmployeeProvider`, `AssetProvider`; `LocalProvider` (default, jujur: tidak ada
  sinkron); mesin penerapan dengan pemetaan external_id, penautan ke data lokal
  berkode sama, **perlindungan perubahan manual** (konflik, kecuali "Utamakan data
  sumber"), data hilang dinonaktifkan (tidak dihapus).
- Halaman **Administrasi › Integrasi** (Admin): status per entitas, **impor CSV**
  (unggah → pratinjau per baris tanpa menyimpan → terapkan), template CSV per
  entitas, log sinkronisasi dengan rincian konflik/galat; impor tercatat di audit.
- Sinkron terjadwal & percobaan ulang otomatis (5, 10, 20 menit … sampai batas) untuk
  adapter SIMRS — siap dipakai saat adapter dibuat; tanpa adapter, SIMRS tidak dapat
  dipilih (tidak ada integrasi palsu).
- Master Data › **Pegawai**; form "Catat tiket" menyarankan nama pelapor dari direktori.
- Dashboard Admin: status integrasi & impor terakhir.
- `docs/INTEGRATION.md`: prinsip, format CSV, aturan sinkron, cara menambah adapter
  SIMRS, dan daftar informasi yang dibutuhkan dari tim SIMRS.

### Diubah
- Mengaktifkan/menonaktifkan data master yang berasal dari sumber luar kini menandai
  perubahan lokal (tidak ditimpa sinkron berikutnya).
- Menu **Integrasi** disembunyikan dari navigasi (keputusan: integrasi lewat API SIMRS);
  halaman `/app/integrations` tetap tersedia untuk Admin lewat URL.

### Diperbaiki
- Badge notifikasi dapat meminta data ke server berulang kali bila halaman sering
  berganti terlihat/tersembunyi; kini dibatasi paling sering sekali per 30 detik.

## PHASE 7: Dashboard & Reporting (2026-11-13)

### Ditambahkan
- Halaman **Laporan** (`/app/reports`) dengan 15 laporan dari data transaksi:
  Laporan Koordinator IT (pengganti spreadsheet: No, Tanggal, Unit/Bagian, Bidang,
  Kegiatan, Pelaksana, Pendukung, Status, Hasil/Keterangan — dari tiket dan
  kegiatan internal), rekap bulanan (dengan grafik), per bidang, per unit, per
  petugas, per prioritas, tiket tertunda, SLA, waktu respons, waktu penyelesaian,
  beban kerja petugas, kegiatan internal, perangkat sering rusak, riwayat
  pemeliharaan, dan downtime perangkat.
- Filter periode (bulan, tahun, rentang tanggal; batas dihitung dalam WITA), unit,
  bidang, petugas, prioritas, dan status.
- Ekspor **Excel** (angka tetap berupa angka, filter otomatis, header beku) dan
  **PDF** (A4 landscape, header tabel berulang tiap halaman, nomor halaman);
  setiap ekspor tercatat di jejak audit.
- Petugas melihat laporan berisi pekerjaannya sendiri; Admin & Koordinator melihat semua.
- Dashboard per role: petugas (tersedia untuk saya, tiket saya, dikerjakan,
  tertunda, selesai bulan ini, kritis, SLA mendekati batas, beban kerja tim);
  koordinator (ringkasan bulan berjalan, belum diambil, SLA terlewati, kinerja
  petugas, distribusi gangguan, kegiatan internal, perangkat sering rusak); admin
  (status database, worker, antrean notifikasi, Web Push, integrasi & backup).
- Dependensi: `github.com/xuri/excelize/v2`, `github.com/go-pdf/fpdf`.

### Diubah
- **Koordinator per bidang** (migrasi `00009_bidang_coordinator`): Master Data › Bidang
  memiliki kolom Koordinator (tepat satu per bidang; satu koordinator boleh memegang
  beberapa bidang; terisi awal dari kompetensi koordinator yang ada). Notifikasi,
  dashboard, dan antrean bawaan koordinator kini hanya bidangnya; ambil alih, ubah
  prioritas, batalkan, tutup, buka kembali, dan atur anggota tim hanya untuk tiket
  bidangnya (tetap dapat melihat semua tiket & laporan). Bidang tanpa koordinator
  aktif dialihkan ke semua koordinator; dashboard Admin memperingatkannya. Detail
  tiket menampilkan koordinator bidang.
- **Triase & koreksi jenis gangguan** (migrasi `00010_triage`): bidang baru
  "Triase / Belum terklasifikasi"; jenis "Lainnya" kini menuju Triase (bukan
  Hardware). Tiket triase terlihat & dinotifikasi ke semua petugas, ditandai
  "Perlu dipilah", dan muncul sebagai banner di dashboard koordinator triase.
  Koordinator bidang tiket (atau penanggung jawab) dapat mengoreksi jenis
  gangguan: bidang ikut berubah, kebijakan SLA dipilih ulang, koordinator &
  petugas bidang baru menerima notifikasi "Dialihkan ke bidang", tercatat di
  histori; jenis pilihan pelapor disimpan. Laporan baru **Pemilahan jenis
  gangguan** sebagai bahan menambah jenis gangguan baru. Formulir pelapor
  memberi petunjuk memilih "Lainnya" bila tidak ada yang cocok.
- Logo baru SIKAT (label + kunci pas + sirkuit) di sidebar, header HP, halaman login,
  favicon, dan ikon PWA (192/512, maskable latar penuh, apple-touch, badge notifikasi).
- Warna aksen teal diganti palet biru brand yang diturunkan dari logo (`brand-50…950`,
  tombol utama `brand-700` #1B57A5, kontras teks putih 7,1:1); theme-color, grafik,
  serta header tabel Excel/PDF ikut disesuaikan.

### Diperbaiki
- Ikon pada tombol (inbox & pengaturan notifikasi) tidak tampil karena tidak diberi ukuran.

## PHASE 6: PWA & Notifikasi (2026-11-12)

### Ditambahkan
- Migrasi `00008_notifications`: `outbox_events` (dedupe_key UNIQUE),
  `notifications` (UNIQUE user+event), `push_subscriptions`, `push_deliveries`,
  `notification_preferences` (preferensi awal per role; Admin tidak menerima
  notifikasi tiket secara default), FK `sla_escalations.outbox_event_id`.
- Notifikasi berbasis event yang ditulis dalam transaksi perubahan data: tiket baru,
  tiket kritis, diambil, mulai dikerjakan, tertunda, selesai, dibuka kembali,
  penugasan, SLA hampir habis, SLA terlewati, perangkat sering rusak.
  Petugas hanya menerima yang relevan (kompetensi / anggota tim tiket); pelaku
  tidak menerima notifikasi tindakannya sendiri.
- Inbox in-app (`/app/notifications`): belum dibaca/semua, tandai semua dibaca,
  badge lonceng di menu & header HP (diperbarui tiap menit dan saat push masuk);
  membuka detail tiket menandai notifikasinya dibaca.
- Web Push dengan VAPID (implementasi pustaka standar Go): retry dengan jeda,
  langganan 404/410 dinonaktifkan, push basi (> 1 jam) tidak dikirim, isi lock screen
  generik (nomor tiket & jenis kejadian, tanpa lokasi/deskripsi/nama).
- Pengaturan notifikasi per pengguna: aktifkan/matikan push di perangkat ini,
  daftar perangkat, kirim notifikasi uji. Logout menghapus langganan perangkat itu.
- Preferensi notifikasi per role untuk Admin (Inbox / Inbox + push / Tidak dikirim),
  tercatat di audit.
- PWA: manifest, ikon (192/512/maskable/apple-touch), service worker (cache aset
  statis berversi, halaman offline), favicon.
- Perintah `sikat vapid-keys`; variabel `SIKAT_VAPID_PUBLIC_KEY`,
  `SIKAT_VAPID_PRIVATE_KEY`, `SIKAT_VAPID_SUBJECT`.
- Worker `kirim-notifikasi` (5 detik) dan `bersih-notifikasi` (harian, retensi 180 hari).

## PHASE 5: SLA & Eskalasi (2026-11-12)

### Ditambahkan
- Migrasi `00007_sla`: `sla_calendars`, `sla_calendar_hours`, `sla_holidays`,
  `sla_policies`, `sla_escalations`; kolom tiket `sla_cycle_started_at`,
  `sla_revision`; FK kebijakan/kalender di tiket. Konfigurasi awal (dapat diubah):
  kalender "Jam kerja" Senin–Jumat 08.00–16.00 WITA + hari libur nasional
  bertanggal tetap 2026–2027, kalender "24 jam", kebijakan per prioritas
  (Kritis 15 mnt/4 jam 24 jam; Tinggi 30 mnt/8 jam kerja; Sedang 2 jam/16 jam
  kerja; Rendah 8 jam/40 jam kerja). Tiket aktif lama mendapat kebijakan standar.
- Kalkulator SLA murni (`internal/sla`): waktu operasional dengan jam kerja per
  hari, hari libur, 24/7, mulai di luar jam kerja; evaluasi respons & penyelesaian
  (waktu terpakai, sisa, persentase, status Aman/Peringatan/Terlambat/Dijeda,
  Tercapai/Terlewati).
- Kebijakan SLA dipilih otomatis saat tiket dibuat (paling spesifik: jenis
  gangguan > bidang > unit) dan disalin ke tiket; dipilih ulang saat prioritas
  diubah; siklus baru saat tiket dibuka kembali.
- Jeda SLA hanya bila alasan tunda bertanda "Menghentikan SLA" **dan** kebijakan
  mengizinkan; batas waktu digeser sebesar jeda.
- Worker "periksa-sla" tiap menit: pengingat saat mencapai ambang (default 75%),
  pelanggaran respons/penyelesaian, eskalasi segera untuk tiket Kritis. Tercatat
  sekali per kejadian (UNIQUE tiket+jenis+siklus+revisi) di `sla_escalations` dan
  histori tiket; notifikasi menyusul di PHASE 6.
- Halaman **SLA** (Koordinator & Admin): ringkasan Terlambat/Hampir habis/Aman/
  Dijeda, tiket aktif paling mendesak dengan bar pemakaian, eskalasi terbaru.
- **Laporan SLA bulanan**: kepatuhan respons & penyelesaian, rekap per prioritas,
  daftar tiket yang melanggar (termasuk pelanggaran pada siklus dibuka ulang).
- Panel SLA di detail tiket (respons & penyelesaian, sisa waktu, batas WITA) dan
  penanda SLA di antrean tiket; ringkasan SLA di beranda.
- Master Data → **Kebijakan SLA** dan **Kalender kerja** (jam kerja per hari,
  hari libur; perubahan menghitung ulang batas tiket aktif dan tercatat di audit).
- Permission baru `sla.view` (Admin, Koordinator).

### Diubah
- Mesin master data mendukung referensi ke tabel tanpa kolom `is_active`.
- Mode development: bila template di disk memakai fungsi Go yang belum ada di
  binary berjalan, server memakai template valid terakhir (peringatan di log)
  alih-alih error 500.

## Tampilan clean & profesional (2026-11-12)

### Diubah
- Sistem desain netral dan datar: latar abu sangat muda, kartu putih bergaris tipis
  tanpa bayangan, satu warna aksen (teal), sudut kecil, tanpa gradien/blur/emoji.
  Hierarki dari tipografi dan jarak, bukan warna.
- Font Inter (variable, OFL) di-host sendiri (`static/fonts`), tanpa CDN. Favicon SVG.
- Badge status: titik berwarna + teks netral (merah hanya untuk kondisi kritis).
- Kerangka desktop: sidebar putih dengan ikon dan grup menu (Utama, Perangkat,
  Administrasi, Akun), kartu pengguna + tombol keluar. Mobile-first tetap: header,
  menu bawah, laci menu.
- Beranda: angka ringkasan dalam panel metrik bergaris tipis (tiket, perangkat,
  master data), banner tiket kritis, kolom samping perangkat sering rusak & aktivitas.
- Antrean tiket: daftar baris ringkas (nomor, prioritas, status, lokasi, waktu,
  tombol Ambil di kanan pada desktop), tab bergaris bawah, panel beban kerja.
- Detail perangkat: dua kolom di desktop; statistik memakai panel metrik.
- Halaman masuk: satu kartu di tengah, polos.
- Judul bagian seragam (`.section-title`), ketebalan huruf dan warna garis diseragamkan.

### Developer
- Mode development: file `static/` dibaca dari disk (no-cache) dan versi `?v=`
  mengikuti waktu ubah file, sehingga hasil `npm run build:css` langsung terlihat
  tanpa restart server dan tanpa cache lama di browser.

## PHASE 4: Asset Maintenance (2026-11-11)

### Ditambahkan
- Migrasi `00006_maintenance`: `asset_maintenance_histories`, `maintenance_parts`,
  `asset_alerts`, kolom `internal_activities.maintenance_type`, trigger larangan
  hapus riwayat & sparepart terpakai, pengaturan ambang kerusakan berulang.
- Riwayat perangkat otomatis:
  - tiket terkait perangkat yang diselesaikan → entri Perbaikan (keluhan, jenis
    kerusakan, tindakan, petugas, hasil, waktu, downtime, durasi), satu entri per
    siklus tiket (tiket dibuka ulang = entri baru, tanpa duplikat);
  - kegiatan internal selesai pada perangkat → entri sesuai pilihan
    (preventif/pemeriksaan/instalasi/perbaikan); kegiatan dibatalkan → entri ditandai batal.
- Form penyelesaian tiket perangkat: sparepart (maks 3 baris, harga kosong =
  harga acuan), biaya jasa/vendor, kondisi perangkat setelahnya (status perangkat
  diperbarui dengan jejak audit).
- Entri pemeliharaan manual (mis. servis vendor) oleh petugas/koordinator;
  koreksi oleh koordinator dengan "batalkan entri" beralasan (tidak dihapus,
  tidak dihitung di statistik).
- Halaman detail perangkat: statistik jendela waktu & sepanjang waktu (kerusakan,
  perbaikan, downtime, biaya jasa+sparepart, perawatan terakhir), riwayat gabungan
  tiket/kegiatan/manual/keputusan.
- Deteksi kerusakan berulang saat tiket perangkat dibuat (ambang & jendela dapat
  diatur), satu peringatan terbuka per perangkat, rekomendasi berbasis aturan
  (evaluasi → pemeriksaan lanjutan → pertimbangan penggantian berdasarkan jumlah,
  jenis berulang, umur, total biaya). Keputusan koordinator menutup peringatan,
  tercatat di riwayat, dan memulai ulang penghitungan.
- Halaman "Perangkat Sering Rusak", ringkasan di beranda koordinator, penanda di
  daftar perangkat. Permission baru `maintenance.record` (petugas, koordinator)
  dan `maintenance.review` (koordinator).
- 87 automated test.

## PHASE 3: Work Management (2026-11-11)

### Ditambahkan
- State machine status tiket (`internal/workflows`): hanya transisi sah yang
  dapat dijalankan; tombol aksi di halaman tiket mengikuti status & hak akses.
- Aksi penanggung jawab: mulai kerjakan, tunda (alasan wajib dari daftar; "Lainnya"
  wajib keterangan), lanjutkan, kembalikan ke antrean (alasan wajib), selesaikan
  (tindakan + hasil wajib, template tindakan, waktu layanan pulih).
- Aksi koordinator: ambil alih (alasan wajib), ubah prioritas (alasan wajib,
  histori dari → ke, sumber MANUAL), tutup, buka kembali (siklus baru);
  koordinator/admin: batalkan (alasan wajib).
- Anggota pendukung tiket (tambah/lepas), catatan internal vs terlihat pelapor,
  unggah foto oleh petugas (opsional terlihat pelapor). Semua tercatat di histori.
- Pelapor: konfirmasi "Sudah beres" (tiket ditutup) atau "Belum beres" (wajib
  keterangan, tiket dibuka kembali ke petugas yang sama), tambah keterangan,
  melihat catatan publik & ringkasan penyelesaian.
- Worker latar belakang dengan leader lock MySQL (`GET_LOCK`); job pertama:
  tutup otomatis tiket Selesai setelah N hari (default 3, pengaturan
  `ticket.auto_close_days`) — idempoten.
- Kegiatan internal IT: nomor `KEG-YYYY-NNNNNN`, jenis, bidang (otomatis dari
  jenis), unit/ruangan/perangkat, tiket terkait, pelaksana & pendukung, status,
  waktu mulai/selesai dengan durasi otomatis (kolom generated), hasil wajib bila
  selesai, lampiran foto, filter bulan/jenis/bidang/petugas/status. Petugas
  melihat kegiatannya sendiri; koordinator melihat & mengubah semua.
- Migrasi `00005_activities`. 82 automated test (matriks transisi lengkap,
  siklus hidup tiket penuh, hak akses, tutup otomatis, leader worker, kegiatan, HTTP).

## PHASE 2: QR Ticketing (2026-11-11)

### Ditambahkan
- Migrasi `00003_qr_tickets` (qr_codes, priority_rules, number_sequences, tickets,
  ticket_assignments, ticket_histories, ticket_comments, attachments + trigger
  append-only) dan `00004_seed_phase2` (5 aturan prioritas awal, pengaturan lampiran).
- QR ruangan & perangkat: token acak 22 karakter (bukan ID), satu QR aktif per
  sasaran, buat satuan/massal, pratinjau, unduh PNG, unduh ZIP, halaman cetak
  stiker A4, nonaktifkan, regenerate (token lama langsung tidak berlaku),
  penghitung scan. QR perangkat mengikuti ruangan perangkat saat ini.
- Form pengajuan publik `/q/{token}` tanpa login: lokasi/perangkat terisi
  otomatis, unit pelapor otomatis, pilihan jenis gangguan berbentuk kartu (jenis
  yang sebidang dengan perangkat tampil lebih dulu), pertanyaan dampak &
  jumlah terdampak, perangkat pengganti, perkiraan awal gangguan, foto (dikompres
  di HP), peringatan data pasien, nama/kontak diingat di perangkat pelapor.
- Mesin prioritas otomatis berbasis aturan berurutan (dapat diubah Admin di
  Master Data › Aturan Prioritas) + halaman "Uji aturan". Status layanan klinis
  kritis dihitung dari ruangan/unit, bukan dari pelapor.
- Pembuatan tiket atomik: nomor `TKT-YYYY-NNNNNN` per tahun (WITA) tanpa lubang
  atau ganda, token pelapor 32 byte (hanya hash SHA-256 disimpan), histori
  CREATED berisi aturan prioritas yang dipakai.
- Lampiran foto: validasi isi file, tolak resolusi tidak wajar, perkecil ke maks
  1920 px, re-encode JPEG (EXIF/GPS terbuang), simpan di luar webroot dengan nama
  acak, disajikan hanya lewat handler berhak akses; file dibersihkan bila tiket gagal dibuat.
- Halaman konfirmasi & pemantauan `/t/{token}`: nomor, lokasi, masalah,
  prioritas, link (salin/bagikan), status, perkembangan, foto; diperbarui otomatis.
- Antrean bersama petugas: tab Antrean / Tiket saya / Semua aktif, urut
  prioritas, filter prioritas & unit, filter kompetensi bidang (default aktif),
  panel beban kerja, pembaruan otomatis tiap 30 detik.
- Klaim tiket atomik (UPDATE bersyarat + UNIQUE index); petugas lain mendapat
  pesan "sudah diambil oleh …". Retry otomatis bila MySQL deadlock/lock timeout.
- Pencatatan tiket oleh petugas atas nama pelapor (Telepon / WA / Datang
  langsung, keputusan Q5) dengan opsi "langsung saya tangani" dan link pemantauan
  untuk pelapor (ditampilkan sekali).
- Detail tiket petugas, ringkasan tiket & peringatan tiket kritis di beranda,
  QR & tiket terkait di halaman perangkat.
- Keamanan: rate limit kiriman per IP dan per QR, honeypot anti-bot, batas
  ukuran body request, token publik disamarkan di log.
- Permission baru `ticket.create` (Koordinator & Petugas).
- 69 automated test, termasuk 10 petugas mengklaim tiket yang sama bersamaan
  (tepat 1 berhasil, diulang 10×) dan 20 tiket dibuat paralel (nomor unik berurutan).

### Diperbaiki
- Respons klaim HTMX terhapus karena atribut `hx-select` kontainer polling
  diwarisi form di dalamnya (ditemukan saat uji browser).

## PHASE 1: Foundation (2026-11-11)

### Ditambahkan
- Struktur proyek Go modular, binary tunggal `sikat` dengan perintah `serve`,
  `migrate up|down|status`, `create-admin`, `version`. Template, aset statis,
  dan migrasi di-embed ke binary.
- Konfigurasi dari environment / `.env` dengan validasi saat start.
- Koneksi MySQL dengan zona waktu sesi UTC, `sql_mode` ketat, helper transaksi,
  dan penerjemahan error MySQL (unik, foreign key, CHECK) menjadi pesan form.
- Migrasi `00001_init` (16 tabel: organisasi, katalog, perangkat, sparepart,
  audit, pengaturan, session) dan `00002_seed_config` (role, bidang, jenis
  gangguan, alasan tunda, jenis kegiatan, pengaturan awal).
- Autentikasi: login/logout, hash Argon2id, session server-side di MySQL,
  rotasi token session saat login dan ganti password, penguncian akun setelah
  gagal login berulang (dapat dikonfigurasi), wajib ganti password sementara.
- RBAC tiga role (Admin IT, Koordinator IT, Petugas IT) dengan matriks permission.
- Keamanan: CSRF dua lapis (CrossOriginProtection + token per session), CSP
  tanpa inline script, header keamanan, HSTS di HTTPS, rate limit login per IP,
  `Cache-Control: no-store` untuk halaman internal, IP proxy hanya dipercaya
  dari loopback.
- Jejak audit append-only (dijaga trigger database) untuk login dan seluruh
  perubahan master data, perangkat, pengguna, dan pengaturan; halaman Jejak Audit.
- Master data: Unit, Ruangan, Bidang, Jenis Gangguan, Kategori Perangkat,
  Alasan Tunda, Jenis Kegiatan, Template Tindakan, Sparepart — tambah, ubah,
  nonaktifkan/aktifkan, hapus (hanya bila belum dipakai).
- Perangkat: daftar dengan filter & pencarian, tambah/ubah, unit otomatis dari
  ruangan, validasi ruangan-unit, status termasuk "Dihapus dari inventaris",
  halaman detail dengan riwayat perubahan data.
- Pengguna: tambah dengan password sementara (ditampilkan sekali), ubah,
  kompetensi bidang, reset password, nonaktifkan (sesi langsung berakhir),
  perlindungan admin terakhir dan akun sendiri.
- Pengaturan sistem: nama rumah sakit, kontak IT, batas gagal login, lama kunci akun.
- Beranda per role dengan ringkasan dari database.
- Layout responsif mobile-first (kartu di HP, tabel di desktop), Tailwind CSS 4, HTMX 2.
- 47 automated test (unit + integrasi MySQL + HTTP end-to-end) pada akhir PHASE 1.
