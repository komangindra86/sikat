-- =====================================================================
-- SIKAT — DRAFT SKEMA DATABASE (PHASE 0)
-- Tujuan file ini: memvalidasi rancangan di MySQL 8 sungguhan.
-- BUKAN migrasi final. Migrasi goose dibuat di PHASE 1 berdasarkan file ini.
-- Semua DATETIME disimpan dalam UTC. Koneksi wajib time_zone = '+00:00'.
-- Membutuhkan MySQL >= 8.0.16 (CHECK constraint ditegakkan).
-- =====================================================================

SET NAMES utf8mb4;
SET time_zone = '+00:00';

-- ---------------------------------------------------------------------
-- 1. ORGANISASI & PENGGUNA
-- ---------------------------------------------------------------------

CREATE TABLE roles (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code          VARCHAR(30)  NOT NULL,
  name          VARCHAR(100) NOT NULL,
  description   VARCHAR(255) NULL,
  is_system     BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at    DATETIME(3)  NOT NULL,
  updated_at    DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_roles_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE departments (
  id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code                VARCHAR(30)  NOT NULL,
  name                VARCHAR(150) NOT NULL,
  is_critical_service BOOLEAN      NOT NULL DEFAULT FALSE,
  is_active           BOOLEAN      NOT NULL DEFAULT TRUE,
  source              ENUM('LOCAL','SIMRS','CSV') NOT NULL DEFAULT 'LOCAL',
  local_modified_at   DATETIME(3)  NULL,
  last_synced_at      DATETIME(3)  NULL,
  created_at          DATETIME(3)  NOT NULL,
  updated_at          DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_departments_code (code),
  UNIQUE KEY uq_departments_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE rooms (
  id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  department_id       BIGINT UNSIGNED NOT NULL,
  code                VARCHAR(30)  NOT NULL,
  name                VARCHAR(150) NOT NULL,
  location_note       VARCHAR(150) NULL COMMENT 'gedung/lantai',
  is_critical_service BOOLEAN      NULL COMMENT 'NULL = ikut unit',
  is_active           BOOLEAN      NOT NULL DEFAULT TRUE,
  source              ENUM('LOCAL','SIMRS','CSV') NOT NULL DEFAULT 'LOCAL',
  local_modified_at   DATETIME(3)  NULL,
  last_synced_at      DATETIME(3)  NULL,
  created_at          DATETIME(3)  NOT NULL,
  updated_at          DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_rooms_code (code),
  UNIQUE KEY uq_rooms_dept_name (department_id, name),
  CONSTRAINT fk_rooms_department FOREIGN KEY (department_id) REFERENCES departments (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE users (
  id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  role_id              BIGINT UNSIGNED NOT NULL,
  department_id        BIGINT UNSIGNED NULL,
  username             VARCHAR(50)  NOT NULL,
  full_name            VARCHAR(150) NOT NULL,
  nip                  VARCHAR(30)  NULL,
  phone                VARCHAR(30)  NULL,
  email                VARCHAR(150) NULL,
  password_hash        VARCHAR(255) NOT NULL,
  must_change_password BOOLEAN      NOT NULL DEFAULT TRUE,
  is_active            BOOLEAN      NOT NULL DEFAULT TRUE,
  failed_login_count   INT UNSIGNED NOT NULL DEFAULT 0,
  locked_until         DATETIME(3)  NULL,
  last_login_at        DATETIME(3)  NULL,
  source               ENUM('LOCAL','SIMRS','CSV') NOT NULL DEFAULT 'LOCAL',
  local_modified_at    DATETIME(3)  NULL,
  last_synced_at       DATETIME(3)  NULL,
  created_at           DATETIME(3)  NOT NULL,
  updated_at           DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_users_username (username),
  UNIQUE KEY uq_users_nip (nip),
  KEY ix_users_role (role_id, is_active),
  CONSTRAINT fk_users_role       FOREIGN KEY (role_id)       REFERENCES roles (id),
  CONSTRAINT fk_users_department FOREIGN KEY (department_id) REFERENCES departments (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 2. KATALOG (bidang, jenis gangguan, template, alasan tunda)
-- ---------------------------------------------------------------------

CREATE TABLE it_categories (            -- "Bidang": Hardware, Software & SIMRS, Jaringan, ...
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code        VARCHAR(30)  NOT NULL,
  name        VARCHAR(100) NOT NULL,
  description VARCHAR(255) NULL,
  sort_order  INT          NOT NULL DEFAULT 0,
  is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_it_categories_code (code),
  UNIQUE KEY uq_it_categories_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE user_competencies (
  user_id        BIGINT UNSIGNED NOT NULL,
  it_category_id BIGINT UNSIGNED NOT NULL,
  created_at     DATETIME(3) NOT NULL,
  PRIMARY KEY (user_id, it_category_id),
  KEY ix_user_competencies_cat (it_category_id),
  CONSTRAINT fk_ucomp_user FOREIGN KEY (user_id)        REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_ucomp_cat  FOREIGN KEY (it_category_id) REFERENCES it_categories (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ticket_categories (        -- "Jenis gangguan" dalam bahasa awam
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  it_category_id   BIGINT UNSIGNED NOT NULL,
  name             VARCHAR(120) NOT NULL,
  help_text        VARCHAR(255) NULL,
  icon             VARCHAR(50)  NULL,
  default_priority ENUM('LOW','MEDIUM','HIGH','CRITICAL') NOT NULL DEFAULT 'MEDIUM',
  sort_order       INT          NOT NULL DEFAULT 0,
  is_active        BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at       DATETIME(3)  NOT NULL,
  updated_at       DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_ticket_categories_name (name),
  KEY ix_ticket_categories_it (it_category_id),
  CONSTRAINT fk_tcat_it FOREIGN KEY (it_category_id) REFERENCES it_categories (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE action_templates (
  id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  it_category_id     BIGINT UNSIGNED NULL,
  ticket_category_id BIGINT UNSIGNED NULL,
  title              VARCHAR(120) NOT NULL,
  body               TEXT         NOT NULL,
  sort_order         INT          NOT NULL DEFAULT 0,
  is_active          BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at         DATETIME(3)  NOT NULL,
  updated_at         DATETIME(3)  NOT NULL,
  CONSTRAINT fk_atpl_it   FOREIGN KEY (it_category_id)     REFERENCES it_categories (id),
  CONSTRAINT fk_atpl_tcat FOREIGN KEY (ticket_category_id) REFERENCES ticket_categories (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE pending_reasons (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code        VARCHAR(30)  NOT NULL,
  label       VARCHAR(120) NOT NULL,
  pauses_sla  BOOLEAN      NOT NULL DEFAULT FALSE COMMENT 'hanya berlaku bila sla_policies.allow_pause',
  sort_order  INT          NOT NULL DEFAULT 0,
  is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_pending_reasons_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE activity_types (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name           VARCHAR(100) NOT NULL,
  it_category_id BIGINT UNSIGNED NULL COMMENT 'bidang default',
  sort_order     INT         NOT NULL DEFAULT 0,
  is_active      BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at     DATETIME(3) NOT NULL,
  updated_at     DATETIME(3) NOT NULL,
  UNIQUE KEY uq_activity_types_name (name),
  CONSTRAINT fk_atype_it FOREIGN KEY (it_category_id) REFERENCES it_categories (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 3. PERANGKAT & QR
-- ---------------------------------------------------------------------

CREATE TABLE asset_categories (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code           VARCHAR(30)  NOT NULL,
  name           VARCHAR(100) NOT NULL,
  it_category_id BIGINT UNSIGNED NULL,
  is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at     DATETIME(3)  NOT NULL,
  updated_at     DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_asset_categories_code (code),
  UNIQUE KEY uq_asset_categories_name (name),
  CONSTRAINT fk_acat_it FOREIGN KEY (it_category_id) REFERENCES it_categories (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE assets (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  asset_code        VARCHAR(50)  NOT NULL,
  name              VARCHAR(150) NOT NULL,
  asset_category_id BIGINT UNSIGNED NOT NULL,
  brand             VARCHAR(80)  NULL,
  model             VARCHAR(80)  NULL,
  serial_number     VARCHAR(100) NULL,
  department_id     BIGINT UNSIGNED NULL,
  room_id           BIGINT UNSIGNED NULL,
  procured_at       DATE         NULL,
  status            ENUM('ACTIVE','BROKEN','IN_REPAIR','UNUSED','DISPOSED') NOT NULL DEFAULT 'ACTIVE',
  disposed_at       DATETIME(3)  NULL,
  notes             TEXT         NULL,
  source            ENUM('LOCAL','SIMRS','CSV') NOT NULL DEFAULT 'LOCAL',
  local_modified_at DATETIME(3)  NULL,
  last_synced_at    DATETIME(3)  NULL,
  created_at        DATETIME(3)  NOT NULL,
  updated_at        DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_assets_code (asset_code),
  KEY ix_assets_serial (serial_number),
  KEY ix_assets_room (room_id),
  KEY ix_assets_department (department_id, status),
  KEY ix_assets_category (asset_category_id),
  CONSTRAINT fk_assets_category   FOREIGN KEY (asset_category_id) REFERENCES asset_categories (id),
  CONSTRAINT fk_assets_department FOREIGN KEY (department_id)     REFERENCES departments (id),
  CONSTRAINT fk_assets_room       FOREIGN KEY (room_id)           REFERENCES rooms (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE qr_codes (
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  token            CHAR(22)     NOT NULL COMMENT 'base62 dari 16 byte crypto/rand',
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
  -- hanya satu QR aktif per ruangan / per perangkat
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

-- ---------------------------------------------------------------------
-- 4. SLA & PRIORITAS (dibuat sebelum tickets karena direferensikan)
-- ---------------------------------------------------------------------

CREATE TABLE sla_calendars (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name        VARCHAR(100) NOT NULL,
  timezone    VARCHAR(40)  NOT NULL DEFAULT 'Asia/Makassar',
  is_24x7     BOOLEAN      NOT NULL DEFAULT FALSE,
  is_default  BOOLEAN      NOT NULL DEFAULT FALSE,
  created_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL,
  default_flag TINYINT AS (IF(is_default, 1, NULL)) STORED,
  UNIQUE KEY uq_sla_calendars_name (name),
  UNIQUE KEY uq_sla_calendars_default (default_flag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sla_calendar_hours (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  calendar_id BIGINT UNSIGNED NOT NULL,
  weekday     TINYINT UNSIGNED NOT NULL COMMENT '1=Senin .. 7=Minggu',
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL,
  UNIQUE KEY uq_sla_hours (calendar_id, weekday, start_time),
  CONSTRAINT chk_sla_hours_weekday CHECK (weekday BETWEEN 1 AND 7),
  CONSTRAINT chk_sla_hours_range   CHECK (start_time < end_time),
  CONSTRAINT fk_sla_hours_cal FOREIGN KEY (calendar_id) REFERENCES sla_calendars (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sla_holidays (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  calendar_id  BIGINT UNSIGNED NOT NULL,
  holiday_date DATE         NOT NULL,
  name         VARCHAR(120) NOT NULL,
  created_at   DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_sla_holiday (calendar_id, holiday_date),
  CONSTRAINT fk_sla_holiday_cal FOREIGN KEY (calendar_id) REFERENCES sla_calendars (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sla_policies (
  id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name               VARCHAR(120) NOT NULL,
  priority           ENUM('LOW','MEDIUM','HIGH','CRITICAL') NOT NULL,
  ticket_category_id BIGINT UNSIGNED NULL,
  it_category_id     BIGINT UNSIGNED NULL,
  department_id      BIGINT UNSIGNED NULL,
  calendar_id        BIGINT UNSIGNED NOT NULL,
  response_minutes   INT UNSIGNED NOT NULL,
  resolution_minutes INT UNSIGNED NOT NULL,
  warn_pct           TINYINT UNSIGNED NOT NULL DEFAULT 75,
  allow_pause        BOOLEAN NOT NULL DEFAULT FALSE,
  is_active          BOOLEAN NOT NULL DEFAULT TRUE,
  created_at         DATETIME(3) NOT NULL,
  updated_at         DATETIME(3) NOT NULL,
  -- satu kebijakan aktif per kombinasi pencocokan
  match_key VARCHAR(80) AS (IF(is_active, CONCAT_WS(':', priority,
              IFNULL(ticket_category_id, 0), IFNULL(it_category_id, 0), IFNULL(department_id, 0)), NULL)) STORED,
  UNIQUE KEY uq_sla_policy_match (match_key),
  KEY ix_sla_policy_priority (priority, is_active),
  CONSTRAINT chk_sla_policy_minutes CHECK (response_minutes > 0 AND resolution_minutes >= response_minutes),
  CONSTRAINT chk_sla_policy_warn    CHECK (warn_pct BETWEEN 1 AND 99),
  CONSTRAINT fk_slap_tcat FOREIGN KEY (ticket_category_id) REFERENCES ticket_categories (id),
  CONSTRAINT fk_slap_it   FOREIGN KEY (it_category_id)     REFERENCES it_categories (id),
  CONSTRAINT fk_slap_dept FOREIGN KEY (department_id)      REFERENCES departments (id),
  CONSTRAINT fk_slap_cal  FOREIGN KEY (calendar_id)        REFERENCES sla_calendars (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE priority_rules (
  id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name               VARCHAR(120) NOT NULL,
  description        VARCHAR(255) NULL,
  sort_order         INT          NOT NULL,
  is_active          BOOLEAN      NOT NULL DEFAULT TRUE,
  -- kondisi; NULL = apa saja
  ticket_category_id BIGINT UNSIGNED NULL,
  it_category_id     BIGINT UNSIGNED NULL,
  department_id      BIGINT UNSIGNED NULL,
  min_impact         TINYINT UNSIGNED NULL COMMENT '0 tidak mengganggu,1 sebagian,2 berhenti',
  max_impact         TINYINT UNSIGNED NULL,
  min_scope          TINYINT UNSIGNED NULL COMMENT '1 sendiri,2 beberapa,3 satu unit,4 >1 unit',
  max_scope          TINYINT UNSIGNED NULL,
  replacement_in     SET('YES','NO','UNKNOWN') NULL,
  critical_service   BOOLEAN NULL,
  -- hasil
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

-- ---------------------------------------------------------------------
-- 5. TIKET
-- ---------------------------------------------------------------------

CREATE TABLE number_sequences (
  name       VARCHAR(10)       NOT NULL,
  year       SMALLINT UNSIGNED NOT NULL,
  current_value INT UNSIGNED   NOT NULL,
  PRIMARY KEY (name, year)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE tickets (
  id                     BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_no              VARCHAR(20)  NOT NULL,
  public_token_hash      BINARY(32)   NOT NULL COMMENT 'SHA-256 token pelapor',
  channel                ENUM('QR','PHONE','WALK_IN','MESSAGE') NOT NULL DEFAULT 'QR',
  created_by_user_id     BIGINT UNSIGNED NULL COMMENT 'diisi bila dicatat petugas atas nama pelapor',
  qr_code_id             BIGINT UNSIGNED NULL,
  -- lokasi & perangkat (snapshot saat pengajuan)
  department_id          BIGINT UNSIGNED NULL,
  room_id                BIGINT UNSIGNED NULL,
  asset_id               BIGINT UNSIGNED NULL,
  -- pelapor (tanpa data pasien)
  reporter_name          VARCHAR(120) NOT NULL,
  reporter_department_id BIGINT UNSIGNED NULL,
  reporter_phone         VARCHAR(30)  NULL,
  -- masalah
  ticket_category_id     BIGINT UNSIGNED NOT NULL,
  it_category_id         BIGINT UNSIGNED NOT NULL COMMENT 'bidang, disalin dari kategori',
  description            TEXT         NOT NULL,
  impact_level           TINYINT UNSIGNED NOT NULL,
  affected_scope         TINYINT UNSIGNED NOT NULL,
  replacement_available  ENUM('YES','NO','UNKNOWN') NOT NULL DEFAULT 'UNKNOWN',
  critical_service       BOOLEAN      NOT NULL DEFAULT FALSE COMMENT 'snapshot flag unit/ruangan',
  incident_started_at    DATETIME(3)  NULL COMMENT 'perkiraan awal gangguan dari pelapor',
  -- prioritas
  priority               ENUM('LOW','MEDIUM','HIGH','CRITICAL') NOT NULL,
  priority_source        ENUM('AUTO','MANUAL') NOT NULL DEFAULT 'AUTO',
  priority_rule_id       BIGINT UNSIGNED NULL,
  -- status & penugasan
  status                 ENUM('NEW','CLAIMED','IN_PROGRESS','PENDING','RESOLVED','CLOSED','CANCELLED') NOT NULL DEFAULT 'NEW',
  assignee_id            BIGINT UNSIGNED NULL COMMENT 'denormalisasi PJ utama aktif',
  pending_reason_id      BIGINT UNSIGNED NULL,
  pending_since          DATETIME(3)  NULL,
  -- SLA (disalin dari kebijakan saat dibuat / saat prioritas berubah)
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
  -- waktu penting
  first_response_at      DATETIME(3)  NULL,
  work_started_at        DATETIME(3)  NULL,
  resolved_at            DATETIME(3)  NULL,
  closed_at              DATETIME(3)  NULL,
  cancelled_at           DATETIME(3)  NULL,
  service_restored_at    DATETIME(3)  NULL,
  -- penyelesaian
  resolution_action      TEXT NULL,
  resolution_result      ENUM('FIXED','WORKAROUND','REPLACED','VENDOR','NOT_REPRODUCIBLE','CANNOT_FIX') NULL,
  resolution_note        TEXT NULL,
  cancel_reason          VARCHAR(255) NULL,
  -- konfirmasi pelapor
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
  CONSTRAINT chk_tickets_pending CHECK (status <> 'PENDING' OR pending_reason_id IS NOT NULL),
  CONSTRAINT chk_tickets_assignee CHECK (status IN ('NEW','CANCELLED') OR assignee_id IS NOT NULL),
  CONSTRAINT fk_tickets_creator   FOREIGN KEY (created_by_user_id)     REFERENCES users (id),
  CONSTRAINT fk_tickets_qr        FOREIGN KEY (qr_code_id)             REFERENCES qr_codes (id),
  CONSTRAINT fk_tickets_dept      FOREIGN KEY (department_id)          REFERENCES departments (id),
  CONSTRAINT fk_tickets_room      FOREIGN KEY (room_id)                REFERENCES rooms (id),
  CONSTRAINT fk_tickets_asset     FOREIGN KEY (asset_id)               REFERENCES assets (id),
  CONSTRAINT fk_tickets_rdept     FOREIGN KEY (reporter_department_id) REFERENCES departments (id),
  CONSTRAINT fk_tickets_tcat      FOREIGN KEY (ticket_category_id)     REFERENCES ticket_categories (id),
  CONSTRAINT fk_tickets_itcat     FOREIGN KEY (it_category_id)         REFERENCES it_categories (id),
  CONSTRAINT fk_tickets_prule     FOREIGN KEY (priority_rule_id)       REFERENCES priority_rules (id),
  CONSTRAINT fk_tickets_assignee  FOREIGN KEY (assignee_id)            REFERENCES users (id),
  CONSTRAINT fk_tickets_preason   FOREIGN KEY (pending_reason_id)      REFERENCES pending_reasons (id),
  CONSTRAINT fk_tickets_slapol    FOREIGN KEY (sla_policy_id)          REFERENCES sla_policies (id),
  CONSTRAINT fk_tickets_slacal    FOREIGN KEY (sla_calendar_id)        REFERENCES sla_calendars (id)
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
  -- pertahanan DB: maks satu PJ utama aktif per tiket; user tidak dobel aktif di tiket yang sama
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
  event_type    VARCHAR(40)  NOT NULL COMMENT 'CREATED, STATUS_CHANGED, CLAIMED, TAKEN_OVER, RELEASED, SUPPORT_ADDED, PRIORITY_CHANGED, SLA_WARNING, SLA_BREACH, REOPENED, REPORTER_CONFIRMED, ...',
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
  is_internal    BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'catatan internal tidak tampil ke pelapor',
  created_at     DATETIME(3) NOT NULL,
  KEY ix_tcomment_ticket (ticket_id, created_at),
  CONSTRAINT chk_tcomment_author CHECK (author_type <> 'USER' OR author_user_id IS NOT NULL),
  CONSTRAINT fk_tcomment_ticket FOREIGN KEY (ticket_id)      REFERENCES tickets (id),
  CONSTRAINT fk_tcomment_user   FOREIGN KEY (author_user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 6. KEGIATAN INTERNAL
-- ---------------------------------------------------------------------

CREATE TABLE internal_activities (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  activity_no       VARCHAR(20)  NOT NULL,
  activity_date     DATE         NOT NULL COMMENT 'tanggal WITA',
  activity_type_id  BIGINT UNSIGNED NOT NULL,
  it_category_id    BIGINT UNSIGNED NOT NULL COMMENT 'bidang',
  department_id     BIGINT UNSIGNED NULL,
  room_id           BIGINT UNSIGNED NULL,
  asset_id          BIGINT UNSIGNED NULL,
  related_ticket_id BIGINT UNSIGNED NULL,
  title             VARCHAR(200) NOT NULL,
  description       TEXT NULL,
  result            TEXT NULL,
  status            ENUM('PLANNED','IN_PROGRESS','DONE','CANCELLED') NOT NULL DEFAULT 'DONE',
  started_at        DATETIME(3)  NULL,
  ended_at          DATETIME(3)  NULL,
  duration_minutes  INT AS (IF(started_at IS NOT NULL AND ended_at IS NOT NULL,
                               TIMESTAMPDIFF(MINUTE, started_at, ended_at), NULL)) STORED,
  created_by        BIGINT UNSIGNED NOT NULL,
  created_at        DATETIME(3)  NOT NULL,
  updated_at        DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_activities_no (activity_no),
  KEY ix_activities_date (activity_date),
  KEY ix_activities_asset (asset_id, activity_date),
  KEY ix_activities_dept (department_id, activity_date),
  CONSTRAINT chk_activities_time CHECK (ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at),
  CONSTRAINT chk_activities_done CHECK (status <> 'DONE' OR result IS NOT NULL),
  CONSTRAINT fk_act_type    FOREIGN KEY (activity_type_id)  REFERENCES activity_types (id),
  CONSTRAINT fk_act_it      FOREIGN KEY (it_category_id)    REFERENCES it_categories (id),
  CONSTRAINT fk_act_dept    FOREIGN KEY (department_id)     REFERENCES departments (id),
  CONSTRAINT fk_act_room    FOREIGN KEY (room_id)           REFERENCES rooms (id),
  CONSTRAINT fk_act_asset   FOREIGN KEY (asset_id)          REFERENCES assets (id),
  CONSTRAINT fk_act_ticket  FOREIGN KEY (related_ticket_id) REFERENCES tickets (id),
  CONSTRAINT fk_act_creator FOREIGN KEY (created_by)        REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE activity_members (
  activity_id BIGINT UNSIGNED NOT NULL,
  user_id     BIGINT UNSIGNED NOT NULL,
  role        ENUM('LEAD','SUPPORT') NOT NULL,
  created_at  DATETIME(3) NOT NULL,
  lead_activity_id BIGINT UNSIGNED AS (IF(role = 'LEAD', activity_id, NULL)) STORED,
  PRIMARY KEY (activity_id, user_id),
  UNIQUE KEY uq_activity_single_lead (lead_activity_id),
  KEY ix_activity_members_user (user_id),
  CONSTRAINT fk_amem_activity FOREIGN KEY (activity_id) REFERENCES internal_activities (id),
  CONSTRAINT fk_amem_user     FOREIGN KEY (user_id)     REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 7. PEMELIHARAAN PERANGKAT
-- ---------------------------------------------------------------------

CREATE TABLE asset_alerts (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  asset_id        BIGINT UNSIGNED NOT NULL,
  status          ENUM('OPEN','ACKNOWLEDGED','CLOSED') NOT NULL DEFAULT 'OPEN',
  ticket_count    INT UNSIGNED NOT NULL,
  window_days     INT UNSIGNED NOT NULL,
  threshold       INT UNSIGNED NOT NULL,
  last_ticket_id  BIGINT UNSIGNED NULL,
  recommendation  ENUM('EVALUATE','INSPECT','CONSIDER_REPLACEMENT') NOT NULL,
  decision        ENUM('NO_ACTION','INSPECT','REPAIR','REPLACEMENT_PROPOSED','OTHER') NULL,
  decision_note   TEXT NULL,
  decided_by      BIGINT UNSIGNED NULL,
  decided_at      DATETIME(3) NULL,
  created_at      DATETIME(3) NOT NULL,
  updated_at      DATETIME(3) NOT NULL,
  open_asset_id   BIGINT UNSIGNED AS (IF(status <> 'CLOSED', asset_id, NULL)) STORED,
  UNIQUE KEY uq_asset_alert_open (open_asset_id),
  KEY ix_asset_alerts_asset (asset_id, created_at),
  KEY ix_asset_alerts_status (status, created_at),
  CONSTRAINT chk_asset_alert_closed CHECK (status <> 'CLOSED' OR decision IS NOT NULL),
  CONSTRAINT fk_aalert_asset  FOREIGN KEY (asset_id)       REFERENCES assets (id),
  CONSTRAINT fk_aalert_ticket FOREIGN KEY (last_ticket_id) REFERENCES tickets (id),
  CONSTRAINT fk_aalert_user   FOREIGN KEY (decided_by)     REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE asset_maintenance_histories (
  id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  asset_id             BIGINT UNSIGNED NOT NULL,
  maintenance_type     ENUM('CORRECTIVE','PREVENTIVE','INSPECTION','INSTALLATION','RELOCATION','ALERT_DECISION') NOT NULL,
  source               ENUM('TICKET','ACTIVITY','MANUAL','ALERT') NOT NULL,
  ticket_id            BIGINT UNSIGNED NULL,
  ticket_cycle         INT UNSIGNED NULL COMMENT 'sla_cycle tiket saat diselesaikan (tiket dibuka ulang = siklus baru)',
  activity_id          BIGINT UNSIGNED NULL,
  asset_alert_id       BIGINT UNSIGNED NULL,
  ticket_category_id   BIGINT UNSIGNED NULL COMMENT 'jenis kerusakan',
  performed_by_user_id BIGINT UNSIGNED NULL,
  complaint            TEXT NULL,
  action_taken         TEXT NOT NULL,
  result               ENUM('FIXED','WORKAROUND','REPLACED','VENDOR','NOT_REPRODUCIBLE','CANNOT_FIX','DONE') NOT NULL,
  problem_started_at   DATETIME(3) NULL,
  started_at           DATETIME(3) NULL,
  finished_at          DATETIME(3) NOT NULL,
  repair_minutes       INT AS (IF(started_at IS NOT NULL, TIMESTAMPDIFF(MINUTE, started_at, finished_at), NULL)) STORED,
  downtime_minutes     INT AS (IF(problem_started_at IS NOT NULL, TIMESTAMPDIFF(MINUTE, problem_started_at, finished_at), NULL)) STORED,
  service_cost         DECIMAL(14,2) NULL COMMENT 'jasa/vendor; sparepart di maintenance_parts',
  asset_status_after   ENUM('ACTIVE','BROKEN','IN_REPAIR','UNUSED','DISPOSED') NULL,
  notes                TEXT NULL,
  created_by           BIGINT UNSIGNED NULL,
  created_at           DATETIME(3) NOT NULL,
  updated_at           DATETIME(3) NOT NULL,
  ticket_cycle_key     VARCHAR(45) AS (IF(ticket_id IS NOT NULL, CONCAT(ticket_id, ':', asset_id, ':', IFNULL(ticket_cycle, 0)), NULL)) STORED,
  UNIQUE KEY uq_amh_ticket_cycle (ticket_cycle_key),
  KEY ix_amh_asset (asset_id, finished_at),
  KEY ix_amh_ticket (ticket_id),
  KEY ix_amh_activity (activity_id),
  CONSTRAINT chk_amh_source CHECK (
    (source = 'TICKET'   AND ticket_id      IS NOT NULL) OR
    (source = 'ACTIVITY' AND activity_id    IS NOT NULL) OR
    (source = 'ALERT'    AND asset_alert_id IS NOT NULL) OR
    (source = 'MANUAL')
  ),
  CONSTRAINT chk_amh_time CHECK (started_at IS NULL OR started_at <= finished_at),
  CONSTRAINT fk_amh_asset    FOREIGN KEY (asset_id)             REFERENCES assets (id),
  CONSTRAINT fk_amh_ticket   FOREIGN KEY (ticket_id)            REFERENCES tickets (id),
  CONSTRAINT fk_amh_activity FOREIGN KEY (activity_id)          REFERENCES internal_activities (id),
  CONSTRAINT fk_amh_alert    FOREIGN KEY (asset_alert_id)       REFERENCES asset_alerts (id),
  CONSTRAINT fk_amh_tcat     FOREIGN KEY (ticket_category_id)   REFERENCES ticket_categories (id),
  CONSTRAINT fk_amh_user     FOREIGN KEY (performed_by_user_id) REFERENCES users (id),
  CONSTRAINT fk_amh_creator  FOREIGN KEY (created_by)           REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE spare_parts (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code              VARCHAR(50)  NOT NULL,
  name              VARCHAR(150) NOT NULL,
  unit              VARCHAR(20)  NOT NULL DEFAULT 'pcs',
  default_unit_cost DECIMAL(14,2) NULL,
  is_active         BOOLEAN NOT NULL DEFAULT TRUE,
  created_at        DATETIME(3) NOT NULL,
  updated_at        DATETIME(3) NOT NULL,
  UNIQUE KEY uq_spare_parts_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE maintenance_parts (
  id                     BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  maintenance_history_id BIGINT UNSIGNED NOT NULL,
  spare_part_id          BIGINT UNSIGNED NOT NULL,
  quantity               DECIMAL(10,2) NOT NULL,
  unit_cost              DECIMAL(14,2) NULL,
  line_cost              DECIMAL(16,2) AS (IF(unit_cost IS NULL, NULL, quantity * unit_cost)) STORED,
  notes                  VARCHAR(255) NULL,
  created_at             DATETIME(3) NOT NULL,
  KEY ix_mparts_history (maintenance_history_id),
  KEY ix_mparts_part (spare_part_id),
  CONSTRAINT chk_mparts_qty CHECK (quantity > 0),
  CONSTRAINT fk_mparts_history FOREIGN KEY (maintenance_history_id) REFERENCES asset_maintenance_histories (id),
  CONSTRAINT fk_mparts_part    FOREIGN KEY (spare_part_id)          REFERENCES spare_parts (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 8. LAMPIRAN (dibuat setelah pemiliknya)
-- ---------------------------------------------------------------------

CREATE TABLE attachments (
  id                     BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id              BIGINT UNSIGNED NULL,
  activity_id            BIGINT UNSIGNED NULL,
  maintenance_history_id BIGINT UNSIGNED NULL,
  uploaded_by_type       ENUM('USER','REPORTER') NOT NULL,
  uploaded_by_user_id    BIGINT UNSIGNED NULL,
  storage_key            VARCHAR(255) NOT NULL COMMENT 'path relatif acak di luar webroot',
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
  CONSTRAINT fk_att_ticket   FOREIGN KEY (ticket_id)              REFERENCES tickets (id),
  CONSTRAINT fk_att_activity FOREIGN KEY (activity_id)            REFERENCES internal_activities (id),
  CONSTRAINT fk_att_mh       FOREIGN KEY (maintenance_history_id) REFERENCES asset_maintenance_histories (id),
  CONSTRAINT fk_att_user     FOREIGN KEY (uploaded_by_user_id)    REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 9. NOTIFIKASI
-- ---------------------------------------------------------------------

CREATE TABLE outbox_events (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  event_type      VARCHAR(50)  NOT NULL COMMENT 'ticket.created, ticket.claimed, sla.breached, asset.repeat_damage, ...',
  aggregate_type  VARCHAR(30)  NOT NULL,
  aggregate_id    BIGINT UNSIGNED NOT NULL,
  payload         JSON         NOT NULL,
  dedupe_key      VARCHAR(150) NOT NULL,
  status          ENUM('PENDING','PROCESSING','DONE','FAILED') NOT NULL DEFAULT 'PENDING',
  attempts        INT UNSIGNED NOT NULL DEFAULT 0,
  next_attempt_at DATETIME(3)  NOT NULL,
  locked_until    DATETIME(3)  NULL,
  last_error      TEXT NULL,
  created_at      DATETIME(3)  NOT NULL,
  processed_at    DATETIME(3)  NULL,
  UNIQUE KEY uq_outbox_dedupe (dedupe_key),
  KEY ix_outbox_pending (status, next_attempt_at),
  KEY ix_outbox_aggregate (aggregate_type, aggregate_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sla_escalations (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id       BIGINT UNSIGNED NOT NULL,
  kind            ENUM('CRITICAL_IMMEDIATE','RESPONSE_WARNING','RESPONSE_BREACH','RESOLUTION_WARNING','RESOLUTION_BREACH') NOT NULL,
  cycle           INT UNSIGNED NOT NULL,
  level           TINYINT UNSIGNED NOT NULL DEFAULT 1,
  due_at          DATETIME(3) NULL,
  used_pct        SMALLINT UNSIGNED NULL,
  triggered_at    DATETIME(3) NOT NULL,
  outbox_event_id BIGINT UNSIGNED NULL,
  acknowledged_by BIGINT UNSIGNED NULL,
  acknowledged_at DATETIME(3) NULL,
  UNIQUE KEY uq_sla_escalation (ticket_id, kind, cycle, level),
  KEY ix_sla_escalation_time (triggered_at),
  CONSTRAINT fk_sesc_ticket FOREIGN KEY (ticket_id)       REFERENCES tickets (id),
  CONSTRAINT fk_sesc_event  FOREIGN KEY (outbox_event_id) REFERENCES outbox_events (id),
  CONSTRAINT fk_sesc_ack    FOREIGN KEY (acknowledged_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE notifications (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id         BIGINT UNSIGNED NOT NULL,
  event_id        BIGINT UNSIGNED NULL,
  type            VARCHAR(50)  NOT NULL,
  title           VARCHAR(150) NOT NULL,
  body            VARCHAR(500) NOT NULL COMMENT 'hanya tampil di dalam aplikasi (setelah login)',
  link_url        VARCHAR(255) NULL,
  severity        ENUM('INFO','WARNING','CRITICAL') NOT NULL DEFAULT 'INFO',
  push_status     ENUM('NONE','PENDING','SENT','FAILED') NOT NULL DEFAULT 'NONE',
  push_attempts   TINYINT UNSIGNED NOT NULL DEFAULT 0,
  push_last_error VARCHAR(255) NULL,
  read_at         DATETIME(3) NULL,
  created_at      DATETIME(3) NOT NULL,
  UNIQUE KEY uq_notifications_user_event (user_id, event_id),
  KEY ix_notifications_inbox (user_id, read_at, created_at),
  KEY ix_notifications_push (push_status, created_at),
  CONSTRAINT fk_notif_user  FOREIGN KEY (user_id)  REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_notif_event FOREIGN KEY (event_id) REFERENCES outbox_events (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE push_subscriptions (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id         BIGINT UNSIGNED NOT NULL,
  endpoint        TEXT         NOT NULL,
  endpoint_hash   BINARY(32)   NOT NULL,
  p256dh          VARCHAR(255) NOT NULL,
  auth_secret     VARCHAR(255) NOT NULL,
  user_agent      VARCHAR(255) NULL,
  device_label    VARCHAR(100) NULL,
  failure_count   INT UNSIGNED NOT NULL DEFAULT 0,
  last_success_at DATETIME(3)  NULL,
  disabled_at     DATETIME(3)  NULL,
  created_at      DATETIME(3)  NOT NULL,
  updated_at      DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_push_endpoint (endpoint_hash),
  KEY ix_push_user (user_id, disabled_at),
  CONSTRAINT fk_push_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE notification_preferences (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  role_id      BIGINT UNSIGNED NOT NULL,
  event_type   VARCHAR(50) NOT NULL,
  scope        ENUM('ALL','ASSIGNED','COMPETENCY') NOT NULL DEFAULT 'ALL' COMMENT 'ALL=semua tiket, ASSIGNED=tiket yg ditangani, COMPETENCY=sesuai bidang',
  inbox        BOOLEAN NOT NULL DEFAULT TRUE,
  push         BOOLEAN NOT NULL DEFAULT FALSE,
  updated_by   BIGINT UNSIGNED NULL,
  updated_at   DATETIME(3) NOT NULL,
  UNIQUE KEY uq_notif_pref (role_id, event_type),
  CONSTRAINT fk_npref_role FOREIGN KEY (role_id)    REFERENCES roles (id),
  CONSTRAINT fk_npref_user FOREIGN KEY (updated_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 10. INTEGRASI
-- ---------------------------------------------------------------------

CREATE TABLE integration_configs (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  entity_type       ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL,
  provider          ENUM('LOCAL','CSV','SIMRS_API','SIMRS_DB') NOT NULL DEFAULT 'LOCAL',
  is_enabled        BOOLEAN NOT NULL DEFAULT FALSE,
  schedule_minutes  INT UNSIGNED NULL,
  settings          JSON NULL COMMENT 'non-rahasia: url, nama tabel/view, pemetaan kolom',
  secret_ciphertext VARBINARY(1024) NULL COMMENT 'AES-GCM, kunci dari environment',
  conflict_policy   ENUM('KEEP_LOCAL','PREFER_SOURCE') NOT NULL DEFAULT 'KEEP_LOCAL',
  max_retries       TINYINT UNSIGNED NOT NULL DEFAULT 3,
  last_run_at       DATETIME(3) NULL,
  last_success_at   DATETIME(3) NULL,
  updated_by        BIGINT UNSIGNED NULL,
  created_at        DATETIME(3) NOT NULL,
  updated_at        DATETIME(3) NOT NULL,
  UNIQUE KEY uq_integration_entity (entity_type),
  CONSTRAINT fk_intcfg_user FOREIGN KEY (updated_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE integration_sync_logs (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  config_id     BIGINT UNSIGNED NULL,
  entity_type   ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL,
  provider      ENUM('LOCAL','CSV','SIMRS_API','SIMRS_DB') NOT NULL,
  trigger_type  ENUM('SCHEDULE','MANUAL','RETRY') NOT NULL,
  attempt       TINYINT UNSIGNED NOT NULL DEFAULT 1,
  status        ENUM('RUNNING','SUCCESS','PARTIAL','FAILED') NOT NULL,
  started_at    DATETIME(3) NOT NULL,
  finished_at   DATETIME(3) NULL,
  total_count   INT UNSIGNED NOT NULL DEFAULT 0,
  inserted      INT UNSIGNED NOT NULL DEFAULT 0,
  updated       INT UNSIGNED NOT NULL DEFAULT 0,
  skipped       INT UNSIGNED NOT NULL DEFAULT 0,
  conflicts     INT UNSIGNED NOT NULL DEFAULT 0,
  failed        INT UNSIGNED NOT NULL DEFAULT 0,
  error_message TEXT NULL,
  details       JSON NULL COMMENT 'daftar konflik/error (dibatasi)',
  triggered_by  BIGINT UNSIGNED NULL,
  KEY ix_sync_logs_entity (entity_type, started_at),
  CONSTRAINT fk_synclog_cfg  FOREIGN KEY (config_id)    REFERENCES integration_configs (id),
  CONSTRAINT fk_synclog_user FOREIGN KEY (triggered_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE external_id_mappings (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  source         VARCHAR(30) NOT NULL COMMENT 'SIMRS, CSV',
  entity_type    ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL,
  external_id    VARCHAR(100) NOT NULL,
  local_id       BIGINT UNSIGNED NOT NULL,
  source_hash    BINARY(32) NULL COMMENT 'hash payload sumber terakhir, untuk deteksi perubahan',
  last_synced_at DATETIME(3) NOT NULL,
  missing_since  DATETIME(3) NULL COMMENT 'tidak lagi ada di sumber',
  created_at     DATETIME(3) NOT NULL,
  UNIQUE KEY uq_extmap_external (source, entity_type, external_id),
  UNIQUE KEY uq_extmap_local (source, entity_type, local_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 11. SISTEM
-- ---------------------------------------------------------------------

CREATE TABLE audit_logs (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  actor_type    ENUM('USER','REPORTER','SYSTEM') NOT NULL,
  actor_user_id BIGINT UNSIGNED NULL,
  action        VARCHAR(60) NOT NULL COMMENT 'login.success, login.failed, asset.update, sla_policy.update, ...',
  entity_type   VARCHAR(40) NULL,
  entity_id     BIGINT UNSIGNED NULL,
  before_data   JSON NULL,
  after_data    JSON NULL,
  ip_address    VARCHAR(45) NULL,
  user_agent    VARCHAR(255) NULL,
  request_id    VARCHAR(40) NULL,
  created_at    DATETIME(3) NOT NULL,
  KEY ix_audit_entity (entity_type, entity_id),
  KEY ix_audit_actor (actor_user_id, created_at),
  KEY ix_audit_created (created_at),
  CONSTRAINT fk_audit_user FOREIGN KEY (actor_user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE system_settings (
  setting_key VARCHAR(100) NOT NULL PRIMARY KEY,
  value       TEXT NOT NULL,
  value_type  ENUM('STRING','INT','BOOL','JSON') NOT NULL,
  description VARCHAR(255) NULL,
  updated_by  BIGINT UNSIGNED NULL,
  updated_at  DATETIME(3) NOT NULL,
  CONSTRAINT fk_settings_user FOREIGN KEY (updated_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sessions (                 -- skema scs mysqlstore
  token  CHAR(43)     NOT NULL PRIMARY KEY,
  data   BLOB         NOT NULL,
  expiry TIMESTAMP(6) NOT NULL,
  KEY sessions_expiry_idx (expiry)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE backup_logs (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  trigger_type      ENUM('SCHEDULE','MANUAL') NOT NULL,
  status            ENUM('RUNNING','SUCCESS','FAILED') NOT NULL,
  started_at        DATETIME(3) NOT NULL,
  finished_at       DATETIME(3) NULL,
  file_name         VARCHAR(255) NULL,
  size_bytes        BIGINT UNSIGNED NULL,
  sha256            BINARY(32) NULL,
  offsite_copied_at DATETIME(3) NULL,
  restore_tested_at DATETIME(3) NULL,
  error_message     TEXT NULL,
  KEY ix_backup_logs_started (started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------
-- 12. TRIGGER: histori tidak boleh diubah/dihapus
-- ---------------------------------------------------------------------

CREATE TRIGGER trg_ticket_histories_no_update BEFORE UPDATE ON ticket_histories
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket_histories bersifat append-only';

CREATE TRIGGER trg_ticket_histories_no_delete BEFORE DELETE ON ticket_histories
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket_histories bersifat append-only';

CREATE TRIGGER trg_audit_logs_no_update BEFORE UPDATE ON audit_logs
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_logs bersifat append-only';

CREATE TRIGGER trg_audit_logs_no_delete BEFORE DELETE ON audit_logs
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_logs bersifat append-only';

CREATE TRIGGER trg_amh_no_delete BEFORE DELETE ON asset_maintenance_histories
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'riwayat pemeliharaan tidak boleh dihapus';

CREATE TRIGGER trg_tickets_no_delete BEFORE DELETE ON tickets
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tiket tidak boleh dihapus; gunakan status CANCELLED';

CREATE TRIGGER trg_ticket_comments_no_update BEFORE UPDATE ON ticket_comments
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'komentar tidak dapat diubah';

CREATE TRIGGER trg_ticket_comments_no_delete BEFORE DELETE ON ticket_comments
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'komentar tidak dapat dihapus';
