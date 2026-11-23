---
name: tampilan-casemix
description: >-
  Tampilan standar aplikasi internal RSUD Bali Mandara, mengikuti aplikasi Casemix: token warna tiga tema
  (Daun, Samudra, Mawar) dengan mode gelap, font Geist, Tailwind CSS 4 dan shadcn (Svelte, React, atau Vue), radius
  10 px, kartu berbingkai, badge status pil, sidebar 3D stack. Pakai skill ini SETIAP KALI membuat, mengubah, atau
  mereview layar, komponen, form, tabel, dashboard, atau halaman masuk aplikasi tim, termasuk saat diminta "buat
  halaman", "samakan dengan Casemix", "tema RS", "mode gelap", atau "rapikan tampilan".
---

# Tampilan Casemix untuk aplikasi tim

Skill ini membuat aplikasi tim terlihat dan terasa sama dengan Casemix: alat kerja padat untuk petugas yang
membuka ratusan data per hari di PC kantor (layar 1366 sampai 1920 px, keyboard dan mouse). Setiap keputusan diuji
dengan satu pertanyaan: **apakah ini membuat satu pekerjaan selesai lebih cepat dan lebih jarang salah?** Elemen
yang tidak menjawabnya dihapus.

Isi folder skill:

| Berkas | Isi |
|---|---|
| `aset/tokens.css` | Token warna tiga tema terang dan gelap, jarak, radius, kepadatan. **Jangan diubah** |
| `aset/tema.css` | Pemetaan token ke Tailwind 4 dan nama shadcn, font, animasi muat |
| `aset/fonts/` | Geist 1.9.2 (woff2, lisensi OFL) |
| `rujukan/komponen.md` | Resep kelas setiap komponen: tombol, isian, kartu, tabel, tab, badge status, dialog, dan lainnya |
| `rujukan/layout.md` | Kerangka halaman: bilah atas, sidebar 3D stack, halaman daftar, ruang kerja, halaman masuk |
| `rujukan/gambar/` | Tangkapan desain acuan (data fiktif). Buka hanya yang relevan |
| `contoh/contoh.html` | Halaman contoh yang bisa dibuka di browser, dengan pemilih tema dan mode gelap |

Baca `rujukan/komponen.md` sebelum menulis komponen, dan `rujukan/layout.md` sebelum menyusun halaman baru.

## 1. Pemasangan

Stack acuan: **Tailwind CSS 4** dan **shadcn** gaya `base-nova` (shadcn-svelte, shadcn/ui React, atau shadcn-vue),
ikon **Phosphor**, notifikasi **Sonner**. Proyek tanpa shadcn tetap bisa memakai token dan resep kelasnya.

1. Salin `aset/tokens.css`, `aset/tema.css`, dan `aset/fonts/` ke satu folder di proyek, misalnya `src/styles/`.
2. CSS utama:

   ```css
   @import './styles/tokens.css';
   @import 'tailwindcss';
   @import 'tw-animate-css';
   @import './styles/tema.css';
   ```

3. `components.json` shadcn: `baseColor` `neutral`, CSS menunjuk ke berkas di atas. Jangan menjalankan `init` yang
   menimpa CSS. Setiap menambah komponen shadcn, sesuaikan dengan bagian **Membersihkan komponen shadcn** di
   `rujukan/komponen.md`.
4. Atribut di `<html>` mengatur tampilan, dipasang skrip kecil di `<head>` sebelum halaman tergambar supaya mode
   gelap tidak berkedip putih (skrip lengkap di `rujukan/layout.md`):

   | Atribut | Nilai | Arti |
   |---|---|---|
   | `data-tema` | tidak ada (Daun), `Samudra`, `Mawar` | Tema warna |
   | `data-semantik` | tidak ada (terang), `Gelap` | Mode gelap |
   | `data-kepadatan` | tidak ada (biasa), `Padat` | Baris tabel 56 px atau 36 px |

   Pilihan tema dan kepadatan disimpan per browser di `localStorage`. Pilihan mode: Terang, Gelap, Sistem.

5. Font hanya dari `aset/fonts/`. Jangan memakai Google Fonts atau CDN font.

## 2. Hanya token, tidak pernah warna mentah

`tema.css` mematikan palet bawaan Tailwind, jadi `bg-red-500`, `text-gray-600`, dan sejenisnya tidak bekerja.
**Hex, rgb, dan nama warna dilarang di layar dan komponen**, karena tema dan mode gelap harus berjalan hanya dengan
mengganti atribut. Kalau sebuah warna tidak bisa dicapai dengan token, tanya tim Casemix, jangan menambah warna.
Tidak ada kelas `dark:` di komponen.

