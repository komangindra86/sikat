// SIKAT: JavaScript pendukung (tanpa inline script karena CSP).
(function () {
  "use strict";

  function storageGet(k) { try { return window.localStorage.getItem(k); } catch (_) { return null; } }
  function storageSet(k, v) { try { window.localStorage.setItem(k, v); } catch (_) { /* abaikan */ } }

  // Token CSRF untuk setiap request HTMX.
  document.addEventListener("htmx:configRequest", function (e) {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (meta) e.detail.headers["X-CSRF-Token"] = meta.content;
  });

  // Dialog konfirmasi (pengganti window.confirm): tanya(pesan) -> Promise<boolean>.
  function tanya(pesan, labelYa) {
    return new Promise(function (selesai) {
      var dlg = document.createElement("dialog");
      dlg.className = "dialog";
      dlg.setAttribute("aria-label", "Konfirmasi");
      var p = document.createElement("p");
      p.className = "m-0 text-base font-bold";
      p.textContent = pesan;
      var aksi = document.createElement("div");
      aksi.className = "mt-5 flex justify-end gap-2";
      var batal = document.createElement("button");
      batal.type = "button"; batal.className = "btn btn-secondary"; batal.textContent = "Batal";
      var ya = document.createElement("button");
      ya.type = "button"; ya.className = "btn btn-primary"; ya.textContent = labelYa || "Lanjutkan";
      aksi.appendChild(batal); aksi.appendChild(ya);
      dlg.appendChild(p); dlg.appendChild(aksi);
      document.body.appendChild(dlg);
      var hasil = false;
      batal.addEventListener("click", function () { dlg.close(); });
      ya.addEventListener("click", function () { hasil = true; dlg.close(); });
      dlg.addEventListener("close", function () { dlg.remove(); selesai(hasil); });
      dlg.showModal();
      ya.focus();
    });
  }

  // Notifikasi singkat di kanan bawah (pengganti window.alert).
  function kabar(pesan, jenis) {
    var box = document.getElementById("toasts");
    if (!box) return;
    var div = document.createElement("div");
    div.className = "flash flash-" + (jenis || "info");
    div.setAttribute("data-flash", jenis || "info");
    div.setAttribute("role", jenis === "error" ? "alert" : "status");
    div.textContent = pesan;
    box.appendChild(div);
    window.setTimeout(function () { div.remove(); }, 6000);
  }

  // Konfirmasi sebelum aksi penting: <form data-confirm="Yakin?">
  document.addEventListener("submit", function (e) {
    var form = e.target;
    var msg = form.getAttribute("data-confirm");
    if (msg && !form.dataset.terkonfirmasi) {
      e.preventDefault();
      var pengirim = e.submitter;
      tanya(msg).then(function (ok) {
        if (!ok) return;
        form.dataset.terkonfirmasi = "1";
        if (form.requestSubmit) form.requestSubmit(pengirim || undefined); else form.submit();
        delete form.dataset.terkonfirmasi;
      });
      return;
    }
    // Cegah kirim ganda & beri umpan balik.
    var btn = form.querySelector('button[type="submit"]:not([data-no-busy])');
    if (btn && !e.defaultPrevented) {
      window.setTimeout(function () {
        btn.disabled = true;
        if (btn.dataset.busyText) btn.textContent = btn.dataset.busyText;
      }, 0);
    }
  });

  // Sidebar: terbuka bawaan di layar kerja; tombol menu menutup/membukanya (diingat per browser).
  // Di HP (< 768 px) sidebar menjadi laci yang dibuka tombol Menu dan ditutup klik di luar atau Escape.
  var layarHP = window.matchMedia ? window.matchMedia("(max-width: 767.98px)") : { matches: false };
  function sidebarTerbuka() {
    var akar = document.documentElement;
    return layarHP.matches ? akar.dataset.laci === "buka" : akar.dataset.sidebar !== "tutup";
  }
  function segarkanSidebar() {
    var sidebar = document.getElementById("sidebar");
    if (!sidebar) return;
    sidebar.inert = !sidebarTerbuka();
    document.querySelectorAll("[data-sidebar-toggle]").forEach(function (b) {
      b.setAttribute("aria-expanded", sidebarTerbuka() ? "true" : "false");
    });
  }
  function aturLaci(buka) {
    var akar = document.documentElement;
    if (buka) akar.dataset.laci = "buka"; else delete akar.dataset.laci;
    segarkanSidebar();
    var sidebar = document.getElementById("sidebar");
    if (buka && sidebar) {
      var pertama = sidebar.querySelector("a[aria-current='page']") || sidebar.querySelector("a");
      if (pertama) pertama.focus({ preventScroll: true });
    }
  }
  document.addEventListener("click", function (e) {
    if (e.target.closest("[data-laci-tutup]")) { aturLaci(false); return; }
    var t = e.target.closest("[data-sidebar-toggle]");
    if (!t) return;
    if (layarHP.matches) { aturLaci(document.documentElement.dataset.laci !== "buka"); return; }
    var akar = document.documentElement;
    var tutup = akar.dataset.sidebar !== "tutup";
    if (tutup) akar.dataset.sidebar = "tutup"; else delete akar.dataset.sidebar;
    storageSet("sikat.sidebar", tutup ? "tutup" : "buka");
    segarkanSidebar();
  });
  if (layarHP.addEventListener) layarHP.addEventListener("change", function () { aturLaci(false); });
  document.addEventListener("DOMContentLoaded", segarkanSidebar);

  // Popover (pemilih tema, menu akun): satu terbuka sekaligus, tutup dengan klik di luar atau Escape.
  function tutupPopover(kecuali) {
    document.querySelectorAll("[data-popover]").forEach(function (w) {
      if (w === kecuali) return;
      var panel = w.querySelector(".popover");
      var tombol = w.querySelector("[data-popover-toggle]");
      if (panel && !panel.hidden) {
        panel.hidden = true;
        if (tombol) tombol.setAttribute("aria-expanded", "false");
      }
    });
  }
  document.addEventListener("click", function (e) {
    var tombol = e.target.closest("[data-popover-toggle]");
    var wadah = e.target.closest("[data-popover]");
    if (tombol) {
      var panel = wadah.querySelector(".popover");
      var buka = panel.hidden;
      tutupPopover(wadah);
      panel.hidden = !buka;
      tombol.setAttribute("aria-expanded", buka ? "true" : "false");
      if (buka) {
        var fokus = panel.querySelector("[aria-checked='true'], a, button");
        if (fokus) fokus.focus({ preventScroll: true });
      }
      return;
    }
    if (!wadah) tutupPopover(null);
  });
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape") return;
    var terbuka = document.querySelector("[data-popover] .popover:not([hidden])");
    if (terbuka) {
      var w = terbuka.closest("[data-popover]");
      tutupPopover(null);
      var b = w.querySelector("[data-popover-toggle]");
      if (b) b.focus();
      return;
    }
    if (document.documentElement.dataset.laci === "buka") {
      aturLaci(false);
      var tombol = document.querySelector("header [data-sidebar-toggle]");
      if (tombol) tombol.focus({ preventScroll: true });
    }
  });

  // Pilihan tampilan: mode, tema, kepadatan (disimpan per browser, lihat tema.js).
  var akar = document.documentElement;
  var gelapSistem = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
  function terapkanMode(mode) {
    var gelap = mode === "gelap" || (mode === "sistem" && gelapSistem && gelapSistem.matches);
    if (gelap) akar.dataset.semantik = "Gelap"; else delete akar.dataset.semantik;
    akar.style.colorScheme = gelap ? "dark" : "light";
  }
  function tandai() {
    var mode = storageGet("sikat.mode") || "terang";
    var tema = akar.dataset.tema || "Daun";
    var padat = akar.dataset.kepadatan === "Padat" ? "padat" : "biasa";
    document.querySelectorAll("[data-pilih-mode]").forEach(function (b) {
      b.setAttribute("aria-checked", b.dataset.pilihMode === mode ? "true" : "false");
    });
    document.querySelectorAll("[data-pilih-tema]").forEach(function (b) {
      var on = b.dataset.pilihTema === tema;
      b.setAttribute("aria-checked", on ? "true" : "false");
      var c = b.querySelector("[data-centang]");
      if (c) c.classList.toggle("invisible", !on);
    });
    document.querySelectorAll("[data-pilih-kepadatan]").forEach(function (b) {
      b.setAttribute("aria-checked", b.dataset.pilihKepadatan === padat ? "true" : "false");
    });
  }
  document.addEventListener("click", function (e) {
    var m = e.target.closest("[data-pilih-mode]");
    var t = e.target.closest("[data-pilih-tema]");
    var k = e.target.closest("[data-pilih-kepadatan]");
    if (m) { storageSet("sikat.mode", m.dataset.pilihMode); terapkanMode(m.dataset.pilihMode); }
    if (t) {
      var nama = t.dataset.pilihTema;
      if (nama === "Daun") delete akar.dataset.tema; else akar.dataset.tema = nama;
      storageSet("sikat.tema", nama === "Daun" ? "" : nama);
    }
    if (k) {
      var padat = k.dataset.pilihKepadatan === "padat";
      if (padat) akar.dataset.kepadatan = "Padat"; else delete akar.dataset.kepadatan;
      storageSet("sikat.kepadatan", padat ? "padat" : "biasa");
    }
    if (m || t || k) tandai();
  });
  if (gelapSistem && gelapSistem.addEventListener) {
    gelapSistem.addEventListener("change", function () {
      if ((storageGet("sikat.mode") || "terang") === "sistem") terapkanMode("sistem");
    });
  }
  document.addEventListener("DOMContentLoaded", tandai);

  // Tutup pesan flash.
  document.addEventListener("click", function (e) {
    var t = e.target.closest("[data-dismiss]");
    if (t) {
      var el = t.closest("[data-flash]");
      if (el) el.remove();
    }
  });

  // Pesan sukses hilang otomatis setelah 6 detik.
  function autoDismiss(root) {
    (root || document).querySelectorAll('[data-flash="success"]').forEach(function (el) {
      window.setTimeout(function () { el.remove(); }, 6000);
    });
  }
  document.addEventListener("DOMContentLoaded", function () { autoDismiss(); });

  // Error dari HTMX (header HX-Trigger "sikat:error") ditampilkan sebagai toast.
  document.addEventListener("sikat:error", function (e) {
    kabar((e.detail && e.detail.value) || "Terjadi kesalahan.", "error");
  });

  // Salin teks: <button data-copy="#id">
  document.addEventListener("click", function (e) {
    var t = e.target.closest("[data-copy]");
    if (!t) return;
    var src = document.querySelector(t.getAttribute("data-copy"));
    if (!src || !navigator.clipboard) return;
    navigator.clipboard.writeText(src.textContent.trim()).then(function () {
      var old = t.textContent;
      t.textContent = "Tersalin";
      window.setTimeout(function () { t.textContent = old; }, 1500);
    });
  });

  // Tampilkan/sembunyikan bagian kompetensi sesuai role yang dipilih.
  function syncRoleSections() {
    var sel = document.querySelector("[data-role-select]");
    if (!sel) return;
    var opt = sel.options[sel.selectedIndex];
    var code = opt ? opt.getAttribute("data-code") : "";
    document.querySelectorAll("[data-show-for-roles]").forEach(function (el) {
      var roles = el.getAttribute("data-show-for-roles").split(",");
      if (roles.indexOf(code) >= 0) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
    });
  }
  document.addEventListener("change", function (e) {
    if (e.target.matches("[data-role-select]")) syncRoleSections();
  });
  document.addEventListener("DOMContentLoaded", syncRoleSections);

  // Filter laporan: tampilkan isian sesuai jenis periode.
  function syncPeriod() {
    var sel = document.querySelector("[data-period-select]");
    if (!sel) return;
    document.querySelectorAll("[data-period]").forEach(function (el) {
      var on = el.getAttribute("data-period") === sel.value;
      if (on) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
      el.querySelectorAll("input,select").forEach(function (i) { i.disabled = !on; });
    });
  }
  document.addEventListener("change", function (e) {
    if (e.target.matches("[data-period-select]")) syncPeriod();
  });
  document.addEventListener("DOMContentLoaded", syncPeriod);

  // Form pelapor: pilih bidang dulu, lalu jenis gangguan di bidang itu.
  // Tanpa JS semua kelompok tampil berurutan dengan judul bidangnya.
  function syncBidang(gulir) {
    document.querySelectorAll("[data-pilih-bidang]").forEach(function (fs) {
      var daftar = fs.querySelector("[data-bidang-daftar]");
      if (!daftar) return;
      daftar.removeAttribute("hidden");
      fs.querySelectorAll("[data-ket-bidang]").forEach(function (el) { el.setAttribute("hidden", ""); });
      fs.querySelectorAll("[data-awalan-bidang]").forEach(function (el) { el.removeAttribute("hidden"); });
      var pilih = daftar.querySelector('input[name="bidang"]:checked');
      fs.querySelectorAll("[data-jenis-bidang]").forEach(function (g) {
        var on = !!pilih && g.getAttribute("data-jenis-bidang") === pilih.value;
        if (on) g.removeAttribute("hidden"); else g.setAttribute("hidden", "");
        var jenis = g.querySelectorAll('input[name="ticket_category_id"]');
        if (!on) jenis.forEach(function (i) { i.checked = false; });
        else if (jenis.length === 1) jenis[0].checked = true;
        if (on && gulir && g.scrollIntoView) g.scrollIntoView({ behavior: "smooth", block: "nearest" });
      });
    });
  }
  document.addEventListener("change", function (e) {
    if (e.target.matches('[data-bidang-daftar] input[name="bidang"]')) syncBidang(true);
  });
  document.addEventListener("DOMContentLoaded", function () { syncBidang(false); });

  // Pilihan yang mengisi select lain: <select data-sets-target="#id"><option data-set="5">
  document.addEventListener("change", function (e) {
    var sel = e.target;
    if (!sel.matches("[data-sets-target]")) return;
    var opt = sel.options[sel.selectedIndex];
    var target = document.querySelector(sel.getAttribute("data-sets-target"));
    var v = opt && opt.getAttribute("data-set");
    if (target && v) {
      target.value = v;
      target.dispatchEvent(new Event("change", { bubbles: true }));
    }
  });

  // Isi textarea dari template: <select data-fill-target="#id"><option data-body="...">
  document.addEventListener("change", function (e) {
    var sel = e.target;
    if (!sel.matches("[data-fill-target]")) return;
    var opt = sel.options[sel.selectedIndex];
    var target = document.querySelector(sel.getAttribute("data-fill-target"));
    if (!opt || !target || !opt.getAttribute("data-body")) return;
    var isi = opt.getAttribute("data-body");
    function pakai() { target.value = isi; target.focus(); }
    if (!target.value.trim()) { pakai(); return; }
    tanya("Ganti isi tindakan dengan template?", "Ganti isi").then(function (ok) { if (ok) pakai(); });
  });

  // Ingat nama/kontak pelapor di perangkat ini (hanya di browser pelapor).
  document.addEventListener("DOMContentLoaded", function () {
    document.querySelectorAll("[data-remember]").forEach(function (el) {
      var key = el.getAttribute("data-remember");
      if (!el.value) { var v = storageGet(key); if (v) el.value = v; }
      el.form && el.form.addEventListener("submit", function () { storageSet(key, el.value.trim()); });
    });
    // Tombol bagikan hanya bila browser mendukung Web Share.
    document.querySelectorAll("[data-share]").forEach(function (b) { if (navigator.share) b.removeAttribute("hidden"); });
  });
  document.addEventListener("click", function (e) {
    var t = e.target.closest("[data-share]");
    if (!t || !navigator.share) return;
    var src = document.querySelector(t.getAttribute("data-share"));
    navigator.share({ title: t.getAttribute("data-share-title") || document.title, url: src.textContent.trim() }).catch(function () {});
  });

  // Kompres foto di HP sebelum dikirim (maks 1600 px, JPEG) dan tampilkan pratinjau.
  var MAX_EDGE = 1600;
  function compress(file) {
    return new Promise(function (resolve) {
      if (!/^image\/(jpeg|png|webp)$/.test(file.type) || !window.createImageBitmap || !window.DataTransfer) { resolve(file); return; }
      createImageBitmap(file).then(function (bmp) {
        var scale = Math.min(1, MAX_EDGE / Math.max(bmp.width, bmp.height));
        var c = document.createElement("canvas");
        c.width = Math.round(bmp.width * scale); c.height = Math.round(bmp.height * scale);
        var ctx = c.getContext("2d");
        ctx.fillStyle = "#fff"; ctx.fillRect(0, 0, c.width, c.height);
        ctx.drawImage(bmp, 0, 0, c.width, c.height);
        c.toBlob(function (blob) {
          if (!blob || blob.size >= file.size) { resolve(file); return; }
          resolve(new File([blob], file.name.replace(/\.\w+$/, "") + ".jpg", { type: "image/jpeg" }));
        }, "image/jpeg", 0.8);
      }).catch(function () { resolve(file); });
    });
  }
  document.addEventListener("change", function (e) {
    var input = e.target;
    if (!input.matches("[data-photo-input]")) return;
    var max = parseInt(input.getAttribute("data-max-files") || "3", 10);
    var preview = (input.closest("[data-kontrol-wadah]") || input).parentElement.querySelector("[data-photo-preview]");
    var files = Array.prototype.slice.call(input.files || []);
    if (files.length > max) {
      kabar("Maksimal " + max + " foto. Hanya " + max + " foto pertama yang dipakai.", "info");
      files = files.slice(0, max);
    }
    Promise.all(files.map(compress)).then(function (out) {
      if (window.DataTransfer) {
        var dt = new DataTransfer();
        out.forEach(function (f) { dt.items.add(f); });
        input.files = dt.files;
      }
      if (!preview) return;
      preview.innerHTML = "";
      out.forEach(function (f) {
        var img = document.createElement("img");
        img.className = "aspect-square w-full rounded-lg object-cover";
        img.alt = "Pratinjau foto";
        img.src = URL.createObjectURL(f);
        preview.appendChild(img);
      });
    });
  });
})();
