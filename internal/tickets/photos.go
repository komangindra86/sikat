package tickets

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"sikat/internal/attachments"
)

// parsePhotos membaca field "photos" dari form multipart yang sudah diparse.
// Mengembalikan pesan error untuk pengguna bila ada foto yang ditolak.
func parsePhotos(r *http.Request, maxFiles int, maxBytes int64) ([]Photo, string, error) {
	if r.MultipartForm == nil {
		return nil, "", nil
	}
	files := r.MultipartForm.File["photos"]
	var nonEmpty int
	for _, fh := range files {
		if fh.Size > 0 {
			nonEmpty++
		}
	}
	if nonEmpty > maxFiles {
		return nil, fmt.Sprintf("Maksimal %d foto.", maxFiles), nil
	}
	var out []Photo
	for _, fh := range files {
		if fh.Size == 0 {
			continue
		}
		if fh.Size > maxBytes {
			return nil, fmt.Sprintf("Foto %q melebihi %d MB.", truncate(fh.Filename, 40), maxBytes>>20), nil
		}
		f, err := fh.Open()
		if err != nil {
			return nil, "", err
		}
		p, err := attachments.Process(f, maxBytes)
		f.Close()
		switch {
		case errors.Is(err, attachments.ErrNotImage), errors.Is(err, attachments.ErrBadImage),
			errors.Is(err, attachments.ErrHugeImage), errors.Is(err, attachments.ErrTooLarge):
			return nil, fmt.Sprintf("Foto %q ditolak: %s.", truncate(fh.Filename, 40), err.Error()), nil
		case err != nil:
			return nil, "", err
		}
		out = append(out, Photo{Processed: p, OriginalName: fh.Filename})
	}
	return out, "", nil
}

// serveAttachment menyajikan foto dari penyimpanan dengan header aman.
func serveAttachment(w http.ResponseWriter, r *http.Request, store attachments.Store, key string) error {
	f, err := store.Open(key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return nil
		}
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Disposition", "inline")
	name := key[strings.LastIndex(key, "/")+1:]
	http.ServeContent(w, r, name, st.ModTime().Truncate(time.Second), f)
	return nil
}
