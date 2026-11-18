# SIKAT — Instalasi

> Berlaku untuk versi PHASE 1 (Foundation). Panduan deployment produksi lengkap
> (Nginx, HTTPS, systemd, backup) ditulis di `DEPLOYMENT.md` pada PHASE 9.

## 1. Kebutuhan

| Komponen | Versi | Keterangan |
|---|---|---|
| Go | 1.25 atau lebih baru | Untuk build. Server produksi cukup menerima binary. |
| MySQL | 8.0.16 atau lebih baru | CHECK constraint & trigger dipakai. |
| Node.js | 18+ (opsional) | Hanya untuk membangun ulang CSS. File `static/css/app.css` sudah tersedia di repository. |

## 2. Database

Buat database dan user khusus aplikasi (jangan pakai root di produksi):

```sql
CREATE DATABASE sikat CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'sikat'@'localhost' IDENTIFIED BY 'ganti-dengan-password-kuat';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, INDEX, REFERENCES, TRIGGER
  ON sikat.* TO 'sikat'@'localhost';
```

`CREATE/ALTER/DROP/INDEX/REFERENCES/TRIGGER` dibutuhkan untuk migrasi. Bila
binary logging aktif di server MySQL, pembuatan trigger oleh user non-SUPER
membutuhkan `SET GLOBAL log_bin_trust_function_creators = 1` atau migrasi
dijalankan oleh user dengan hak tersebut.

## 3. Konfigurasi

```bash
cp .env.example .env
```

Isi minimal `SIKAT_DB_DSN` dan `SIKAT_BASE_URL`. Penjelasan setiap variabel ada
di `.env.example`. Aplikasi menolak start bila konfigurasi tidak valid dan
menampilkan daftar kesalahannya. Di `production`, `SIKAT_BASE_URL` wajib
`https://` (cookie `Secure` dan HSTS otomatis aktif).

Foto lampiran disimpan di `SIKAT_STORAGE_DIR/attachments` (default `./storage`).
Folder ini harus dapat ditulisi aplikasi, berada di luar folder yang disajikan
Nginx, dan ikut dicadangkan. Aplikasi menolak start bila folder tidak dapat ditulisi.

Backup database harian memakai `mysqldump` (variabel `SIKAT_BACKUP_*`,
`SIKAT_MYSQLDUMP`, `SIKAT_MYSQL`). Di Laragon, isi path lengkap binary, mis.
`SIKAT_MYSQLDUMP=C:/laragon/bin/mysql/mysql-8.0.30-winx64/bin/mysqldump.exe` dan
`SIKAT_MYSQL=C:/laragon/bin/mysql/mysql-8.0.30-winx64/bin/mysql.exe` (gunakan `/`).
Produksi: lihat [DEPLOYMENT.md](DEPLOYMENT.md).

## 4. Build & migrasi

```bash
go build -o bin/sikat ./cmd/server
```

```bash
./bin/sikat migrate up
```

```bash
./bin/sikat migrate status
```

Migrasi menambahkan konfigurasi awal (role, bidang, jenis gangguan, alasan
tunda, jenis kegiatan, pengaturan sistem). Semuanya dapat diubah Admin IT.
Master data rumah sakit (unit, ruangan, perangkat) diisi lewat aplikasi.

## 5. Akun Admin IT pertama

```bash
./bin/sikat create-admin
```

Perintah ini menanyakan username, nama lengkap, dan password (input password
tidak ditampilkan). Tidak ada akun atau password bawaan di kode.

## 6. Menjalankan

```bash
./bin/sikat serve
```

Buka `SIKAT_BASE_URL`, login dengan akun admin, lalu isi master data berurutan:
Unit → Ruangan → Kategori perangkat → Perangkat → Pengguna (petugas/koordinator).
Setelah itu buat QR di **Master Data › QR Code** (tombol "Buat semua QR ruangan"),
cetak, dan tempel stikernya. Pastikan `SIKAT_BASE_URL` sudah alamat final sebelum
mencetak, karena alamat itulah yang dikodekan di QR.

Health check untuk monitoring/reverse proxy: `GET /healthz` → `ok` bila database terjangkau.

### Web Push (notifikasi ke HP/komputer)

Opsional; tanpa konfigurasi ini notifikasi tetap tersedia di inbox aplikasi.

```bash
./bin/sikat vapid-keys >> .env   # lalu edit SIKAT_VAPID_SUBJECT
```

