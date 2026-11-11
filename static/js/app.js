// SIKAT — JavaScript pendukung (tanpa inline script karena CSP).
(function () {
  "use strict";

  // Token CSRF untuk setiap request HTMX.
  document.addEventListener("htmx:configRequest", function (e) {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (meta) e.detail.headers["X-CSRF-Token"] = meta.content;
  });

  // Konfirmasi sebelum aksi penting: <form data-confirm="Yakin?">
  document.addEventListener("submit", function (e) {
    var form = e.target;
    var msg = form.getAttribute("data-confirm");
    if (msg && !window.confirm(msg)) {
      e.preventDefault();
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

  // Menu samping di HP.
  document.addEventListener("click", function (e) {
    var t = e.target.closest("[data-menu-toggle]");
    if (!t) return;
    var menu = document.getElementById("mobile-menu");
    if (!menu) return;
    var open = menu.hasAttribute("hidden");
    if (open) menu.removeAttribute("hidden"); else menu.setAttribute("hidden", "");
    document.querySelectorAll("[data-menu-toggle]").forEach(function (b) {
      b.setAttribute("aria-expanded", open ? "true" : "false");
    });
  });

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
    var box = document.getElementById("toasts");
    if (!box) return;
    var div = document.createElement("div");
    div.className = "flash flash-error";
    div.setAttribute("data-flash", "error");
    div.setAttribute("role", "alert");
    div.textContent = (e.detail && e.detail.value) || "Terjadi kesalahan.";
    box.appendChild(div);
    window.setTimeout(function () { div.remove(); }, 8000);
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

  // Ingat nama/kontak pelapor di perangkat ini (hanya di browser pelapor).
  function storageGet(k) { try { return window.localStorage.getItem(k); } catch (_) { return null; } }
  function storageSet(k, v) { try { window.localStorage.setItem(k, v); } catch (_) { /* abaikan */ } }
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
    var preview = input.parentElement.querySelector("[data-photo-preview]");
    var files = Array.prototype.slice.call(input.files || []);
    if (files.length > max) {
      window.alert("Maksimal " + max + " foto. Hanya " + max + " foto pertama yang dipakai.");
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
