// SIKAT — PWA: service worker, badge notifikasi, dan langganan Web Push.
// Hanya dimuat di halaman setelah login (lihat layouts/base.html).
(function () {
  "use strict";

  var userMeta = document.querySelector('meta[name="sikat-user"]');
  if (!userMeta) return;
  var userId = userMeta.content;
  // Perangkat hanya disinkronkan otomatis untuk pengguna yang mengaktifkannya
  // sendiri (perangkat bersama: pengguna lain tidak otomatis berlangganan).
  var OWNER_KEY = "sikat.push.user";
  var SYNC_KEY = "sikat.push.synced";

  function csrf() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? m.content : "";
  }
  function load(store, k) { try { return window[store].getItem(k); } catch (_) { return null; } }
  function save(store, k, v) {
    try { if (v === null) window[store].removeItem(k); else window[store].setItem(k, v); } catch (_) { /* abaikan */ }
  }
  function toggle(el, on) { if (!el) return; if (on) el.removeAttribute("hidden"); else el.setAttribute("hidden", ""); }

  var swSupported = "serviceWorker" in navigator && window.isSecureContext;
  var pushSupported = swSupported && "PushManager" in window && "Notification" in window;

  // ---- Badge belum dibaca ------------------------------------------------
  function setUnread(n) {
    document.querySelectorAll("[data-unread]").forEach(function (el) {
      el.textContent = n > 99 ? "99+" : String(n);
      toggle(el, n > 0);
    });
  }
  function refresh() {
    if (document.visibilityState === "hidden") return;
    fetch("/app/notifications/badge", { credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (d) { if (d && typeof d.unread === "number") setUnread(d.unread); })
      .catch(function () { /* offline / session habis: abaikan */ });
  }
  window.setInterval(refresh, 60000);
  document.addEventListener("visibilitychange", function () { if (document.visibilityState === "visible") refresh(); });

  // ---- Service worker ----------------------------------------------------
  // Registrasi bisa gagal (mis. browser/pengaturan memblokir service worker);
  // simpan hasilnya agar panel push tidak menunggu selamanya.
  var swReg = null;
  if (swSupported) {
    swReg = navigator.serviceWorker.register("/sw.js");
    swReg.catch(function () { /* ditangani panel push */ });
    navigator.serviceWorker.addEventListener("message", function (e) {
      if (e.data && e.data.type === "sikat:notification") refresh();
    });
  }

  function b64ToBytes(s) {
    var pad = "=".repeat((4 - (s.length % 4)) % 4);
    var raw = window.atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
    var out = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  }
  function sameKey(buf, b64) {
    if (!buf) return true;
    var a = new Uint8Array(buf), b = b64ToBytes(b64);
    if (a.length !== b.length) return false;
    for (var i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
    return true;
  }
  function ready() {
    return swReg.then(function () { return navigator.serviceWorker.ready; }, function () {
      throw new Error("Service worker tidak dapat dipasang di browser ini (mungkin diblokir pengaturan browser atau mode penyamaran). Notifikasi tetap tersedia di inbox.");
    });
  }
  function currentSub() {
    if (!pushSupported) return Promise.resolve(null);
    return ready().then(function (reg) { return reg.pushManager.getSubscription(); });
  }
  function post(path, body) {
    return fetch(path, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json", Accept: "application/json", "X-CSRF-Token": csrf() },
      body: JSON.stringify(body)
    }).then(function (r) {
      if (r.ok) return r;
      return r.json().catch(function () { return {}; }).then(function (d) {
        throw new Error(d.error || "Server menolak permintaan (HTTP " + r.status + ").");
      });
    });
  }

  // Sinkron otomatis sekali per login: endpoint bisa berganti, dan saat logout
  // langganan di server dihapus sehingga perlu didaftarkan ulang.
  if (pushSupported && Notification.permission === "granted" && load("localStorage", OWNER_KEY) === userId &&
      load("sessionStorage", SYNC_KEY) !== csrf()) {
    currentSub().then(function (sub) {
      if (!sub) return null;
      return post("/app/notifications/push/subscribe", sub.toJSON()).then(function () { save("sessionStorage", SYNC_KEY, csrf()); });
    }).catch(function () { /* abaikan */ });
  }

  // Ajakan mengaktifkan push di halaman inbox.
  var hint = document.querySelector("[data-push-hint]");
  if (hint && pushSupported && Notification.permission !== "denied") {
    currentSub().then(function (sub) {
      toggle(hint, !(sub && Notification.permission === "granted" && load("localStorage", OWNER_KEY) === userId));
    }).catch(function () {});
  }

  // ---- Panel "Push di perangkat ini" ------------------------------------
  var panel = document.querySelector("[data-push-panel]");
  if (!panel) return;
  var stateEl = panel.querySelector("[data-push-state]");
  var detailEl = panel.querySelector("[data-push-detail]");
  var enableBtn = panel.querySelector("[data-push-enable]");
  var disableBtn = panel.querySelector("[data-push-disable]");
  var errEl = panel.querySelector("[data-push-error]");
  var vapidKey = panel.getAttribute("data-vapid-key");

  function show(state, detail, canEnable, canDisable) {
    stateEl.textContent = state;
    detailEl.textContent = detail || "";
    toggle(enableBtn, canEnable);
    toggle(disableBtn, canDisable);
  }
  function fail(e) {
    errEl.textContent = (e && e.message) || "Terjadi kesalahan.";
    toggle(errEl, true);
  }
  function markThisDevice(sub) {
    if (!window.crypto || !window.crypto.subtle || !window.TextEncoder) return;
    window.crypto.subtle.digest("SHA-256", new TextEncoder().encode(sub.endpoint)).then(function (buf) {
      var hex = Array.prototype.map.call(new Uint8Array(buf), function (b) { return ("0" + b.toString(16)).slice(-2); }).join("");
      var row = document.querySelector('[data-device-hash="' + hex + '"]');
      if (row) toggle(row.querySelector("[data-this-device]"), true);
    });
  }

  if (!window.isSecureContext) {
    show("Butuh koneksi aman (HTTPS)", "Notifikasi push hanya dapat diaktifkan bila SIKAT dibuka lewat alamat https://. Notifikasi tetap tersedia di inbox.", false, false);
    return;
  }
  if (!pushSupported) {
    var ios = /iphone|ipad|ipod/i.test(navigator.userAgent);
    var standalone = (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) || navigator.standalone;
    show("Browser ini belum mendukung notifikasi push",
      ios && !standalone
        ? "Di iPhone/iPad, tambahkan SIKAT ke Layar Utama terlebih dahulu (Safari → Bagikan → Tambah ke Layar Utama), lalu buka dari ikon tersebut."
        : "Gunakan Chrome, Edge, Firefox, atau Safari versi terbaru. Notifikasi tetap tersedia di inbox.",
      false, false);
    return;
  }

  function render() {
    if (Notification.permission === "denied") {
      show("Notifikasi diblokir di browser ini", "Izinkan notifikasi untuk situs ini lewat pengaturan browser (ikon gembok di bilah alamat), lalu muat ulang halaman.", false, false);
      return;
    }
    currentSub().then(function (sub) {
      var mine = sub && load("localStorage", OWNER_KEY) === userId;
      if (mine && Notification.permission === "granted") {
        show("Aktif di perangkat ini", "Perangkat ini menerima push sesuai preferensi role Anda.", false, true);
        markThisDevice(sub);
      } else {
        show("Belum aktif di perangkat ini", "Aktifkan agar tiket baru dan peringatan SLA muncul walau aplikasi sedang tidak dibuka.", true, false);
      }
    }).catch(function (e) {
      show("Notifikasi push tidak tersedia", "", false, false);
      fail(e);
    });
  }

  enableBtn.addEventListener("click", function () {
    toggle(errEl, false);
    enableBtn.disabled = true;
    Notification.requestPermission()
      .then(function (perm) {
        if (perm !== "granted") throw new Error("Izin notifikasi tidak diberikan.");
        return ready();
      })
      .then(function (reg) {
        return reg.pushManager.getSubscription().then(function (old) {
          // Langganan lama dengan kunci server berbeda harus diganti.
          if (old && !sameKey(old.options && old.options.applicationServerKey, vapidKey)) {
            return old.unsubscribe().then(function () { return null; });
          }
          return old;
        }).then(function (old) {
          return old || reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(vapidKey) });
        });
      })
      .then(function (sub) { return post("/app/notifications/push/subscribe", sub.toJSON()); })
      .then(function () {
        save("localStorage", OWNER_KEY, userId);
        save("sessionStorage", SYNC_KEY, csrf());
        window.location.reload();
      })
      .catch(function (e) { enableBtn.disabled = false; fail(e); });
  });

  disableBtn.addEventListener("click", function () {
    toggle(errEl, false);
    disableBtn.disabled = true;
    currentSub()
      .then(function (sub) {
        if (!sub) return null;
        return post("/app/notifications/push/unsubscribe", { endpoint: sub.endpoint }).then(function () { return sub.unsubscribe(); });
      })
      .then(function () { save("localStorage", OWNER_KEY, null); window.location.reload(); })
      .catch(function (e) { disableBtn.disabled = false; fail(e); });
  });

  render();
})();
