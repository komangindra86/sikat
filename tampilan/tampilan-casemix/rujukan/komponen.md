# Resep komponen

Kelas di bawah diambil dari kode Casemix rilis v12.5.2 dan berlaku sama di shadcn-svelte, shadcn/ui React, dan
shadcn-vue (ganti `class` dengan `className` di React). Atribut `data-state`, `data-slot`, dan `aria-*` dipasang
pustaka primitif (Bits UI, Radix, Reka). Proyek tanpa shadcn bisa menempelkan kelas yang sama ke elemen HTML biasa.

## Membersihkan komponen shadcn

Setiap komponen yang ditambah dengan CLI shadcn disesuaikan sebelum dipakai:

1. Ganti ikon Lucide dengan Phosphor regular (`phosphor-svelte`, `@phosphor-icons/react`, `@phosphor-icons/vue`).
2. Hapus `dark:*`, `shadow-xs` pada kontrol selain tombol garis, `shadow-sm`, dan `font-medium`.
3. Ganti `transition-all` dengan `transition-colors` atau daftar properti yang memang bergerak.
4. Radius: kontrol `rounded-lg`, item menu `rounded-sm` atau `rounded-md`, kartu `rounded-xl`.
5. Lapisan melayang: animasi 150 ms masuk, 100 ms keluar, `zoom-in-[0.97]`, `ring-1 ring-border`, `shadow-md`,
   `align="start"` kalau lebih lebar dari pemicunya.
6. Ukur hasilnya di browser (tinggi, padding, radius) dan samakan dengan `rujukan/gambar/fondasi-komponen.png`.

## Tombol

```text
dasar   : inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-lg border
          border-transparent bg-clip-padding text-base outline-none select-none
          transition-[color,background-color,border-color,scale] duration-100 ease-out
          active:not-aria-[haspopup]:scale-[0.97]
          focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50
          disabled:pointer-events-none disabled:opacity-50 [&_svg:not([class*='size-'])]:size-4
```

| Varian | Kelas tambahan | Dipakai untuk |
|---|---|---|
| `default` | `bg-primary text-primary-foreground hover:bg-primary-hover font-bold disabled:bg-accent disabled:text-tertiary disabled:opacity-100` | Satu aksi utama per layar |
| `outline` | `border-input bg-control shadow-xs hover:border-strong hover:bg-accent aria-expanded:bg-accent` | Aksi kedua, pemicu pemilih |
| `ghost` | `text-muted-foreground hover:bg-accent hover:text-foreground disabled:text-tertiary disabled:opacity-100` | Bilah atas, halaman sebelumnya/berikutnya |
| `destructive` | `bg-destructive/10 hover:bg-destructive/20 text-destructive` | Hapus, batalkan |
| `link` | `text-primary underline-offset-4 hover:underline` | Tautan di dalam teks |

| Ukuran | Kelas | Dipakai untuk |
|---|---|---|
| bawaan | `h-8 px-3` | Bilah, kepala kartu |
| `lg` | `h-9 px-3` | Form, dialog, keadaan gagal |
| `sm` | `h-7 px-2.5 rounded-md text-[0.8rem]` | Di dalam baris tabel |
| `icon` | `size-8` | Tombol ikon bilah atas |

Teks tombol menyebut aksinya. Ikon di depan teks hanya bila memperjelas (unduh, cetak).

## Isian

```text
input   : h-9 w-full min-w-0 rounded-lg border border-input bg-control px-3 py-1 text-base outline-none
          transition-colors placeholder:text-muted-foreground
          focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/20
          aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20
          disabled:cursor-not-allowed disabled:opacity-50
textarea: sama dengan input, ditambah min-h-16 px-2.5 py-2 field-sizing-content
label   : text-xs font-bold (di atas isian, jarak gap-1.5)
galat   : text-xs text-bad di bawah isian
satuan  : <span class="text-tertiary pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-xs">hari</span>
          dan isian diberi pr-12 text-right
```

Isian selalu bergaris. Isian form 36 px (`h-9`), isian di bilah filter 32 px (`h-8`). Nilai kosong pada pemilih memakai
`text-tertiary`.

