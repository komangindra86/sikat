-- PHASE 1: organisasi, katalog, perangkat, sistem, audit.
-- Sumber rancangan: docs/schema-draft.sql. Semua DATETIME dalam UTC.

-- +goose Up
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
  location_note       VARCHAR(150) NULL,
  is_critical_service BOOLEAN      NULL,
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
  password_changed_at  DATETIME(3)  NULL,
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

CREATE TABLE it_categories (
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

CREATE TABLE ticket_categories (
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
  pauses_sla  BOOLEAN      NOT NULL DEFAULT FALSE,
  sort_order  INT          NOT NULL DEFAULT 0,
  is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_pending_reasons_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE activity_types (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name           VARCHAR(100) NOT NULL,
  it_category_id BIGINT UNSIGNED NULL,
  sort_order     INT         NOT NULL DEFAULT 0,
  is_active      BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at     DATETIME(3) NOT NULL,
  updated_at     DATETIME(3) NOT NULL,
  UNIQUE KEY uq_activity_types_name (name),
  CONSTRAINT fk_atype_it FOREIGN KEY (it_category_id) REFERENCES it_categories (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

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

CREATE TABLE spare_parts (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code              VARCHAR(50)  NOT NULL,
  name              VARCHAR(150) NOT NULL,
  unit              VARCHAR(20)  NOT NULL DEFAULT 'pcs',
  default_unit_cost DECIMAL(14,2) NULL,
  is_active         BOOLEAN NOT NULL DEFAULT TRUE,
  created_at        DATETIME(3) NOT NULL,
  updated_at        DATETIME(3) NOT NULL,
  UNIQUE KEY uq_spare_parts_code (code),
  CONSTRAINT chk_spare_parts_cost CHECK (default_unit_cost IS NULL OR default_unit_cost >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE audit_logs (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  actor_type    ENUM('USER','REPORTER','SYSTEM') NOT NULL,
  actor_user_id BIGINT UNSIGNED NULL,
  action        VARCHAR(60) NOT NULL,
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
  KEY ix_audit_action (action, created_at),
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

CREATE TABLE sessions (
  token  CHAR(43)     NOT NULL PRIMARY KEY,
  data   BLOB         NOT NULL,
  expiry TIMESTAMP(6) NOT NULL,
  KEY sessions_expiry_idx (expiry)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose StatementBegin
CREATE TRIGGER trg_audit_logs_no_update BEFORE UPDATE ON audit_logs
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_logs bersifat append-only';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_audit_logs_no_delete BEFORE DELETE ON audit_logs
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_logs bersifat append-only';
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS system_settings;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS spare_parts;
DROP TABLE IF EXISTS assets;
DROP TABLE IF EXISTS asset_categories;
DROP TABLE IF EXISTS activity_types;
DROP TABLE IF EXISTS pending_reasons;
DROP TABLE IF EXISTS action_templates;
DROP TABLE IF EXISTS ticket_categories;
DROP TABLE IF EXISTS user_competencies;
DROP TABLE IF EXISTS it_categories;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS departments;
DROP TABLE IF EXISTS roles;
