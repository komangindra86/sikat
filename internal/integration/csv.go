package integration

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// utf8BOM: penanda UTF-8 yang ditambahkan Excel (dibuang saat membaca, ditulis di template).
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Batas file CSV.
const (
	MaxCSVBytes = 5 << 20
	MaxCSVRows  = 20000
)

// csvSpec mendefinisikan kolom CSV per entitas (nama kolom bahasa Indonesia,
// huruf kecil). Kolom lain diabaikan.
type csvSpec struct {
	Columns  []string // urutan untuk template
	Required []string
	Example  [][]string
	Help     map[string]string
}

var csvSpecs = map[string]csvSpec{
	EntityDepartment: {
		Columns:  []string{"external_id", "kode", "nama", "layanan_kritis", "aktif"},
		Required: []string{"external_id", "kode", "nama"},
		Example:  [][]string{{"U-001", "IGD", "Instalasi Gawat Darurat", "ya", "ya"}, {"U-002", "KEU", "Keuangan", "tidak", "ya"}},
		Help: map[string]string{"external_id": "ID unit di sistem sumber (tidak berubah)", "kode": "Kode singkat unik, maks. 30 karakter",
			"layanan_kritis": "ya/tidak (kosong = tidak)", "aktif": "ya/tidak (kosong = ya)"},
	},
	EntityRoom: {
		Columns:  []string{"external_id", "kode", "nama", "unit", "lokasi", "aktif"},
		Required: []string{"external_id", "kode", "nama", "unit"},
		Example:  [][]string{{"R-001", "IGD-TRS", "Ruang Triase", "U-001", "Gedung A Lt.1", "ya"}},
		Help: map[string]string{"unit": "external_id unit pada file/sumber yang sama, atau kode unit di SIKAT",
			"lokasi": "Keterangan lokasi (opsional)"},
	},
	EntityEmployee: {
		Columns:  []string{"nip", "nama", "unit", "jabatan", "telepon", "aktif"},
		Required: []string{"nip", "nama"},
		Example:  [][]string{{"198701012010012001", "Ners Ani Lestari", "U-001", "Perawat", "0812-0000-0000", "ya"}},
		Help:     map[string]string{"nip": "NIP/NIK pegawai, dipakai sebagai external_id", "unit": "external_id atau kode unit (opsional)"},
	},
	EntityAsset: {
		Columns:  []string{"external_id", "kode_aset", "nama", "kategori", "unit", "ruangan", "merek", "model", "nomor_seri"},
		Required: []string{"external_id", "kode_aset", "nama", "kategori"},
		Example:  [][]string{{"INV-0001", "IGD-PC-01", "PC Triase", "PC", "U-001", "R-001", "Lenovo", "ThinkCentre M70", "SN123456"}},
		Help: map[string]string{"kategori": "Kode atau nama kategori perangkat yang sudah ada di SIKAT",
			"ruangan": "external_id atau kode ruangan (opsional)", "unit": "external_id atau kode unit (opsional; diisi dari ruangan bila kosong)"},
	},
}

// CSVColumns mengembalikan kolom template dan penjelasannya.
func CSVColumns(entity string) ([]string, []string, map[string]string) {
	s := csvSpecs[entity]
	return s.Columns, s.Required, s.Help
}

// Template menghasilkan contoh CSV (BOM UTF-8 + pemisah ';' agar langsung rapi
// dibuka Excel berbahasa Indonesia).
func Template(entity string) []byte {
	s := csvSpecs[entity]
	var buf bytes.Buffer
	buf.Write(utf8BOM)
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	_ = w.Write(s.Columns)
	for _, r := range s.Example {
		_ = w.Write(r)
	}
	w.Flush()
	return buf.Bytes()
}