## Pemilih (Select)

```text
pemicu  : flex w-fit items-center justify-between gap-1.5 rounded-lg border border-input bg-control py-2 pr-2 pl-3
          text-sm whitespace-nowrap outline-none data-[size=default]:h-8
          data-placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/20
isi     : min-w-36 rounded-lg bg-popover p-1 shadow-md ring-1 ring-border
item    : relative flex h-8 w-full items-center gap-1.5 rounded-sm pr-8 pl-2 text-sm
          data-highlighted:bg-accent data-disabled:opacity-50
```

Panah pemilih memakai caret ini, bukan ikon 16 px:

```html
<svg class="text-muted-foreground flex-none" width="10" height="6" viewBox="0 0 10 6" fill="none" aria-hidden="true">
  <path d="M1 1L5 5L9 1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
</svg>
```

Pemilih ringkas di bilah filter: tombol `outline` 32 px berisi nilai terpilih `font-bold` dan caret.

## Centang dan saklar

```text
checkbox: flex size-4 flex-none items-center justify-center rounded-[3px] border border-input bg-card
          data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground
          focus-visible:ring-3 focus-visible:ring-ring/50
switch  : inline-flex h-4 w-7 flex-none items-center rounded-full p-0.5 transition-colors
          data-[state=checked]:bg-primary data-[state=unchecked]:bg-strong
thumb   : block size-3 rounded-full bg-primary-foreground transition-transform data-[state=checked]:translate-x-3
```

## Kartu

```text
kartu biasa    : rounded-xl border bg-card shadow-xs
kartu filter   : flex flex-wrap items-center gap-2 rounded-xl border border-tema-garis bg-tema p-3 shadow-xs
pemisah filter : <span class="bg-border mx-1 h-5 w-px flex-none" aria-hidden="true"></span>
label filter   : text-muted-foreground whitespace-nowrap (di depan pemilih, satu grup supaya turun baris bersama)
```

Kartu ringkasan (paling banyak empat sejajar, `grid grid-cols-4 gap-4 max-lg:grid-cols-2`):

```html
<div class="bg-card flex h-[104px] min-w-0 flex-col justify-between gap-2 rounded-xl border p-4 shadow-xs">
  <span class="text-muted-foreground flex items-center justify-between gap-2 text-sm font-bold">
    Tarif RS
    <span class="bg-primary/10 text-aksen flex size-7 flex-none items-center justify-center rounded-md">
      <!-- ikon Phosphor 15 px -->
    </span>
  </span>
  <b class="text-angka truncate font-extrabold">Rp 58.412.300</b>
</div>
```

Angka minus: kotak ikon `bg-bad-solid/10 text-bad`, angka `text-bad`. Catatan kecil di sebelah angka berupa pil
`bg-bad-solid/10 text-bad rounded-full px-2 py-px text-xs font-bold`, hanya kalau nilainya bukan nol.

## Tabel

Tabel selalu di dalam kartu: `bg-card rounded-xl border shadow-xs flex flex-col overflow-hidden`.

```text
table   : w-full min-w-[1320px] table-fixed border-separate border-spacing-0 text-sm (lebar kolom di <colgroup>,
          satu kolom tanpa lebar menyerap sisa)
kepala  : sticky top-0 z-10 h-10 border-b border-tema-garis bg-tema-kuat px-2 text-left text-xs font-bold
          text-foreground whitespace-nowrap first:pl-4 last:pr-4
sel     : h-(--baris-tabel) overflow-hidden border-b px-2 py-0 align-middle whitespace-nowrap first:pl-4 last:pr-4
baris   : hover:bg-accent data-[state=selected]:bg-selected (baris kepala: hover:bg-transparent)
```

- Sel dua baris (mode biasa): baris atas `font-bold` atau teks utama, baris bawah `text-muted-foreground text-xs`.
  Mode padat menyatukannya satu baris dengan `padat:flex padat:items-baseline padat:gap-2`.
