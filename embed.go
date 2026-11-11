// Package sikat meng-embed aset yang dibutuhkan binary: template HTML, file
// statis, dan migrasi SQL. Dengan begitu deployment cukup satu file binary.
package sikat

import "embed"

// TemplatesFS berisi seluruh template HTML (folder templates/).
//
//go:embed templates
var TemplatesFS embed.FS

// StaticFS berisi CSS, JS, ikon, dan gambar (folder static/).
//
//go:embed static
var StaticFS embed.FS

// MigrationsFS berisi migrasi SQL goose (folder migrations/).
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
