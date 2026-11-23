package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"sikat/internal/web"
)

// pwaHandler menyajikan service worker, manifest, dan favicon di root (scope "/").
type pwaHandler struct {
	static   fs.FS // isi folder static/
	renderer *web.Renderer
	dev      bool
}

// precache: aset yang disimpan saat service worker dipasang (URL berversi).
var precache = []string{
	"css/app.css", "js/tema.js", "js/app.js", "js/kontrol.js", "js/pwa.js", "js/htmx.min.js",
	"css/fonts/geist-regular.woff2", "css/fonts/geist-semibold.woff2", "css/fonts/geist-bold.woff2",
	"icons/favicon-32.png", "icons/logo-64.png", "icons/icon-192.png", "icons/badge-96.png",
}

func (p *pwaHandler) serviceWorker(w http.ResponseWriter, r *http.Request) {
	src, err := fs.ReadFile(p.static, "sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	urls := make([]string, 0, len(precache))
	for _, f := range precache {
		urls = append(urls, p.renderer.StaticURL(f))
	}
	list, _ := json.Marshal(urls)
	// Versi berubah bila sw.js atau salah satu aset berubah → cache lama dibuang.
	sum := sha256.Sum256(append(append([]byte{}, src...), list...))
	body := strings.Replace(string(src), "__PRECACHE__", string(list), 1)
	body = strings.Replace(body, "__VERSION__", hex.EncodeToString(sum[:])[:12], 1)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

func (p *pwaHandler) manifest(w http.ResponseWriter, r *http.Request) {
	icon := func(file, size, purpose string) map[string]string {
		return map[string]string{"src": p.renderer.StaticURL(file), "sizes": size, "type": "image/png", "purpose": purpose}
	}
	m := map[string]any{
		"id":               "/app",
		"name":             "SIKAT — Kelola Aduan & Tindak Lanjut IT",
		"short_name":       "SIKAT",
		"description":      "Pengaduan, penanganan, dan pemeliharaan perangkat IT rumah sakit.",
		"lang":             "id",
		"start_url":        "/app",
		"scope":            "/",
		"display":          "standalone",
		"orientation":      "any",
		"background_color": "#FFFFFF",
		"theme_color":      "#00786F",
		"icons": []map[string]string{
			icon("icons/icon-192.png", "192x192", "any"),
			icon("icons/icon-512.png", "512x512", "any"),
			icon("icons/maskable-512.png", "512x512", "maskable"),
		},
		"shortcuts": []map[string]any{
			{"name": "Antrean tiket", "url": "/app/tickets", "icons": []map[string]string{icon("icons/icon-192.png", "192x192", "any")}},
			{"name": "Notifikasi", "url": "/app/notifications", "icons": []map[string]string{icon("icons/icon-192.png", "192x192", "any")}},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	if p.dev {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	_ = json.NewEncoder(w).Encode(m)
}

func (p *pwaHandler) favicon(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(p.static, "icons/favicon-32.png")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}
