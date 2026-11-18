package attachments

import (
	"context"
	"database/sql"
	"time"
)

// PurgeExpired menghapus FILE foto tiket yang sudah ditutup/dibatalkan lebih
// dari retentionDays hari (Q9, default 730). Baris metadata tetap (dengan
// file_deleted_at) agar histori tiket utuh. retentionDays <= 0 = simpan selamanya.
// Foto kegiatan & pemeliharaan tidak tersentuh. Diproses bertahap (maks 500).
func PurgeExpired(ctx context.Context, db *sql.DB, store Store, now time.Time, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cut := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	rows, err := db.QueryContext(ctx, `
		SELECT a.id, a.storage_key FROM attachments a JOIN tickets t ON t.id = a.ticket_id
		 WHERE a.file_deleted_at IS NULL
		   AND ((t.status = 'CLOSED' AND t.closed_at < ?) OR (t.status = 'CANCELLED' AND t.cancelled_at < ?))
		 ORDER BY a.id LIMIT 500`, cut, cut)
	if err != nil {
		return 0, err
	}
	type item struct {
		id  int64
		key string
	}
	var list []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.key); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, it := range list {
		if _, err := db.ExecContext(ctx, `UPDATE attachments SET file_deleted_at = ? WHERE id = ? AND file_deleted_at IS NULL`, now, it.id); err != nil {
			return n, err
		}
		store.Remove(it.key) // file dihapus setelah ditandai; file yang sudah hilang diabaikan
		n++
	}
	return n, nil
}