- Metadata dipisah `  ·  ` dengan `whitespace-pre`, misalnya `No RM  ·  No SEP`.
- Nilai yang belum ada ditulis sebagai teks `text-tertiary` (`Belum dikoding`), bukan tanda strip.
- Teks panjang `truncate` dengan atribut `title` berisi teks lengkap, atau `line-clamp-2 whitespace-normal`.
- Baris dipilih dengan klik, dibuka dengan klik ganda atau Enter, baris terpilih memakai `aria-current="true"`.
- `<caption class="sr-only">` berisi nama tabel, kepala memakai `scope="col"`.

Bilah tab di kepala kartu tabel:

```html
<div class="bg-tema border-tema-garis flex flex-none items-center border-b pr-4">
  <!-- Tabs variant line, list: h-12 gap-5 rounded-none p-0 -->
  <button role="tab" class="group/tab relative h-12 flex-none gap-1.5 px-0.5 text-sm text-muted-foreground
          hover:text-foreground data-active:font-bold data-active:text-foreground
          after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-primary after:opacity-0
          data-active:after:opacity-100">
    Perlu perbaikan
    <small class="bg-card text-muted-foreground group-data-active/tab:bg-primary
                  group-data-active/tab:text-primary-foreground rounded-full px-1.5 text-xs font-bold">12</small>
  </button>
</div>
```

Kaki kartu tabel:

```html
<footer class="bg-tema border-tema-garis flex h-13 flex-none items-center justify-between gap-2 border-t px-4">
  <span class="text-muted-foreground truncate" role="status">Menampilkan 1–20</span>
  <span class="text-muted-foreground flex gap-3.5 text-xs max-lg:hidden">
    <span class="flex items-center gap-1"><kbd>A</kbd> <kbd>S</kbd> pindah baris</span>
  </span>
  <span class="flex flex-none items-center gap-1">
    <!-- tombol ghost Sebelumnya, "1 / 52" text-muted-foreground, tombol ghost Berikutnya -->
  </span>
</footer>
```

## Segmen (pilihan dua sampai empat)

```text
list    : inline-flex h-8 items-center gap-0.5 rounded-lg bg-tema-kuat p-0.5
item    : h-7 flex-none gap-1.5 rounded-md px-2.5 text-sm text-muted-foreground hover:text-foreground
          data-active:bg-card data-active:font-bold data-active:text-foreground data-active:shadow-xs
hitungan: <small class="text-aksen text-xs font-bold">512</small>
```

## Pita bagian dan isian baca saja (ruang kerja)

```html
<section class="bg-card scroll-mt-3 overflow-hidden rounded-xl border shadow-xs">
  <div class="bg-tema-kuat border-tema-garis flex h-(--pita-bagian) items-center justify-between border-b px-5">
    <h3 class="m-0 flex items-center gap-2.5 text-base font-bold">
      <span class="bg-primary text-primary-foreground flex size-5.5 flex-none items-center justify-center rounded-full text-xs">1</span>
      Peserta
    </h3>
    <!-- badge status bagian -->
  </div>
  <div class="grid grid-cols-2 gap-x-4 gap-y-(--isi-jarak) px-5 pt-(--isi-atas) pb-(--isi-bawah)">
    <!-- isian -->
  </div>
</section>
```

Data baca saja sebagai kisi bergaris:

```html
<div class="bg-border grid grid-cols-2 gap-px overflow-hidden rounded-lg border">
  <div class="bg-card flex min-w-0 flex-col gap-px px-3 py-2">
    <span class="text-muted-foreground text-xs">Kelas hak</span>
    <span class="truncate font-bold">Kelas 1</span>
  </div>
</div>
```

Pesan di dalam bagian: `bg-muted flex flex-col gap-0.5 rounded-md border px-2.5 py-2`, judul `font-bold`, isi
`text-muted-foreground text-xs`, `role="status"`.

## Badge status

```text
pil     : inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-bold
polos   : inline-flex items-center gap-1.5 (tanpa wadah, untuk deretan tanda di bilah)
```

