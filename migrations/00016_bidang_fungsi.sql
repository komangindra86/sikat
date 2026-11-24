-- Bidang mengikuti struktur 6 koordinator Instalasi SIMRS:
--   Pelayanan & Support       (dulu Hardware + Printer + Jaringan, juga memilah tiket Triase)
--   Administrasi & Inventaris (baru: pencatatan, distribusi, kajian kebutuhan, rekap perangkat)
--   Mutu                      (baru: program kerja & indikator mutu SIMRS, dikerjakan lewat Kegiatan)
--   ERM                       (dulu Software & SIMRS)
--   System Developer          (baru: usulan sistem, penambahan form, perbaikan sistem)
--   Keamanan Sistem           (dulu Akun & Akses + Server & Infrastruktur)
-- Bidang teknis lama yang dilebur dinonaktifkan dan semua rujukannya (jenis gangguan, tiket,
-- kegiatan, kompetensi petugas, aturan prioritas, kebijakan SLA, template, kategori perangkat)
-- dipindah ke bidang barunya. Semua perpindahan dicatat di bidang_lama dan bidang_lama_relasi
-- sehingga migrasi dapat dibatalkan (Down). Bidang dicocokkan berdasarkan nama bawaan,
-- bidang yang tidak ditemukan dilewati. Koordinator tiap bidang dipilih Admin di Master Data.

-- +goose Up
SET @t = UTC_TIMESTAMP(3);