| Kebutuhan | Kelas |
|---|---|
| Latar aplikasi, bilah atas | `bg-background` |
| Latar isi halaman (berona base tema) | `bg-halaman` |
| Kartu, tabel, form, panel | `bg-card` |
| Kartu filter, bilah tab di kepala kartu, kaki tabel, bilah identitas | `bg-tema` + `border-tema-garis` |
| Kepala tabel, pita judul bagian, segmen | `bg-tema-kuat` + `border-tema-garis` |
| Kepala dan pita di luar halaman utama | `bg-muted` |
| Hover | `bg-accent` |
| Ditekan | `bg-pressed` |
| Baris terpilih | `bg-selected` (garis `border-selected` bila perlu) |
| Isian | `bg-control` + `border-input` |
| Teks | `text-foreground` (utama), `text-muted-foreground` (label, metadata), `text-tertiary` (placeholder, nilai belum ada) |
| Garis | `border` (tipis), `border-input` (kontrol), `border-strong` (hover kontrol) |
| Aksi utama | `bg-primary` `text-primary-foreground`, hover `bg-primary-hover` |
| Teks beraksen, tautan | `text-aksen` |
| Status | `text-ok` `text-warn` `text-bad`; `bg-ok-solid` `bg-warn-solid` `bg-bad-solid` `bg-idle-solid`; `bg-warn-bg` (latar peringatan) |
| Tint status | `bg-primary/12`, `bg-bad-solid/10`, `bg-primary/10` |
| Grafik | `chart-1` sampai `chart-5` |
| Skeleton | `bg-skeleton` |

Tiga tema siap pakai (warna aksen 700 dari palet Tailwind, semua pasangan teks lolos kontras 4,5:1):

| Tema | Base | Aksen | Catatan |
|---|---|---|---|
| Daun (bawaan) | white | teal `#00786F` | Latar halaman putih |
| Samudra | white | sky `#0069A8` | |
| Mawar | taupe | pink `#C6005C` | Latar halaman taupe-100 |

**Tidak ada hijau.** Tanda selesai memakai warna tema (`ok` sama dengan aksen). Peringatan amber, gagal merah.

## 3. Tipografi

- **Satu keluarga: Geist**, tiga bobot: Regular 400, Semi Bold 600 (`font-bold`), Bold 700 (`font-extrabold`, hanya
  angka besar dan judul halaman masuk). Jangan memakai `font-medium` atau bobot di bawah 400.
- `body` 13/18 dengan `tabular-nums`, jadi angka di tabel selalu sejajar. Tidak ada font mono, kode memakai Geist
  `font-bold`.
- Hierarki dari bobot dan warna teks, bukan ukuran. Tanpa huruf kapital semua untuk label dan nama. Nama dari
  database yang kapital semua ditampilkan Title Case.

| Kelas | Ukuran | Pakai untuk |
|---|---|---|
| `text-judul font-bold tracking-[-0.01em]` | 24/30 | Judul halaman |
| `text-angka font-extrabold` | 22/28 | Angka kartu ringkasan |
| `text-xl` | 20/26 | Judul dialog besar, total |
| `text-lg font-bold` | 16/22 | Nama di bilah identitas, judul kartu |
| `text-base` | 14/20 | Tombol, isian, subjudul halaman, judul pita bagian (`font-bold`) |
| `text-sm` | 13/18 | Isi tabel, teks umum (bawaan `body`) |
| `text-xs` | 12/16 | Kepala tabel (`font-bold`), label isian, metadata, badge |
| `text-kbd` | 11/14 | Tombol keyboard |

## 4. Bentuk, ukuran, kepadatan

- Grid 4 px (`--ruang-1` sampai `--ruang-10`).
- Radius: kontrol (tombol, isian, pemilih) `rounded-lg` 10 px, item menu dan segmen `rounded-md` 8 px, kbd
  `rounded-xs` 4 px, kartu dan panel `rounded-xl` 14 px, badge dan hitungan `rounded-full`. Elemen di dalam wadah
  memakai radius lebih kecil dari wadahnya.
- Kartu berbingkai: `bg-card rounded-xl border shadow-xs`. Bayangan tipis hanya untuk kartu (`shadow-xs`) dan
  lapisan melayang (`shadow-md`: dialog, popover, menu). Tidak ada bayangan lain.
- Tinggi: tombol di bilah 32 px (bawaan), tombol dan isian form 36 px (`size="lg"`, `h-9`), bilah atas 56 px, kepala
  tabel 40 px, bilah tab di kepala kartu 48 px, kaki tabel 52 px.
- Ukuran yang berubah menurut kepadatan memakai variabel, bukan angka tetap: `h-(--baris-tabel)`,
  `h-(--pita-bagian)`, `h-(--kepala-panel)`, `h-(--bilah-pasien)`, `pt-(--isi-atas)`, `pb-(--isi-bawah)`,
  `gap-(--isi-jarak)`. Kolom tabel tidak berpindah di antara Biasa dan Padat. Varian `padat:` hanya untuk isi yang
  berubah bentuk (dua baris jadi satu).
