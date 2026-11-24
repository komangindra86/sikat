-- Form pelapor per bidang dan QR global:
--   1. QR jenis GLOBAL: satu QR untuk seluruh rumah sakit, pelapor memilih lokasinya sendiri.
--      Hanya satu QR global yang aktif (dijaga UNIQUE index), bisa dibuat ulang/dinonaktifkan
--      seperti QR ruangan.
--   2. it_categories.reporter_help: penjelasan bidang dengan bahasa awam untuk pelapor
--      (keterangan bidang yang ada tetap untuk petugas).
--   3. Jenis gangguan tambahan agar tiap bidang punya pilihan: Mutu, hak akses (Keamanan
--      Sistem), dan "Lainnya (bidang)" untuk masalah di bidang itu yang tidak ada di daftar.
-- Semua perubahan data dicatat di lapor_per_bidang_lama agar Down dapat membatalkannya.

-- +goose Up
SET @t = UTC_TIMESTAMP(3);

CREATE TABLE lapor_per_bidang_lama (
  jenis   VARCHAR(30)     NOT NULL COMMENT 'jenis_gangguan_baru | jenis_gangguan_aktif | bantuan_lainnya',
  row_key BIGINT UNSIGNED NOT NULL,
  nilai   VARCHAR(255)    NULL COMMENT 'nilai sebelum diubah',
  PRIMARY KEY (jenis, row_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT 'data sebelum migrasi 00017';

-- 1. QR global.
ALTER TABLE qr_codes DROP CHECK chk_qr_target;
ALTER TABLE qr_codes MODIFY type ENUM('ROOM','ASSET','GLOBAL') NOT NULL;
ALTER TABLE qr_codes
  ADD COLUMN active_global TINYINT UNSIGNED AS (IF(is_active AND type = 'GLOBAL', 1, NULL)) STORED,
  ADD UNIQUE KEY uq_qr_active_global (active_global),
  ADD CONSTRAINT chk_qr_target CHECK (
    (type = 'ROOM'   AND room_id  IS NOT NULL AND asset_id IS NULL) OR
    (type = 'ASSET'  AND asset_id IS NOT NULL AND room_id  IS NULL) OR
    (type = 'GLOBAL' AND room_id  IS NULL     AND asset_id IS NULL)
  );

-- 2. Penjelasan bidang untuk pelapor.
ALTER TABLE it_categories ADD COLUMN reporter_help VARCHAR(255) NULL AFTER description;
UPDATE it_categories c JOIN (
  SELECT 'Pelayanan & Support' AS nama,
         'Komputer, laptop, monitor, mouse, keyboard, printer, scanner, internet, atau Wi-Fi bermasalah.' AS ket
  UNION ALL SELECT 'Administrasi & Inventaris',
         'Minta perangkat baru atau tambahan, memindahkan perangkat ke ruangan lain, atau serah terima perangkat.'
  UNION ALL SELECT 'Mutu',
         'Melaporkan kejadian atau kasus tidak sesuai saat memakai SIMRS, atau meminta data indikator mutu SIMRS.'
  UNION ALL SELECT 'ERM',
         'SIMRS atau rekam medis elektronik error, tidak bisa dibuka, lambat, data tidak tampil, atau aplikasi lain bermasalah.'
  UNION ALL SELECT 'System Developer',
         'Usul aplikasi atau fitur baru, minta form ditambah atau diubah, atau fitur tidak berjalan sesuai kebutuhan.'
  UNION ALL SELECT 'Keamanan Sistem',
         'Lupa password, akun terkunci, minta atau mencabut hak akses aplikasi, atau curiga akun dipakai orang lain.'
) k ON k.nama = c.name
SET c.reporter_help = k.ket, c.updated_at = @t;
UPDATE it_categories
SET reporter_help = 'Tidak yakin masalahnya termasuk yang mana? Pilih ini lalu ceritakan kendalanya, tim IT akan memilahnya.',
    updated_at = @t
WHERE is_triage;

INSERT INTO lapor_per_bidang_lama (jenis, row_key, nilai)
SELECT 'bantuan_lainnya', x.id, x.help_text FROM ticket_categories x WHERE x.name = 'Lainnya';
UPDATE ticket_categories SET help_text = 'Masalah lain yang tidak ada di daftar. Tim IT akan memilahnya.', updated_at = @t
WHERE name = 'Lainnya';

-- 3. Jenis gangguan tambahan (yang tertinggal nonaktif diaktifkan lagi).
CREATE TEMPORARY TABLE tmp_jenis_baru (
  name VARCHAR(120) PRIMARY KEY, bidang VARCHAR(100), help VARCHAR(255), prio VARCHAR(10), urut INT
);
INSERT INTO tmp_jenis_baru VALUES
  ('Laporan kasus atau penyimpangan SIMRS', 'Mutu',
   'Kejadian tidak sesuai prosedur saat memakai SIMRS, misalnya data salah input atau kasus yang perlu ditelusuri.', 'MEDIUM', 310),
  ('Permintaan data indikator mutu SIMRS', 'Mutu',
   'Butuh data, rekap, atau penjelasan indikator mutu SIMRS untuk unit Anda.', 'LOW', 320),
  ('Permintaan atau pencabutan hak akses', 'Keamanan Sistem',
   'Minta akses ke aplikasi atau menu tertentu, atau akses pegawai yang pindah atau keluar perlu dicabut.', 'LOW', 85),
  ('Lainnya (Pelayanan & Support)', 'Pelayanan & Support',
   'Masalah perangkat, printer, atau jaringan lain yang tidak ada di daftar.', 'MEDIUM', 910),
  ('Lainnya (Administrasi & Inventaris)', 'Administrasi & Inventaris',
   'Urusan perangkat lain, misalnya pendataan atau pengembalian perangkat.', 'LOW', 920),
  ('Lainnya (Mutu)', 'Mutu',
   'Hal lain terkait mutu SIMRS yang tidak ada di daftar.', 'LOW', 930),
  ('Lainnya (ERM)', 'ERM',
   'Masalah SIMRS, rekam medis elektronik, atau aplikasi lain yang tidak ada di daftar.', 'MEDIUM', 940),
  ('Lainnya (System Developer)', 'System Developer',
   'Permintaan pengembangan sistem lain yang tidak ada di daftar.', 'LOW', 950),
  ('Lainnya (Keamanan Sistem)', 'Keamanan Sistem',
   'Masalah akun, keamanan, atau server lain yang tidak ada di daftar.', 'MEDIUM', 960);

INSERT INTO lapor_per_bidang_lama (jenis, row_key)
SELECT 'jenis_gangguan_aktif', x.id FROM ticket_categories x JOIN tmp_jenis_baru t ON t.name = x.name
WHERE NOT x.is_active;
UPDATE ticket_categories x JOIN lapor_per_bidang_lama l ON l.jenis = 'jenis_gangguan_aktif' AND l.row_key = x.id
SET x.is_active = TRUE, x.updated_at = @t;

INSERT INTO ticket_categories (it_category_id, name, help_text, default_priority, sort_order, created_at, updated_at)
SELECT c.id, t.name, t.help, t.prio, t.urut, @t, @t
FROM tmp_jenis_baru t JOIN it_categories c ON c.name = t.bidang AND c.is_active
WHERE NOT EXISTS (SELECT 1 FROM ticket_categories x WHERE x.name = t.name);
INSERT INTO lapor_per_bidang_lama (jenis, row_key)
SELECT 'jenis_gangguan_baru', x.id FROM ticket_categories x JOIN tmp_jenis_baru t ON t.name = x.name
WHERE x.created_at = @t;

DROP TEMPORARY TABLE tmp_jenis_baru;

-- +goose Down
-- Jenis gangguan baru: dihapus bila belum dipakai, dinonaktifkan bila sudah dirujuk.
UPDATE ticket_categories x JOIN lapor_per_bidang_lama l ON l.jenis IN ('jenis_gangguan_baru', 'jenis_gangguan_aktif') AND l.row_key = x.id
SET x.is_active = FALSE;
DELETE x FROM ticket_categories x JOIN lapor_per_bidang_lama l ON l.jenis = 'jenis_gangguan_baru' AND l.row_key = x.id
WHERE NOT EXISTS (SELECT 1 FROM tickets t WHERE t.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM tickets t WHERE t.reported_ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM asset_maintenance_histories h WHERE h.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM priority_rules p WHERE p.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM sla_policies s WHERE s.ticket_category_id = x.id)
  AND NOT EXISTS (SELECT 1 FROM action_templates a WHERE a.ticket_category_id = x.id);
UPDATE ticket_categories x JOIN lapor_per_bidang_lama l ON l.jenis = 'bantuan_lainnya' AND l.row_key = x.id
SET x.help_text = l.nilai;

ALTER TABLE it_categories DROP COLUMN reporter_help;

-- QR global dihapus; tiket dari QR global tetap ada tanpa rujukan QR.
UPDATE tickets SET qr_code_id = NULL WHERE qr_code_id IN (SELECT id FROM (SELECT id FROM qr_codes WHERE type = 'GLOBAL') g);
UPDATE qr_codes SET replaced_by_id = NULL WHERE replaced_by_id IN (SELECT id FROM (SELECT id FROM qr_codes WHERE type = 'GLOBAL') g);
DELETE FROM qr_codes WHERE type = 'GLOBAL';
ALTER TABLE qr_codes DROP CHECK chk_qr_target;
ALTER TABLE qr_codes DROP INDEX uq_qr_active_global, DROP COLUMN active_global;
ALTER TABLE qr_codes MODIFY type ENUM('ROOM','ASSET') NOT NULL;
ALTER TABLE qr_codes ADD CONSTRAINT chk_qr_target CHECK (
  (type = 'ROOM'  AND room_id  IS NOT NULL AND asset_id IS NULL) OR
  (type = 'ASSET' AND asset_id IS NOT NULL AND room_id  IS NULL)
);

DROP TABLE lapor_per_bidang_lama;
