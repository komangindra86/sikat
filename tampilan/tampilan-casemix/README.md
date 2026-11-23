# Skill tampilan Casemix

Skill Claude Code supaya aplikasi tim lain memakai tampilan yang sama dengan Casemix RSUD Bali Mandara (rilis
v12.5.2, 23 Nov 2026).

## Memasang

Salin seluruh folder `tampilan-casemix` ke salah satu tempat ini:

- `.claude/skills/tampilan-casemix/` di repo aplikasi tim (berlaku untuk semua anggota tim yang membuka repo itu), atau
- `%USERPROFILE%\.claude\skills\tampilan-casemix\` (berlaku untuk satu komputer di semua proyek).

Claude Code akan memakai skill ini setiap kali diminta membuat atau mengubah tampilan. Bisa juga dipanggil langsung
dengan `/tampilan-casemix`.

Lalu pasang asetnya di proyek (langkah lengkap di `SKILL.md` bagian 1): salin `aset/tokens.css`, `aset/tema.css`,
dan `aset/fonts/`, kemudian impor di CSS utama.

## Melihat contoh

Buka `contoh/contoh.html` lewat server lokal (misalnya Laragon di `http://localhost/...`), bukan klik dua kali, supaya
font terbaca. Contoh ini memuat Tailwind dan ikon Phosphor dari CDN, jadi perlu internet. Tombol di bilah atas
mengganti tema, mode gelap, kepadatan, dan membuka sidebar.

## Memperbarui

Token warna dan aturan dipegang tim Casemix. Kalau tim butuh warna atau komponen baru, minta ke tim Casemix supaya
semua aplikasi tetap sama, lalu ganti folder ini dengan versi terbarunya. Jangan mengedit `aset/tokens.css` di proyek
tim.

## Lisensi font

Geist 1.9.2, SIL Open Font License (`aset/fonts/GEIST-OFL.txt`).