CREATE TABLE bidang_lama (
  jenis               VARCHAR(10)     NOT NULL COMMENT 'ubah | gabung | baru | triase',
  it_category_id      BIGINT UNSIGNED NOT NULL,
  ke_id               BIGINT UNSIGNED NULL COMMENT 'bidang tujuan (gabung)',
  name                VARCHAR(100)    NULL,
  description         VARCHAR(255)    NULL,
  sort_order          INT             NULL,
  is_active           BOOLEAN         NULL,
  coordinator_user_id BIGINT UNSIGNED NULL,
  PRIMARY KEY (jenis, it_category_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT 'bidang sebelum migrasi 00016';

CREATE TABLE bidang_lama_relasi (
  table_name     VARCHAR(40)     NOT NULL,
  row_key        VARCHAR(64)     NOT NULL,
  it_category_id BIGINT UNSIGNED NULL COMMENT 'nilai sebelum dipindah',
  PRIMARY KEY (table_name, row_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT 'rujukan bidang yang dipindah migrasi 00016';

-- 1. Bidang utama lama diganti nama (baris & kode tetap).
CREATE TEMPORARY TABLE tmp_bidang_ubah (
  lama VARCHAR(100) PRIMARY KEY, baru VARCHAR(100), ket VARCHAR(255), urut INT
);
INSERT INTO tmp_bidang_ubah VALUES
  ('Hardware',         'Pelayanan & Support', 'Komputer, printer, jaringan, dan layanan pengguna. Memilah tiket triase.', 10),
  ('Software & SIMRS', 'ERM',                 'Rekam medis elektronik (ERM), SIMRS, dan aplikasi.',                      40),
  ('Akun & Akses',     'Keamanan Sistem',     'Akun dan hak akses, server, dan infrastruktur.',                          60);

INSERT INTO bidang_lama (jenis, it_category_id, name, description, sort_order, is_active, coordinator_user_id)
SELECT 'ubah', c.id, c.name, c.description, c.sort_order, c.is_active, c.coordinator_user_id
FROM it_categories c JOIN tmp_bidang_ubah u ON u.lama = c.name
WHERE NOT EXISTS (SELECT 1 FROM (SELECT name FROM it_categories) x WHERE x.name = u.baru);

UPDATE it_categories c
JOIN bidang_lama l ON l.jenis = 'ubah' AND l.it_category_id = c.id
JOIN tmp_bidang_ubah u ON u.lama = l.name
SET c.name = u.baru, c.description = u.ket, c.sort_order = u.urut, c.is_active = TRUE, c.updated_at = @t;

-- 2. Bidang yang belum ada dibuat dengan kode otomatis berikutnya (BDG-NN).
CREATE TEMPORARY TABLE tmp_bidang_baru (nama VARCHAR(100) PRIMARY KEY, ket VARCHAR(255), urut INT);
INSERT INTO tmp_bidang_baru VALUES
  ('Pelayanan & Support',       'Komputer, printer, jaringan, dan layanan pengguna. Memilah tiket triase.', 10),
  ('Administrasi & Inventaris', 'Pencatatan, distribusi, kajian kebutuhan, dan rekap perangkat.',           20),
  ('Mutu',                      'Program kerja dan indikator mutu SIMRS, laporan kinerja.',                 30),
  ('ERM',                       'Rekam medis elektronik (ERM), SIMRS, dan aplikasi.',                      40),
  ('System Developer',          'Usulan sistem, penambahan form, dan perbaikan sistem.',                    50),
  ('Keamanan Sistem',           'Akun dan hak akses, server, dan infrastruktur.',                          60);

INSERT INTO it_categories (code, name, description, sort_order, is_active, created_at, updated_at)
WITH base AS (
  SELECT GREATEST(
    COALESCE((SELECT current_value FROM number_sequences WHERE name = 'BDG' AND year = 0), 0),
    COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) FROM it_categories WHERE REGEXP_LIKE(code, '^BDG-[0-9]+$', 'c')), 0)
  ) AS b
), todo AS (
  SELECT nb.nama, nb.ket, nb.urut, base.b + ROW_NUMBER() OVER (ORDER BY nb.urut) AS n
  FROM tmp_bidang_baru nb CROSS JOIN base
  WHERE NOT EXISTS (SELECT 1 FROM it_categories c WHERE c.name = nb.nama)
)
SELECT CONCAT('BDG-', LPAD(n, GREATEST(2, CHAR_LENGTH(n)), '0')), nama, ket, urut, TRUE, @t, @t FROM todo;

INSERT INTO bidang_lama (jenis, it_category_id)
SELECT 'baru', c.id FROM it_categories c
WHERE c.created_at = @t AND c.name IN ('Pelayanan & Support', 'Administrasi & Inventaris', 'Mutu', 'ERM', 'System Developer', 'Keamanan Sistem');

-- Bidang bernama sama yang sudah ada tetapi nonaktif (mis. tertinggal dari Down sebelumnya
-- karena masih dirujuk data) diaktifkan lagi. Keadaan lamanya dicatat untuk Down.
INSERT INTO bidang_lama (jenis, it_category_id, name, description, sort_order, is_active, coordinator_user_id)
SELECT 'ubah', c.id, c.name, c.description, c.sort_order, c.is_active, c.coordinator_user_id
FROM it_categories c JOIN tmp_bidang_baru nb ON nb.nama = c.name
WHERE NOT c.is_active AND NOT EXISTS (SELECT 1 FROM bidang_lama l WHERE l.it_category_id = c.id);
UPDATE it_categories c
JOIN bidang_lama l ON l.jenis = 'ubah' AND l.it_category_id = c.id
JOIN tmp_bidang_baru nb ON nb.nama = c.name
SET c.is_active = TRUE, c.sort_order = nb.urut, c.updated_at = @t;

INSERT INTO number_sequences (name, year, current_value)
SELECT * FROM (
  SELECT 'BDG' AS name, 0 AS year, MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED)) AS v
  FROM it_categories WHERE REGEXP_LIKE(code, '^BDG-[0-9]+$', 'c')
) s WHERE s.v IS NOT NULL
ON DUPLICATE KEY UPDATE current_value = GREATEST(current_value, s.v);

