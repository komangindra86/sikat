-- PHASE 4: riwayat pemeliharaan perangkat, sparepart terpakai, peringatan kerusakan berulang.
-- Riwayat tidak pernah dihapus; koreksi dilakukan dengan menandai entri "dibatalkan"
-- (voided_at + alasan) sehingga statistik mengecualikannya tanpa kehilangan jejak.

-- +goose Up
CREATE TABLE asset_alerts (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  asset_id        BIGINT UNSIGNED NOT NULL,
  status          ENUM('OPEN','ACKNOWLEDGED','CLOSED') NOT NULL DEFAULT 'OPEN',
  ticket_count    INT UNSIGNED NOT NULL,
  window_days     INT UNSIGNED NOT NULL,
  threshold       INT UNSIGNED NOT NULL,
  last_ticket_id  BIGINT UNSIGNED NULL,
  recommendation  ENUM('EVALUATE','INSPECT','CONSIDER_REPLACEMENT') NOT NULL,
  reasons         VARCHAR(500) NULL,
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
  ticket_cycle         INT UNSIGNED NULL,
  activity_id          BIGINT UNSIGNED NULL,
  asset_alert_id       BIGINT UNSIGNED NULL,
  ticket_category_id   BIGINT UNSIGNED NULL,
  performed_by_user_id BIGINT UNSIGNED NULL,
  complaint            TEXT NULL,
  action_taken         TEXT NOT NULL,
  result               ENUM('FIXED','WORKAROUND','REPLACED','VENDOR','NOT_REPRODUCIBLE','CANNOT_FIX','DONE') NOT NULL,
  problem_started_at   DATETIME(3) NULL,
  started_at           DATETIME(3) NULL,
  finished_at          DATETIME(3) NOT NULL,
  repair_minutes       INT AS (IF(started_at IS NOT NULL, TIMESTAMPDIFF(MINUTE, started_at, finished_at), NULL)) STORED,
  downtime_minutes     INT AS (IF(problem_started_at IS NOT NULL, TIMESTAMPDIFF(MINUTE, problem_started_at, finished_at), NULL)) STORED,
  service_cost         DECIMAL(14,2) NULL,
  asset_status_after   ENUM('ACTIVE','BROKEN','IN_REPAIR','UNUSED','DISPOSED') NULL,
  notes                TEXT NULL,
  voided_at            DATETIME(3) NULL,
  voided_by            BIGINT UNSIGNED NULL,
  void_reason          VARCHAR(255) NULL,
  created_by           BIGINT UNSIGNED NULL,
  created_at           DATETIME(3) NOT NULL,
  updated_at           DATETIME(3) NOT NULL,
  ticket_cycle_key     VARCHAR(45) AS (IF(ticket_id IS NOT NULL, CONCAT(ticket_id, ':', asset_id, ':', IFNULL(ticket_cycle, 0)), NULL)) STORED,
  UNIQUE KEY uq_amh_ticket_cycle (ticket_cycle_key),
  UNIQUE KEY uq_amh_activity (activity_id),
  KEY ix_amh_asset (asset_id, finished_at),
  KEY ix_amh_ticket (ticket_id),
  CONSTRAINT chk_amh_source CHECK (
    (source = 'TICKET'   AND ticket_id      IS NOT NULL) OR
    (source = 'ACTIVITY' AND activity_id    IS NOT NULL) OR
    (source = 'ALERT'    AND asset_alert_id IS NOT NULL) OR
    (source = 'MANUAL')
  ),
  CONSTRAINT chk_amh_time CHECK (started_at IS NULL OR started_at <= finished_at),
  CONSTRAINT chk_amh_problem CHECK (problem_started_at IS NULL OR problem_started_at <= finished_at),
  CONSTRAINT chk_amh_cost CHECK (service_cost IS NULL OR service_cost >= 0),
  CONSTRAINT chk_amh_void CHECK (voided_at IS NULL OR void_reason IS NOT NULL),
  CONSTRAINT fk_amh_asset    FOREIGN KEY (asset_id)             REFERENCES assets (id),
  CONSTRAINT fk_amh_ticket   FOREIGN KEY (ticket_id)            REFERENCES tickets (id),
  CONSTRAINT fk_amh_activity FOREIGN KEY (activity_id)          REFERENCES internal_activities (id),
  CONSTRAINT fk_amh_alert    FOREIGN KEY (asset_alert_id)       REFERENCES asset_alerts (id),
  CONSTRAINT fk_amh_tcat     FOREIGN KEY (ticket_category_id)   REFERENCES ticket_categories (id),
  CONSTRAINT fk_amh_user     FOREIGN KEY (performed_by_user_id) REFERENCES users (id),
  CONSTRAINT fk_amh_voider   FOREIGN KEY (voided_by)            REFERENCES users (id),
  CONSTRAINT fk_amh_creator  FOREIGN KEY (created_by)           REFERENCES users (id)
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
  CONSTRAINT chk_mparts_cost CHECK (unit_cost IS NULL OR unit_cost >= 0),
  CONSTRAINT fk_mparts_history FOREIGN KEY (maintenance_history_id) REFERENCES asset_maintenance_histories (id),
  CONSTRAINT fk_mparts_part    FOREIGN KEY (spare_part_id)          REFERENCES spare_parts (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE attachments
  ADD CONSTRAINT fk_att_mh FOREIGN KEY (maintenance_history_id) REFERENCES asset_maintenance_histories (id);

ALTER TABLE internal_activities
  ADD COLUMN maintenance_type ENUM('CORRECTIVE','PREVENTIVE','INSPECTION','INSTALLATION') NULL
    COMMENT 'jenis entri riwayat perangkat bila kegiatan terkait perangkat' AFTER asset_id;

-- +goose StatementBegin
CREATE TRIGGER trg_amh_no_delete BEFORE DELETE ON asset_maintenance_histories
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'riwayat pemeliharaan tidak boleh dihapus; gunakan pembatalan (void)';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_mparts_no_delete BEFORE DELETE ON maintenance_parts
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'sparepart terpakai tidak boleh dihapus';
-- +goose StatementEnd

INSERT INTO system_settings (setting_key, value, value_type, description, updated_at) VALUES
  ('repeat_damage.threshold',         '3',       'INT', 'Jumlah gangguan dalam jendela waktu yang memicu peringatan', UTC_TIMESTAMP(3)),
  ('repeat_damage.window_days',       '90',      'INT', 'Jendela waktu deteksi kerusakan berulang (hari)', UTC_TIMESTAMP(3)),
  ('repeat_damage.inspect_threshold', '5',       'INT', 'Jumlah gangguan untuk rekomendasi pemeriksaan lanjutan', UTC_TIMESTAMP(3)),
  ('repeat_damage.replace_age_years', '5',       'INT', 'Umur perangkat (tahun) untuk rekomendasi pertimbangan penggantian', UTC_TIMESTAMP(3)),
  ('repeat_damage.replace_cost',      '3000000', 'INT', 'Total biaya perbaikan (Rp) untuk rekomendasi pertimbangan penggantian', UTC_TIMESTAMP(3));

-- +goose Down
DELETE FROM system_settings WHERE setting_key LIKE 'repeat_damage.%';
DROP TRIGGER IF EXISTS trg_mparts_no_delete;
DROP TRIGGER IF EXISTS trg_amh_no_delete;
ALTER TABLE internal_activities DROP COLUMN maintenance_type;
ALTER TABLE attachments DROP FOREIGN KEY fk_att_mh;
DROP TABLE IF EXISTS maintenance_parts;
DROP TABLE IF EXISTS asset_maintenance_histories;
DROP TABLE IF EXISTS asset_alerts;
