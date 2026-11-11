-- PHASE 3: kegiatan internal IT, anggota kegiatan, FK lampiran kegiatan,
-- pengaturan tutup otomatis tiket.

-- +goose Up
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
  KEY ix_activities_itcat (it_category_id, activity_date),
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

ALTER TABLE attachments
  ADD CONSTRAINT fk_att_activity FOREIGN KEY (activity_id) REFERENCES internal_activities (id);

INSERT INTO system_settings (setting_key, value, value_type, description, updated_at) VALUES
  ('ticket.auto_close_days', '3', 'INT', 'Tiket Selesai ditutup otomatis bila pelapor tidak merespons (hari)', UTC_TIMESTAMP(3));

-- +goose Down
DELETE FROM system_settings WHERE setting_key = 'ticket.auto_close_days';
ALTER TABLE attachments DROP FOREIGN KEY fk_att_activity;
DROP TABLE IF EXISTS activity_members;
DROP TABLE IF EXISTS internal_activities;
