-- Koordinator per bidang (keputusan 13/11/2026): setiap bidang memiliki tepat satu
-- koordinator; satu koordinator boleh memegang beberapa bidang. Bila bidang belum
-- punya koordinator aktif, notifikasi/eskalasi dialihkan ke semua koordinator dan
-- semua koordinator boleh menindak tiket bidang tersebut.

-- +goose Up
ALTER TABLE it_categories
  ADD COLUMN coordinator_user_id BIGINT UNSIGNED NULL AFTER description,
  ADD CONSTRAINT fk_itcat_coordinator FOREIGN KEY (coordinator_user_id) REFERENCES users (id);

-- Isi awal: bidang yang kompetensinya dimiliki tepat satu koordinator aktif.
UPDATE it_categories ic
JOIN (
  SELECT c.it_category_id, MIN(u.id) AS uid, COUNT(*) AS n
    FROM user_competencies c
    JOIN users u ON u.id = c.user_id AND u.is_active = TRUE
    JOIN roles r ON r.id = u.role_id AND r.code = 'COORDINATOR'
   GROUP BY c.it_category_id
) x ON x.it_category_id = ic.id
SET ic.coordinator_user_id = x.uid
WHERE x.n = 1;

-- +goose Down
ALTER TABLE it_categories DROP FOREIGN KEY fk_itcat_coordinator;
ALTER TABLE it_categories DROP INDEX fk_itcat_coordinator, DROP COLUMN coordinator_user_id;
