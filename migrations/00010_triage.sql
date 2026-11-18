-- Triase & koreksi jenis gangguan (16/11/2026).
-- Masalah di luar daftar ("Lainnya") masuk bidang Triase: dipilah koordinatornya
-- lalu dipindahkan ke jenis gangguan/bidang yang tepat. Koreksi tercatat di
-- histori tiket; jenis pilihan pelapor disimpan untuk laporan pemilahan.

-- +goose Up
ALTER TABLE it_categories
  ADD COLUMN is_triage BOOLEAN NOT NULL DEFAULT FALSE AFTER coordinator_user_id,
  ADD COLUMN triage_flag TINYINT AS (IF(is_triage, 1, NULL)) STORED,
  ADD UNIQUE KEY uq_it_categories_triage (triage_flag);

INSERT INTO it_categories (code, name, description, coordinator_user_id, is_triage, sort_order, is_active, created_at, updated_at)
SELECT 'TRIASE', 'Triase / Belum terklasifikasi',
       'Masalah di luar daftar. Koordinator memilah lalu memindahkan ke jenis gangguan yang tepat.',
       -- Bila hanya ada satu koordinator aktif, ia menjadi koordinator triase.
       (SELECT IF(COUNT(*) = 1, MIN(u.id), NULL) FROM users u JOIN roles r ON r.id = u.role_id
         WHERE r.code = 'COORDINATOR' AND u.is_active = TRUE),
       TRUE, 999, TRUE, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
WHERE NOT EXISTS (SELECT 1 FROM it_categories WHERE code = 'TRIASE');

-- "Lainnya" (data awal) kini menuju Triase, bukan Hardware. Tiket lama tidak berubah.
UPDATE ticket_categories
   SET it_category_id = (SELECT id FROM (SELECT id FROM it_categories WHERE code = 'TRIASE') x),
       help_text = 'Masalah lain yang tidak ada di daftar — tim IT akan memilahnya',
       updated_at = UTC_TIMESTAMP(3)
 WHERE name = 'Lainnya';

ALTER TABLE tickets
  ADD COLUMN reported_ticket_category_id BIGINT UNSIGNED NULL COMMENT 'jenis pilihan pelapor sebelum dikoreksi' AFTER it_category_id,
  ADD CONSTRAINT fk_tickets_reported_cat FOREIGN KEY (reported_ticket_category_id) REFERENCES ticket_categories (id);

-- Preferensi notifikasi untuk event baru "Dialihkan ke bidang".
INSERT INTO notification_preferences (role_id, event_type, channel, updated_at)
SELECT r.id, 'TICKET_TRANSFERRED', p.ch, UTC_TIMESTAMP(3) FROM (
            SELECT 'COORDINATOR' role, 'PUSH' ch
  UNION ALL SELECT 'TECHNICIAN', 'PUSH'
  UNION ALL SELECT 'ADMIN', 'NONE'
) p JOIN roles r ON r.code = p.role
ON DUPLICATE KEY UPDATE event_type = event_type;

-- +goose Down
DELETE FROM notification_preferences WHERE event_type = 'TICKET_TRANSFERRED';
ALTER TABLE tickets DROP FOREIGN KEY fk_tickets_reported_cat;
ALTER TABLE tickets DROP INDEX fk_tickets_reported_cat, DROP COLUMN reported_ticket_category_id;
UPDATE ticket_categories
   SET it_category_id = (SELECT id FROM (SELECT id FROM it_categories WHERE code = 'HW') x),
       help_text = 'Masalah lain yang tidak ada di daftar'
 WHERE name = 'Lainnya';
DELETE FROM it_categories WHERE code = 'TRIASE'
  AND NOT EXISTS (SELECT 1 FROM tickets WHERE it_category_id = it_categories.id);
ALTER TABLE it_categories DROP INDEX uq_it_categories_triage, DROP COLUMN triage_flag, DROP COLUMN is_triage;
