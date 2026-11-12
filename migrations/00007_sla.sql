-- PHASE 5: SLA & eskalasi.
-- Kalender kerja (jam operasional + hari libur), kebijakan SLA yang dapat diubah admin,
-- dan catatan eskalasi idempoten. Nilai kebijakan disalin ke tiket saat dibuat
-- sehingga perubahan kebijakan tidak mengubah histori tiket lama.
--
-- Angka di bawah hanya KONFIGURASI AWAL (Q2, disetujui 12/11/2026 — docs/IMPLEMENTATION_PLAN.md);
-- admin dapat mengubahnya di Master Data → Kebijakan SLA dan SLA → Kalender kerja.

-- +goose Up
CREATE TABLE sla_calendars (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name         VARCHAR(100) NOT NULL,
  timezone     VARCHAR(40)  NOT NULL DEFAULT 'Asia/Makassar',
  is_24x7      BOOLEAN      NOT NULL DEFAULT FALSE,
  is_default   BOOLEAN      NOT NULL DEFAULT FALSE,
  created_at   DATETIME(3)  NOT NULL,
  updated_at   DATETIME(3)  NOT NULL,
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
  KEY ix_sla_holiday_date (holiday_date),
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

-- Buku besar eskalasi: satu baris per kejadian (tiket, jenis, siklus, revisi SLA).
-- UNIQUE mencegah kejadian yang sama tercatat/dinotifikasi dua kali.
-- notify = FALSE untuk pelanggaran yang baru terdeteksi saat tiket diambil/diselesaikan
-- (sudah lewat, tidak perlu notifikasi). outbox_event_id diisi PHASE 6.
CREATE TABLE sla_escalations (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id       BIGINT UNSIGNED NOT NULL,
  kind            ENUM('CRITICAL_IMMEDIATE','RESPONSE_WARNING','RESPONSE_BREACH','RESOLUTION_WARNING','RESOLUTION_BREACH') NOT NULL,
  cycle           INT UNSIGNED NOT NULL,
  level           TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT 'tickets.sla_revision saat kejadian',
  priority        ENUM('LOW','MEDIUM','HIGH','CRITICAL') NOT NULL,
  due_at          DATETIME(3) NULL,
  used_pct        SMALLINT UNSIGNED NULL,
  notify          BOOLEAN NOT NULL DEFAULT TRUE,
  triggered_at    DATETIME(3) NOT NULL,
  outbox_event_id BIGINT UNSIGNED NULL,
  acknowledged_by BIGINT UNSIGNED NULL,
  acknowledged_at DATETIME(3) NULL,
  UNIQUE KEY uq_sla_escalation (ticket_id, kind, cycle, level),
  KEY ix_sla_escalation_time (triggered_at),
  KEY ix_sla_escalation_kind (kind, triggered_at),
  CONSTRAINT fk_sesc_ticket FOREIGN KEY (ticket_id)       REFERENCES tickets (id),
  CONSTRAINT fk_sesc_ack    FOREIGN KEY (acknowledged_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Kolom tambahan tiket: awal siklus SLA (dibuat / dibuka ulang) dan revisi
-- (naik saat prioritas berubah sehingga peringatan untuk target baru tetap tercatat).
ALTER TABLE tickets
  ADD COLUMN sla_cycle_started_at DATETIME(3) NULL AFTER sla_cycle,
  ADD COLUMN sla_revision INT UNSIGNED NOT NULL DEFAULT 1 AFTER sla_cycle_started_at,
  ADD CONSTRAINT fk_tickets_slapol FOREIGN KEY (sla_policy_id)   REFERENCES sla_policies (id),
  ADD CONSTRAINT fk_tickets_slacal FOREIGN KEY (sla_calendar_id) REFERENCES sla_calendars (id);

-- ---------- Konfigurasi awal (dapat diubah admin) ----------
INSERT INTO sla_calendars (name, is_24x7, is_default, created_at, updated_at) VALUES
  ('Jam kerja', FALSE, TRUE,  UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('24 jam',    TRUE,  FALSE, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));

-- Senin–Jumat 08.00–16.00 WITA
INSERT INTO sla_calendar_hours (calendar_id, weekday, start_time, end_time)
SELECT c.id, d.wd, '08:00:00', '16:00:00'
FROM sla_calendars c
JOIN (SELECT 1 AS wd UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5) d
WHERE c.name = 'Jam kerja';

-- Hari libur nasional bertanggal tetap. Libur keagamaan yang tanggalnya berubah
-- setiap tahun ditambahkan admin sesuai SKB pemerintah.
INSERT INTO sla_holidays (calendar_id, holiday_date, name, created_at)
SELECT c.id, h.d, h.n, UTC_TIMESTAMP(3)
FROM sla_calendars c
JOIN (
  SELECT DATE('2026-01-01') AS d, 'Tahun Baru Masehi' AS n
  UNION ALL SELECT '2026-05-01', 'Hari Buruh Internasional'
  UNION ALL SELECT '2026-06-01', 'Hari Lahir Pancasila'
  UNION ALL SELECT '2026-08-17', 'Hari Kemerdekaan RI'
  UNION ALL SELECT '2026-12-25', 'Hari Raya Natal'
  UNION ALL SELECT '2027-01-01', 'Tahun Baru Masehi'
  UNION ALL SELECT '2027-05-01', 'Hari Buruh Internasional'
  UNION ALL SELECT '2027-06-01', 'Hari Lahir Pancasila'
  UNION ALL SELECT '2027-08-17', 'Hari Kemerdekaan RI'
  UNION ALL SELECT '2027-12-25', 'Hari Raya Natal'
) h
WHERE c.name = 'Jam kerja';

-- Kritis: 24 jam, respons 15 mnt, selesai 4 jam.
-- Tinggi/Sedang/Rendah: jam kerja.
INSERT INTO sla_policies (name, priority, calendar_id, response_minutes, resolution_minutes, warn_pct, created_at, updated_at)
SELECT p.n, p.pr, c.id, p.resp, p.reso, 75, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM (
  SELECT 'Kritis (standar)' AS n, 'CRITICAL' AS pr, '24 jam' AS cal, 15 AS resp, 240 AS reso
  UNION ALL SELECT 'Tinggi (standar)', 'HIGH',   'Jam kerja', 30,  480
  UNION ALL SELECT 'Sedang (standar)', 'MEDIUM', 'Jam kerja', 120, 960
  UNION ALL SELECT 'Rendah (standar)', 'LOW',    'Jam kerja', 480, 2400
) p JOIN sla_calendars c ON c.name = p.cal;

-- Tiket aktif yang sudah ada mendapat kebijakan standar sesuai prioritasnya.
-- Batas waktu (due) dihitung worker SLA pada putaran pertama.
UPDATE tickets t
JOIN sla_policies p ON p.priority = t.priority AND p.ticket_category_id IS NULL
                   AND p.it_category_id IS NULL AND p.department_id IS NULL AND p.is_active
SET t.sla_policy_id = p.id, t.sla_calendar_id = p.calendar_id,
    t.sla_response_minutes = p.response_minutes, t.sla_resolution_minutes = p.resolution_minutes,
    t.sla_warn_pct = p.warn_pct, t.sla_allow_pause = p.allow_pause,
    t.sla_cycle_started_at = t.created_at
WHERE t.status IN ('NEW','CLAIMED','IN_PROGRESS','PENDING') AND t.sla_policy_id IS NULL;

-- +goose Down
ALTER TABLE tickets
  DROP FOREIGN KEY fk_tickets_slapol,
  DROP FOREIGN KEY fk_tickets_slacal;
ALTER TABLE tickets
  DROP INDEX fk_tickets_slapol,
  DROP INDEX fk_tickets_slacal,
  DROP COLUMN sla_revision,
  DROP COLUMN sla_cycle_started_at;
UPDATE tickets SET sla_policy_id = NULL, sla_calendar_id = NULL;
DROP TABLE IF EXISTS sla_escalations;
DROP TABLE IF EXISTS sla_policies;
DROP TABLE IF EXISTS sla_holidays;
DROP TABLE IF EXISTS sla_calendar_hours;
DROP TABLE IF EXISTS sla_calendars;
