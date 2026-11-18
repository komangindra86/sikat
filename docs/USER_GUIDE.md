# SIKAT — Panduan Pengguna

SIKAT (Sistem Informasi Kelola Aduan & Tindak Lanjut) dipakai untuk melaporkan
gangguan IT dan memantau penanganannya. Panduan ini untuk **pelapor** (semua
pegawai), **petugas IT**, dan **koordinator IT**. Admin IT: lihat ADMIN_GUIDE.md.

---

## 1. Pelapor (pegawai rumah sakit) — tanpa login

### Melaporkan gangguan

1. **Scan QR** yang tertempel di ruangan atau di perangkat dengan kamera HP.
   Lokasi (dan perangkat, bila QR perangkat) terisi otomatis.
2. Isi formulir singkat:
   - **Nama** dan **unit/bagian** Anda (nama diingat di HP untuk laporan berikutnya).
   - **Apa masalahnya?** Pilih yang paling mirip. *Tidak ada yang cocok? Pilih
     **Lainnya** lalu ceritakan — tim IT akan memilahnya.*
   - **Ceritakan kendalanya** (wajib), apakah pelayanan terganggu, siapa yang terdampak.
   - Foto (opsional). **Jangan memotret layar/kertas yang menampilkan data pasien**,
     dan jangan menulis nama pasien, nomor rekam medis, atau diagnosis.
3. Tekan **Kirim laporan**. Anda mendapat **nomor tiket** dan **link pemantauan**.
   Simpan link tersebut (salin atau bagikan ke diri sendiri) — hanya pemegang link
   yang dapat melihat tiket.

### Memantau & mengonfirmasi

- Buka link pemantauan untuk melihat status: Baru Masuk → Diterima → Sedang
  Dikerjakan → (Tertunda) → Selesai → Ditutup. Halaman diperbarui otomatis.
- Anda dapat **menambah keterangan** bila ada informasi baru.
- Setelah petugas menyelesaikan, jawab **"Apakah masalah sudah teratasi?"**:
  *Sudah beres* menutup tiket; *Belum beres* (beri alasan) membuka kembali tiket.
  Bila tidak dijawab, tiket ditutup otomatis setelah 3 hari.

Tidak bisa scan QR? Hubungi IT lewat telepon/WA — petugas akan mencatatkan
laporan Anda.

---

## 2. Petugas IT

Masuk di alamat SIKAT dengan akun dari Admin IT. Login pertama wajib mengganti
password. Di HP, SIKAT dapat dipasang seperti aplikasi (menu browser → *Instal
aplikasi* / *Tambah ke Layar Utama*).

### Beranda

Ringkasan: tiket **tersedia untuk saya** (sesuai kompetensi), **tiket saya**,
sedang dikerjakan, tertunda, selesai bulan ini, tiket kritis, **SLA mendekati
batas**, dan beban kerja tim.

### Antrean & mengambil tiket

- Menu **Tiket** → tab *Antrean*: tiket baru sesuai bidang kompetensi Anda (centang
  *Tampilkan semua bidang* untuk melihat semua). Tiket **Perlu dipilah** (di luar
  daftar) tampil untuk semua petugas.
- Tekan **Ambil tiket**. Satu tiket hanya punya satu penanggung jawab; bila dua
  petugas menekan bersamaan, hanya satu yang berhasil.

### Mengerjakan tiket

Di halaman detail tiket:

| Tindakan | Keterangan |
|---|---|
| **Mulai kerjakan** | Status → Sedang Dikerjakan |
| **Tunda** | Pilih alasan (mis. menunggu sparepart); SLA berhenti hanya bila alasan & kebijakan mengizinkan |
| **Lanjutkan** | Kembali dikerjakan |
| **Selesaikan** | Isi tindakan (boleh pakai template), hasil, waktu layanan pulih; untuk perangkat: sparepart, biaya, kondisi perangkat. Riwayat perangkat terisi otomatis. |
| **Kembalikan ke antrean** | Bila tidak dapat menangani |
| **Tambah anggota pendukung** | Petugas lain ikut menangani |
| **Koreksi jenis gangguan** | Bila jenis yang dipilih pelapor keliru; tiket pindah ke bidang yang tepat |
| **Catatan & foto** | Catatan internal tim, atau catatan yang terlihat pelapor |

