-- Sparepart tanpa harga, dengan stok.
--   1. Harga acuan (default_unit_cost) dihapus dari master sparepart.
--   2. Kolom stok ditambahkan (awal 0; admin mengisinya di Master > Sparepart).
--      Stok berkurang saat sparepart dicatat terpakai dan kembali bila entrinya dibatalkan.
-- Harga satuan pada maintenance_parts (unit_cost/line_cost) dibiarkan apa adanya sebagai
-- arsip; aplikasi tidak lagi mengisi, menampilkan, atau menjumlahkannya.
-- Down mengembalikan kolom harga acuan (kosong) dan menghapus kolom stok.

-- +goose Up
ALTER TABLE spare_parts
  DROP CHECK chk_spare_parts_cost,
  DROP COLUMN default_unit_cost,
  ADD COLUMN stock DECIMAL(10,2) NOT NULL DEFAULT 0 AFTER unit,
  ADD CONSTRAINT chk_spare_parts_stock CHECK (stock >= 0);

-- +goose Down
ALTER TABLE spare_parts
  DROP CHECK chk_spare_parts_stock,
  DROP COLUMN stock,
  ADD COLUMN default_unit_cost DECIMAL(14,2) NULL AFTER unit,
  ADD CONSTRAINT chk_spare_parts_cost CHECK (default_unit_cost IS NULL OR default_unit_cost >= 0);
