// SIKAT: kontrol form gaya shadcn (Select, Calendar dalam Popover, tombol pilih file).
// Elemen bawaan (<select>, <input type=date|month|datetime-local|file>) tetap ada dan tetap dikirim
// bersama form, tetapi disembunyikan; pengguna memakai kontrol buatan ini. Perubahan nilai memicu
// event "change" pada elemen asli, jadi HTMX (hx-trigger="change") dan app.js tetap berjalan.
(function () {
  "use strict";

  var BULAN = ["Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"];
  var BLN = ["Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"];
  var HARI = ["Sen", "Sel", "Rab", "Kam", "Jum", "Sab", "Min"];
  var IKON = {
    check: "M229.66,77.66l-128,128a8,8,0,0,1-11.32,0l-56-56a8,8,0,0,1,11.32-11.32L96,188.69,218.34,66.34a8,8,0,0,1,11.32,11.32Z",
    kiri: "M165.66,202.34a8,8,0,0,1-11.32,11.32l-80-80a8,8,0,0,1,0-11.32l80-80a8,8,0,0,1,11.32,11.32L91.31,128Z",
    kanan: "M181.66,133.66l-80,80a8,8,0,0,1-11.32-11.32L164.69,128,90.34,53.66a8,8,0,0,1,11.32-11.32l80,80A8,8,0,0,1,181.66,133.66Z",
    kalender: "M208,32H184V24a8,8,0,0,0-16,0v8H88V24a8,8,0,0,0-16,0v8H48A16,16,0,0,0,32,48V208a16,16,0,0,0,16,16H208a16,16,0,0,0,16-16V48A16,16,0,0,0,208,32ZM72,48v8a8,8,0,0,0,16,0V48h80v8a8,8,0,0,0,16,0V48h24V80H48V48ZM208,208H48V96H208V208Z",
    klip: "M209.66,122.34a8,8,0,0,1,0,11.32l-82.05,82a56,56,0,0,1-79.2-79.21L147.67,35.73a40,40,0,1,1,56.61,56.55L105,193A24,24,0,1,1,71,159L154.3,74.38A8,8,0,1,1,165.7,85.6L82.39,170.31a8,8,0,1,0,11.27,11.36L192.93,81A24,24,0,1,0,159,47L59.76,147.68a40,40,0,1,0,56.53,56.62l82.06-82A8,8,0,0,1,209.66,122.34Z",
    cari: "M229.66,218.34l-50.07-50.06a88.11,88.11,0,1,0-11.31,11.31l50.06,50.07a8,8,0,0,0,11.32-11.32ZM40,112a72,72,0,1,1,72,72A72.08,72.08,0,0,1,40,112Z"
  };
  var NS = "http://www.w3.org/2000/svg";
  var urut = 0;

  function el(tag, cls, teks) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (teks != null) e.textContent = teks;
    return e;
  }
  function ikon(nama, cls) {
    var s = document.createElementNS(NS, "svg");
    s.setAttribute("viewBox", "0 0 256 256");
    s.setAttribute("fill", "currentColor");
    s.setAttribute("aria-hidden", "true");
    s.setAttribute("class", cls || "size-4 flex-none");
    var p = document.createElementNS(NS, "path");
    p.setAttribute("d", IKON[nama]);
    s.appendChild(p);
    return s;
  }
  function caret() {
    var s = document.createElementNS(NS, "svg");
    s.setAttribute("width", "10"); s.setAttribute("height", "6"); s.setAttribute("viewBox", "0 0 10 6");
    s.setAttribute("fill", "none"); s.setAttribute("aria-hidden", "true");
    s.setAttribute("class", "flex-none text-muted-foreground");
    var p = document.createElementNS(NS, "path");
    p.setAttribute("d", "M1 1L5 5L9 1"); p.setAttribute("stroke", "currentColor"); p.setAttribute("stroke-width", "1.5");
    p.setAttribute("stroke-linecap", "round"); p.setAttribute("stroke-linejoin", "round");
    s.appendChild(p);
    return s;
  }
  function tombol(cls, teks) {
    var b = el("button", cls, teks);
    b.type = "button";
    return b;
  }
  function picu(asli) {
    asli.dispatchEvent(new Event("input", { bubbles: true }));
    asli.dispatchEvent(new Event("change", { bubbles: true }));
  }
  function namaLabel(asli) {
    if (asli.getAttribute("aria-label")) return asli.getAttribute("aria-label");
    if (asli.id) {
      var l = document.querySelector('label[for="' + asli.id + '"]');
      if (l) return l.textContent.trim();
    }
    var lw = asli.closest("label");
    return lw ? lw.textContent.trim() : "";
  }
  // Kelas tata letak (lebar, kolom grid) pindah ke wadah; kelas tampilan isian tetap di pemicu.
  function kelasTataLetak(asli) {
    var kelas = (asli.getAttribute("class") || "").split(/\s+/).filter(function (c) {
      return c && c !== "input" && c !== "input-error" && c !== "block" && !/^file:/.test(c) && !/^text-(sm|xs|base)$/.test(c);
    });
    var adaLebar = kelas.some(function (c) { return /(^|:)w-/.test(c); });
    return (adaLebar ? "" : "w-full ") + kelas.join(" ");
  }

  // ---------- Popover bersama: satu terbuka sekaligus ----------
  var terbuka = null; // { panel, tutup }
  function tutupTerbuka(kembalikanFokus) {
    if (terbuka) { var t = terbuka; terbuka = null; t.tutup(kembalikanFokus); }
  }
  document.addEventListener("mousedown", function (e) {
    if (terbuka && !terbuka.wadah.contains(e.target)) tutupTerbuka(false);
  });
  document.addEventListener("focusin", function (e) {
    if (terbuka && !terbuka.wadah.contains(e.target)) tutupTerbuka(false);
  });
  function bukaPanel(wadah, panel, pemicu, saatTutup) {
    tutupTerbuka(false);
    panel.hidden = false;
    pemicu.setAttribute("aria-expanded", "true");
    // Balik ke atas bila ruang di bawah tidak cukup.
    panel.classList.remove("ke-atas");
    var r = pemicu.getBoundingClientRect();
    var tinggi = panel.offsetHeight;
    if (r.bottom + tinggi + 8 > window.innerHeight && r.top > window.innerHeight - r.bottom) panel.classList.add("ke-atas");
    // Rata kanan bila panel melewati tepi kanan layar.
    panel.classList.remove("rata-kanan");
    if (r.left + panel.offsetWidth > window.innerWidth - 8) panel.classList.add("rata-kanan");
    terbuka = {
      wadah: wadah,
      tutup: function (kembalikanFokus) {
        panel.hidden = true;
        pemicu.setAttribute("aria-expanded", "false");
        if (saatTutup) saatTutup();
        if (kembalikanFokus) pemicu.focus();
      }
    };
  }
  function wadahKontrol(asli, jenis) {
    var w = el("div", "kontrol relative min-w-0 " + kelasTataLetak(asli));
    w.setAttribute("data-kontrol-wadah", jenis);
    asli.parentNode.insertBefore(w, asli);
    w.appendChild(asli);
    asli.classList.add("kontrol-asli");
    asli.tabIndex = -1;
    asli.setAttribute("aria-hidden", "true");
    return w;
  }
  function pasangPemicu(asli, pemicu) {
    pemicu.classList.toggle("input-error", asli.classList.contains("input-error"));
    pemicu.disabled = asli.disabled;
    var nama = namaLabel(asli);
    if (nama) pemicu.setAttribute("aria-label", nama);
    asli.addEventListener("invalid", function () { pemicu.classList.add("input-error"); });
  }

  // ---------- Select ----------
  function pilih(asli) {
    if (asli.dataset.kontrol || asli.multiple) return;
    asli.dataset.kontrol = "pilih";
    var id = "kontrol-" + (++urut);
    var wadah = wadahKontrol(asli, "pilih");
    var pemicu = tombol("input pilih-pemicu");
    pemicu.setAttribute("role", "combobox");
    pemicu.setAttribute("aria-haspopup", "listbox");
    pemicu.setAttribute("aria-expanded", "false");
    pemicu.setAttribute("aria-controls", id);
    var nilai = el("span", "min-w-0 flex-1 truncate");
    pemicu.appendChild(nilai);
    pemicu.appendChild(caret());
    var panel = el("div", "kontrol-panel min-w-full p-1");
    panel.hidden = true;
    var cari = null;
    var daftar = el("div", "pilih-daftar");
    daftar.id = id;
    daftar.setAttribute("role", "listbox");
    daftar.tabIndex = -1;
    panel.appendChild(daftar);
    wadah.appendChild(pemicu);
    wadah.appendChild(panel);
    pasangPemicu(asli, pemicu);

    var opsi = []; // elemen pilihan yang tampil
    var aktif = -1;
    var kotor = true;

    function tulisNilai() {
      var o = asli.options[asli.selectedIndex];
      var kosong = !o || o.value === "";
      nilai.textContent = o ? o.textContent.trim() : "";
      nilai.classList.toggle("text-tertiary", kosong);
      pemicu.title = nilai.textContent;
    }
    function bangun() {
      kotor = false;
      daftar.textContent = "";
      var semua = asli.querySelectorAll("option");
      if (semua.length > 10 && !cari) {
        var baris = el("div", "pilih-cari");
        baris.appendChild(ikon("cari", "size-4 flex-none text-muted-foreground"));
        cari = el("input", "min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-tertiary");
        cari.type = "text";
        cari.placeholder = "Cari…";
        cari.setAttribute("aria-label", "Cari pilihan");
        cari.setAttribute("autocomplete", "off");
        cari.setAttribute("role", "combobox");
        cari.setAttribute("aria-controls", id);
        cari.addEventListener("input", saring);
        baris.appendChild(cari);
        panel.insertBefore(baris, daftar);
      }
      Array.prototype.forEach.call(asli.children, function (anak) {
        if (anak.tagName === "OPTGROUP") {
          var g = el("div", "pilih-grup", anak.label);
          g.setAttribute("role", "presentation");
          daftar.appendChild(g);
          Array.prototype.forEach.call(anak.children, buatOpsi);
        } else if (anak.tagName === "OPTION") {
          buatOpsi(anak);
        }
      });
      var kosong = el("div", "pilih-kosong", "Tidak ada yang cocok.");
      kosong.hidden = true;
      daftar.appendChild(kosong);
    }
    function buatOpsi(o) {
      var d = el("div", "pilih-opsi");
      d.id = id + "-" + o.index;
      d.setAttribute("role", "option");
      d.dataset.indeks = o.index;
      d.setAttribute("aria-selected", o.selected ? "true" : "false");
      if (o.disabled) d.setAttribute("aria-disabled", "true");
      var t = el("span", "min-w-0 flex-1 truncate", o.textContent.trim());
      if (o.value === "") t.classList.add("text-muted-foreground");
      d.title = o.textContent.trim();
      d.appendChild(t);
      d.appendChild(ikon("check", "centang size-4 flex-none"));
      daftar.appendChild(d);
    }
    function terlihat() {
      return Array.prototype.filter.call(daftar.querySelectorAll(".pilih-opsi"), function (d) {
        return !d.hidden && d.getAttribute("aria-disabled") !== "true";
      });
    }
    function saring() {
      var q = cari.value.trim().toLowerCase();
      var ada = 0;
      daftar.querySelectorAll(".pilih-opsi").forEach(function (d) {
        var cocok = !q || d.textContent.toLowerCase().indexOf(q) >= 0;
        d.hidden = !cocok;
        if (cocok) ada++;
      });
      daftar.querySelectorAll(".pilih-grup").forEach(function (g) {
        var n = g.nextElementSibling, tampil = false;
        while (n && n.classList.contains("pilih-opsi")) { if (!n.hidden) tampil = true; n = n.nextElementSibling; }
        g.hidden = !tampil;
      });
      daftar.querySelector(".pilih-kosong").hidden = ada > 0;
      opsi = terlihat();
      tandai(opsi.length ? 0 : -1);
    }
    function tandai(i) {
      opsi.forEach(function (d) { d.removeAttribute("data-aktif"); });
      aktif = i;
      var fokus = cari || daftar;
      if (i < 0 || !opsi[i]) { fokus.removeAttribute("aria-activedescendant"); return; }
      opsi[i].setAttribute("data-aktif", "");
      fokus.setAttribute("aria-activedescendant", opsi[i].id);
      var d = opsi[i], top = d.offsetTop, bawah = top + d.offsetHeight;
      if (top < daftar.scrollTop) daftar.scrollTop = top;
      else if (bawah > daftar.scrollTop + daftar.clientHeight) daftar.scrollTop = bawah - daftar.clientHeight;
    }
    function buka() {
      if (asli.disabled) return;
      if (kotor) bangun();
      daftar.querySelectorAll(".pilih-opsi").forEach(function (d) {
        d.setAttribute("aria-selected", Number(d.dataset.indeks) === asli.selectedIndex ? "true" : "false");
      });
      if (cari) { cari.value = ""; saring(); }
      bukaPanel(wadah, panel, pemicu);
      opsi = terlihat();
      var i = opsi.findIndex(function (d) { return Number(d.dataset.indeks) === asli.selectedIndex; });
      tandai(i >= 0 ? i : 0);
      (cari || daftar).focus({ preventScroll: true });
    }
    function ambil(d) {
      if (!d || d.getAttribute("aria-disabled") === "true") return;
      var berubah = asli.selectedIndex !== Number(d.dataset.indeks);
      asli.selectedIndex = Number(d.dataset.indeks);
      tulisNilai();
      tutupTerbuka(true);
      if (berubah) picu(asli);
    }

    pemicu.addEventListener("click", function () {
      if (terbuka && terbuka.wadah === wadah) tutupTerbuka(true); else buka();
    });
    pemicu.addEventListener("keydown", function (e) {
      if (["ArrowDown", "ArrowUp", "Enter", " "].indexOf(e.key) >= 0) { e.preventDefault(); buka(); }
    });
    daftar.addEventListener("mousemove", function (e) {
      var d = e.target.closest(".pilih-opsi");
      if (d) { var i = opsi.indexOf(d); if (i >= 0 && i !== aktif) tandai(i); }
    });
    daftar.addEventListener("click", function (e) { ambil(e.target.closest(".pilih-opsi")); });
    var ketik = "", ketikJeda = 0;
    panel.addEventListener("keydown", function (e) {
      if (e.key === "ArrowDown") { e.preventDefault(); tandai(Math.min(aktif + 1, opsi.length - 1)); }
      else if (e.key === "ArrowUp") { e.preventDefault(); tandai(Math.max(aktif - 1, 0)); }
      else if (e.key === "Home" && !cari) { e.preventDefault(); tandai(0); }
      else if (e.key === "End" && !cari) { e.preventDefault(); tandai(opsi.length - 1); }
      else if (e.key === "Enter") { e.preventDefault(); ambil(opsi[aktif]); }
      else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); tutupTerbuka(true); }
      else if (e.key === "Tab") { tutupTerbuka(false); }
      else if (!cari && e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
        // Lompat ke pilihan berawalan huruf yang diketik.
        window.clearTimeout(ketikJeda);
        ketik += e.key.toLowerCase();
        ketikJeda = window.setTimeout(function () { ketik = ""; }, 600);
        var i = opsi.findIndex(function (d) { return d.textContent.trim().toLowerCase().indexOf(ketik) === 0; });
        if (i >= 0) tandai(i);
      }
    });

    // Pilihan bisa diganti HTMX (mis. daftar ruangan per unit) atau status nonaktif berubah.
    new MutationObserver(function () {
      kotor = true;
      tulisNilai();
      pemicu.disabled = asli.disabled;
      pemicu.classList.toggle("input-error", asli.classList.contains("input-error"));
    }).observe(asli, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ["disabled"] });
    asli.addEventListener("change", tulisNilai);
    tulisNilai();
  }

  // ---------- Tanggal, bulan, tanggal & jam ----------
  function dua(n) { return (n < 10 ? "0" : "") + n; }
  function iso(y, m, d) { return y + "-" + dua(m + 1) + "-" + dua(d); }
  function hariIni() { var d = new Date(); return iso(d.getFullYear(), d.getMonth(), d.getDate()); }
  function urai(v) { // "YYYY-MM-DD" -> {y, m, d}
    var m = /^(\d{4})-(\d{2})(?:-(\d{2}))?/.exec(v || "");
    return m ? { y: +m[1], m: +m[2] - 1, d: m[3] ? +m[3] : 1 } : null;
  }

  function tanggal(asli) {
    if (asli.dataset.kontrol) return;
    var jenis = asli.type; // date | month | datetime-local
    asli.dataset.kontrol = jenis;
    var id = "kontrol-" + (++urut);
    var wadah = wadahKontrol(asli, "tanggal");
    var pemicu = tombol("input pilih-pemicu");
    pemicu.setAttribute("aria-haspopup", "dialog");
    pemicu.setAttribute("aria-expanded", "false");
    pemicu.setAttribute("aria-controls", id);
    pemicu.appendChild(ikon("kalender", "size-4 flex-none text-muted-foreground"));
    var nilai = el("span", "min-w-0 flex-1 truncate");
    pemicu.appendChild(nilai);
    var panel = el("div", "kontrol-panel kalender p-3");
    panel.id = id;
    panel.setAttribute("role", "dialog");
    panel.hidden = true;
    wadah.appendChild(pemicu);
    wadah.appendChild(panel);
    pasangPemicu(asli, pemicu);
    if (pemicu.getAttribute("aria-label")) panel.setAttribute("aria-label", pemicu.getAttribute("aria-label"));

    var lihat = { y: 0, m: 0 }; // bulan/tahun yang sedang ditampilkan
    var fokus = "";              // tanggal yang difokus papan ketik (ISO)
    var jamInput = null;

    function bagianTanggal() { return (asli.value || "").slice(0, jenis === "month" ? 7 : 10); }
    function bagianJam() { var m = /T(\d{2}):(\d{2})/.exec(asli.value || ""); return m ? m[1] + "." + m[2] : ""; }
    function batas(v) {
      var n = jenis === "month" ? 7 : 10;
      var min = (asli.min || "").slice(0, n), max = (asli.max || "").slice(0, n);
      return (min && v < min) || (max && v > max);
    }
    function tulisNilai() {
      var t = urai(asli.value), teks;
      if (!t) {
        teks = jenis === "month" ? "Pilih bulan" : jenis === "date" ? "Pilih tanggal" : "Pilih tanggal dan jam";
      } else if (jenis === "month") {
        teks = BULAN[t.m] + " " + t.y;
      } else {
        teks = t.d + " " + BLN[t.m] + " " + t.y;
        if (jenis === "datetime-local" && bagianJam()) teks += ", " + bagianJam();
      }
      nilai.textContent = teks;
      nilai.classList.toggle("text-tertiary", !t);
    }
    function setel(v) {
      if (asli.value === v) { tulisNilai(); return; }
      asli.value = v;
      tulisNilai();
      picu(asli);
    }
    function setelTanggal(tgl) {
      if (jenis === "datetime-local") {
        var jam = /T(\d{2}:\d{2})/.exec(asli.value || "");
        var j = jam ? jam[1] : (function () { var d = new Date(); return dua(d.getHours()) + ":" + dua(d.getMinutes()); })();
        setel(tgl + "T" + j);
        if (jamInput) jamInput.value = j.replace(":", ".");
      } else {
        setel(tgl);
      }
    }

    function kepala(judul, mundur, maju, labelMundur, labelMaju) {
      var h = el("div", "mb-2 flex items-center justify-between gap-2");
      var a = tombol("btn btn-ghost btn-icon size-7"); a.setAttribute("aria-label", labelMundur); a.appendChild(ikon("kiri"));
      var b = tombol("btn btn-ghost btn-icon size-7"); b.setAttribute("aria-label", labelMaju); b.appendChild(ikon("kanan"));
      var j = el("div", "text-sm font-bold", judul);
      j.setAttribute("aria-live", "polite");
      a.addEventListener("click", mundur);
      b.addEventListener("click", maju);
      h.appendChild(a); h.appendChild(j); h.appendChild(b);
      return h;
    }
    function kaki() {
      var f = el("div", "mt-3 flex items-center justify-between gap-2 border-t pt-3");
      var kiri = el("div", "flex gap-1");
      var sekarang = tombol("btn btn-ghost btn-sm", jenis === "month" ? "Bulan ini" : "Hari ini");
      sekarang.addEventListener("click", function () {
        var t = hariIni();
        if (jenis === "month") { setel(t.slice(0, 7)); tutupTerbuka(true); }
        else { setelTanggal(t); if (jenis === "date") tutupTerbuka(true); else gambar(); }
      });
      kiri.appendChild(sekarang);
      if (!asli.required) {
        var hapus = tombol("btn btn-ghost btn-sm", "Kosongkan");
        hapus.addEventListener("click", function () { setel(""); tutupTerbuka(true); });
        kiri.appendChild(hapus);
      }
      f.appendChild(kiri);
      if (jenis === "datetime-local") {
        var selesai = tombol("btn btn-primary btn-sm", "Selesai");
        selesai.addEventListener("click", function () { simpanJam(); tutupTerbuka(true); });
        f.appendChild(selesai);
      }
      return f;
    }
    function simpanJam() {
      if (!jamInput) return;
      var m = /^(\d{1,2})[.:](\d{2})$/.exec(jamInput.value.trim());
      if (!m || +m[1] > 23 || +m[2] > 59) { jamInput.setAttribute("aria-invalid", "true"); return; }
      jamInput.removeAttribute("aria-invalid");
      var j = dua(+m[1]) + ":" + m[2];
      jamInput.value = dua(+m[1]) + "." + m[2];
      var tgl = bagianTanggal() || hariIni();
      setel(tgl + "T" + j);
    }

    function gambar() {
      panel.textContent = "";
      if (jenis === "month") return gambarBulan();
      var y = lihat.y, m = lihat.m;
      panel.appendChild(kepala(BULAN[m] + " " + y,
        function () { geser(-1); }, function () { geser(1); }, "Bulan sebelumnya", "Bulan berikutnya"));
      var grid = el("div", "grid grid-cols-7 gap-0.5");
      grid.setAttribute("role", "grid");
      HARI.forEach(function (h) { var c = el("div", "flex h-7 items-center justify-center text-xs text-muted-foreground", h); c.setAttribute("role", "columnheader"); grid.appendChild(c); });
      var awal = (new Date(y, m, 1).getDay() + 6) % 7;
      var pilihan = bagianTanggal(), kini = hariIni();
      for (var i = 0; i < 42; i++) {
        var d = new Date(y, m, 1 - awal + i);
        var v = iso(d.getFullYear(), d.getMonth(), d.getDate());
        var b = tombol("kalender-hari", String(d.getDate()));
        b.dataset.tgl = v;
        b.setAttribute("role", "gridcell");
        b.setAttribute("aria-label", d.getDate() + " " + BULAN[d.getMonth()] + " " + d.getFullYear());
        if (d.getMonth() !== m) b.classList.add("luar");
        if (v === kini) b.classList.add("kini");
        if (v === pilihan) b.setAttribute("aria-selected", "true");
        if (batas(v)) b.disabled = true;
        b.tabIndex = v === fokus ? 0 : -1;
        grid.appendChild(b);
      }
      panel.appendChild(grid);
      if (jenis === "datetime-local") {
        var baris = el("label", "mt-3 flex items-center justify-between gap-2 text-sm");
        baris.appendChild(el("span", "text-muted-foreground", "Jam"));
        jamInput = el("input", "input h-8 w-24 text-right");
        jamInput.type = "text";
        jamInput.inputMode = "numeric";
        jamInput.placeholder = "08.00";
        jamInput.value = bagianJam();
        jamInput.addEventListener("change", simpanJam);
        jamInput.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); simpanJam(); tutupTerbuka(true); } });
        baris.appendChild(jamInput);
        panel.appendChild(baris);
      }
      panel.appendChild(kaki());
    }
    function gambarBulan() {
      var y = lihat.y;
      panel.appendChild(kepala(String(y),
        function () { lihat.y--; gambar(); }, function () { lihat.y++; gambar(); }, "Tahun sebelumnya", "Tahun berikutnya"));
      var grid = el("div", "grid grid-cols-3 gap-1");
      var pilihan = bagianTanggal(), kini = hariIni().slice(0, 7);
      BLN.forEach(function (n, i) {
        var v = y + "-" + dua(i + 1);
        var b = tombol("kalender-hari kalender-bulan", n);
        b.dataset.tgl = v;
        b.setAttribute("aria-label", BULAN[i] + " " + y);
        if (v === kini) b.classList.add("kini");
        if (v === pilihan) b.setAttribute("aria-selected", "true");
        if (batas(v)) b.disabled = true;
        b.tabIndex = v === fokus ? 0 : -1;
        grid.appendChild(b);
      });
      panel.appendChild(grid);
      panel.appendChild(kaki());
    }
    function geser(n) {
      var d = new Date(lihat.y, lihat.m + n, 1);
      lihat = { y: d.getFullYear(), m: d.getMonth() };
      gambar();
    }
    function fokuskan(v) {
      fokus = v;
      var t = urai(v);
      if (jenis === "month") { if (t.y !== lihat.y) { lihat.y = t.y; } }
      else if (t.y !== lihat.y || t.m !== lihat.m) { lihat = { y: t.y, m: t.m }; }
      gambar();
      var b = panel.querySelector('[data-tgl="' + v + '"]');
      if (b) b.focus({ preventScroll: true });
    }
    function buka() {
      if (asli.disabled) return;
      var t = urai(asli.value) || urai(hariIni());
      lihat = { y: t.y, m: t.m };
      fokus = jenis === "month" ? t.y + "-" + dua(t.m + 1) : iso(t.y, t.m, t.d);
      gambar();
      bukaPanel(wadah, panel, pemicu, function () { if (jenis === "datetime-local") simpanJam(); });
      var b = panel.querySelector('[data-tgl="' + fokus + '"]') || panel.querySelector(".kalender-hari:not([disabled])");
      if (b) b.focus({ preventScroll: true });
    }

    pemicu.addEventListener("click", function () {
      if (terbuka && terbuka.wadah === wadah) tutupTerbuka(true); else buka();
    });
    pemicu.addEventListener("keydown", function (e) {
      if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") { e.preventDefault(); buka(); }
    });
    panel.addEventListener("click", function (e) {
      var b = e.target.closest(".kalender-hari");
      if (!b || b.disabled) return;
      if (jenis === "month") { setel(b.dataset.tgl); tutupTerbuka(true); return; }
      setelTanggal(b.dataset.tgl);
      if (jenis === "date") { tutupTerbuka(true); return; }
      fokuskan(b.dataset.tgl);
    });
    panel.addEventListener("keydown", function (e) {
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); tutupTerbuka(true); return; }
      var b = e.target.closest(".kalender-hari");
      if (!b) return;
      var langkah = jenis === "month"
        ? { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -3, ArrowDown: 3 }
        : { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
      var t = urai(b.dataset.tgl), v = null;
      if (langkah[e.key] != null) {
        if (jenis === "month") { var dm = new Date(t.y, t.m + langkah[e.key], 1); v = dm.getFullYear() + "-" + dua(dm.getMonth() + 1); }
        else { var dd = new Date(t.y, t.m, t.d + langkah[e.key]); v = iso(dd.getFullYear(), dd.getMonth(), dd.getDate()); }
      } else if ((e.key === "PageUp" || e.key === "PageDown") && jenis !== "month") {
        var dp = new Date(t.y, t.m + (e.key === "PageUp" ? -1 : 1), t.d); v = iso(dp.getFullYear(), dp.getMonth(), dp.getDate());
      }
      if (v) { e.preventDefault(); fokuskan(v); }
    });
    new MutationObserver(function () {
      pemicu.disabled = asli.disabled;
      pemicu.classList.toggle("input-error", asli.classList.contains("input-error"));
    }).observe(asli, { attributes: true, attributeFilter: ["disabled", "class"] });
    asli.addEventListener("change", tulisNilai);
    tulisNilai();
  }

  // ---------- Pilih file ----------
  function berkas(asli) {
    if (asli.dataset.kontrol) return;
    asli.dataset.kontrol = "berkas";
    var wadah = wadahKontrol(asli, "berkas");
    wadah.classList.add("flex", "items-center", "gap-2");
    var foto = /image/.test(asli.accept || "");
    var b = tombol("btn btn-secondary flex-none");
    b.appendChild(ikon("klip"));
    b.appendChild(document.createTextNode(foto ? (asli.multiple ? "Pilih foto" : "Pilih foto") : "Pilih file"));
    var nama = namaLabel(asli);
    if (nama) b.setAttribute("aria-label", nama);
    var ket = el("span", "min-w-0 truncate text-sm text-muted-foreground");
    ket.setAttribute("aria-live", "polite");
    wadah.appendChild(b);
    wadah.appendChild(ket);
    function tulis() {
      var f = Array.prototype.map.call(asli.files || [], function (x) { return x.name; });
      ket.textContent = f.length === 0 ? (foto ? "Belum ada foto dipilih" : "Belum ada file dipilih")
        : f.length === 1 ? f[0] : f.length + " file: " + f.join(", ");
      ket.title = f.join(", ");
      ket.classList.toggle("text-tertiary", f.length === 0);
      b.classList.toggle("input-error", asli.classList.contains("input-error"));
    }
    b.addEventListener("click", function () { asli.click(); });
    asli.addEventListener("change", tulis);
    new MutationObserver(function () { b.disabled = asli.disabled; }).observe(asli, { attributes: true, attributeFilter: ["disabled"] });
    b.disabled = asli.disabled;
    tulis();
  }

  // Klik label milik kontrol asli memfokuskan kontrol buatan.
  document.addEventListener("click", function (e) {
    var l = e.target.closest("label[for]");
    if (!l) return;
    var asli = document.getElementById(l.htmlFor);
    if (!asli || !asli.dataset.kontrol) return;
    var w = asli.closest("[data-kontrol-wadah]");
    var p = w && w.querySelector(".pilih-pemicu, .btn");
    if (p) { e.preventDefault(); p.focus(); }
  });

  function rangkai(akar) {
    (akar || document).querySelectorAll("select").forEach(pilih);
    (akar || document).querySelectorAll('input[type="date"], input[type="month"], input[type="datetime-local"]').forEach(tanggal);
    (akar || document).querySelectorAll('input[type="file"]').forEach(berkas);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", function () { rangkai(); });
  else rangkai();
  // Isi baru dari HTMX.
  document.addEventListener("htmx:load", function (e) { rangkai(e.target); });
})();