Panel **SLA** menunjukkan sisa waktu respons/penyelesaian (hijau aman, kuning
hampir habis, merah terlambat).

### Mencatat tiket dari telepon/WA/datang langsung

Menu Tiket → **Catat tiket**. Pilih sumber laporan, lokasi, perangkat, dan nama
pelapor (ketik untuk memilih dari direktori pegawai). Anda dapat langsung
mengambil tiket tersebut dan membagikan link pemantauan ke pelapor.

### Kegiatan internal

Menu **Kegiatan**: catat pekerjaan di luar tiket (pemeliharaan rutin, instalasi,
backup server, rapat). Kegiatan masuk ke laporan koordinator dan, bila terkait
perangkat, ke riwayat perangkat.

### Notifikasi

- Lonceng di menu menampilkan notifikasi belum dibaca (tiket baru sesuai
  kompetensi, penugasan, tiket tim yang ditunda/selesai/dibuka kembali, SLA).
- **Notifikasi › Pengaturan → Aktifkan notifikasi** agar push muncul di HP walau
  aplikasi ditutup (iPhone: pasang dulu ke Layar Utama). Isi notifikasi di layar
  kunci sengaja umum; detail terlihat setelah membuka aplikasi.
- Saat logout, perangkat berhenti menerima notifikasi akun Anda.

### Laporan pribadi

Menu **Laporan**: laporan berisi pekerjaan Anda sendiri, dapat diunduh Excel/PDF.

---

## 3. Koordinator IT

Semua kemampuan petugas, ditambah:

- **Bidang yang Anda koordinasi** (ditetapkan Admin). Beranda, antrean bawaan, dan
  notifikasi difokuskan ke bidang tersebut; Anda tetap dapat melihat semua tiket
  (*Tampilkan semua bidang*).
- Untuk tiket bidang Anda: **ambil alih**, **ubah prioritas** (dengan alasan),
  **batalkan**, **tutup**, **buka kembali**, atur anggota tim. Bidang tanpa
  koordinator dapat ditindak semua koordinator.
- **Pilah tiket Triase**: banner "N tiket perlu dipilah" di Beranda → buka tiket →
  *Pilah: tentukan jenis gangguan*. Koordinator & petugas bidang tujuan diberi tahu.
- **SLA**: dashboard tiket paling mendesak, eskalasi, dan laporan SLA bulanan.
- **Perangkat sering rusak**: peringatan dengan rekomendasi (evaluasi, pemeriksaan
  lanjutan, pertimbangan penggantian). Rekomendasi bukan keputusan — Anda yang
  memutuskan dan keputusan tercatat di riwayat perangkat.
- **Laporan**: 16 laporan termasuk **Laporan Koordinator IT** (pengganti
  spreadsheet: No, Tanggal, Unit/Bagian, Bidang, Kegiatan, Pelaksana, Pendukung,
  Status, Hasil/Keterangan — terisi otomatis dari tiket & kegiatan), rekap per
  bidang/unit/petugas/prioritas, SLA, waktu respons/penyelesaian, beban kerja,
  pemilahan jenis gangguan, downtime perangkat. Filter periode (WITA) dan unduh
  **Excel/PDF**.

---

## 4. Pertanyaan umum

| Pertanyaan | Jawaban |
|---|---|
| Lupa password | Hubungi Admin IT untuk reset (password sementara, wajib diganti). |
| Akun terkunci | Terlalu banyak gagal login; tunggu beberapa menit atau hubungi Admin IT. |
| Link pemantauan hilang | Hubungi IT dengan menyebut nomor tiket; petugas dapat membagikan ulang dari detail tiket bila tersedia. |
| Notifikasi tidak muncul di HP | Pastikan diaktifkan di Notifikasi › Pengaturan, izin notifikasi browser tidak diblokir, dan (iPhone) SIKAT dibuka dari Layar Utama. Notifikasi tetap tersedia di inbox. |
| Salah memilih jenis gangguan | Tidak masalah — petugas/koordinator akan mengoreksinya. |
