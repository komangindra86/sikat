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

## 8. Menjalankan automated test

Test integrasi memakai MySQL sungguhan. Setiap test membuat database
`sikat_test_*` sendiri dan menghapusnya setelah selesai. **Jangan arahkan ke
server produksi.**

```bash
SIKAT_TEST_DSN="root@tcp(127.0.0.1:3306)/" ./scripts/test.sh
```

Tanpa `SIKAT_TEST_DSN`, test yang membutuhkan database dilewati (SKIP) dan
hanya unit test murni yang berjalan.
