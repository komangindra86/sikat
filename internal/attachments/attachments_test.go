package attachments

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pngBytes(t *testing.T, w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.NRGBA{R: 255, A: 128})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestProcessReencodesAndResizes(t *testing.T) {
	p, err := Process(bytes.NewReader(pngBytes(t, 3000, 1500)), 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	if p.Mime != "image/jpeg" || p.Width != MaxLongEdge || p.Height != 960 {
		t.Fatalf("hasil tidak sesuai: %s %dx%d", p.Mime, p.Width, p.Height)
	}
	if _, err := jpeg.Decode(bytes.NewReader(p.Data)); err != nil {
		t.Fatalf("hasil harus JPEG valid: %v", err)
	}
}

func TestProcessStripsExif(t *testing.T) {
	var src bytes.Buffer
	_ = jpeg.Encode(&src, image.NewRGBA(image.Rect(0, 0, 20, 20)), nil)
	// Sisipkan segmen APP1 "Exif" palsu berisi penanda GPS setelah SOI.
	exif := append([]byte{0xFF, 0xE1, 0x00, 0x16}, []byte("Exif\x00\x00GPS-RAHASIA-XY")...)
	withExif := append(append([]byte{}, src.Bytes()[:2]...), append(exif, src.Bytes()[2:]...)...)
	p, err := Process(bytes.NewReader(withExif), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(p.Data, []byte("GPS-RAHASIA")) || bytes.Contains(p.Data, []byte("Exif")) {
		t.Fatal("metadata EXIF harus terbuang setelah re-encode")
	}
}

func TestProcessRejects(t *testing.T) {
	if _, err := Process(strings.NewReader("<html>bukan gambar</html>"), 1<<20); err != ErrNotImage {
		t.Fatalf("bukan gambar: %v", err)
	}
	if _, err := Process(bytes.NewReader(pngBytes(t, 50, 50)), 10); err != ErrTooLarge {
		t.Fatalf("terlalu besar: %v", err)
	}
	broken := pngBytes(t, 50, 50)[:40]
	if _, err := Process(bytes.NewReader(broken), 1<<20); err != ErrBadImage {
		t.Fatalf("rusak: %v", err)
	}
	// PNG valid berukuran kecil tapi header mengklaim 20000x20000 piksel.
	hdr := pngBytes(t, 1, 1)
	hdr[16], hdr[17], hdr[18], hdr[19] = 0, 0, 0x4E, 0x20 // width 20000
	hdr[20], hdr[21], hdr[22], hdr[23] = 0, 0, 0x4E, 0x20 // height 20000
	crc := crc32.ChecksumIEEE(hdr[12:29])                 // CRC chunk IHDR (tipe + data)
	hdr[29], hdr[30], hdr[31], hdr[32] = byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc)
	if _, err := Process(bytes.NewReader(hdr), 1<<20); err != ErrHugeImage {
		t.Fatalf("resolusi raksasa harus ditolak sebelum decode: %v", err)
	}
}

func TestStore(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	key, err := s.Save([]byte("isi"), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "2026/11/") || !strings.HasSuffix(key, ".jpg") {
		t.Fatalf("kunci: %s", key)
	}
	f, err := s.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, bad := range []string{"../etc/passwd", "/abs", `a\b`, ""} {
		if _, err := s.Open(bad); err == nil {
			t.Errorf("kunci %q harus ditolak", bad)
		}
	}
	s.Remove(key)
	if _, err := os.Stat(filepath.Join(s.Dir, key)); !os.IsNotExist(err) {
		t.Fatal("file harus terhapus")
	}
}
