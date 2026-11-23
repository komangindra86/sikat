-- Verifikasi migrasi 00015_uniform_codes pada SALINAN database produksi.
-- Hanya membaca (SELECT). Jalankan dua kali: sebelum dan sesudah migrasi, lalu
-- bandingkan. Bagian 1 dan 2 HARUS identik; bagian 3 hanya berarti sesudah migrasi.
--
--   mysql --force -u root -p sikat_salinan < verify-uniform-codes.sql > sebelum.txt
--   (jalankan migrasi pada salinan)
--   mysql --force -u root -p sikat_salinan < verify-uniform-codes.sql > sesudah.txt
--   diff sebelum.txt sesudah.txt     -- hanya bagian 3 yang boleh berbeda

SELECT '== 1. Relasi (harus identik) ==' AS bagian;
SELECT 'tickets' AS tabel, COUNT(*) AS baris,
       SUM(IFNULL(department_id, 0)) AS s_unit, SUM(IFNULL(room_id, 0)) AS s_ruangan,
       SUM(IFNULL(asset_id, 0)) AS s_perangkat, SUM(IFNULL(it_category_id, 0)) AS s_bidang,
       SUM(IFNULL(pending_reason_id, 0)) AS s_alasan
  FROM tickets
UNION ALL
SELECT 'internal_activities', COUNT(*), SUM(IFNULL(department_id, 0)), SUM(IFNULL(room_id, 0)),
       SUM(IFNULL(asset_id, 0)), SUM(IFNULL(it_category_id, 0)), 0
  FROM internal_activities
UNION ALL
SELECT 'asset_maintenance_histories', COUNT(*), 0, 0, SUM(asset_id), 0, 0 FROM asset_maintenance_histories
UNION ALL
SELECT 'qr_codes', COUNT(*), 0, SUM(IFNULL(room_id, 0)), SUM(IFNULL(asset_id, 0)), 0, 0 FROM qr_codes
UNION ALL
SELECT 'maintenance_parts', COUNT(*), 0, 0, 0, 0, SUM(spare_part_id) FROM maintenance_parts
UNION ALL
SELECT 'rooms', COUNT(*), SUM(department_id), 0, 0, 0, 0 FROM rooms
UNION ALL
SELECT 'assets', COUNT(*), SUM(IFNULL(department_id, 0)), SUM(IFNULL(room_id, 0)), 0, SUM(asset_category_id), 0 FROM assets;

SELECT '== 2. Pemetaan SIMRS/CSV (harus identik; kolom yatim harus 0) ==' AS bagian;
SELECT m.source, m.entity_type, COUNT(*) AS pemetaan, SUM(m.local_id) AS s_local_id,
       SUM(CASE m.entity_type
             WHEN 'DEPARTMENT' THEN NOT EXISTS (SELECT 1 FROM departments x WHERE x.id = m.local_id)
             WHEN 'ROOM'       THEN NOT EXISTS (SELECT 1 FROM rooms x WHERE x.id = m.local_id)
             WHEN 'ASSET'      THEN NOT EXISTS (SELECT 1 FROM assets x WHERE x.id = m.local_id)
             WHEN 'EMPLOYEE'   THEN NOT EXISTS (SELECT 1 FROM employees x WHERE x.id = m.local_id)
           END) AS yatim
  FROM external_id_mappings m
 GROUP BY m.source, m.entity_type
 ORDER BY m.source, m.entity_type;

SELECT '== 3. Hasil penyeragaman (sesudah migrasi) ==' AS bagian;
-- Jumlah kode yang diganti per tabel (tabel legacy_codes baru ada sesudah migrasi;
-- sebelum migrasi baris ini error "doesn't exist" dan itu wajar).
SELECT table_name, COUNT(*) AS diganti FROM legacy_codes GROUP BY table_name ORDER BY table_name;
-- Kode LEGACY- yang tersisa (harus kosong).
SELECT 'departments' AS tabel, code FROM departments WHERE code LIKE 'LEGACY-%'
UNION ALL SELECT 'rooms', code FROM rooms WHERE code LIKE 'LEGACY-%'
UNION ALL SELECT 'assets', asset_code FROM assets WHERE asset_code LIKE 'LEGACY-%';
-- Kode di luar format baru yang sengaja dibiarkan (hanya data SIMRS/CSV non-LEGACY).
SELECT 'departments' AS tabel, source, code FROM departments WHERE NOT REGEXP_LIKE(code, '^UNT-[0-9]+$', 'c')
UNION ALL SELECT 'rooms', source, code FROM rooms WHERE NOT REGEXP_LIKE(code, '^RNG-[0-9]+$', 'c')
UNION ALL SELECT 'assets', source, asset_code FROM assets WHERE NOT REGEXP_LIKE(asset_code, '^PRK-[0-9]{4}-[0-9]+$', 'c');
-- Contoh pemetaan kode lama → baru.
SELECT table_name, old_code, new_code FROM legacy_codes ORDER BY table_name, new_code LIMIT 30;
