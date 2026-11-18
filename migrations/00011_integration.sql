-- PHASE 8: integrasi SIMRS siap pakai (tanpa ketergantungan pada SIMRS).
-- Master data tetap lokal; sumber luar (CSV kini, SIMRS API/DB setelah
-- spesifikasinya tersedia) dipetakan lewat external_id, tanpa FK ke SIMRS.

-- +goose Up

-- Direktori pegawai rumah sakit (bukan akun login). Dipakai untuk saran nama
-- pelapor saat petugas mencatat tiket; dapat diisi manual, CSV, atau SIMRS.
CREATE TABLE employees (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  nip               VARCHAR(30)  NOT NULL,
  full_name         VARCHAR(150) NOT NULL,
  department_id     BIGINT UNSIGNED NULL,
  position          VARCHAR(100) NULL,
  phone             VARCHAR(30)  NULL,
  is_active         BOOLEAN      NOT NULL DEFAULT TRUE,
  source            ENUM('LOCAL','SIMRS','CSV') NOT NULL DEFAULT 'LOCAL',
  local_modified_at DATETIME(3)  NULL,
  last_synced_at    DATETIME(3)  NULL,
  created_at        DATETIME(3)  NOT NULL,
  updated_at        DATETIME(3)  NOT NULL,
  UNIQUE KEY uq_employees_nip (nip),
  KEY ix_employees_name (full_name),
  KEY ix_employees_dept (department_id, is_active),
  CONSTRAINT fk_employees_department FOREIGN KEY (department_id) REFERENCES departments (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Konfigurasi sumber data per entitas. LOCAL = dikelola di aplikasi (default).
CREATE TABLE integration_configs (
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  entity_type      ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL,
  provider         ENUM('LOCAL','CSV','SIMRS_API','SIMRS_DB') NOT NULL DEFAULT 'LOCAL',
  is_enabled       BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'sinkron terjadwal (provider SIMRS)',
  schedule_minutes INT UNSIGNED NULL,
  settings         JSON NULL COMMENT 'non-rahasia: url, nama view, pemetaan kolom',
  conflict_policy  ENUM('KEEP_LOCAL','PREFER_SOURCE') NOT NULL DEFAULT 'KEEP_LOCAL',
  max_retries      TINYINT UNSIGNED NOT NULL DEFAULT 3,
  retry_count      TINYINT UNSIGNED NOT NULL DEFAULT 0,
  next_retry_at    DATETIME(3) NULL,
  last_run_at      DATETIME(3) NULL,
  last_success_at  DATETIME(3) NULL,
  updated_by       BIGINT UNSIGNED NULL,
  created_at       DATETIME(3) NOT NULL,
  updated_at       DATETIME(3) NOT NULL,
  UNIQUE KEY uq_integration_entity (entity_type),
  CONSTRAINT fk_intcfg_user FOREIGN KEY (updated_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Setiap sinkronisasi/impor tercatat: hitungan dan rincian konflik/error.
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
  unchanged     INT UNSIGNED NOT NULL DEFAULT 0,
  conflicts     INT UNSIGNED NOT NULL DEFAULT 0,
  failed        INT UNSIGNED NOT NULL DEFAULT 0,
  deactivated   INT UNSIGNED NOT NULL DEFAULT 0,
  error_message TEXT NULL,
  details       JSON NULL COMMENT 'daftar konflik/error (dibatasi)',
  file_name     VARCHAR(200) NULL,
  triggered_by  BIGINT UNSIGNED NULL,
  KEY ix_sync_logs_entity (entity_type, started_at),
  KEY ix_sync_logs_started (started_at),
  CONSTRAINT fk_synclog_cfg  FOREIGN KEY (config_id)    REFERENCES integration_configs (id),
  CONSTRAINT fk_synclog_user FOREIGN KEY (triggered_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Pemetaan ID sumber luar → ID lokal. source_hash mendeteksi perubahan di sumber.
CREATE TABLE external_id_mappings (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  source         VARCHAR(30) NOT NULL COMMENT 'SIMRS, CSV',
  entity_type    ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL,
  external_id    VARCHAR(100) NOT NULL,
  local_id       BIGINT UNSIGNED NOT NULL,
  source_hash    BINARY(32) NULL,
  last_synced_at DATETIME(3) NOT NULL,
  missing_since  DATETIME(3) NULL COMMENT 'tidak lagi ada di sumber',
  created_at     DATETIME(3) NOT NULL,
  UNIQUE KEY uq_extmap_external (source, entity_type, external_id),
  UNIQUE KEY uq_extmap_local (source, entity_type, local_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- File CSV yang diunggah, menunggu pratinjau → terapkan. Isi dihapus setelah
-- diterapkan/dibatalkan atau setelah 7 hari.
CREATE TABLE integration_imports (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  entity_type     ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL,
  file_name       VARCHAR(200) NOT NULL,
  content         MEDIUMBLOB NULL,
  mark_missing    BOOLEAN NOT NULL DEFAULT FALSE,
  conflict_policy ENUM('KEEP_LOCAL','PREFER_SOURCE') NOT NULL DEFAULT 'KEEP_LOCAL',
  status          ENUM('PREVIEW','APPLIED','CANCELLED') NOT NULL DEFAULT 'PREVIEW',
  log_id          BIGINT UNSIGNED NULL,
  uploaded_by     BIGINT UNSIGNED NOT NULL,
  created_at      DATETIME(3) NOT NULL,
  applied_at      DATETIME(3) NULL,
  KEY ix_imports_created (created_at),
  CONSTRAINT fk_imports_user FOREIGN KEY (uploaded_by) REFERENCES users (id),
  CONSTRAINT fk_imports_log  FOREIGN KEY (log_id)      REFERENCES integration_sync_logs (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO integration_configs (entity_type, provider, created_at, updated_at) VALUES
  ('DEPARTMENT', 'LOCAL', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('ROOM',       'LOCAL', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('EMPLOYEE',   'LOCAL', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('ASSET',      'LOCAL', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));

-- +goose Down
DROP TABLE IF EXISTS integration_imports;
DROP TABLE IF EXISTS external_id_mappings;
DROP TABLE IF EXISTS integration_sync_logs;
DROP TABLE IF EXISTS integration_configs;
DROP TABLE IF EXISTS employees;
