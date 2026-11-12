/* SIKAT service worker.
 * - Aset statis berversi: cache-first (offline tetap tampil rapi).
 * - Halaman: selalu dari jaringan; bila offline tampilkan /offline.
 *   Halaman yang memuat data (tiket, pengguna) TIDAK disimpan di cache.
 * - Web Push: tampilkan notifikasi generik, buka aplikasi saat diklik.
 * Versi cache dan daftar precache diisi server saat /sw.js diminta (internal/server/pwa.go).
 */
"use strict";

var VERSION = "__VERSION__";
var CACHE = "sikat-" + VERSION;
var PRECACHE = __PRECACHE__;
var OFFLINE_URL = "/offline";

self.addEventListener("install", function (event) {
  event.waitUntil(
    caches.open(CACHE)
      .then(function (cache) { return cache.addAll(PRECACHE.concat([OFFLINE_URL])); })
      .then(function () { return self.skipWaiting(); })
  );
});

self.addEventListener("activate", function (event) {
  event.waitUntil(
    caches.keys()
      .then(function (keys) {
        return Promise.all(keys.filter(function (k) { return k.indexOf("sikat-") === 0 && k !== CACHE; })
          .map(function (k) { return caches.delete(k); }));
      })
      .then(function () { return self.clients.claim(); })
  );
});

self.addEventListener("fetch", function (event) {
  var req = event.request;
  if (req.method !== "GET") return;
  var url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  if (req.mode === "navigate") {
    event.respondWith(fetch(req).catch(function () {
      return caches.match(OFFLINE_URL);
    }));
    return;
  }
  if (url.pathname.indexOf("/static/") === 0) {
    event.respondWith(
      caches.match(req).then(function (hit) {
        if (hit) return hit;
        return fetch(req).then(function (res) {
          // Hanya aset berversi (?v=) yang aman disimpan lama.
          if (res.ok && url.searchParams.has("v")) {
            var copy = res.clone();
            caches.open(CACHE).then(function (c) { c.put(req, copy); });
          }
          return res;
        });
      })
    );
  }
});

// ---- Web Push ------------------------------------------------------------

self.addEventListener("push", function (event) {
  var data = {};
  try { data = event.data ? event.data.json() : {}; } catch (_) { data = {}; }
  var title = data.title || "SIKAT";
  var options = {
    body: data.body || "Ada notifikasi baru. Buka aplikasi untuk melihat.",
    icon: "/static/icons/icon-192.png",
    badge: "/static/icons/badge-96.png",
    tag: data.tag || undefined,
    renotify: !!data.tag,
    requireInteraction: !!data.urgent,
    data: { url: data.url || "/app/notifications" }
  };
  event.waitUntil(
    self.registration.showNotification(title, options).then(function () {
      // Perbarui badge di tab aplikasi yang sedang terbuka.
      return self.clients.matchAll({ type: "window", includeUncontrolled: true }).then(function (list) {
        list.forEach(function (c) { c.postMessage({ type: "sikat:notification" }); });
      });
    })
  );
});

self.addEventListener("notificationclick", function (event) {
  event.notification.close();
  var target = (event.notification.data && event.notification.data.url) || "/app/notifications";
  var url = new URL(target, self.location.origin);
  if (url.origin !== self.location.origin) url = new URL("/app/notifications", self.location.origin);
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then(function (list) {
      for (var i = 0; i < list.length; i++) {
        var c = list[i];
        if (new URL(c.url).origin === self.location.origin && "focus" in c) {
          return c.focus().then(function (win) { return win.navigate ? win.navigate(url.href) : win; });
        }
      }
      return self.clients.openWindow(url.href);
    })
  );
});
