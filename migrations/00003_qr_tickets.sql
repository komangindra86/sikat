-- PHASE 2: QR code, aturan prioritas, tiket, penugasan, histori, komentar, lampiran.
-- Kolom SLA di tickets sudah disiapkan (nullable); FK ke tabel SLA ditambahkan
-- di migrasi PHASE 5 saat tabel SLA dibuat. Kolom pemilik lampiran untuk
-- kegiatan & pemeliharaan disiapkan; FK-nya ditambahkan di PHASE 3/4.

-- +goose Up
CREATE TABLE qr_codes (
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  token            CHAR(22)     NOT NULL,
  type             ENUM('ROOM','ASSET') NOT NULL,
  room_id          BIGINT UNSIGNED NULL,
  asset_id         BIGINT UNSIGNED NULL,
  is_active        BOOLEAN      NOT NULL DEFAULT TRUE,
  generation       INT UNSIGNED NOT NULL DEFAULT 1,
  replaced_by_id   BIGINT UNSIGNED NULL,
  scan_count       INT UNSIGNED NOT NULL DEFAULT 0,
  last_scanned_at  DATETIME(3)  NULL,
  created_by       BIGINT UNSIGNED NULL,
  deactivated_by   BIGINT UNSIGNED NULL,
  deactivated_at   DATETIME(3)  NULL,
  created_at       DATETIME(3)  NOT NULL,
  updated_at       DATETIME(3)  NOT NULL,
  active_room_id   BIGINT UNSIGNED AS (IF(is_active AND type = 'ROOM',  room_id,  NULL)) STORED,
  active_asset_id  BIGINT UNSIGNED AS (IF(is_active AND type = 'ASSET', asset_id, NULL)) STORED,
  UNIQUE KEY uq_qr_token (token),
  UNIQUE KEY uq_qr_active_room (active_room_id),
  UNIQUE KEY uq_qr_active_asset (active_asset_id),
  KEY ix_qr_room (room_id),
  KEY ix_qr_asset (asset_id),
  CONSTRAINT chk_qr_target CHECK (
    (type = 'ROOM'  AND room_id  IS NOT NULL AND asset_id IS NULL) OR
    (type = 'ASSET' AND asset_id IS NOT NULL AND room_id  IS NULL)
  ),
  CONSTRAINT fk_qr_room     FOREIGN KEY (room_id)        REFERENCES rooms (id),
  CONSTRAINT fk_qr_asset    FOREIGN KEY (asset_id)       REFERENCES assets (id),
  CONSTRAINT fk_qr_replaced FOREIGN KEY (replaced_by_id) REFERENCES qr_codes (id),
  CONSTRAINT fk_qr_creator  FOREIGN KEY (created_by)     REFERENCES users (id),
  CONSTRAINT fk_qr_deact    FOREIGN KEY (deactivated_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE priority_rules (
  id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name               VARCHAR(120) NOT NULL,
  description        VARCHAR(255) NULL,
  sort_order         INT          NOT NULL,
  is_active          BOOLEAN      NOT NULL DEFAULT TRUE,
  ticket_category_id BIGINT UNSIGNED NULL,
  it_category_id     BIGINT UNSIGNED NULL,
  department_id      BIGINT UNSIGNED NULL,
  min_impact         TINYINT UNSIGNED NULL,
  max_impact         TINYINT UNSIGNED NULL,
  min_scope          TINYINT UNSIGNED NULL,
  max_scope          TINYINT UNSIGNED NULL,
  replacement_in     SET('YES','NO','UNKNOWN') NULL,
  critical_service   BOOLEAN NULL,
  result_priority    ENUM('LOW','MEDIUM','HIGH','CRITICAL') NOT NULL,
  created_by         BIGINT UNSIGNED NULL,
  updated_by         BIGINT UNSIGNED NULL,
  created_at         DATETIME(3) NOT NULL,
  updated_at         DATETIME(3) NOT NULL,
  KEY ix_priority_rules_order (is_active, sort_order),
  CONSTRAINT chk_prule_impact CHECK (min_impact IS NULL OR max_impact IS NULL OR min_impact <= max_impact),
  CONSTRAINT chk_prule_scope  CHECK (min_scope  IS NULL OR max_scope  IS NULL OR min_scope  <= max_scope),
  CONSTRAINT fk_prule_tcat FOREIGN KEY (ticket_category_id) REFERENCES ticket_categories (id),
  CONSTRAINT fk_prule_it   FOREIGN KEY (it_category_id)     REFERENCES it_categories (id),
  CONSTRAINT fk_prule_dept FOREIGN KEY (department_id)      REFERENCES departments (id),
  CONSTRAINT fk_prule_cby  FOREIGN KEY (created_by)         REFERENCES users (id),
  CONSTRAINT fk_prule_uby  FOREIGN KEY (updated_by)         REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE number_sequences (
  name          VARCHAR(10)       NOT NULL,
  year          SMALLINT UNSIGNED NOT NULL,
  current_value INT UNSIGNED      NOT NULL,
  PRIMARY KEY (name, year)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE tickets (
  id                     BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_no              VARCHAR(20)  NOT NULL,
  public_token_hash      BINARY(32)   NOT NULL,
  channel                ENUM('QR','PHONE','WALK_IN','MESSAGE') NOT NULL DEFAULT 'QR',
  created_by_user_id     BIGINT UNSIGNED NULL,
  qr_code_id             BIGINT UNSIGNED NULL,
  department_id          BIGINT UNSIGNED NULL,
  room_id                BIGINT UNSIGNED NULL,
  asset_id               BIGINT UNSIGNED NULL,
  reporter_name          VARCHAR(120) NOT NULL,
  reporter_department_id BIGINT UNSIGNED NULL,
  reporter_phone         VARCHAR(30)  NULL,
  ticket_category_id     BIGINT UNSIGNED NOT NULL,
  it_category_id         BIGINT UNSIGNED NOT NULL,
  description            TEXT         NOT NULL,
  impact_level           TINYINT UNSIGNED NOT NULL,
  affected_scope         TINYINT UNSIGNED NOT NULL,
  replacement_available  ENUM('YES','NO','UNKNOWN') NOT NULL DEFAULT 'UNKNOWN',
  critical_service       BOOLEAN      NOT NULL DEFAULT FALSE,
  incident_started_at    DATETIME(3)  NULL,
  priority               ENUM('LOW','MEDIUM','HIGH','CRITICAL') NOT NULL,
  priority_source        ENUM('AUTO','MANUAL') NOT NULL DEFAULT 'AUTO',
  priority_rule_id       BIGINT UNSIGNED NULL,
  status                 ENUM('NEW','CLAIMED','IN_PROGRESS','PENDING','RESOLVED','CLOSED','CANCELLED') NOT NULL DEFAULT 'NEW',
  assignee_id            BIGINT UNSIGNED NULL,
  pending_reason_id      BIGINT UNSIGNED NULL,
  pending_since          DATETIME(3)  NULL,
  sla_policy_id          BIGINT UNSIGNED NULL,
  sla_calendar_id        BIGINT UNSIGNED NULL,
  sla_response_minutes   INT UNSIGNED NULL,
  sla_resolution_minutes INT UNSIGNED NULL,
  sla_warn_pct           TINYINT UNSIGNED NULL,
  sla_allow_pause        BOOLEAN NOT NULL DEFAULT FALSE,
  sla_cycle              INT UNSIGNED NOT NULL DEFAULT 1,
  sla_paused_since       DATETIME(3)  NULL,
  sla_paused_minutes     INT UNSIGNED NOT NULL DEFAULT 0,
  response_due_at        DATETIME(3)  NULL,
  resolution_due_at      DATETIME(3)  NULL,
  response_breached_at   DATETIME(3)  NULL,
  resolution_breached_at DATETIME(3)  NULL,
  first_response_at      DATETIME(3)  NULL,
  work_started_at        DATETIME(3)  NULL,
  resolved_at            DATETIME(3)  NULL,
  closed_at              DATETIME(3)  NULL,
  cancelled_at           DATETIME(3)  NULL,
  service_restored_at    DATETIME(3)  NULL,
  resolution_action      TEXT NULL,
  resolution_result      ENUM('FIXED','WORKAROUND','REPLACED','VENDOR','NOT_REPRODUCIBLE','CANNOT_FIX') NULL,
  resolution_note        TEXT NULL,
  cancel_reason          VARCHAR(255) NULL,
  reporter_feedback      ENUM('OK','NOT_OK') NULL,
  reporter_feedback_at   DATETIME(3) NULL,
  version                INT UNSIGNED NOT NULL DEFAULT 1,
  created_at             DATETIME(3)  NOT NULL,
  updated_at             DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_tickets_no (ticket_no),
  UNIQUE KEY uq_tickets_token (public_token_hash),
  KEY ix_tickets_queue (status, priority, created_at),
  KEY ix_tickets_assignee (assignee_id, status),
  KEY ix_tickets_asset (asset_id, created_at),
  KEY ix_tickets_department (department_id, created_at),
  KEY ix_tickets_itcat (it_category_id, created_at),
  KEY ix_tickets_created (created_at),
  KEY ix_tickets_resolved (resolved_at),
  KEY ix_tickets_resp_due (status, response_due_at),
  KEY ix_tickets_reso_due (status, resolution_due_at),
  CONSTRAINT chk_tickets_impact CHECK (impact_level BETWEEN 0 AND 2),
  CONSTRAINT chk_tickets_scope  CHECK (affected_scope BETWEEN 1 AND 4),
  CONSTRAINT chk_tickets_resolved_fields CHECK (
    status NOT IN ('RESOLVED','CLOSED') OR (resolution_action IS NOT NULL AND resolution_result IS NOT NULL AND resolved_at IS NOT NULL)
  ),
  CONSTRAINT chk_tickets_pending  CHECK (status <> 'PENDING' OR pending_reason_id IS NOT NULL),
  CONSTRAINT chk_tickets_assignee CHECK (status IN ('NEW','CANCELLED') OR assignee_id IS NOT NULL),
  CONSTRAINT chk_tickets_channel  CHECK (channel = 'QR' OR created_by_user_id IS NOT NULL),
  CONSTRAINT fk_tickets_creator  FOREIGN KEY (created_by_user_id)     REFERENCES users (id),
  CONSTRAINT fk_tickets_qr       FOREIGN KEY (qr_code_id)             REFERENCES qr_codes (id),
  CONSTRAINT fk_tickets_dept     FOREIGN KEY (department_id)          REFERENCES departments (id),
  CONSTRAINT fk_tickets_room     FOREIGN KEY (room_id)                REFERENCES rooms (id),
  CONSTRAINT fk_tickets_asset    FOREIGN KEY (asset_id)               REFERENCES assets (id),
  CONSTRAINT fk_tickets_rdept    FOREIGN KEY (reporter_department_id) REFERENCES departments (id),
  CONSTRAINT fk_tickets_tcat     FOREIGN KEY (ticket_category_id)     REFERENCES ticket_categories (id),
  CONSTRAINT fk_tickets_itcat    FOREIGN KEY (it_category_id)         REFERENCES it_categories (id),
  CONSTRAINT fk_tickets_prule    FOREIGN KEY (priority_rule_id)       REFERENCES priority_rules (id),
  CONSTRAINT fk_tickets_assignee FOREIGN KEY (assignee_id)            REFERENCES users (id),
  CONSTRAINT fk_tickets_preason  FOREIGN KEY (pending_reason_id)      REFERENCES pending_reasons (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ticket_assignments (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id      BIGINT UNSIGNED NOT NULL,
  user_id        BIGINT UNSIGNED NOT NULL,
  role           ENUM('PRIMARY','SUPPORT') NOT NULL,
  assigned_by    BIGINT UNSIGNED NULL,
  assigned_at    DATETIME(3) NOT NULL,
  released_at    DATETIME(3) NULL,
  released_by    BIGINT UNSIGNED NULL,
  release_reason VARCHAR(255) NULL,
  active_primary_ticket_id BIGINT UNSIGNED AS (IF(role = 'PRIMARY' AND released_at IS NULL, ticket_id, NULL)) STORED,
  active_member_key        VARCHAR(45)     AS (IF(released_at IS NULL, CONCAT(ticket_id, ':', user_id), NULL)) STORED,
  UNIQUE KEY uq_tassign_active_primary (active_primary_ticket_id),
  UNIQUE KEY uq_tassign_active_member (active_member_key),
  KEY ix_tassign_ticket (ticket_id, assigned_at),
  KEY ix_tassign_user (user_id, released_at),
  CONSTRAINT fk_tassign_ticket FOREIGN KEY (ticket_id)   REFERENCES tickets (id),
  CONSTRAINT fk_tassign_user   FOREIGN KEY (user_id)     REFERENCES users (id),
  CONSTRAINT fk_tassign_by     FOREIGN KEY (assigned_by) REFERENCES users (id),
  CONSTRAINT fk_tassign_rel    FOREIGN KEY (released_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ticket_histories (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id     BIGINT UNSIGNED NOT NULL,
  event_type    VARCHAR(40)  NOT NULL,
  from_status   VARCHAR(20)  NULL,
  to_status     VARCHAR(20)  NULL,
  from_priority VARCHAR(10)  NULL,
  to_priority   VARCHAR(10)  NULL,
  actor_type    ENUM('USER','REPORTER','SYSTEM') NOT NULL,
  actor_user_id BIGINT UNSIGNED NULL,
  note          TEXT NULL,
  meta          JSON NULL,
  created_at    DATETIME(3) NOT NULL,
  KEY ix_thist_ticket (ticket_id, created_at),
  KEY ix_thist_actor (actor_user_id, created_at),
  CONSTRAINT chk_thist_actor CHECK (actor_type <> 'USER' OR actor_user_id IS NOT NULL),
  CONSTRAINT fk_thist_ticket FOREIGN KEY (ticket_id)     REFERENCES tickets (id),
  CONSTRAINT fk_thist_actor  FOREIGN KEY (actor_user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ticket_comments (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id      BIGINT UNSIGNED NOT NULL,
  author_type    ENUM('USER','REPORTER') NOT NULL,
  author_user_id BIGINT UNSIGNED NULL,
  body           TEXT NOT NULL,
  is_internal    BOOLEAN NOT NULL DEFAULT FALSE,
  created_at     DATETIME(3) NOT NULL,
  KEY ix_tcomment_ticket (ticket_id, created_at),
  CONSTRAINT chk_tcomment_author CHECK (author_type <> 'USER' OR author_user_id IS NOT NULL),
  CONSTRAINT fk_tcomment_ticket FOREIGN KEY (ticket_id)      REFERENCES tickets (id),
  CONSTRAINT fk_tcomment_user   FOREIGN KEY (author_user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE attachments (
  id                     BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id              BIGINT UNSIGNED NULL,
  activity_id            BIGINT UNSIGNED NULL,
  maintenance_history_id BIGINT UNSIGNED NULL,
  uploaded_by_type       ENUM('USER','REPORTER') NOT NULL,
  uploaded_by_user_id    BIGINT UNSIGNED NULL,
  storage_key            VARCHAR(255) NOT NULL,
  original_name          VARCHAR(255) NULL,
  mime_type              VARCHAR(50)  NOT NULL,
  size_bytes             INT UNSIGNED NOT NULL,
  width                  INT UNSIGNED NULL,
  height                 INT UNSIGNED NULL,
  sha256                 BINARY(32)   NOT NULL,
  visible_to_reporter    BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at             DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_attachments_key (storage_key),
  KEY ix_attachments_ticket (ticket_id),
  KEY ix_attachments_activity (activity_id),
  KEY ix_attachments_mh (maintenance_history_id),
  CONSTRAINT chk_attachments_owner CHECK (
    (ticket_id IS NOT NULL) + (activity_id IS NOT NULL) + (maintenance_history_id IS NOT NULL) = 1
  ),
  CONSTRAINT chk_attachments_mime CHECK (mime_type IN ('image/jpeg','image/png','image/webp')),
  CONSTRAINT chk_attachments_uploader CHECK (uploaded_by_type <> 'USER' OR uploaded_by_user_id IS NOT NULL),
  CONSTRAINT fk_att_ticket FOREIGN KEY (ticket_id)           REFERENCES tickets (id),
  CONSTRAINT fk_att_user   FOREIGN KEY (uploaded_by_user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose StatementBegin
CREATE TRIGGER trg_ticket_histories_no_update BEFORE UPDATE ON ticket_histories
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket_histories bersifat append-only';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ticket_histories_no_delete BEFORE DELETE ON ticket_histories
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket_histories bersifat append-only';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_tickets_no_delete BEFORE DELETE ON tickets
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tiket tidak boleh dihapus; gunakan status CANCELLED';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ticket_comments_no_update BEFORE UPDATE ON ticket_comments
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'komentar tidak dapat diubah';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ticket_comments_no_delete BEFORE DELETE ON ticket_comments
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'komentar tidak dapat dihapus';
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS ticket_comments;
DROP TABLE IF EXISTS ticket_histories;
DROP TABLE IF EXISTS ticket_assignments;
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS number_sequences;
DROP TABLE IF EXISTS priority_rules;
DROP TABLE IF EXISTS qr_codes;
