-- Master pegawai dihapus: nama pegawai (saran nama pelapor saat petugas mencatat
-- tiket) kini diambil langsung dari API pegawai SIMRS, sama dengan aplikasi CSSD,
-- dan tidak disimpan di SIKAT. Tidak ada tabel lain yang merujuk ke employees.
--   1. Jejak integrasi pegawai (pemetaan ID, impor CSV, log sinkron, konfigurasi) dihapus
--      dan nilai EMPLOYEE dibuang dari kolom entity_type.
--   2. Tabel employees dihapus.
-- Down membuat ulang tabel employees (kosong) dan konfigurasi integrasinya (LOCAL);
-- isi pegawai dan jejak integrasinya tidak dikembalikan.

-- +goose Up
DELETE FROM external_id_mappings WHERE entity_type = 'EMPLOYEE';
UPDATE integration_imports SET log_id = NULL
WHERE log_id IN (SELECT id FROM (SELECT id FROM integration_sync_logs WHERE entity_type = 'EMPLOYEE') l);
DELETE FROM integration_imports WHERE entity_type = 'EMPLOYEE';
DELETE FROM integration_sync_logs WHERE entity_type = 'EMPLOYEE';
UPDATE integration_sync_logs SET config_id = NULL
WHERE config_id IN (SELECT id FROM (SELECT id FROM integration_configs WHERE entity_type = 'EMPLOYEE') c);
DELETE FROM integration_configs WHERE entity_type = 'EMPLOYEE';

ALTER TABLE integration_configs   MODIFY entity_type ENUM('DEPARTMENT','ROOM','ASSET') NOT NULL;
ALTER TABLE integration_sync_logs MODIFY entity_type ENUM('DEPARTMENT','ROOM','ASSET') NOT NULL;
ALTER TABLE external_id_mappings  MODIFY entity_type ENUM('DEPARTMENT','ROOM','ASSET') NOT NULL;
ALTER TABLE integration_imports   MODIFY entity_type ENUM('DEPARTMENT','ROOM','ASSET') NOT NULL;

DROP TABLE employees;

-- +goose Down
ALTER TABLE integration_configs   MODIFY entity_type ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL;
ALTER TABLE integration_sync_logs MODIFY entity_type ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL;
ALTER TABLE external_id_mappings  MODIFY entity_type ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL;
ALTER TABLE integration_imports   MODIFY entity_type ENUM('DEPARTMENT','ROOM','EMPLOYEE','ASSET') NOT NULL;

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

INSERT INTO integration_configs (entity_type, provider, created_at, updated_at)
VALUES ('EMPLOYEE', 'LOCAL', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));
