# Kerangka halaman

Markup di bawah ditulis sebagai HTML dengan kelas Tailwind supaya bisa dipindah ke Svelte, React, atau Vue. Bagian
yang interaktif (sidebar, tema) cukup mengganti atribut dan kelas, tanpa pustaka animasi.

## Skrip awal di `<head>`

Dipasang sebelum CSS dan sebelum halaman tergambar, supaya mode gelap tidak berkedip putih. Ganti awalan kunci
`aplikasi.` dengan nama aplikasi tim.

```html
<script>
  (function () {
    try {
      var akar = document.documentElement;
      var mode = localStorage.getItem('aplikasi.mode'); // terang | gelap | sistem
      var gelap = mode === 'gelap' || (mode === 'sistem' && matchMedia('(prefers-color-scheme: dark)').matches);
      if (gelap) akar.dataset.semantik = 'Gelap';
      akar.style.colorScheme = gelap ? 'dark' : 'light';
      var tema = localStorage.getItem('aplikasi.tema'); // Samudra | Mawar, kosong = Daun
      if (tema === 'Samudra' || tema === 'Mawar') akar.dataset.tema = tema;
      if (localStorage.getItem('aplikasi.kepadatan') === 'padat') akar.dataset.kepadatan = 'Padat';
    } catch (e) {}
  })();
</script>
```

Saat pengguna mengganti pilihan, ubah atribut yang sama di `document.documentElement` dan simpan ke `localStorage`.
Mode `sistem` mendengarkan `matchMedia('(prefers-color-scheme: dark)')`. `<html lang="id">`.

## Kerangka aplikasi dengan sidebar 3D stack

Sidebar tertutup bawaan. Saat dibuka, halaman mundur dan bergeser ke kanan dengan dua lapisan di belakangnya, sidebar
tampak di bawah tumpukan. Klik halaman, `Escape`, atau pindah halaman menutupnya. Fokus pindah ke menu pertama saat
dibuka dan kembali ke tombol saat ditutup; halaman di belakang `inert`.

```html
<div class="tumpukan bg-background relative h-screen overflow-hidden" data-sidebar="tutup">
  <aside id="sidebar" class="sidebar absolute inset-y-0 left-0 flex w-64 flex-col gap-2 p-2" aria-label="Menu utama" inert>
    <a class="text-foreground hover:bg-accent flex items-center gap-2.5 rounded-md p-2 no-underline" href="/">
      <span class="bg-primary text-primary-foreground flex size-8 flex-none items-center justify-center rounded-md">
        <!-- ikon aplikasi Phosphor 18 px -->
      </span>
      <span class="flex min-w-0 flex-col">
        <strong class="text-base">Nama aplikasi</strong>
        <small class="text-muted-foreground text-xs">RSUD Bali Mandara</small>
      </span>
    </a>
    <nav class="flex flex-col gap-0.5 py-1">
      <p class="text-muted-foreground m-0 flex h-7 items-center px-2 text-xs font-bold">Menu</p>
      <!-- item aktif: tambah bg-accent font-bold, ikon text-aksen, aria-current="page" -->
      <a class="item-menu" href="/daftar"><!-- ikon size-4 -->Daftar</a>
    </nav>
    <div class="flex-1"></div>
    <!-- kartu kecil di bawah: Pintasan keyboard, "Tekan ? untuk melihat semua pintasan." -->
  </aside>

  <div class="lapisan lapisan-2 bg-pressed absolute inset-0 border" aria-hidden="true"></div>
  <div class="lapisan lapisan-1 bg-accent absolute inset-0 border" aria-hidden="true"></div>

  <div class="halaman bg-background absolute inset-0 flex flex-col overflow-hidden">
    <header class="bg-halaman flex h-14 flex-none items-center gap-2 border-b pr-6 pl-4 max-md:pr-3 max-md:pl-2">
      <!-- tombol ghost icon "Buka menu", aria-expanded, aria-controls="sidebar", ikon SidebarSimple -->
      <span class="bg-border h-4 w-px" aria-hidden="true"></span>
      <nav aria-label="Lokasi" class="flex min-w-0 flex-1 items-center gap-1.5 pl-1.5">
        <a href="/" class="text-muted-foreground hover:text-foreground no-underline max-md:hidden">Nama aplikasi</a>
        <!-- ikon CaretRight size-3.5 text-muted-foreground -->
        <span class="truncate" aria-current="page">Daftar</span>
      </nav>
      <!-- pencarian (opsional), pemilih tema, menu akun -->
    </header>
    <!-- isi halaman -->
  </div>
  <!-- saat terbuka: <button class="halaman penutup absolute inset-0" aria-label="Tutup menu"></button> -->
</div>
```