- `SIKAT_VAPID_SUBJECT`: kontak pengelola (`mailto:it@rs.example` atau URL `https://`).
- Kunci privat **rahasia** dan jangan diganti setelah dipakai: perangkat yang sudah
  berlangganan harus mengaktifkan ulang notifikasi bila kunci berubah.
- Server butuh akses internet keluar (HTTPS) ke push service browser
  (fcm.googleapis.com, updates.push.services.mozilla.com, web.push.apple.com, dll.).
- Push & service worker hanya bekerja di konteks aman: `https://` (produksi) atau
  `http://localhost` (development). iPhone/iPad (iOS 16.4+) mewajibkan SIKAT
  di-*Tambah ke Layar Utama* lalu dibuka dari ikon tersebut.
- Pengguna mengaktifkan push di **Notifikasi › Pengaturan** (per perangkat, ada
  tombol "Kirim notifikasi uji"). Admin mengatur kejadian per role di
  **Pengaturan › Preferensi notifikasi**.

## 7. Development

- Mode `development` membaca ulang template dari folder `templates/` setiap
  request, sehingga perubahan HTML langsung terlihat tanpa restart.
- Setelah mengubah kelas Tailwind di template, bangun ulang CSS:

```bash
npm install
```

```bash
npm run build:css
```

- Memperbarui HTMX dari `node_modules`:

```bash
npm run vendor:js
```

## 8. Domain lokal dengan Laragon (beberapa aplikasi sekaligus)

Di development, SIKAT diakses lewat `http://sikat.test` melalui Nginx Laragon
(reverse proxy ke `127.0.0.1:8080`). Dengan pola ini beberapa aplikasi dapat
berjalan bersamaan, masing-masing dengan domain `.test` sendiri.

1. Salin `deploy/laragon/Sikat.test.conf` ke `C:\laragon\etc\nginx\sites-enabled\`
   dan hapus `auto.Sikat.test.conf` (file tanpa awalan `auto.` tidak ditimpa Laragon).
2. Reload Nginx: menu Laragon › Nginx › Reload, atau `nginx.exe -s reload` dari
   folder Nginx Laragon.
3. Di `.env`: `SIKAT_BASE_URL=http://sikat.test` dan `SIKAT_TRUST_PROXY_HEADERS=true`.
4. Jalankan `JALANKAN-SIKAT.bat` (build, migrasi, lalu `serve`).

Entri hosts `sikat.test` dibuat otomatis oleh Laragon untuk setiap folder di
`C:\laragon\www`. Untuk aplikasi Go lain: pakai port berbeda (`8081`, `8082`, …)
dan buat file vhost serupa dengan `proxy_pass` ke port tersebut. Aplikasi PHP
tetap memakai vhost otomatis Laragon.

Catatan: domain `.test` hanya berlaku di komputer ini. Untuk mencoba scan QR dari
HP, HP harus dapat menjangkau server (mis. lewat IP LAN komputer atau tunnel), dan
`SIKAT_BASE_URL` harus alamat yang dapat dibuka HP sebelum QR dicetak.

Service worker, instal PWA, dan Web Push tidak berjalan di `http://sikat.test`
(bukan konteks aman). Untuk mencobanya di komputer ini, buka
`http://localhost:8080` langsung di Chrome/Edge. Bila browser menampilkan halaman
"offline" milik aplikasi lain, hapus service worker lama untuk `localhost:8080`
(DevTools › Application › Service Workers › Unregister) — origin `localhost:8080`
dipakai bergantian oleh aplikasi lokal lain.

## 9. Menjalankan automated test

Test integrasi memakai MySQL sungguhan. Setiap test membuat database
`sikat_test_*` sendiri dan menghapusnya setelah selesai. **Jangan arahkan ke
server produksi.**

```bash
SIKAT_TEST_DSN="root@tcp(127.0.0.1:3306)/" ./scripts/test.sh
```

Tanpa `SIKAT_TEST_DSN`, test yang membutuhkan database dilewati (SKIP) dan
hanya unit test murni yang berjalan.

Test backup membutuhkan `mysqldump`/`mysql` (variabel `SIKAT_MYSQLDUMP`/`SIKAT_MYSQL`,
atau terdeteksi otomatis dari Laragon); bila tidak ada, test tersebut dilewati.

Uji beban (opsional, ±1–2 menit, membuat 20.000 tiket di database test sementara):

```bash
SIKAT_LOAD_TEST=1 SIKAT_TEST_DSN="root@tcp(127.0.0.1:3306)/" go test ./internal/server -run TestLoad -v
```