-- 3. Bidang teknis lama dilebur ke bidang barunya.
CREATE TEMPORARY TABLE tmp_bidang_gabung (lama VARCHAR(100) PRIMARY KEY, baru VARCHAR(100));
INSERT INTO tmp_bidang_gabung VALUES
  ('Hardware',               'Pelayanan & Support'),
  ('Printer',                'Pelayanan & Support'),
  ('Jaringan',               'Pelayanan & Support'),
  ('Software & SIMRS',       'ERM'),
  ('Akun & Akses',           'Keamanan Sistem'),
  ('Server & Infrastruktur', 'Keamanan Sistem');

INSERT INTO bidang_lama (jenis, it_category_id, ke_id, is_active, coordinator_user_id)
SELECT 'gabung', s.id, t.id, s.is_active, s.coordinator_user_id
FROM tmp_bidang_gabung g
JOIN it_categories s ON s.name = g.lama
JOIN it_categories t ON t.name = g.baru;

-- Bidang tujuan tanpa koordinator mewarisi koordinator bidang yang dilebur.
UPDATE it_categories t
JOIN (SELECT ke_id, MIN(coordinator_user_id) AS k FROM bidang_lama
      WHERE jenis = 'gabung' AND coordinator_user_id IS NOT NULL GROUP BY ke_id) g ON g.ke_id = t.id
SET t.coordinator_user_id = g.k
WHERE t.coordinator_user_id IS NULL;

-- Rujukan dipindah (nilai lama dicatat dulu).
INSERT INTO bidang_lama_relasi SELECT 'ticket_categories', x.id, x.it_category_id FROM ticket_categories x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE ticket_categories x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;
INSERT INTO bidang_lama_relasi SELECT 'action_templates', x.id, x.it_category_id FROM action_templates x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE action_templates x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;
INSERT INTO bidang_lama_relasi SELECT 'activity_types', x.id, x.it_category_id FROM activity_types x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE activity_types x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;
INSERT INTO bidang_lama_relasi SELECT 'asset_categories', x.id, x.it_category_id FROM asset_categories x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE asset_categories x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;
INSERT INTO bidang_lama_relasi SELECT 'priority_rules', x.id, x.it_category_id FROM priority_rules x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE priority_rules x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;
INSERT INTO bidang_lama_relasi SELECT 'sla_policies', x.id, x.it_category_id FROM sla_policies x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE sla_policies x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;
INSERT INTO bidang_lama_relasi SELECT 'tickets', x.id, x.it_category_id FROM tickets x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE tickets x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;
INSERT INTO bidang_lama_relasi SELECT 'internal_activities', x.id, x.it_category_id FROM internal_activities x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id;
UPDATE internal_activities x JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = x.it_category_id SET x.it_category_id = l.ke_id;

-- Kompetensi petugas: bidang lama diganti bidang barunya (tanpa duplikat).
INSERT INTO bidang_lama_relasi
SELECT 'user_competencies', CONCAT(u.user_id, '-', u.it_category_id), u.it_category_id
FROM user_competencies u JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = u.it_category_id;
INSERT INTO bidang_lama_relasi
SELECT DISTINCT 'kompetensi_baru', CONCAT(u.user_id, '-', l.ke_id), l.ke_id
FROM user_competencies u JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = u.it_category_id
WHERE NOT EXISTS (SELECT 1 FROM user_competencies x WHERE x.user_id = u.user_id AND x.it_category_id = l.ke_id);
INSERT IGNORE INTO user_competencies (user_id, it_category_id, created_at)
SELECT u.user_id, l.ke_id, @t
FROM user_competencies u JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = u.it_category_id;
DELETE u FROM user_competencies u JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = u.it_category_id;

UPDATE it_categories c JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = c.id
SET c.is_active = FALSE, c.updated_at = @t;

-- 4. Triase dipilah Pelayanan & Support: bila belum ada koordinator, ikut koordinatornya.
INSERT INTO bidang_lama (jenis, it_category_id, coordinator_user_id)
SELECT 'triase', c.id, c.coordinator_user_id FROM it_categories c
WHERE c.is_triage AND c.coordinator_user_id IS NULL;
SET @koordinator_pelayanan = (SELECT coordinator_user_id FROM it_categories WHERE name = 'Pelayanan & Support');
UPDATE it_categories c JOIN bidang_lama l ON l.jenis = 'triase' AND l.it_category_id = c.id
SET c.coordinator_user_id = @koordinator_pelayanan;

