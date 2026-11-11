// Package attachments memproses dan menyimpan foto lampiran.
//
// Setiap foto:
//   - diverifikasi dari isi file (magic bytes), bukan ekstensi;
//   - ditolak bila dimensinya tidak wajar (mencegah "decompression bomb");
//   - diperkecil bila terlalu besar, lalu di-encode ulang menjadi JPEG sehingga
//     metadata EXIF/GPS dan data tersembunyi lain terbuang;
//   - disimpan di luar webroot dengan nama acak dan hanya disajikan lewat handler
//     yang memeriksa hak akses.
package attachments

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registrasi decoder PNG
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registrasi decoder WebP
)

// Batas pemrosesan gambar.
const (
	MaxPixels   = 40_000_000 // ±40 megapiksel
	MaxLongEdge = 1920
	jpegQuality = 82
)

// Error yang ditampilkan ke pengguna.
var (
	ErrTooLarge    = errors.New("ukuran foto melebihi batas")
	ErrNotImage    = errors.New("file bukan foto JPG, PNG, atau WebP")
	ErrBadImage    = errors.New("foto rusak atau tidak dapat dibaca")
	ErrHugeImage   = errors.New("resolusi foto terlalu besar")
	allowedSniffed = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
)

// Processed adalah foto yang sudah dibersihkan dan siap disimpan.
type Processed struct {
	Data   []byte
	Width  int
	Height int
	SHA256 [32]byte
	Mime   string
}

// Process membaca foto (maksimal maxBytes), memvalidasi, memperkecil, dan
// meng-encode ulang menjadi JPEG.
func Process(r io.Reader, maxBytes int64) (Processed, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return Processed{}, err
	}
	if int64(len(raw)) > maxBytes {
		return Processed{}, ErrTooLarge
	}
	if len(raw) == 0 || !allowedSniffed[http.DetectContentType(raw)] {
		return Processed{}, ErrNotImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Processed{}, ErrBadImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels {
		return Processed{}, ErrHugeImage
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Processed{}, ErrBadImage
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > MaxLongEdge || h > MaxLongEdge {
		if w >= h {
			h = h * MaxLongEdge / w
			w = MaxLongEdge
		} else {
			w = w * MaxLongEdge / h
			h = MaxLongEdge
		}
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// Latar putih untuk gambar transparan (PNG/WebP) karena JPEG tidak punya alfa.
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return Processed{}, err
	}
	return Processed{Data: out.Bytes(), Width: w, Height: h, SHA256: sha256.Sum256(out.Bytes()), Mime: "image/jpeg"}, nil
}

// Store menyimpan file di folder lokal.
type Store struct {
	Dir string
}

// Save menulis data dengan nama acak di subfolder tahun/bulan dan mengembalikan kuncinya.
func (s Store) Save(data []byte, now time.Time) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := fmt.Sprintf("%04d/%02d/%s.jpg", now.Year(), int(now.Month()), hex.EncodeToString(b))
	full := filepath.Join(s.Dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", err
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(full)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(full)
		return "", err
	}
	return key, nil
}

// path mengubah kunci menjadi path aman di dalam Dir.
func (s Store) path(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", errors.New("kunci file tidak valid")
	}
	return filepath.Join(s.Dir, filepath.FromSlash(key)), nil
}

// Open membuka file untuk disajikan.
func (s Store) Open(key string) (*os.File, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Remove menghapus file (dipakai untuk membersihkan file bila transaksi gagal).
func (s Store) Remove(key string) {
	if p, err := s.path(key); err == nil {
		_ = os.Remove(p)
	}
}

// Check memastikan folder penyimpanan dapat ditulisi (dipanggil saat start).
func (s Store) Check() error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return fmt.Errorf("folder lampiran %s tidak dapat dibuat: %w", s.Dir, err)
	}
	f, err := os.CreateTemp(s.Dir, ".tulis-uji-*")
	if err != nil {
		return fmt.Errorf("folder lampiran %s tidak dapat ditulisi: %w", s.Dir, err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}