- Cincin fokus: tombol `focus-visible:ring-3 ring-ring/50`, isian `focus-visible:ring-2 ring-ring/20` (tipis).

## 5. Pola layar

Rincian markup ada di `rujukan/layout.md`. Gambaran acuan: `rujukan/gambar/layar-daftar.png`,
`layar-ruang-kerja.png`, `layar-sidebar.png`, `layar-masuk.png`, `layar-gelap.png`.

1. **Kerangka aplikasi.** Bilah atas 56 px `bg-halaman border-b`: tombol ikon sidebar, garis pemisah, jejak lokasi
   (`Nama aplikasi > Halaman`), lalu di kanan pencarian, pemilih tema, dan menu akun. Sidebar 256 px **tertutup
   bawaan**, dibuka sebagai 3D stack (halaman mundur ke kanan dengan dua lapisan di belakangnya). Menu sedikit dan
   sesuai izin akun.
2. **Halaman daftar (worklist).** Isi `bg-halaman p-4 gap-4` (24 px dan 20 px mulai 1440 px). Urutan dari atas: judul
   dan subjudul satu kalimat dengan aksi di kanan; kartu filter `bg-tema`; kartu ringkasan (paling banyak 4, hanya
   angka yang tidak ada di tempat lain, bisa disembunyikan); kartu tabel berisi bilah tab berhitungan, tabel, dan
   kaki berisi posisi, pintasan, dan halaman. Satu baris satu data. Tabel memakai lebar penuh, hanya tabel yang
   boleh bergeser ke samping.
3. **Ruang kerja (detail).** Mode fokus tanpa sidebar: bilah identitas `bg-tema` di atas, dokumen di kiri dan panel
   kerja di kanan pada halaman yang sama (pemisah bisa digeser). Tidak ada popup dokumen dan tidak ada modal untuk
   pekerjaan utama. Panel kanan berisi bagian bertumpuk yang di-scroll (kartu per bagian dengan pita judul bernomor
   `bg-tema-kuat`), tab di atasnya menandai bagian yang terlihat dan hanya untuk lompat. Satu tombol utama per
   layar, di kanan bawah panel.
4. **Halaman masuk.** Dua kolom mulai 1300 px: kiri panel `bg-primary` berisi identitas, judul besar, dan tiga
   keunggulan; kanan form 380 px. Di bawah 1300 px hanya form.
5. **Layar HP** (di bawah 768 px, `max-md:`) cukup terbaca, tidak dirancang ulang: tabel menjadi daftar kartu, ruang
   kerja menumpuk dokumen di atas panel, jarak tepi 12 px.

Target 1366 sampai 1920 px. Pada zoom 150% (lebar 911 px) halaman tidak boleh bergeser ke samping.

## 6. Status

Status selalu **badge pil dengan ikon berbentuk**, jadi tetap terbaca oleh buta warna dan di cetakan hitam putih.
Bentuk ikon membawa arti (lingkaran putus-putus = belum, seperempat dan setengah = berjalan, centang bergaris =
final, centang penuh = selesai, seru = perlu tindakan, silang = gagal). Resep dan SVG di `rujukan/komponen.md`.

| Arti | Badge |
|---|---|
| Belum dimulai | Garis `border-input bg-card text-foreground` |
| Berjalan, final, selesai | Tint `bg-primary/12 text-aksen` |
| Disetujui (satu status paling penting) | Solid `bg-primary text-primary-foreground` |
| Perlu perbaikan, peringatan | `bg-warn-bg text-warn` |
| Gagal | `bg-bad-solid/10 text-bad` |
| Menunggu sistem | `bg-muted text-muted-foreground` |

Tekankan dengan meredupkan yang lain: yang sudah beres tenang, yang perlu tindakan berwarna dan tebal. Nilai nol tidak
ditampilkan sebagai badge (`3 file gagal` hanya muncul kalau ada).

## 7. Memuat, kosong, gagal

Setiap tampilan yang mengambil data wajib punya ketiganya.

| Keadaan | Aturan |
|---|---|
| Memuat | **Skeleton** dengan bentuk dan ukuran sama persis dengan hasil akhir (baris tabel setinggi baris asli). Tidak ada teks `Memuat...`. Spinner hanya di dalam tombol untuk aksi di bawah 1 detik |
| Memuat ulang | Garis progres 2 px di bawah kepala tabel, muncul setelah 200 ms (`animate-tunda`, `animate-muat`). Data lama tetap tampil |
| Kosong | Judul `text-base font-bold` + keterangan apa yang terjadi dan langkah berikutnya: `Tidak ada klaim rawat jalan untuk 1–22 Nov 2026. Ubah periode, atau lihat Rawat inap.` |
| Gagal | Judul `text-bad`, pesan dari server, tombol garis `Coba lagi`, `role="alert"`. Input yang ditolak server (422) memakai keadaan kosong dengan alasannya, tanpa coba lagi |

