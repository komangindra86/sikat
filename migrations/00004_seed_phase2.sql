-- Konfigurasi awal PHASE 2. Dapat diubah Admin IT lewat aplikasi.

-- +goose Up
INSERT INTO system_settings (setting_key, value, value_type, description, updated_at) VALUES
  ('upload.max_mb',    '8', 'INT', 'Ukuran maksimal satu foto (MB)', UTC_TIMESTAMP(3)),
  ('upload.max_files', '3', 'INT', 'Jumlah foto maksimal per pengajuan', UTC_TIMESTAMP(3));

-- Aturan prioritas awal (dievaluasi berurutan; aturan pertama yang cocok menang).
-- Dampak: 0 tidak mengganggu, 1 sebagian, 2 pelayanan berhenti.
-- Terdampak: 1 satu orang, 2 beberapa orang, 3 satu unit, 4 lebih dari satu unit.
INSERT INTO priority_rules (name, description, sort_order, min_impact, max_impact, min_scope, max_scope,
                            replacement_in, critical_service, result_priority, created_at, updated_at) VALUES
  ('Layanan klinis kritis berhenti', 'Unit layanan kritis, pelayanan berhenti, berdampak minimal satu unit',
   10, 2, NULL, 3, NULL, NULL, TRUE, 'CRITICAL', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('Satu unit atau lebih berhenti', 'Pelayanan berhenti dan berdampak minimal satu unit',
   20, 2, NULL, 3, NULL, NULL, NULL, 'HIGH', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('Berhenti tanpa pengganti', 'Pelayanan berhenti dan tidak ada perangkat pengganti',
   30, 2, NULL, NULL, NULL, 'NO,UNKNOWN', NULL, 'HIGH', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('Sedikit pengguna dan ada pengganti', 'Berdampak pada satu/beberapa orang dan tersedia perangkat pengganti',
   40, NULL, NULL, NULL, 2, 'YES', NULL, 'MEDIUM', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('Tidak mengganggu pelayanan', 'Pelayanan tetap berjalan normal',
   50, NULL, 0, NULL, NULL, NULL, NULL, 'LOW', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));

-- +goose Down
DELETE FROM priority_rules;
DELETE FROM system_settings WHERE setting_key IN ('upload.max_mb', 'upload.max_files');
