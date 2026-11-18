# SIKAT — Deployment Produksi

Target: satu server Linux (Ubuntu 22.04/24.04 atau Debian 12), MySQL 8, Nginx,
HTTPS. Aplikasi adalah **satu binary** (template, CSS/JS, migrasi ter-embed);
server produksi tidak membutuhkan Go maupun Node.js.

```
Internet/Intranet ──HTTPS──> Nginx (TLS, gzip) ──> 127.0.0.1:8080  sikat serve (systemd, user sikat)
                                                        ├─ MySQL 8 (localhost)
                                                        ├─ /var/lib/sikat           (foto lampiran)
                                                        └─ /var/backups/sikat       (backup harian → salin off-site)
```

## 1. Membangun paket rilis (di komputer development)

```bash
./deploy/build-release.sh 1.0.0
```

Hasil: `dist/sikat-1.0.0-linux-amd64.tar.gz` (+ `.sha256`) berisi binary `sikat`,
`.env.example`, unit systemd, konfigurasi Nginx, dan dokumen ini. Di Windows dapat
dijalankan lewat Git Bash. Sebelum merilis: `go test ./...` harus lulus (lihat
INSTALLATION.md §9).

## 2. Menyiapkan server

```bash
sudo apt update && sudo apt install -y nginx mysql-server certbot python3-certbot-nginx
sudo useradd --system --home /opt/sikat --shell /usr/sbin/nologin sikat
sudo mkdir -p /opt/sikat /var/lib/sikat /var/backups/sikat
sudo chown sikat:sikat /var/lib/sikat /var/backups/sikat
sudo chmod 750 /var/lib/sikat /var/backups/sikat
```

`mysqldump` dan `mysql` (untuk backup & uji restore) ikut terpasang bersama paket
`mysql-server`/`mysql-client`.

### Database

```sql
CREATE DATABASE sikat CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'sikat'@'localhost' IDENTIFIED BY '<password-kuat>';
GRANT ALL PRIVILEGES ON sikat.* TO 'sikat'@'localhost';
-- Uji restore membuat database sementara bernama sikat_restoretest_<waktu>:
GRANT CREATE, DROP, ALTER, INDEX, INSERT, SELECT, REFERENCES, CREATE VIEW, TRIGGER
  ON `sikat\_restoretest\_%`.* TO 'sikat'@'localhost';
FLUSH PRIVILEGES;
```

Aplikasi tidak membutuhkan hak SUPER. Jangan membuka port 3306 ke internet.

## 3. Memasang aplikasi

```bash
tar -xzf sikat-1.0.0-linux-amd64.tar.gz
sudo install -o root -g sikat -m 750 sikat-1.0.0-linux-amd64/sikat /opt/sikat/sikat
sudo install -o sikat -g sikat -m 600 sikat-1.0.0-linux-amd64/.env.example /opt/sikat/.env
sudo -u sikat nano /opt/sikat/.env
```

Isi minimal `.env` produksi:

```ini
SIKAT_ENV=production
SIKAT_ADDR=127.0.0.1:8080
SIKAT_BASE_URL=https://sikat.rs.example
SIKAT_DB_DSN=sikat:<password>@tcp(127.0.0.1:3306)/sikat
SIKAT_TRUST_PROXY_HEADERS=true
SIKAT_STORAGE_DIR=/var/lib/sikat
SIKAT_BACKUP_DIR=/var/backups/sikat
SIKAT_BACKUP_HOUR=2
SIKAT_BACKUP_KEEP_DAYS=14
```

Web Push (opsional): `sudo -u sikat /opt/sikat/sikat vapid-keys` lalu salin tiga
baris hasilnya ke `.env` (`SIKAT_VAPID_SUBJECT` diisi `mailto:` alamat tim IT).
Kunci VAPID jangan diganti setelah dipakai.

Di `production`, aplikasi menolak start bila `SIKAT_BASE_URL` bukan `https://`.

```bash
cd /opt/sikat
sudo -u sikat ./sikat migrate up
sudo -u sikat ./sikat create-admin        # akun Admin IT pertama (interaktif)
sudo cp sikat-1.0.0-linux-amd64/systemd/sikat.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sikat
systemctl status sikat
curl -s http://127.0.0.1:8080/healthz      # → ok
```

## 4. Nginx & HTTPS

```bash
sudo cp sikat-1.0.0-linux-amd64/nginx/sikat.conf /etc/nginx/sites-available/sikat.conf
sudo sed -i 's/sikat.rs.example/DOMAIN-ANDA/g' /etc/nginx/sites-available/sikat.conf
sudo ln -s /etc/nginx/sites-available/sikat.conf /etc/nginx/sites-enabled/
sudo mkdir -p /var/www/letsencrypt
# Sertifikat pertama: sementara komentari blok server 443, lalu:
sudo certbot certonly --webroot -w /var/www/letsencrypt -d DOMAIN-ANDA
sudo nginx -t && sudo systemctl reload nginx
```

