-- Konfigurasi awal agar sistem dapat dipakai. BUKAN data produksi:
-- semua nilai di bawah dapat diubah/dinonaktifkan Admin IT lewat aplikasi.
-- Master data rumah sakit (unit, ruangan, perangkat, pegawai) TIDAK di-seed.

-- +goose Up
INSERT INTO roles (code, name, description, is_system, created_at, updated_at) VALUES
  ('ADMIN',       'Admin IT',       'Mengelola akun, master data, konfigurasi, dan integrasi', TRUE, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('COORDINATOR', 'Koordinator IT', 'Memantau seluruh pekerjaan, SLA, perangkat, dan laporan',  TRUE, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('TECHNICIAN',  'Petugas IT',     'Mengambil dan menyelesaikan tiket, mencatat kegiatan',     TRUE, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));

INSERT INTO system_settings (setting_key, value, value_type, description, updated_at) VALUES
  ('app.hospital_name',              'Rumah Sakit', 'STRING', 'Nama rumah sakit yang tampil di aplikasi', UTC_TIMESTAMP(3)),
  ('app.it_contact',                 '',            'STRING', 'Kontak IT yang ditampilkan (mis. ekstensi 123)', UTC_TIMESTAMP(3)),
  ('security.login_max_attempts',    '5',           'INT',    'Jumlah gagal login sebelum akun dikunci sementara', UTC_TIMESTAMP(3)),
  ('security.login_lockout_minutes', '15',          'INT',    'Lama akun dikunci setelah gagal login berulang (menit)', UTC_TIMESTAMP(3));

INSERT INTO it_categories (code, name, description, sort_order, created_at, updated_at) VALUES
  ('HW',    'Hardware',               'Komputer, laptop, monitor, perangkat input', 10, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('PRN',   'Printer',                'Printer, scanner, label printer',            20, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('SW',    'Software & SIMRS',       'Aplikasi, SIMRS, sistem operasi',            30, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('NET',   'Jaringan',               'LAN, Wi-Fi, internet',                       40, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('INFRA', 'Server & Infrastruktur', 'Server, storage, UPS ruang server',          50, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('ACC',   'Akun & Akses',           'Akun pengguna, password, hak akses',         60, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));

INSERT INTO ticket_categories (it_category_id, name, help_text, default_priority, sort_order, created_at, updated_at)
SELECT c.id, t.name, t.help_text, t.prio, t.sort_order, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM (
  SELECT 'HW' code, 'Komputer tidak menyala' name, 'Komputer mati atau tidak mau hidup' help_text, 'MEDIUM' prio, 10 sort_order
  UNION ALL SELECT 'HW',    'Komputer lambat atau hang',   'Komputer menyala tetapi sangat lambat atau macet', 'LOW',    20
  UNION ALL SELECT 'HW',    'Monitor, mouse, atau keyboard bermasalah', NULL,                                  'LOW',    30
  UNION ALL SELECT 'PRN',   'Tidak bisa mencetak',         'Printer tidak mencetak, kertas macet, hasil buram', 'MEDIUM', 40
  UNION ALL SELECT 'SW',    'SIMRS error atau tidak bisa dibuka', 'Pesan error, tidak bisa login SIMRS, data tidak tampil', 'HIGH', 50
  UNION ALL SELECT 'SW',    'Aplikasi lain bermasalah',    'Aplikasi selain SIMRS',                            'MEDIUM', 60
  UNION ALL SELECT 'NET',   'Internet atau jaringan mati', 'Tidak bisa terhubung ke jaringan atau internet',   'HIGH',   70
  UNION ALL SELECT 'ACC',   'Lupa password atau akun terkunci', NULL,                                          'MEDIUM', 80
  UNION ALL SELECT 'HW',    'Lainnya',                     'Masalah lain yang tidak ada di daftar',            'MEDIUM', 90
) t JOIN it_categories c ON c.code = t.code;

INSERT INTO pending_reasons (code, label, pauses_sla, sort_order, created_at, updated_at) VALUES
  ('WAIT_PART',   'Menunggu sparepart',          FALSE, 10, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('WAIT_VENDOR', 'Menunggu vendor',             FALSE, 20, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('WAIT_USER',   'Menunggu pengguna/unit',      FALSE, 30, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('SCHEDULED',   'Menunggu jadwal pengerjaan',  FALSE, 40, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('OTHER',       'Lainnya',                     FALSE, 90, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));

INSERT INTO activity_types (name, it_category_id, sort_order, created_at, updated_at)
SELECT t.name, c.id, t.sort_order, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM (
  SELECT 'Maintenance' name, 'HW' code, 10 sort_order
  UNION ALL SELECT 'Instalasi',            'HW',    20
  UNION ALL SELECT 'Pemasangan jaringan',  'NET',   30
  UNION ALL SELECT 'Pengecekan server',    'INFRA', 40
  UNION ALL SELECT 'Inventarisasi',        NULL,    50
  UNION ALL SELECT 'Pendampingan',         'SW',    60
  UNION ALL SELECT 'Rapat',                NULL,    70
  UNION ALL SELECT 'Pekerjaan proyek',     NULL,    80
  UNION ALL SELECT 'Audit dan evaluasi',   NULL,    90
) t LEFT JOIN it_categories c ON c.code = t.code;

-- +goose Down
DELETE FROM activity_types;
DELETE FROM pending_reasons;
DELETE FROM ticket_categories;
DELETE FROM it_categories;
DELETE FROM system_settings;
DELETE FROM roles;
