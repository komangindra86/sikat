-- Daftar lisensi perangkat lunak/layanan (Master Data › Lisensi) dan notifikasi
-- "Lisensi segera habis" ke Admin IT, agar lisensi diperpanjang sebelum berakhir.
--   expires_at kosong = lisensi tanpa batas waktu (tidak diingatkan).
--   Pengingat dikirim worker 30, 14, 7, 1 hari sebelum berakhir, pada hari berakhir,
--   lalu tiap 7 hari sampai tanggal berakhir diperbarui atau lisensi dinonaktifkan.
-- Kunci lisensi tidak disimpan di SIKAT.

-- +goose Up
CREATE TABLE licenses (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code        VARCHAR(50)  NOT NULL,
  name        VARCHAR(150) NOT NULL,
  vendor      VARCHAR(150) NULL,
  quantity    INT UNSIGNED NULL COMMENT 'jumlah seat/perangkat/pengguna',
  started_at  DATE NULL,
  expires_at  DATE NULL COMMENT 'NULL = tanpa batas waktu',
  notes       VARCHAR(1000) NULL,
  is_active   BOOLEAN NOT NULL DEFAULT TRUE,
  created_at  DATETIME(3) NOT NULL,
  updated_at  DATETIME(3) NOT NULL,
  UNIQUE KEY uq_licenses_code (code),
  KEY ix_licenses_expiry (is_active, expires_at),
  CONSTRAINT chk_licenses_dates CHECK (started_at IS NULL OR expires_at IS NULL OR started_at <= expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO notification_preferences (role_id, event_type, channel, updated_at)
SELECT r.id, 'LICENSE_EXPIRY', p.ch, UTC_TIMESTAMP(3) FROM (
            SELECT 'ADMIN' role, 'PUSH' ch
  UNION ALL SELECT 'COORDINATOR', 'NONE'
  UNION ALL SELECT 'TECHNICIAN', 'NONE'
) p JOIN roles r ON r.code = p.role
ON DUPLICATE KEY UPDATE event_type = event_type;

-- +goose Down
DELETE FROM notification_preferences WHERE event_type = 'LICENSE_EXPIRY';
DELETE d FROM push_deliveries d JOIN notifications n ON n.id = d.notification_id WHERE n.event_type = 'LICENSE_EXPIRY';
DELETE FROM notifications WHERE event_type = 'LICENSE_EXPIRY';
DELETE FROM outbox_events WHERE event_type = 'LICENSE_EXPIRY';
DROP TABLE licenses;