Proses panjang memakai progres yang jujur (`5 dari 9 dokumen siap`), tampilkan bagian yang sudah siap lebih dulu.

## 8. Gerak

| Interaksi | Aturan |
|---|---|
| Aksi keyboard (pindah baris, simpan, pindah dokumen) | Tanpa animasi sama sekali |
| Hover | Hanya warna, tanpa transform |
| Tombol ditekan | `scale(0.97)` 100 ms `ease-out` |
| Dialog, popover, menu | Masuk 150 ms `cubic-bezier(0.23, 1, 0.32, 1)` dari `scale(0.97)` dan opacity 0, keluar 100 ms, tanpa geser, `transform-origin` dari pemicu |
| Sidebar 3D stack | 250 ms, hanya `transform` dan `opacity` |
| `prefers-reduced-motion` | Transform dihilangkan, fade opacity tetap |

Tidak pernah `transition: all` (pakai `transition-colors` atau daftar properti), tidak pernah `ease-in`, tidak ada
animasi di atas 250 ms.

## 9. Bahasa dan format

- Bahasa Indonesia baku yang singkat. Label tombol menyebut aksinya: `Simpan & data berikutnya`, `Kirim ke E-Klaim`,
  bukan `Submit` atau `OK`.
- Tanpa em dash (—) di kalimat. Pakai koma, titik dua, atau kalimat baru. Rentang memakai en dash tanpa spasi
  (`1–22 Nov 2026`) atau kata "sampai". Tanpa pola "bukan X, melainkan Y". Tanpa emoji.
- Rupiah `Rp 198.100`, minus `-Rp 206.342` dengan `text-bad`. Angka ribuan bertitik (`1.284`).
- Tanggal `26 Okt 2026`, jam `14.32`, zona WITA. Waktu relatif hanya sampai 6 hari (`2 hari lalu`).
- Jumlah `1 klaim`, `34 klaim`, dan angka 0 punya kalimat sendiri (`Belum ada klaim`).
- Notifikasi memakai Sonner di kanan bawah, tidak pernah `alert()`. Konfirmasi memakai Dialog, tidak pernah
  `confirm()`.

## 10. Ikon

Phosphor gaya regular 16 px (`size-4`), hanya bila memperjelas: navigasi, unduh, cetak, kartu ringkasan, menu.
Selebihnya cukup label teks. Tidak ada Lucide (ganti impor bawaan shadcn), tidak ada ikon kilau/AI, tidak ada centang
sebagai bullet. Panah pemilih memakai caret 10x6 px (SVG di `rujukan/komponen.md`).

## 11. Keyboard

- Pintasan untuk aksi yang sering: pindah baris, buka, cari, simpan dan lanjut. `?` membuka daftar semua pintasan.
- Chip `kbd` hanya tampil di kaki tabel, pencarian, dan tombol utama. Selebihnya lewat tooltip.
- Baris yang dipilih keyboard memakai `aria-current`, teks jumlah memakai `role="status"`.

## 12. Dilarang

Gradasi, latar dekoratif (orb, dot grid, blur, kaca), warna pelangi atau neon, ungu-hitam, garis warna di sisi kiri
kartu, bento grid, tiga kartu fitur berjajar, animasi hover atau panah beranimasi, radius di atas 14 px, Inter atau
font lain, ikon Lucide, emoji, em dash, `font-medium`, `shadow-sm` atau bayangan besar, `transition-all`, kelas
`dark:`, hex di komponen, kolom panduan yang menetap di layar (bantuan dibuka dari tombol dan ditutup `Esc`).

## 13. Sebelum selesai

- Periksa layar di lebar 1366 px, 1920 px, zoom 150%, dan 375 px. Tidak ada geser samping selain tabel.
- Ganti tema ke Samudra dan Mawar, nyalakan mode gelap dan Padat: semua harus tetap terbaca tanpa mengubah kode.
- Isi dengan data terburuk: nama sangat panjang dan sangat pendek, 0 / 1 / ratusan baris, nominal `Rp 0` dan
  `Rp 1.284.550.000`, semua status sekaligus. Teks panjang dipotong dengan `truncate` dan `title`, tidak meluber.
- Data contoh, tangkapan layar, dan tes memakai data fiktif. Data pasien asli tidak boleh masuk desain, tes, log,
  atau commit.
- Bandingkan dengan `contoh/contoh.html` dan gambar di `rujukan/gambar/`. Kalau ragu, ikuti yang ada di Casemix.
