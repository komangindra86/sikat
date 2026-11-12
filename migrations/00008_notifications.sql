-- PHASE 6: notifikasi berbasis event (outbox → inbox → Web Push).
-- Semua DATETIME dalam UTC. Rancangan: docs/ARCHITECTURE.md §7.

-- +goose Up

-- Outbox: ditulis dalam transaksi yang sama dengan perubahan data. dedupe_key
-- UNIQUE mencegah satu kejadian menghasilkan dua event. Worker memproses PENDING
-- (fan-out ke inbox + antrean push) lalu menandai DONE; error → coba lagi dengan
-- jeda, FAILED setelah batas percobaan.
CREATE TABLE outbox_events (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  event_type     VARCHAR(40)  NOT NULL,
  dedupe_key     VARCHAR(150) NOT NULL,
  ticket_id      BIGINT UNSIGNED NULL,
  asset_id       BIGINT UNSIGNED NULL,
  actor_user_id  BIGINT UNSIGNED NULL,
  target_user_id BIGINT UNSIGNED NULL COMMENT 'penerima khusus (penugasan, uji)',
  payload        JSON         NULL,
  status         ENUM('PENDING','DONE','FAILED') NOT NULL DEFAULT 'PENDING',
  attempts       SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  available_at   DATETIME(3)  NOT NULL,
  recipients     INT UNSIGNED NULL,
  last_error     VARCHAR(500) NULL,
  created_at     DATETIME(3)  NOT NULL,
  processed_at   DATETIME(3)  NULL,
  UNIQUE KEY uq_outbox_dedupe (dedupe_key),
  KEY ix_outbox_pending (status, available_at, id),
  KEY ix_outbox_created (created_at),
  CONSTRAINT fk_outbox_ticket FOREIGN KEY (ticket_id)      REFERENCES tickets (id),
  CONSTRAINT fk_outbox_asset  FOREIGN KEY (asset_id)       REFERENCES assets (id),
  CONSTRAINT fk_outbox_actor  FOREIGN KEY (actor_user_id)  REFERENCES users (id),
  CONSTRAINT fk_outbox_target FOREIGN KEY (target_user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Inbox in-app (selalu terisi; push hanya pelengkap). UNIQUE (user, event)
-- adalah lapis kedua pencegah notifikasi ganda. push_body adalah teks generik
-- untuk lock screen (tanpa lokasi, deskripsi, atau nama).
CREATE TABLE notifications (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id     BIGINT UNSIGNED NOT NULL,
  event_id    BIGINT UNSIGNED NOT NULL,
  event_type  VARCHAR(40)  NOT NULL,
  title       VARCHAR(150) NOT NULL,
  body        VARCHAR(500) NOT NULL,
  push_body   VARCHAR(200) NOT NULL,
  url         VARCHAR(255) NOT NULL,
  ticket_id   BIGINT UNSIGNED NULL,
  channel     ENUM('INBOX','PUSH') NOT NULL,
  read_at     DATETIME(3)  NULL,
  created_at  DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_notification_user_event (user_id, event_id),
  KEY ix_notification_inbox (user_id, read_at, id),
  KEY ix_notification_ticket (ticket_id),
  KEY ix_notification_created (created_at),
  CONSTRAINT fk_notif_user   FOREIGN KEY (user_id)   REFERENCES users (id),
  CONSTRAINT fk_notif_event  FOREIGN KEY (event_id)  REFERENCES outbox_events (id),
  CONSTRAINT fk_notif_ticket FOREIGN KEY (ticket_id) REFERENCES tickets (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Langganan Web Push per perangkat/browser. Endpoint bisa panjang, sehingga
-- keunikan memakai SHA-256-nya. Endpoint yang ditolak push service (404/410)
-- dinonaktifkan, bukan dihapus, agar alasan tetap terlihat di daftar perangkat.
CREATE TABLE push_subscriptions (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id         BIGINT UNSIGNED NOT NULL,
  endpoint        TEXT         NOT NULL,
  endpoint_hash   BINARY(32)   NOT NULL,
  p256dh          VARCHAR(200) NOT NULL,
  auth_secret     VARCHAR(50)  NOT NULL,
  device_label    VARCHAR(100) NOT NULL,
  user_agent      VARCHAR(255) NULL,
  created_at      DATETIME(3)  NOT NULL,
  last_seen_at    DATETIME(3)  NOT NULL,
  last_success_at DATETIME(3)  NULL,
  failure_count   INT UNSIGNED NOT NULL DEFAULT 0,
  last_error      VARCHAR(255) NULL,
  disabled_at     DATETIME(3)  NULL,
  disabled_reason VARCHAR(100) NULL,
  UNIQUE KEY uq_push_endpoint (endpoint_hash),
  KEY ix_push_user (user_id, disabled_at),
  CONSTRAINT fk_push_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Antrean pengiriman push: satu baris per notifikasi × perangkat, dengan retry.
CREATE TABLE push_deliveries (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  notification_id BIGINT UNSIGNED NOT NULL,
  subscription_id BIGINT UNSIGNED NOT NULL,
  status          ENUM('PENDING','SENT','FAILED','GONE','EXPIRED') NOT NULL DEFAULT 'PENDING',
  attempts        SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  next_attempt_at DATETIME(3)  NOT NULL,
  last_status     SMALLINT UNSIGNED NULL,
  last_error      VARCHAR(255) NULL,
  created_at      DATETIME(3)  NOT NULL,
  sent_at         DATETIME(3)  NULL,
  UNIQUE KEY uq_push_delivery (notification_id, subscription_id),
  KEY ix_push_delivery_due (status, next_attempt_at),
  CONSTRAINT fk_pdel_notif FOREIGN KEY (notification_id) REFERENCES notifications (id) ON DELETE CASCADE,
  CONSTRAINT fk_pdel_sub   FOREIGN KEY (subscription_id) REFERENCES push_subscriptions (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Preferensi per role: event apa yang diterima, lewat inbox saja atau inbox + push.
CREATE TABLE notification_preferences (
  role_id     BIGINT UNSIGNED NOT NULL,
  event_type  VARCHAR(40) NOT NULL,
  channel     ENUM('NONE','INBOX','PUSH') NOT NULL,
  updated_by  BIGINT UNSIGNED NULL,
  updated_at  DATETIME(3) NOT NULL,
  PRIMARY KEY (role_id, event_type),
  CONSTRAINT fk_npref_role FOREIGN KEY (role_id)    REFERENCES roles (id),
  CONSTRAINT fk_npref_user FOREIGN KEY (updated_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Eskalasi SLA kini menunjuk event outbox-nya. Eskalasi yang tercatat sebelum
-- PHASE 6 tidak dikirim ulang (sudah lewat).
ALTER TABLE sla_escalations
  ADD CONSTRAINT fk_sesc_outbox FOREIGN KEY (outbox_event_id) REFERENCES outbox_events (id) ON DELETE SET NULL;

-- Preferensi awal (dapat diubah Admin di Pengaturan → Notifikasi).
-- Admin IT tidak menangani tiket (Q6) sehingga awalnya tidak menerima notifikasi.
INSERT INTO notification_preferences (role_id, event_type, channel, updated_at)
SELECT r.id, p.ev, p.ch, UTC_TIMESTAMP(3) FROM (
            SELECT 'COORDINATOR' role, 'TICKET_CREATED' ev, 'INBOX' ch
  UNION ALL SELECT 'COORDINATOR', 'TICKET_CRITICAL', 'PUSH'
  UNION ALL SELECT 'COORDINATOR', 'TICKET_CLAIMED',  'INBOX'
  UNION ALL SELECT 'COORDINATOR', 'TICKET_STARTED',  'NONE'
  UNION ALL SELECT 'COORDINATOR', 'TICKET_PENDING',  'INBOX'
  UNION ALL SELECT 'COORDINATOR', 'TICKET_RESOLVED', 'INBOX'
  UNION ALL SELECT 'COORDINATOR', 'TICKET_REOPENED', 'PUSH'
  UNION ALL SELECT 'COORDINATOR', 'TICKET_ASSIGNED', 'PUSH'
  UNION ALL SELECT 'COORDINATOR', 'SLA_WARNING',     'INBOX'
  UNION ALL SELECT 'COORDINATOR', 'SLA_BREACH',      'PUSH'
  UNION ALL SELECT 'COORDINATOR', 'REPEAT_DAMAGE',   'PUSH'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_CREATED',  'PUSH'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_CRITICAL', 'PUSH'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_CLAIMED',  'NONE'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_STARTED',  'NONE'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_PENDING',  'INBOX'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_RESOLVED', 'INBOX'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_REOPENED', 'PUSH'
  UNION ALL SELECT 'TECHNICIAN',  'TICKET_ASSIGNED', 'PUSH'
  UNION ALL SELECT 'TECHNICIAN',  'SLA_WARNING',     'PUSH'
  UNION ALL SELECT 'TECHNICIAN',  'SLA_BREACH',      'PUSH'
  UNION ALL SELECT 'TECHNICIAN',  'REPEAT_DAMAGE',   'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_CREATED',  'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_CRITICAL', 'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_CLAIMED',  'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_STARTED',  'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_PENDING',  'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_RESOLVED', 'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_REOPENED', 'NONE'
  UNION ALL SELECT 'ADMIN',       'TICKET_ASSIGNED', 'NONE'
  UNION ALL SELECT 'ADMIN',       'SLA_WARNING',     'NONE'
  UNION ALL SELECT 'ADMIN',       'SLA_BREACH',      'NONE'
  UNION ALL SELECT 'ADMIN',       'REPEAT_DAMAGE',   'NONE'
) p JOIN roles r ON r.code = p.role;

-- +goose Down
ALTER TABLE sla_escalations DROP FOREIGN KEY fk_sesc_outbox;
ALTER TABLE sla_escalations DROP INDEX fk_sesc_outbox;
UPDATE sla_escalations SET outbox_event_id = NULL;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS push_deliveries;
DROP TABLE IF EXISTS push_subscriptions;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS outbox_events;