```css
.tumpukan {
  perspective: 1800px;
  --geser: calc(var(--spacing) * 64 + 72px);
  --gerak: 250ms cubic-bezier(0.23, 1, 0.32, 1);
}
.sidebar { opacity: 0; translate: -16px 0; transition: opacity var(--gerak), translate var(--gerak); }
.halaman, .lapisan {
  transform-origin: left center;
  transition: transform var(--gerak), opacity var(--gerak), border-radius var(--gerak), box-shadow var(--gerak);
}
.lapisan { opacity: 0; }
.penutup { background: transparent; border-radius: var(--radius-xl); cursor: pointer; }
[data-sidebar='buka'] .sidebar { opacity: 1; translate: 0 0; }
[data-sidebar='buka'] .halaman {
  transform: translateX(var(--geser)) scale(0.9) rotateY(-6deg);
  border-radius: var(--radius-xl);
  box-shadow: var(--bayangan-melayang), 0 24px 48px rgb(0 0 0 / 0.18);
}
[data-sidebar='buka'] .lapisan-1 {
  opacity: 1; border-radius: var(--radius-xl);
  transform: translateX(calc(var(--geser) - 28px)) scale(0.86) rotateY(-6deg);
}
[data-sidebar='buka'] .lapisan-2 {
  opacity: 0.8; border-radius: var(--radius-xl);
  transform: translateX(calc(var(--geser) - 56px)) scale(0.82) rotateY(-6deg);
}
.item-menu {
  display: flex; align-items: center; gap: calc(var(--spacing) * 2);
  height: calc(var(--spacing) * 8); padding-inline: calc(var(--spacing) * 2);
  border-radius: var(--radius-md); color: var(--color-foreground); font-size: var(--text-base);
  text-decoration: none; outline: none; transition: background-color 100ms ease-out;
}
.item-menu:hover { background: var(--color-accent); }
.item-menu:focus-visible { box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-ring) 50%, transparent); }
@media (prefers-reduced-motion: reduce) {
  .sidebar, .halaman, .lapisan { transition: opacity var(--gerak); }
  .sidebar { translate: 0 0; }
  [data-sidebar='buka'] .halaman { transform: translateX(var(--geser)); }
  [data-sidebar='buka'] .lapisan-1, [data-sidebar='buka'] .lapisan-2 { opacity: 0; }
}
```

Hanya `transform` dan `opacity` yang bergerak, jadi animasi tetap mulus. Menu sidebar sedikit (di bawah tujuh) dan
hanya berisi halaman yang boleh dibuka akun itu.

## Halaman daftar

```html
<main class="bg-halaman flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4
             max-md:gap-3 max-md:p-3 min-[1440px]:gap-5 min-[1440px]:p-6">
  <!-- 1. Judul dan aksi -->
  <div class="flex flex-none flex-wrap items-end gap-3">
    <div class="flex min-w-0 flex-1 flex-col gap-0.5">
      <h1 class="text-judul m-0 font-bold tracking-[-0.01em]">Antrean klaim</h1>
      <p class="text-muted-foreground m-0 text-base max-md:hidden">Satu kalimat tentang isi halaman dan alurnya.</p>
    </div>
    <div class="flex flex-wrap items-center gap-2 max-md:w-full">
      <!-- tombol outline bg-card, pencarian, satu tombol utama -->
    </div>
  </div>

  <!-- 2. Kartu filter berona tema (lihat komponen.md) -->
  <!-- 3. Kartu ringkasan, paling banyak empat, bisa disembunyikan -->
  <!-- 4. Kartu tabel: bilah tab, tabel, kaki -->
  <div class="bg-card flex min-h-[420px] flex-1 flex-col overflow-hidden rounded-xl border shadow-xs
              max-md:min-h-0 max-md:flex-none">
    <!-- bilah tab bg-tema h-12 -->
    <div class="relative min-h-0 flex-1">
      <!-- tabel (max-md:hidden) dan daftar kartu (md:hidden) -->
    </div>
    <!-- kaki bg-tema h-13 -->
  </div>
</main>
```

- Di bawah 1440 px jarak tepi 16 px supaya tabel 1320 px tetap muat di layar 1366 px. Pada layar pendek (zoom 150%)
  halaman bergulir dan tabel tetap minimal 420 px.
- Kartu filter: pemilih tetap `bg-control` di atas `bg-tema`. Segmen jenis data di ujung kanan (`ml-auto`).
- Di HP tabel diganti daftar kartu: `<ul>` dengan tiap baris tombol penuh `flex flex-col gap-1.5 px-3 py-3 border-b`,
  baris terpilih `bg-selected`, isi: nama dan status di atas, metadata `text-xs text-muted-foreground`, lalu nilai.

## Ruang kerja (detail satu data)

Mode fokus tanpa sidebar. Bilah identitas di atas, dua panel berdampingan di bawahnya.