| Jenis | Pil | Warna ikon | Bentuk ikon |
|---|---|---|---|
| Belum | `border border-input bg-card text-foreground` | `--idle-solid` | Lingkaran putus-putus |
| Proses | `bg-primary/12 text-aksen` | `--accent-solid` | Lingkaran, seperempat terisi |
| Berjalan lanjut | `bg-primary/12 text-aksen` | `--accent-solid` | Lingkaran, setengah terisi |
| Final | `bg-primary/12 text-aksen` | `--accent-solid` | Lingkaran garis dengan centang |
| Selesai | `bg-primary/12 text-aksen` | `--ok-solid` | Lingkaran penuh dengan centang |
| Disetujui | `bg-primary text-primary-foreground` | `currentColor` | Lingkaran penuh dengan centang |
| Perbaikan | `bg-warn-bg text-warn` | `--warn-solid` | Lingkaran penuh dengan seru |
| Gagal | `bg-bad-solid/10 text-bad` | `--bad-solid` | Lingkaran penuh dengan silang |
| Menunggu sistem | `bg-muted text-muted-foreground` | `currentColor` | Sesuai jenis |

Ikon 12 px di dalam pil (14 px tanpa pil), `viewBox="0 0 14 14"`:

```html
<!-- Belum -->
<circle cx="7" cy="7" r="6" fill="none" stroke="var(--idle-solid)" stroke-width="1.5" stroke-dasharray="2.4 2.4" />
<!-- Proses: lingkaran garis + seperempat -->
<circle cx="7" cy="7" r="6" fill="none" stroke="var(--accent-solid)" stroke-width="1.5" />
<path d="M7 3A4 4 0 0 1 11 7L7 7Z" fill="var(--accent-solid)" />
<!-- Berjalan lanjut: setengah -->
<path d="M7 3A4 4 0 0 1 7 11Z" fill="var(--accent-solid)" />
<!-- Final: lingkaran garis + centang garis warna sama -->
<path d="M4.3 7.2L6.2 9.1L9.8 5.2" fill="none" stroke="var(--accent-solid)" stroke-width="1.5"
      stroke-linecap="round" stroke-linejoin="round" />
<!-- Selesai, Perbaikan, Gagal: lingkaran fill = stroke = warna jenis, tanda memakai --accent-on-solid.
     Di pil Disetujui lingkaran currentColor dan tanda --accent-solid. -->
<path d="M4.3 7.2L6.2 9.1L9.8 5.2" />   <!-- centang -->
<path d="M7 3.9L7 7.6M7 10.1L7 10.1" /> <!-- seru -->
<path d="M5 5L9 9M9 5L5 9" />           <!-- silang -->
```

Hanya satu status yang memakai pil solid, yaitu keputusan akhir yang paling penting di layar itu.

## Kelengkapan (progres kecil)

```html
<span class="inline-flex items-center gap-2 text-xs" title="9 dari 13 dokumen">
  <span class="bg-accent flex h-1.5 w-12 overflow-hidden rounded-full" aria-hidden="true">
    <span class="bg-warn-solid h-full rounded-full" style="width: 69%"></span>
  </span>
  <span class="text-warn font-bold">9/13</span>
</span>
```

Lengkap `bg-primary` / `text-aksen`, sebagian besar `bg-warn-solid` / `text-warn`, sedikit `bg-bad-solid` /
`text-bad`, nol tanpa isi dan `text-tertiary`. Isi minimal 8% kalau bukan nol.

## Kbd

```text
h-5 min-w-5 w-fit inline-flex items-center justify-center gap-1 rounded-xs border border-input bg-muted px-1
font-sans text-kbd font-bold text-muted-foreground pointer-events-none select-none
```

## Lapisan melayang

