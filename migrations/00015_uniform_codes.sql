-- Menyeragamkan kode lama ke format kode otomatis (lihat database.NextCode):
-- UNT-001, RNG-0001, BDG-01, KAT-01, ALS-01, SPR-0001, PRK-<tahun>-0001.
-- Kode yang sudah berformat baru tidak diubah; kode lama diberi nomor lanjutan
-- (tidak bentrok dengan nomor yang sudah ada/teralokasi). Data dari SIMRS/CSV
-- (source <> 'LOCAL') tetap memakai kode sumber, kecuali kode berawalan 'LEGACY-'
-- (hasil pindahan sekali jalan dari aplikasi lama) yang ikut diseragamkan. Kode
-- lama disimpan di legacy_codes sehingga migrasi dapat dibatalkan (Down).
--
-- Relasi (tiket, kegiatan, riwayat perangkat, external_id_mappings) memakai ID,
-- bukan kode, sehingga tidak berubah. Baris non-LOCAL yang kodenya diganti
-- ditandai local_modified_at agar impor ulang dari sumber tidak diam-diam
-- mengembalikan kode LEGACY- (tercatat sebagai konflik).
--
-- Nomor 00015: 00013 dan 00014 dipakai tim server untuk kompatibilitas data
-- lama di produksi.