-- 5. Jenis gangguan untuk pelapor di bidang baru (yang tertinggal nonaktif diaktifkan lagi).
INSERT INTO bidang_lama_relasi (table_name, row_key)
SELECT 'jenis_gangguan_aktif', x.id FROM ticket_categories x
WHERE NOT x.is_active AND x.name IN ('Permintaan perangkat baru', 'Pemindahan atau serah terima perangkat',
  'Usulan sistem atau fitur baru', 'Penambahan atau perubahan form', 'Perbaikan sistem');
UPDATE ticket_categories x JOIN bidang_lama_relasi r ON r.table_name = 'jenis_gangguan_aktif' AND r.row_key = x.id
SET x.is_active = TRUE, x.updated_at = @t;
INSERT INTO ticket_categories (it_category_id, name, help_text, default_priority, sort_order, created_at, updated_at)
SELECT c.id, t.name, t.help, t.prio, t.urut, @t, @t
FROM (
  SELECT 'Administrasi & Inventaris' AS bidang, 'Permintaan perangkat baru' AS name,
         'Unit membutuhkan perangkat baru atau tambahan. Tim IT mengkaji kebutuhannya.' AS help, 'LOW' AS prio, 110 AS urut
  UNION ALL SELECT 'Administrasi & Inventaris', 'Pemindahan atau serah terima perangkat',
         'Perangkat perlu dipindah ke ruangan lain atau diserahkan ke unit.', 'LOW', 120
  UNION ALL SELECT 'System Developer', 'Usulan sistem atau fitur baru',
         'Usulan aplikasi baru atau fitur tambahan untuk mendukung pekerjaan unit.', 'LOW', 210
  UNION ALL SELECT 'System Developer', 'Penambahan atau perubahan form',
         'Form di aplikasi perlu ditambah atau diubah isinya.', 'LOW', 220
  UNION ALL SELECT 'System Developer', 'Perbaikan sistem',
         'Fitur aplikasi tidak berjalan sesuai kebutuhan dan perlu diperbaiki tim developer.', 'MEDIUM', 230
) t JOIN it_categories c ON c.name = t.bidang
WHERE NOT EXISTS (SELECT 1 FROM ticket_categories x WHERE x.name = t.name);
INSERT INTO bidang_lama_relasi (table_name, row_key)
SELECT 'jenis_gangguan_baru', x.id FROM ticket_categories x
WHERE x.created_at = @t AND x.name IN ('Permintaan perangkat baru', 'Pemindahan atau serah terima perangkat',
  'Usulan sistem atau fitur baru', 'Penambahan atau perubahan form', 'Perbaikan sistem');

-- 6. Jenis kegiatan internal per bidang baru.
INSERT INTO bidang_lama_relasi
SELECT 'activity_types', x.id, x.it_category_id FROM activity_types x
WHERE x.it_category_id IS NULL AND x.name IN ('Inventarisasi', 'Audit dan evaluasi');
UPDATE activity_types x JOIN it_categories c ON c.name = 'Administrasi & Inventaris'
SET x.it_category_id = c.id, x.updated_at = @t WHERE x.name = 'Inventarisasi' AND x.it_category_id IS NULL;
UPDATE activity_types x JOIN it_categories c ON c.name = 'Mutu'
SET x.it_category_id = c.id, x.updated_at = @t WHERE x.name = 'Audit dan evaluasi' AND x.it_category_id IS NULL;

INSERT INTO bidang_lama_relasi (table_name, row_key)
SELECT 'jenis_kegiatan_aktif', x.id FROM activity_types x
WHERE NOT x.is_active AND x.name IN ('Pencatatan perangkat', 'Distribusi perangkat', 'Kajian kebutuhan perangkat',
  'Rekap perangkat', 'Program kerja mutu', 'Penyusunan indikator mutu', 'Analisis dan laporan indikator mutu',
  'Sosialisasi mutu', 'Penanganan penyimpangan SIMRS', 'Laporan kinerja bulanan', 'Pengembangan sistem',
  'Penambahan form', 'Perbaikan sistem');