```html
<div class="bg-background flex h-screen flex-col overflow-hidden">
  <header class="bg-tema border-tema-garis flex h-(--bilah-pasien) flex-none items-center gap-3 border-b px-4">
    <!-- tombol kembali ghost, nama text-lg font-bold, metadata text-xs text-muted-foreground dipisah  ·  ,
         status di kanan, tombol hantu "Cara kerja" paling kanan -->
  </header>
  <div class="bg-halaman flex min-h-0 flex-1 gap-3 p-3 max-md:flex-col">
    <section class="bg-well min-w-0 flex-1 overflow-hidden rounded-lg border"><!-- dokumen / PDF --></section>
    <!-- pemisah yang bisa digeser, lebar panel disimpan per browser -->
    <!-- lebar awal 420 px, bisa diperlebar selama dokumen tetap minimal 480 px -->
    <aside class="bg-card flex w-[420px] max-w-full flex-none flex-col overflow-hidden rounded-lg border max-md:h-[85svh]">
      <nav class="bg-tema border-tema-garis flex h-(--kepala-panel) flex-none items-center justify-between border-b px-3">
        <!-- tab lompat ke bagian, menandai bagian yang terlihat (scroll-spy), dengan penanda status -->
      </nav>
      <div class="bg-halaman flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
        <!-- kartu bagian bernomor (komponen.md: Pita bagian), berurutan, tanpa tombol Lanjut -->
      </div>
      <footer class="flex h-(--kaki-panel) flex-none items-center justify-between gap-3 border-t px-4">
        <span class="text-muted-foreground text-xs" role="status">Tersimpan 14.32</span>
        <!-- satu tombol utama: "Simpan & data berikutnya"  kbd Ctrl Enter -->
      </footer>
    </aside>
  </div>
</div>
```

- Data berikutnya dimuat di belakang layar, dan tombol berikutnya selalu terlihat.
- Isian tidak berlaku disembunyikan sampai relevan. Nilai nol diringkas (`12 kategori lain Rp 0`).
- Bantuan tidak menetap di layar: panel melayang 420 px dari tombol hantu, ditutup `Tutup` atau `Esc`.

## Halaman masuk

```html
<main class="bg-background flex min-h-screen">
  <aside class="bg-primary text-primary-foreground relative hidden w-1/2 flex-none flex-col justify-between
                overflow-hidden p-12 min-[1300px]:flex">
    <span class="bg-primary-foreground/5 pointer-events-none absolute -top-24 -right-24 size-[380px] rounded-full" aria-hidden="true"></span>
    <span class="border-primary-foreground/10 pointer-events-none absolute -top-44 -right-10 size-[560px] rounded-full border" aria-hidden="true"></span>
    <div class="relative flex items-center gap-3">
      <span class="bg-primary-foreground text-aksen flex size-10 items-center justify-center rounded-lg"><!-- ikon 22 px --></span>
      <div class="flex flex-col"><span class="text-lg font-bold">Nama aplikasi</span><span class="text-xs opacity-75">RSUD Bali Mandara</span></div>
    </div>
    <div class="relative flex flex-col gap-7">
      <span class="border-primary-foreground/20 bg-primary-foreground/10 flex w-fit items-center gap-1.5 rounded-full
                   border py-1 pr-3 pl-2.5 text-xs font-bold"><!-- ikon 14 px -->Lingkup aplikasi</span>
      <h1 class="m-0 max-w-[560px] text-[40px] leading-[1.2] font-extrabold tracking-[-0.02em]">Kalimat tujuan aplikasi</h1>
      <p class="m-0 max-w-[520px] text-lg leading-[1.5] opacity-80">Satu atau dua kalimat penjelas.</p>
      <ul class="m-0 flex list-none flex-col gap-3.5 p-0">
        <li class="flex items-center gap-3 text-base font-bold">
          <span class="bg-primary-foreground/10 flex size-8 items-center justify-center rounded-md"><!-- ikon 16 px --></span>
          Keunggulan pertama
        </li>
      </ul>
    </div>
    <p class="border-primary-foreground/20 relative m-0 border-t pt-5 text-xs opacity-60">Instalasi pemilik dan tahun</p>
  </aside>
  <section class="relative flex min-w-0 flex-1 flex-col items-center justify-center px-4 py-12 min-[1300px]:px-12">
    <p class="text-muted-foreground absolute top-10 right-12 m-0 text-sm">
      Tidak bisa masuk? <span class="text-aksen font-bold">Hubungi bagian IT</span>
    </p>
    <div class="flex w-full max-w-[380px] flex-col gap-6">
      <!-- judul "Masuk" text-judul font-bold, keterangan text-muted-foreground,
           isian nama pengguna dan kata sandi h-9, galat dari server di bawah form text-bad,
           tombol utama lebar penuh size lg -->
    </div>
  </section>
</main>
```

Lingkaran di panel kiri adalah satu-satunya hiasan yang diizinkan di seluruh aplikasi, dan hanya di halaman masuk.