// ParseCSV membaca CSV untuk entitas. Pemisah ',' atau ';' dideteksi dari baris
// header. Baris yang tidak valid dicatat di RowErrors (nomor baris file).
func ParseCSV(entity string, data []byte) (Batch, error) {
	b := Batch{Entity: entity, RowErrors: map[int]string{}}
	spec, ok := csvSpecs[entity]
	if !ok {
		return b, ErrUnsupported
	}
	if len(data) > MaxCSVBytes {
		return b, fmt.Errorf("ukuran file maksimal %d MB", MaxCSVBytes>>20)
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		return b, errors.New("file harus berformat CSV UTF-8 (di Excel: Simpan Sebagai → CSV UTF-8)")
	}
	firstLine := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		firstLine = data[:i]
	}
	r := csv.NewReader(bytes.NewReader(data))
	if bytes.Count(firstLine, []byte(";")) > bytes.Count(firstLine, []byte(",")) {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return b, errors.New("file kosong")
	}
	if err != nil {
		return b, fmt.Errorf("baris judul tidak dapat dibaca: %v", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	var missing []string
	for _, c := range spec.Required {
		if _, ok := idx[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return b, fmt.Errorf("kolom wajib tidak ada: %s (unduh template untuk format yang benar)", strings.Join(missing, ", "))
	}
	line, count := 1, 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		count++
		line++
		if l, _ := r.FieldPos(0); l > 0 && err == nil {
			line = l // nomor baris sebenarnya (baris kosong dilewati pembaca CSV)
		}
		if err != nil {
			b.RowErrors[line] = "Baris tidak dapat dibaca: " + err.Error()
			continue
		}
		if count > MaxCSVRows {
			return b, fmt.Errorf("maksimal %d baris per file", MaxCSVRows)
		}
		get := func(col string) string {
			i, ok := idx[col]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		empty := true
		for _, v := range rec {
			if strings.TrimSpace(v) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		var reqMissing []string
		for _, c := range spec.Required {
			if get(c) == "" {
				reqMissing = append(reqMissing, c)
			}
		}
		if len(reqMissing) > 0 {
			b.RowErrors[line] = "Kolom wajib kosong: " + strings.Join(reqMissing, ", ")
			continue
		}
		active, aerr := parseBool(get("aktif"), true)
		if aerr != nil {
			b.RowErrors[line] = "Kolom aktif: " + aerr.Error()
			continue
		}
		switch entity {
		case EntityDepartment:
			crit, cerr := parseBool(get("layanan_kritis"), false)
			if cerr != nil {
				b.RowErrors[line] = "Kolom layanan_kritis: " + cerr.Error()
				continue
			}
			b.Departments = append(b.Departments, Department{Row: line, ExternalID: get("external_id"), Code: get("kode"),
				Name: get("nama"), Critical: crit, Active: active})
		case EntityRoom:
			b.Rooms = append(b.Rooms, Room{Row: line, ExternalID: get("external_id"), Code: get("kode"), Name: get("nama"),
				DepartmentRef: get("unit"), LocationNote: get("lokasi"), Active: active})
		case EntityEmployee:
			b.Employees = append(b.Employees, Employee{Row: line, ExternalID: get("nip"), Name: get("nama"),
				DepartmentRef: get("unit"), Position: get("jabatan"), Phone: get("telepon"), Active: active})
		case EntityAsset:
			b.Assets = append(b.Assets, Asset{Row: line, ExternalID: get("external_id"), Code: get("kode_aset"), Name: get("nama"),
				CategoryRef: get("kategori"), DepartmentRef: get("unit"), RoomRef: get("ruangan"),
				Brand: get("merek"), Model: get("model"), Serial: get("nomor_seri")})
		}
	}
	if b.Len() == 0 && len(b.RowErrors) == 0 {
		return b, errors.New("file tidak berisi data")
	}
	return b, nil
}

func parseBool(v string, def bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return def, nil
	case "ya", "y", "1", "true", "aktif", "yes":
		return true, nil
	case "tidak", "t", "n", "0", "false", "nonaktif", "no":
		return false, nil
	}
	return def, fmt.Errorf("isi \"ya\" atau \"tidak\", bukan %q", v)
}
