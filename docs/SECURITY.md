# SIKAT — Audit Keamanan (PHASE 9)

Tanggal audit: 18 November 2026. Cakupan: kode aplikasi (Go, template, JS),
konfigurasi deploy (systemd, Nginx), dan alur data. Acuan: OWASP Top 10 (2021)
dan OWASP ASVS level 1 untuk aplikasi internal.

## 1. Ringkasan

| | |
|---|---|
| Temuan diperbaiki | 2 (lihat §3) |
| Temuan terbuka | 0 tingkat tinggi/sedang |
| Belum dijalankan | `govulncheck` dan race detector (§4) |

## 2. Daftar periksa OWASP Top 10

| Risiko | Kontrol di SIKAT | Bukti test |
|---|---|---|
| A01 Broken Access Control | RBAC per route (`rbac.Require*`); petugas hanya tiket sendiri/antrean bidangnya; koordinator hanya menindak bidangnya; halaman pelapor hanya lewat token acak 256-bit (disimpan sebagai hash SHA-256); ID numerik tidak pernah cukup untuk akses publik | `TestLoginFlowAndRBAC`, `TestBackupPageAccess`, test koordinator & workflow |
| A02 Cryptographic Failures | Password Argon2id; cookie `HttpOnly`, `SameSite=Lax`, `Secure` + HSTS di production; TLS 1.2/1.3 di Nginx; push dienkripsi RFC 8291 | test auth & webpush |
| A03 Injection | Seluruh SQL memakai parameter (`?`); `html/template` auto-escape; ekspor Excel/PDF dari teks biasa; CSV impor diparse `encoding/csv` | test repository |
| A04 Insecure Design | Transisi status lewat state machine; klaim tiket atomik (UPDATE bersyarat); konfirmasi pelapor; peringatan data pasien di formulir | test workflow & konkurensi klaim |
| A05 Security Misconfiguration | CSP ketat tanpa inline (`script-src 'self'`), `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, COOP; aplikasi menolak start bila konfigurasi production tidak aman (`SIKAT_BASE_URL` harus https); systemd hardening (`NoNewPrivileges`, `ProtectSystem=strict`) | `TestStaticAndHealth` |
| A06 Vulnerable Components | Dependensi minimal (chi, goose, mysql driver, excelize, fpdf, x/crypto); binary statis | `govulncheck` **perlu dijalankan** (§4) |
| A07 Identification & Auth Failures | Batas percobaan login per IP + kunci akun; session ID baru saat login (anti fixation); wajib ganti password sementara; **session berakhir saat password diganti/direset atau akun dinonaktifkan** | `TestSessionFixationPrevented`, `TestLoginRateLimit`, `TestForcedPasswordChange`, `TestDeactivatedUserLosesSession`, `TestPasswordChangeEndsOtherSessions` |
| A08 Software & Data Integrity | CSRF token di semua form & request JSON (header `X-CSRF-Token`); backup dengan SHA-256 dan uji restore; migrasi ter-embed dalam binary | `TestCSRFProtection`, test backup |
| A09 Logging & Monitoring | Jejak audit tidak dapat diubah (trigger DB) untuk login, perubahan master data/pengguna/konfigurasi, ekspor, impor, backup; log JSON dengan request ID; path berisi token disamarkan di log | `TestRedactPath` |
| A10 SSRF | Satu-satunya request keluar adalah Web Push; endpoint hanya diterima untuk host push service resmi (FCM, Mozilla, Windows, Apple) tanpa port eksplisit | test notifikasi |

Kontrol tambahan:

- **Upload**: batas ukuran per request (`MaxBytesReader`), tipe diperiksa dari isi
  file (JPEG/PNG/WebP saja), nama file acak, disimpan di luar web root, disajikan
  lewat handler yang memeriksa hak akses.
- **Formulir publik**: batas kiriman per IP dan per QR; tidak ada login sehingga
  tidak ada akun yang dapat ditebak.
- **Privasi**: tidak ada data pasien yang disimpan; isi push di layar kunci umum;
  foto dihapus otomatis sesuai masa simpan (default 2 tahun setelah tiket ditutup).
- **Rahasia**: hanya di `.env` (izin 600); kredensial backup diteruskan ke
  `mysqldump` lewat file sementara 0600, bukan argumen command line.

## 3. Temuan & perbaikan dalam audit ini

| # | Temuan | Tingkat | Perbaikan |
|---|---|---|---|
| 1 | Endpoint langganan push menerima URL apa pun, sehingga worker dapat dipakai mengirim request ke alamat internal (SSRF) oleh pengguna yang login | Sedang | Allowlist host push service + tolak port eksplisit (`notifications.AllowedPushHost`) |
| 2 | Mengganti/mereset password tidak mengakhiri session lain pengguna tersebut (mis. HP yang hilang tetap login) | Sedang | Session menyimpan stempel kredensial (turunan hash password); session dengan stempel lama otomatis berakhir |

## 4. Belum dijalankan / keterbatasan

- **`govulncheck`** belum dijalankan dalam audit ini. Jalankan di komputer
  development sebelum setiap rilis:

  ```bash
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  ```

- **Race detector** (`go test -race`) membutuhkan compiler C (CGO) — tidak tersedia
  di lingkungan Windows development. Jalankan di Linux/CI: `go test -race ./...`.
- **Uji penetrasi eksternal** belum dilakukan; disarankan oleh tim keamanan RS
  setelah server produksi siap.
- **Uji beban** (lokal, MySQL 8.0.30): 20.000 tiket & 54.000 histori, 20 pengguna
  bersamaan × 15 request, tanpa error; p95 Beranda 436 ms, daftar tiket aktif
  929 ms, SLA 78 ms, laporan ±430 ms, badge notifikasi 32 ms. Jalankan ulang:
  `SIKAT_LOAD_TEST=1` (lihat INSTALLATION.md §9).

## 5. Rekomendasi operasional

1. Batasi SSH berdasarkan IP dan pakai kunci, bukan password.
2. Jalankan uji restore minimal bulanan; pastikan salinan off-site berjalan.
3. Nonaktifkan akun pegawai yang keluar pada hari yang sama.
4. Tinjau Jejak Audit (login gagal, perubahan pengguna) secara berkala.
5. Perbarui binary SIKAT dan paket OS (`unattended-upgrades`) secara rutin.
