# Changelog

Format mengikuti [Keep a Changelog](https://keepachangelog.com/id/1.1.0/).

## [Unreleased] — PHASE 2: QR Ticketing (2026-11-11)

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