Certbot memperbarui sertifikat otomatis (timer systemd). Firewall: buka hanya
22 (SSH, sebaiknya dibatasi IP), 80, dan 443 — mis. `sudo ufw allow 22,80,443/tcp && sudo ufw enable`.

Setelah HTTPS aktif, aplikasi otomatis memasang cookie `Secure` dan header HSTS.

## 5. Backup & pemulihan

- **Otomatis:** worker membuat `sikat-YYYYMMDD-HHMMSS.sql.gz` setiap hari pukul
  `SIKAT_BACKUP_HOUR` (WITA) di `/var/backups/sikat`, menyimpan `SIKAT_BACKUP_KEEP_DAYS`
  hari (3 file terbaru selalu disimpan). Riwayat, ukuran, dan SHA-256 tercatat dan
  tampil di menu **Backup** (Admin) serta dashboard Admin. Gagal → dicoba lagi tiap jam.
- **Manual:** menu Backup → *Backup sekarang*, atau `sudo -u sikat /opt/sikat/sikat backup`.
- **Uji restore** (disarankan mingguan): menu Backup → *Uji restore*, atau
  `sudo -u sikat /opt/sikat/sikat restore-test`. Backup terbaru dipulihkan ke database
  sementara, versi migrasi dan jumlah baris tabel utama dibandingkan dengan saat
  backup, lalu database sementara dihapus.
- **Foto lampiran** (`/var/lib/sikat/attachments`) tidak masuk backup database.
  Salin bersama backup ke lokasi lain.

### Salinan off-site (wajib)

Backup di server yang sama tidak melindungi dari kerusakan disk/server. Contoh
cron harian pukul 03.00 menyalin ke server/NAS lain:

```cron
0 3 * * * sikat rsync -a --delete /var/backups/sikat/ backup@nas.rs.local:/backup/sikat/db/ && rsync -a /var/lib/sikat/attachments/ backup@nas.rs.local:/backup/sikat/attachments/
```

### Memulihkan ke produksi (bila terjadi bencana)

```bash
sudo systemctl stop sikat
gunzip -c /var/backups/sikat/sikat-YYYYMMDD-HHMMSS.sql.gz | mysql -u root -p sikat
sudo -u sikat /opt/sikat/sikat migrate up      # bila binary lebih baru dari backup
sudo systemctl start sikat
```

Pulihkan juga folder foto dari salinan off-site. Lakukan di luar jam sibuk dan
catat di log operasional.

## 6. Memperbarui versi

```bash
sudo -u sikat /opt/sikat/sikat backup                       # backup sebelum update
sudo install -o root -g sikat -m 750 sikat-X.Y.Z-linux-amd64/sikat /opt/sikat/sikat.new
sudo mv /opt/sikat/sikat /opt/sikat/sikat.prev && sudo mv /opt/sikat/sikat.new /opt/sikat/sikat
sudo systemctl restart sikat                                # migrasi berjalan otomatis (ExecStartPre)
```

Rollback: kembalikan `sikat.prev`; bila migrasi baru sudah berjalan dan perlu
dibatalkan, pulihkan backup sebelum update (§5). Periksa CHANGELOG sebelum update.

## 7. Operasional

| Hal | Cara |
|---|---|
| Log aplikasi | `journalctl -u sikat -f` (format JSON di production) |
| Kesehatan | `GET /healthz` → `ok` bila database terjangkau |
| Worker | Berjalan di dalam proses `sikat serve` (SLA, notifikasi, tutup otomatis, backup, retensi foto). Dashboard Admin menampilkan statusnya. |
| Retensi foto | Pengaturan → Lampiran → "Simpan foto tiket" (default 730 hari setelah tiket ditutup) |
| Audit | Menu Jejak Audit (Admin) |
| Pemeriksaan dependensi | Di komputer development: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` sebelum rilis |

## 8. Daftar periksa go-live

- [ ] `SIKAT_ENV=production`, `SIKAT_BASE_URL` https, `.env` izin 600 milik `sikat`
- [ ] HTTPS valid; HTTP dialihkan ke HTTPS; firewall hanya 22/80/443
- [ ] Akun Admin IT dibuat; password sementara pengguna lain wajib diganti saat login pertama
- [ ] Master data diisi (unit, ruangan, perangkat, pengguna, koordinator per bidang)
- [ ] QR dicetak **setelah** `SIKAT_BASE_URL` final
- [ ] Backup pertama berhasil & **uji restore berhasil**; salinan off-site berjalan
- [ ] Nama RS & kontak IT diisi di Pengaturan
- [ ] (Opsional) Kunci VAPID & uji notifikasi push di HP petugas
- [ ] Pelatihan singkat pelapor/petugas (USER_GUIDE.md), admin (ADMIN_GUIDE.md)
