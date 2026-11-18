-- PHASE 9: riwayat backup database & uji restore.

-- +goose Up
CREATE TABLE backup_logs (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  trigger_type      ENUM('SCHEDULE','MANUAL','CLI') NOT NULL,
  status            ENUM('RUNNING','SUCCESS','FAILED') NOT NULL,
  backup_date       DATE NULL COMMENT 'tanggal WITA (backup terjadwal: satu per hari)',
  started_at        DATETIME(3) NOT NULL,
  finished_at       DATETIME(3) NULL,
  file_name         VARCHAR(255) NULL,
  size_bytes        BIGINT UNSIGNED NULL,
  sha256            CHAR(64) NULL,
  migration_version BIGINT NULL,
  row_counts        JSON NULL COMMENT 'jumlah baris tabel utama saat backup (pembanding uji restore)',
  error_message     TEXT NULL,
  restore_tested_at DATETIME(3) NULL,
  restore_status    ENUM('SUCCESS','FAILED') NULL,
  restore_message   TEXT NULL,
  triggered_by      BIGINT UNSIGNED NULL,
  KEY ix_backup_started (started_at),
  KEY ix_backup_date (backup_date, status),
  CONSTRAINT fk_backup_user FOREIGN KEY (triggered_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Retensi foto lampiran (Q9, default: 2 tahun setelah tiket ditutup; metadata tetap).
ALTER TABLE attachments
  ADD COLUMN file_deleted_at DATETIME(3) NULL COMMENT 'file dihapus sesuai retensi; baris metadata tetap',
  ADD KEY ix_attachments_retention (file_deleted_at, ticket_id);

INSERT INTO system_settings (setting_key, value, value_type, description, updated_at) VALUES
  ('attachments.retention_days', '730', 'INT', 'Hapus file foto tiket N hari setelah tiket ditutup/dibatalkan (0 = simpan selamanya); data tiket tetap', UTC_TIMESTAMP(3));

-- +goose Down
DELETE FROM system_settings WHERE setting_key = 'attachments.retention_days';
ALTER TABLE attachments DROP INDEX ix_attachments_retention, DROP COLUMN file_deleted_at;
DROP TABLE IF EXISTS backup_logs;