UPDATE activity_types x JOIN bidang_lama_relasi r ON r.table_name = 'jenis_kegiatan_aktif' AND r.row_key = x.id
SET x.is_active = TRUE, x.updated_at = @t;

INSERT INTO activity_types (name, it_category_id, sort_order, created_at, updated_at)
SELECT t.name, c.id, t.urut, @t, @t
FROM (
  SELECT 'Administrasi & Inventaris' AS bidang, 'Pencatatan perangkat' AS name, 110 AS urut
  UNION ALL SELECT 'Administrasi & Inventaris', 'Distribusi perangkat', 120
  UNION ALL SELECT 'Administrasi & Inventaris', 'Kajian kebutuhan perangkat', 130
  UNION ALL SELECT 'Administrasi & Inventaris', 'Rekap perangkat', 140
  UNION ALL SELECT 'Mutu', 'Program kerja mutu', 210
  UNION ALL SELECT 'Mutu', 'Penyusunan indikator mutu', 220
  UNION ALL SELECT 'Mutu', 'Analisis dan laporan indikator mutu', 230
  UNION ALL SELECT 'Mutu', 'Sosialisasi mutu', 240
  UNION ALL SELECT 'Mutu', 'Penanganan penyimpangan SIMRS', 250
  UNION ALL SELECT 'Mutu', 'Laporan kinerja bulanan', 260
  UNION ALL SELECT 'System Developer', 'Pengembangan sistem', 310
  UNION ALL SELECT 'System Developer', 'Penambahan form', 320
  UNION ALL SELECT 'System Developer', 'Perbaikan sistem', 330
) t JOIN it_categories c ON c.name = t.bidang
WHERE NOT EXISTS (SELECT 1 FROM activity_types x WHERE x.name = t.name);
INSERT INTO bidang_lama_relasi (table_name, row_key)
SELECT 'jenis_kegiatan_baru', x.id FROM activity_types x
WHERE x.created_at = @t AND x.name IN ('Pencatatan perangkat', 'Distribusi perangkat', 'Kajian kebutuhan perangkat',
  'Rekap perangkat', 'Program kerja mutu', 'Penyusunan indikator mutu', 'Analisis dan laporan indikator mutu',
  'Sosialisasi mutu', 'Penanganan penyimpangan SIMRS', 'Laporan kinerja bulanan', 'Pengembangan sistem',
  'Penambahan form', 'Perbaikan sistem');

DROP TEMPORARY TABLE tmp_bidang_ubah, tmp_bidang_baru, tmp_bidang_gabung;

-- +goose Down
-- Jenis gangguan & kegiatan baru: dihapus bila belum dipakai, dinonaktifkan bila sudah dirujuk
-- (tiket, jenis pilihan pelapor, riwayat pemeliharaan, aturan prioritas, SLA, template, kegiatan).
UPDATE ticket_categories x JOIN bidang_lama_relasi r ON r.table_name IN ('jenis_gangguan_baru', 'jenis_gangguan_aktif') AND r.row_key = x.id SET x.is_active = FALSE;
DELETE x FROM ticket_categories x JOIN bidang_lama_relasi r ON r.table_name = 'jenis_gangguan_baru' AND r.row_key = x.id
WHERE NOT EXISTS (SELECT 1 FROM tickets t WHERE t.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM tickets t WHERE t.reported_ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM asset_maintenance_histories h WHERE h.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM priority_rules p WHERE p.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM sla_policies s WHERE s.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM action_templates a WHERE a.ticket_category_id = x.id);
UPDATE activity_types x JOIN bidang_lama_relasi r ON r.table_name IN ('jenis_kegiatan_baru', 'jenis_kegiatan_aktif') AND r.row_key = x.id SET x.is_active = FALSE;
DELETE x FROM activity_types x JOIN bidang_lama_relasi r ON r.table_name = 'jenis_kegiatan_baru' AND r.row_key = x.id
WHERE NOT EXISTS (SELECT 1 FROM internal_activities a WHERE a.activity_type_id = x.id);

