-- Arsip kajian kebutuhan perangkat/sistem baru. Pemohon (pegawai unit) mengirim
-- dokumen kajian lewat QR lapor tanpa login; setiap kajian masuk diterima oleh
-- Admin IT. Tidak ada alur persetujuan di SIKAT: kajian disimpan sebagai arsip.
--   Jenis gangguan "Permintaan perangkat baru" (Administrasi & Inventaris) dan
--   "Usulan sistem atau fitur baru" (System Developer) diberi proposal_kind: di form
--   lapor, memilihnya membuka form kajian, bukan membuat tiket.
--   status SUBMITTED = baru masuk, RECEIVED = sudah diterima admin.
--   Dokumen (PDF/Word/Excel) disimpan di storage/kajian, hanya bisa diunduh Admin IT.

-- +goose Up
CREATE TABLE proposals (
  id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  public_token_hash  BINARY(32)   NOT NULL COMMENT 'SHA-256 token link pemantauan pemohon',
  kind               ENUM('DEVICE','SYSTEM') NOT NULL,
  title              VARCHAR(200) NOT NULL,
  summary            VARCHAR(2000) NOT NULL,
  department_id      BIGINT UNSIGNED NULL,
  requester_name     VARCHAR(120) NOT NULL,
  requester_phone    VARCHAR(30)  NULL,
  qr_code_id         BIGINT UNSIGNED NULL,
  file_key           VARCHAR(255) NOT NULL,
  file_name          VARCHAR(255) NOT NULL,
  file_mime          VARCHAR(100) NOT NULL,
  file_size          INT UNSIGNED NOT NULL,
  file_sha256        BINARY(32)   NOT NULL,
  status             ENUM('SUBMITTED','RECEIVED') NOT NULL DEFAULT 'SUBMITTED',
  received_by        BIGINT UNSIGNED NULL,
  received_at        DATETIME(3)  NULL,
  received_note      VARCHAR(1000) NULL,
  created_at         DATETIME(3)  NOT NULL,
  updated_at         DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_proposals_token (public_token_hash),
  KEY ix_proposals_status (status, created_at),
  CONSTRAINT fk_proposals_dept FOREIGN KEY (department_id) REFERENCES departments (id),
  CONSTRAINT fk_proposals_qr   FOREIGN KEY (qr_code_id)    REFERENCES qr_codes (id),
  CONSTRAINT fk_proposals_recv FOREIGN KEY (received_by)   REFERENCES users (id),
  CONSTRAINT chk_proposals_received CHECK ((status = 'RECEIVED') = (received_at IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO notification_preferences (role_id, event_type, channel, updated_at)
SELECT r.id, 'PROPOSAL_SUBMITTED', p.ch, UTC_TIMESTAMP(3) FROM (
            SELECT 'ADMIN' role, 'PUSH' ch
  UNION ALL SELECT 'COORDINATOR', 'NONE'
  UNION ALL SELECT 'TECHNICIAN', 'NONE'
) p JOIN roles r ON r.code = p.role
ON DUPLICATE KEY UPDATE event_type = event_type;

ALTER TABLE ticket_categories
  ADD COLUMN proposal_kind ENUM('DEVICE','SYSTEM') NULL
      COMMENT 'terisi = di form lapor publik diarahkan ke form kajian kebutuhan' AFTER default_priority;
UPDATE ticket_categories SET proposal_kind = 'DEVICE', updated_at = UTC_TIMESTAMP(3) WHERE name = 'Permintaan perangkat baru';
UPDATE ticket_categories SET proposal_kind = 'SYSTEM', updated_at = UTC_TIMESTAMP(3) WHERE name = 'Usulan sistem atau fitur baru';

-- +goose Down
-- File dokumen di storage/kajian tidak dihapus (tetap sebagai arsip di disk).
ALTER TABLE ticket_categories DROP COLUMN proposal_kind;
DELETE FROM notification_preferences WHERE event_type = 'PROPOSAL_SUBMITTED';
DELETE d FROM push_deliveries d JOIN notifications n ON n.id = d.notification_id WHERE n.event_type = 'PROPOSAL_SUBMITTED';
DELETE FROM notifications WHERE event_type = 'PROPOSAL_SUBMITTED';
DELETE FROM outbox_events WHERE event_type = 'PROPOSAL_SUBMITTED';
DROP TABLE proposals;