```text
dialog  : fixed top-1/2 left-1/2 z-50 flex w-[420px] max-w-[calc(100vw-32px)] -translate-x-1/2 -translate-y-1/2
          flex-col gap-1 rounded-lg bg-popover p-5 text-popover-foreground shadow-md ring-1 ring-border
          data-open:animate-in data-open:fade-in-0 data-open:zoom-in-[0.97]
          data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-[0.97]
          duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] data-closed:duration-100
          (overlay transparan, tanpa blur)
popover : w-72 flex flex-col gap-2.5 rounded-lg bg-popover p-2.5 text-sm shadow-md ring-1 ring-border + animasi sama,
          origin dari pemicu
menu    : min-w-32 rounded-lg bg-popover p-1 shadow-md ring-1 ring-border + animasi sama;
          item: flex items-center gap-1.5 rounded-sm px-2 py-1 text-sm focus:bg-accent;
          item hapus: data-[variant=destructive]:text-destructive focus:bg-destructive/10
tooltip : rounded-md bg-foreground px-3 py-1.5 text-xs text-background max-w-xs, tunda 400 ms
toast   : Sonner, kanan bawah, durasi bawaan
```

Panel bantuan (`Cara kerja`) melayang 420 px dari tombol hantu, ditutup dengan `Tutup` atau `Esc`.

## Skeleton dan garis muat

```text
skeleton: bg-skeleton rounded-sm, tinggi 11 px untuk teks 13 px dan 9 px untuk teks 12 px, lebar bervariasi per
          baris (misalnya 200 - (i * 9) % 60 px) supaya tidak terlihat seperti pola
garis muat ulang:
<div class="bg-accent animate-tunda absolute inset-x-0 top-10 z-20 h-0.5 overflow-hidden">
  <div class="bg-primary animate-muat h-full w-1/3 motion-reduce:w-full motion-reduce:animate-none"></div>
</div>
```

## Kosong dan gagal

```html
<!-- Kosong -->
<div class="flex flex-col items-center gap-1 text-center">
  <p class="m-0 text-base font-bold">Belum ada klaim</p>
  <p class="text-muted-foreground m-0 max-w-md text-sm">Tidak ada klaim rawat jalan untuk 1–22 Nov 2026. Ubah periode, atau lihat Rawat inap.</p>
</div>

<!-- Gagal -->
<div class="flex flex-col items-center gap-3 text-center" role="alert">
  <div class="flex flex-col gap-1">
    <p class="text-bad m-0 text-base font-bold">Antrean gagal dimuat</p>
    <p class="text-muted-foreground m-0 text-sm">{pesan dari server}</p>
  </div>
  <!-- tombol outline ukuran lg: Coba lagi, nonaktif dan aria-busy selama mencoba -->
</div>
```

Keadaan kosong dan gagal di tabel ditaruh di tengah area tabel (`absolute inset-0 top-10 flex items-center
justify-center p-4`), kepala tabel tetap terlihat.

## Avatar dan menu akun

Pemicu: `flex max-w-60 items-center gap-2 rounded-lg px-1 py-0.5 hover:bg-accent`, berisi avatar bulat 28 px
(`size-7 rounded-full bg-primary text-primary-foreground text-xs font-bold`, dua huruf awal nama), nama `font-bold`
dan peran `text-muted-foreground text-xs` (disembunyikan di HP), lalu caret.

Menu 300 px, `align="end"`, urutan dipisah garis: `Pintasan keyboard` dengan `kbd ?`; `Kepadatan` dengan segmen
Biasa/Padat; `Keluar`; lalu catatan `text-tertiary text-xs px-2 pt-2 pb-1.5` berisi pemberitahuan bahwa akses data
pasien dicatat, kontak IT, dan versi aplikasi.

## Pemilih tema

Tombol ikon palet di bilah atas membuka popover 272 px (`align="end"`, `p-1`):

1. Label `Mode` (`text-muted-foreground text-xs font-bold px-2`), lalu segmen tiga pilihan Terang, Gelap, Sistem:
   `bg-accent mx-1 flex gap-0.5 rounded-lg p-0.5`, item `h-7 flex-1 rounded-md text-xs aria-checked:bg-card
   aria-checked:font-bold aria-checked:shadow-xs`, `role="radiogroup"` dan `role="radio"`.
2. Garis, label `Tema`, lalu tiga baris tema: nama `font-bold`, keterangan `White · Teal · Teal` `text-xs
   text-muted-foreground`, tiga titik contoh `size-2.5 rounded-full`, dan centang `text-aksen` pada yang aktif
   (`aria-checked:bg-accent`).