-- Rujukan kembali ke bidang lama.
UPDATE ticket_categories x JOIN bidang_lama_relasi r ON r.table_name = 'ticket_categories' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
UPDATE action_templates x JOIN bidang_lama_relasi r ON r.table_name = 'action_templates' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
UPDATE activity_types x JOIN bidang_lama_relasi r ON r.table_name = 'activity_types' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
UPDATE asset_categories x JOIN bidang_lama_relasi r ON r.table_name = 'asset_categories' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
UPDATE priority_rules x JOIN bidang_lama_relasi r ON r.table_name = 'priority_rules' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
UPDATE sla_policies x JOIN bidang_lama_relasi r ON r.table_name = 'sla_policies' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
UPDATE tickets x JOIN bidang_lama_relasi r ON r.table_name = 'tickets' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
UPDATE internal_activities x JOIN bidang_lama_relasi r ON r.table_name = 'internal_activities' AND r.row_key = x.id SET x.it_category_id = r.it_category_id;
DELETE u FROM user_competencies u
JOIN bidang_lama_relasi r ON r.table_name = 'kompetensi_baru' AND r.row_key = CONCAT(u.user_id, '-', u.it_category_id);
INSERT IGNORE INTO user_competencies (user_id, it_category_id, created_at)
SELECT CAST(SUBSTRING_INDEX(r.row_key, '-', 1) AS UNSIGNED), r.it_category_id, UTC_TIMESTAMP(3)
FROM bidang_lama_relasi r WHERE r.table_name = 'user_competencies'
  AND EXISTS (SELECT 1 FROM users u WHERE u.id = CAST(SUBSTRING_INDEX(r.row_key, '-', 1) AS UNSIGNED));

-- Bidang lama kembali seperti semula.
UPDATE it_categories c JOIN bidang_lama l ON l.jenis = 'gabung' AND l.it_category_id = c.id
SET c.is_active = l.is_active, c.updated_at = UTC_TIMESTAMP(3);
UPDATE it_categories c JOIN bidang_lama l ON l.jenis = 'ubah' AND l.it_category_id = c.id
SET c.name = l.name, c.description = l.description, c.sort_order = l.sort_order, c.is_active = l.is_active,
    c.coordinator_user_id = l.coordinator_user_id, c.updated_at = UTC_TIMESTAMP(3);
UPDATE it_categories c JOIN bidang_lama l ON l.jenis = 'triase' AND l.it_category_id = c.id
SET c.coordinator_user_id = l.coordinator_user_id;

-- Bidang baru: dihapus bila tidak dirujuk lagi, dinonaktifkan bila masih dipakai data baru.
UPDATE it_categories c JOIN bidang_lama l ON l.jenis = 'baru' AND l.it_category_id = c.id SET c.is_active = FALSE;
DELETE c FROM it_categories c JOIN bidang_lama l ON l.jenis = 'baru' AND l.it_category_id = c.id
WHERE NOT EXISTS (SELECT 1 FROM ticket_categories x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM action_templates x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM activity_types x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM asset_categories x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM priority_rules x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM sla_policies x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM tickets x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM internal_activities x WHERE x.it_category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM user_competencies x WHERE x.it_category_id = c.id);

DROP TABLE bidang_lama_relasi;
DROP TABLE bidang_lama;
-- Nomor urut bidang kembali ke kode tertinggi yang masih dipakai.
UPDATE number_sequences SET current_value = COALESCE((SELECT MAX(CAST(SUBSTRING_INDEX(code, '-', -1) AS UNSIGNED))
  FROM it_categories WHERE REGEXP_LIKE(code, '^BDG-[0-9]+$', 'c')), 0) WHERE name = 'BDG' AND year = 0;