-- +goose Up
CREATE TABLE legacy_codes (
  table_name            VARCHAR(40)     NOT NULL,
  row_id                BIGINT UNSIGNED NOT NULL,
  old_code              VARCHAR(50)     NOT NULL,
  new_code              VARCHAR(50)     NOT NULL,
  old_local_modified_at DATETIME(3)     NULL COMMENT 'nilai sebelum ditandai (baris non-LOCAL)',
  migrated_at           DATETIME(3)     NOT NULL DEFAULT (UTC_TIMESTAMP(3)),
  PRIMARY KEY (table_name, row_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT 'kode sebelum diseragamkan (migrasi 00015)';

-- Alasan tunda yang wajib diberi keterangan dulu dikenali dari kode 'OTHER';
-- kini memakai penanda eksplisit agar kode bebas diseragamkan.
ALTER TABLE pending_reasons
  ADD COLUMN requires_note BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'petugas wajib menulis keterangan' AFTER pauses_sla;
UPDATE pending_reasons SET requires_note = TRUE WHERE code = 'OTHER';

-- departments: UNT-001
INSERT INTO legacy_codes (table_name, row_id, old_code, new_code, old_local_modified_at)
WITH base AS (
  SELECT GREATEST(
    COALESCE((SELECT current_value FROM number_sequences WHERE name = 'UNT' AND year = 0), 0),
    COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM departments WHERE REGEXP_LIKE(code, '^UNT-[0-9]+$', 'c')), 0)
  ) AS b
), todo AS (
  SELECT t.id, t.code AS old_code, t.local_modified_at, base.b + ROW_NUMBER() OVER (ORDER BY t.id) AS n
  FROM departments t CROSS JOIN base
  WHERE (t.source = 'LOCAL' OR t.code LIKE 'LEGACY-%') AND NOT REGEXP_LIKE(t.code, '^UNT-[0-9]+$', 'c')
)
SELECT 'departments', id, old_code, CONCAT('UNT-', LPAD(n, GREATEST(3, CHAR_LENGTH(n)), '0')), local_modified_at FROM todo;
UPDATE departments t JOIN legacy_codes l ON l.table_name = 'departments' AND l.row_id = t.id SET t.code = l.new_code;
UPDATE departments t JOIN legacy_codes l ON l.table_name = 'departments' AND l.row_id = t.id SET t.local_modified_at = UTC_TIMESTAMP(3) WHERE t.source <> 'LOCAL';
INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'UNT' AS name, 0 AS year, MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) AS v
  FROM departments WHERE REGEXP_LIKE(code, '^UNT-[0-9]+$', 'c')
) s WHERE s.v IS NOT NULL
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- rooms: RNG-0001
INSERT INTO legacy_codes (table_name, row_id, old_code, new_code, old_local_modified_at)
WITH base AS (
  SELECT GREATEST(
    COALESCE((SELECT current_value FROM number_sequences WHERE name = 'RNG' AND year = 0), 0),
    COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM rooms WHERE REGEXP_LIKE(code, '^RNG-[0-9]+$', 'c')), 0)
  ) AS b
), todo AS (
  SELECT t.id, t.code AS old_code, t.local_modified_at, base.b + ROW_NUMBER() OVER (ORDER BY t.id) AS n
  FROM rooms t CROSS JOIN base
  WHERE (t.source = 'LOCAL' OR t.code LIKE 'LEGACY-%') AND NOT REGEXP_LIKE(t.code, '^RNG-[0-9]+$', 'c')
)
SELECT 'rooms', id, old_code, CONCAT('RNG-', LPAD(n, GREATEST(4, CHAR_LENGTH(n)), '0')), local_modified_at FROM todo;
UPDATE rooms t JOIN legacy_codes l ON l.table_name = 'rooms' AND l.row_id = t.id SET t.code = l.new_code;
UPDATE rooms t JOIN legacy_codes l ON l.table_name = 'rooms' AND l.row_id = t.id SET t.local_modified_at = UTC_TIMESTAMP(3) WHERE t.source <> 'LOCAL';
INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'RNG' AS name, 0 AS year, MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) AS v
  FROM rooms WHERE REGEXP_LIKE(code, '^RNG-[0-9]+$', 'c')
) s WHERE s.v IS NOT NULL
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- it_categories: BDG-01
INSERT INTO legacy_codes (table_name, row_id, old_code, new_code)
WITH base AS (
  SELECT GREATEST(
    COALESCE((SELECT current_value FROM number_sequences WHERE name = 'BDG' AND year = 0), 0),
    COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM it_categories WHERE REGEXP_LIKE(code, '^BDG-[0-9]+$', 'c')), 0)
  ) AS b
), todo AS (
  SELECT t.id, t.code AS old_code, base.b + ROW_NUMBER() OVER (ORDER BY t.sort_order, t.id) AS n
  FROM it_categories t CROSS JOIN base
  WHERE NOT REGEXP_LIKE(t.code, '^BDG-[0-9]+$', 'c')
)
SELECT 'it_categories', id, old_code, CONCAT('BDG-', LPAD(n, GREATEST(2, CHAR_LENGTH(n)), '0')) FROM todo;
UPDATE it_categories t JOIN legacy_codes l ON l.table_name = 'it_categories' AND l.row_id = t.id SET t.code = l.new_code;
INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'BDG' AS name, 0 AS year, MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) AS v
  FROM it_categories WHERE REGEXP_LIKE(code, '^BDG-[0-9]+$', 'c')
) s WHERE s.v IS NOT NULL
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- asset_categories: KAT-01
INSERT INTO legacy_codes (table_name, row_id, old_code, new_code)
WITH base AS (
  SELECT GREATEST(
    COALESCE((SELECT current_value FROM number_sequences WHERE name = 'KAT' AND year = 0), 0),
    COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM asset_categories WHERE REGEXP_LIKE(code, '^KAT-[0-9]+$', 'c')), 0)
  ) AS b
), todo AS (
  SELECT t.id, t.code AS old_code, base.b + ROW_NUMBER() OVER (ORDER BY t.id) AS n
  FROM asset_categories t CROSS JOIN base
  WHERE NOT REGEXP_LIKE(t.code, '^KAT-[0-9]+$', 'c')
)
SELECT 'asset_categories', id, old_code, CONCAT('KAT-', LPAD(n, GREATEST(2, CHAR_LENGTH(n)), '0')) FROM todo;
UPDATE asset_categories t JOIN legacy_codes l ON l.table_name = 'asset_categories' AND l.row_id = t.id SET t.code = l.new_code;
INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'KAT' AS name, 0 AS year, MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) AS v
  FROM asset_categories WHERE REGEXP_LIKE(code, '^KAT-[0-9]+$', 'c')
) s WHERE s.v IS NOT NULL
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- pending_reasons: ALS-01
INSERT INTO legacy_codes (table_name, row_id, old_code, new_code)
WITH base AS (
  SELECT GREATEST(
    COALESCE((SELECT current_value FROM number_sequences WHERE name = 'ALS' AND year = 0), 0),
    COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM pending_reasons WHERE REGEXP_LIKE(code, '^ALS-[0-9]+$', 'c')), 0)
  ) AS b
), todo AS (
  SELECT t.id, t.code AS old_code, base.b + ROW_NUMBER() OVER (ORDER BY t.sort_order, t.id) AS n
  FROM pending_reasons t CROSS JOIN base
  WHERE NOT REGEXP_LIKE(t.code, '^ALS-[0-9]+$', 'c')
)
SELECT 'pending_reasons', id, old_code, CONCAT('ALS-', LPAD(n, GREATEST(2, CHAR_LENGTH(n)), '0')) FROM todo;
UPDATE pending_reasons t JOIN legacy_codes l ON l.table_name = 'pending_reasons' AND l.row_id = t.id SET t.code = l.new_code;
INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'ALS' AS name, 0 AS year, MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) AS v
  FROM pending_reasons WHERE REGEXP_LIKE(code, '^ALS-[0-9]+$', 'c')
) s WHERE s.v IS NOT NULL
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- spare_parts: SPR-0001
INSERT INTO legacy_codes (table_name, row_id, old_code, new_code)
WITH base AS (
  SELECT GREATEST(
    COALESCE((SELECT current_value FROM number_sequences WHERE name = 'SPR' AND year = 0), 0),
    COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM spare_parts WHERE REGEXP_LIKE(code, '^SPR-[0-9]+$', 'c')), 0)
  ) AS b
), todo AS (
  SELECT t.id, t.code AS old_code, base.b + ROW_NUMBER() OVER (ORDER BY t.id) AS n
  FROM spare_parts t CROSS JOIN base
  WHERE NOT REGEXP_LIKE(t.code, '^SPR-[0-9]+$', 'c')
)
SELECT 'spare_parts', id, old_code, CONCAT('SPR-', LPAD(n, GREATEST(4, CHAR_LENGTH(n)), '0')) FROM todo;
UPDATE spare_parts t JOIN legacy_codes l ON l.table_name = 'spare_parts' AND l.row_id = t.id SET t.code = l.new_code;
INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'SPR' AS name, 0 AS year, MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) AS v
  FROM spare_parts WHERE REGEXP_LIKE(code, '^SPR-[0-9]+$', 'c')
) s WHERE s.v IS NOT NULL
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- assets: PRK-<tahun dibuat, WITA>-0001, urutan per tahun
INSERT INTO legacy_codes (table_name, row_id, old_code, new_code, old_local_modified_at)
WITH used AS (
  SELECT CAST(SUBSTRING(asset_code, 5, 4) AS UNSIGNED) AS y, CAST(SUBSTRING_INDEX(asset_code, '-', -1) AS UNSIGNED) AS v
  FROM assets WHERE REGEXP_LIKE(asset_code, '^PRK-[0-9]{4}-[0-9]+$', 'c')
  UNION ALL
  SELECT year, current_value FROM number_sequences WHERE name = 'PRK'
), base AS (
  SELECT y, MAX(v) AS b FROM used GROUP BY y
), todo AS (
  SELECT a.id, a.asset_code AS old_code, a.local_modified_at, a.y,
         COALESCE(base.b, 0) + ROW_NUMBER() OVER (PARTITION BY a.y ORDER BY a.id) AS n
  FROM (SELECT id, asset_code, local_modified_at, YEAR(CONVERT_TZ(created_at, '+00:00', '+08:00')) AS y
          FROM assets WHERE (source = 'LOCAL' OR asset_code LIKE 'LEGACY-%') AND NOT REGEXP_LIKE(asset_code, '^PRK-[0-9]{4}-[0-9]+$', 'c')) a
  LEFT JOIN base ON base.y = a.y
)
SELECT 'assets', id, old_code, CONCAT('PRK-', y, '-', LPAD(n, GREATEST(4, CHAR_LENGTH(n)), '0')), local_modified_at FROM todo;
UPDATE assets t JOIN legacy_codes l ON l.table_name = 'assets' AND l.row_id = t.id SET t.asset_code = l.new_code;
UPDATE assets t JOIN legacy_codes l ON l.table_name = 'assets' AND l.row_id = t.id SET t.local_modified_at = UTC_TIMESTAMP(3) WHERE t.source <> 'LOCAL';
INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'PRK' AS name, CAST(SUBSTRING(asset_code, 5, 4) AS UNSIGNED) AS year,
         MAX(CAST(SUBSTRING_INDEX(asset_code, '-', -1) AS UNSIGNED)) AS v
  FROM assets WHERE REGEXP_LIKE(asset_code, '^PRK-[0-9]{4}-[0-9]+$', 'c')
  GROUP BY CAST(SUBSTRING(asset_code, 5, 4) AS UNSIGNED)
) s
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- +goose Down
UPDATE departments t JOIN legacy_codes l ON l.table_name = 'departments' AND l.row_id = t.id SET t.code = l.old_code;
UPDATE rooms t JOIN legacy_codes l ON l.table_name = 'rooms' AND l.row_id = t.id SET t.code = l.old_code;
UPDATE it_categories t JOIN legacy_codes l ON l.table_name = 'it_categories' AND l.row_id = t.id SET t.code = l.old_code;
UPDATE asset_categories t JOIN legacy_codes l ON l.table_name = 'asset_categories' AND l.row_id = t.id SET t.code = l.old_code;
UPDATE pending_reasons t JOIN legacy_codes l ON l.table_name = 'pending_reasons' AND l.row_id = t.id SET t.code = l.old_code;
UPDATE spare_parts t JOIN legacy_codes l ON l.table_name = 'spare_parts' AND l.row_id = t.id SET t.code = l.old_code;
UPDATE assets t JOIN legacy_codes l ON l.table_name = 'assets' AND l.row_id = t.id SET t.asset_code = l.old_code;
UPDATE departments t JOIN legacy_codes l ON l.table_name = 'departments' AND l.row_id = t.id SET t.local_modified_at = l.old_local_modified_at WHERE t.source <> 'LOCAL';
UPDATE rooms t JOIN legacy_codes l ON l.table_name = 'rooms' AND l.row_id = t.id SET t.local_modified_at = l.old_local_modified_at WHERE t.source <> 'LOCAL';
UPDATE assets t JOIN legacy_codes l ON l.table_name = 'assets' AND l.row_id = t.id SET t.local_modified_at = l.old_local_modified_at WHERE t.source <> 'LOCAL';
DROP TABLE legacy_codes;
ALTER TABLE pending_reasons DROP COLUMN requires_note;
-- Nomor urut kembali ke kode berformat baru tertinggi yang masih dipakai.
UPDATE number_sequences SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM departments WHERE REGEXP_LIKE(code, '^UNT-[0-9]+$', 'c')), 0) WHERE name = 'UNT' AND year = 0;
UPDATE number_sequences SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM rooms WHERE REGEXP_LIKE(code, '^RNG-[0-9]+$', 'c')), 0) WHERE name = 'RNG' AND year = 0;
UPDATE number_sequences SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM it_categories WHERE REGEXP_LIKE(code, '^BDG-[0-9]+$', 'c')), 0) WHERE name = 'BDG' AND year = 0;
UPDATE number_sequences SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM asset_categories WHERE REGEXP_LIKE(code, '^KAT-[0-9]+$', 'c')), 0) WHERE name = 'KAT' AND year = 0;
UPDATE number_sequences SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM pending_reasons WHERE REGEXP_LIKE(code, '^ALS-[0-9]+$', 'c')), 0) WHERE name = 'ALS' AND year = 0;
UPDATE number_sequences SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM spare_parts WHERE REGEXP_LIKE(code, '^SPR-[0-9]+$', 'c')), 0) WHERE name = 'SPR' AND year = 0;
UPDATE number_sequences n SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(a.asset_code, '-', -1) AS UNSIGNED)) FROM assets a WHERE REGEXP_LIKE(a.asset_code, '^PRK-[0-9]{4}-[0-9]+$', 'c') AND CAST(SUBSTRING(a.asset_code, 5, 4) AS UNSIGNED) = n.year), 0) WHERE n.name = 'PRK';
