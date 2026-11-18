# SIKAT

**Sistem Informasi Kelola Aduan & Tindak Lanjut** — helpdesk IT, pemeliharaan
perangkat, dan manajemen pekerjaan IT rumah sakit.

## Status

| Phase | Status |
|---|---|
| 0 — Analisis & arsitektur | Selesai |
| 1 — Foundation | Selesai |
| 2 — QR Ticketing | Selesai |
| 3 — Work Management | Selesai |
| 4 — Asset Maintenance | Selesai |
| 5 — SLA & Eskalasi | Selesai |
| 6 — PWA & Notifikasi | Selesai |
| 7 — Dashboard & Reporting | Selesai |
| 8 — SIMRS Integration Ready | Selesai |
| 9 — Production Ready | **Selesai, menunggu validasi** |

Fitur yang sudah berjalan: login & keamanan, RBAC, master data, perangkat,
pengguna, pengaturan sistem, jejak audit, QR ruangan/perangkat, pengajuan
tiket tanpa login, prioritas otomatis, pemantauan tiket pelapor, antrean & klaim
tiket, pencatatan tiket via telepon/WA, alur kerja tiket lengkap (tunda, selesai,
konfirmasi pelapor, ambil alih, prioritas manual), tutup otomatis, kegiatan internal IT,
riwayat & statistik perangkat, sparepart & biaya, deteksi perangkat sering rusak,
SLA berbasis kalender kerja & hari libur, eskalasi otomatis, dashboard & laporan SLA bulanan,
inbox notifikasi, Web Push (VAPID) dengan preferensi per role, PWA installable,
dashboard per role, 15 laporan (termasuk laporan koordinator pengganti spreadsheet)
dengan filter dan ekspor Excel/PDF, koordinator per bidang, triase & koreksi jenis gangguan,
integrasi master data siap SIMRS lewat API (pemetaan external_id, perlindungan perubahan
lokal, log & retry; panduan di [docs/INTEGRATION.md](docs/INTEGRATION.md)), backup harian
dengan uji restore, retensi foto lampiran, dan paket deploy Linux (systemd + Nginx + HTTPS).
Rincian di [CHANGELOG.md](CHANGELOG.md).

## Mulai cepat

```bash
cp .env.example .env
```

```bash
go build -o bin/sikat ./cmd/server
```

```bash
./bin/sikat migrate up
```

```bash
./bin/sikat create-admin
```

```bash
./bin/sikat serve
```

Panduan lengkap: [docs/INSTALLATION.md](docs/INSTALLATION.md).

## Dokumen

| Dokumen | Isi |
|---|---|
| [docs/USER_GUIDE.md](docs/USER_GUIDE.md) | Panduan pelapor, petugas, koordinator |
| [docs/ADMIN_GUIDE.md](docs/ADMIN_GUIDE.md) | Panduan Admin IT |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Deployment produksi, backup & pemulihan, update |
| [docs/SECURITY.md](docs/SECURITY.md) | Hasil audit keamanan & uji beban |
| [docs/INTEGRATION.md](docs/INTEGRATION.md) | Integrasi SIMRS |
| [docs/INSTALLATION.md](docs/INSTALLATION.md) | Instalasi, konfigurasi, development, menjalankan test |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Arsitektur sistem & keputusan teknis |
| [docs/ERD.md](docs/ERD.md) | Entity Relationship Diagram |
| [docs/DATABASE.md](docs/DATABASE.md) | Rancangan database |
| [docs/schema-draft.sql](docs/schema-draft.sql) | DDL lengkap seluruh fase (tervalidasi) |
| [docs/UI_WIREFRAMES.md](docs/UI_WIREFRAMES.md) | Wireframe antarmuka |
| [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) | Rencana per phase, asumsi, risiko, pertanyaan terbuka |
| [CHANGELOG.md](CHANGELOG.md) | Riwayat perubahan |

## Stack

Go · chi · html/template · HTMX · Tailwind CSS · MySQL 8 · PWA (Web Push/VAPID)

## Struktur

```
cmd/server/        binary sikat (serve, migrate, create-admin, vapid-keys, backup, restore-test)
deploy/            build-release.sh, unit systemd, konfigurasi Nginx
internal/          modul aplikasi (handler → service → repository)
migrations/        migrasi SQL (goose, di-embed)
templates/         template HTML (di-embed)
static/            CSS, JS, ikon (di-embed)
web/tailwind.css   sumber CSS
docs/              dokumentasi
scripts/test.sh    menjalankan seluruh test
```
