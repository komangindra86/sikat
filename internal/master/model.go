// Package master menyediakan CRUD seragam untuk master data referensi
// (unit, ruangan, bidang, jenis gangguan, kategori perangkat, alasan tunda,
// jenis kegiatan, template tindakan, sparepart).
//
// Setiap entitas dideskripsikan secara deklaratif (Entity/Field). Nama tabel dan
// kolom adalah konstanta di kode — bukan input pengguna — sehingga SQL yang
// dibangun aman; seluruh nilai tetap dikirim sebagai parameter.
package master

import (
	"database/sql"
	"fmt"
	"time"
)

// Kind adalah jenis input field.
type Kind int

// Jenis field.
const (
	KindText Kind = iota
	KindTextarea
	KindBool
	KindInt
	KindSelect // referensi ke tabel lain (foreign key)
	KindEnum   // pilihan tetap
	KindQty    // jumlah desimal >= 0 (mis. stok), 2 angka di belakang koma
	KindDate   // tanggal (YYYY-MM-DD)
)

// Ref mendeskripsikan tabel yang direferensikan field KindSelect.
type Ref struct {
	Table    string
	LabelCol string
	OrderBy  string
	NoActive bool // tabel tanpa kolom is_active (semua baris dapat dipilih)
	// Where: syarat tambahan untuk baris yang boleh dipilih (SQL statis, tanpa input pengguna).
	Where string
}

// Choice adalah pilihan untuk select/enum.
type Choice struct {
	Value string
	Label string
}

// Field mendeskripsikan satu kolom yang dapat diedit.
type Field struct {
	Column   string
	Label    string
	Help     string
	Kind     Kind
	Required bool
	MaxLen   int
	Min, Max int64
	Upper    bool // ubah ke huruf besar (kode)
	Ref      *Ref
	Choices  []Choice // untuk KindEnum; Value "" berarti NULL
	Default  string
	InList   bool // tampil sebagai kolom di daftar
	// Expiry: tanggal berakhir; daftar menampilkan sisa hari dengan penanda warna.
	Expiry bool
	// Auto: nilai dibuat sistem saat data dibuat dan tidak dapat diubah lewat form.
	Auto *AutoCode
}

// AutoCode adalah format kode otomatis: Prefix-NNN (Width digit).
type AutoCode struct {
	Prefix string
	Width  int
}

// Example mengembalikan contoh kode pertama, mis. "UNT-001".
func (a AutoCode) Example() string { return fmt.Sprintf("%s-%0*d", a.Prefix, a.Width, 1) }

// Helper jenis field untuk template.
func (f Field) IsText() bool     { return f.Kind == KindText }
func (f Field) IsTextarea() bool { return f.Kind == KindTextarea }
func (f Field) IsBool() bool     { return f.Kind == KindBool }
func (f Field) IsInt() bool      { return f.Kind == KindInt }
func (f Field) IsSelect() bool   { return f.Kind == KindSelect }
func (f Field) IsEnum() bool     { return f.Kind == KindEnum }
func (f Field) IsQty() bool      { return f.Kind == KindQty }
func (f Field) IsDate() bool     { return f.Kind == KindDate }

// ChoiceLabel mengembalikan label pilihan enum.
func (f Field) ChoiceLabel(v string) string {
	for _, c := range f.Choices {
		if c.Value == v {
			return c.Label
		}
	}
	return v
}

// Entity mendeskripsikan satu master data.
type Entity struct {
	Slug        string // segmen URL
	Table       string
	Title       string // judul jamak, mis. "Unit"
	Singular    string // mis. "unit"
	Description string
	Fields      []Field
	OrderBy     string
	Search      []string
	HasActive   bool
	TracksSync  bool // memiliki kolom source/local_modified_at
	// UniqueFields memetakan nama index unik → kolom dan pesan error.
	UniqueFields map[string]UniqueRule
	// CheckFields memetakan nama CHECK constraint → kolom dan pesan error.
	CheckFields map[string]UniqueRule
	// Links adalah tautan tambahan di kepala halaman daftar.
	Links []Link
	// Group mengelompokkan entitas di halaman indeks (mis. "Konfigurasi").
	Group string
}

// Link adalah tautan tambahan.
type Link struct {
	Label string
	URL   string
}

// UniqueRule adalah pesan untuk pelanggaran index unik.
type UniqueRule struct {
	Column  string
	Message string
}

// Field mencari definisi field berdasarkan kolom.
func (e *Entity) Field(col string) (Field, bool) {
	for _, f := range e.Fields {
		if f.Column == col {
			return f, true
		}
	}
	return Field{}, false
}

// ListFields mengembalikan field yang tampil di daftar.
func (e *Entity) ListFields() []Field {
	var out []Field
	for _, f := range e.Fields {
		if f.InList {
			out = append(out, f)
		}
	}
	return out
}

// Record adalah satu baris master data. Nilai disimpan sebagai string mentah
// dari database ("" untuk NULL).
type Record struct {
	ID        int64
	Values    map[string]string
	IsActive  bool
	Source    string
	UpdatedAt time.Time
}

// Get mengembalikan nilai kolom.
func (r Record) Get(col string) string { return r.Values[col] }

// ListQuery adalah parameter daftar.
type ListQuery struct {
	Search          string
	IncludeInactive bool
	Limit, Offset   int
}

// nullable mengubah nilai hasil validasi menjadi argumen SQL.
func nullable(v *string) any {
	if v == nil {
		return sql.NullString{}
	}
	return *v
}
