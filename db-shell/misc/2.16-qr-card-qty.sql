-- 2.16 — QR Card (printedBanner) siparis adedi: en az 8, 8'in katlari.
--
-- Kural kodda "fiziksel urun" olmaya bagli DEGIL: welcome_board ve aesel de fiziksel
-- ama tek adet satiliyor. Frontend yalnizca products.options.min_qty / qty_step
-- alanlarini okur (src/client/cart.ts -> getQtyRule), bu yuzden kural veriyle aciliyor.
-- Bu iki alan yoksa urun bugunku davranisini korur (birer birer, en az 1).
--
-- Calistirmadan once mevcut hali gor:
--   SELECT id, options FROM products WHERE id = 'printedBanner';

BEGIN;

UPDATE products
SET options = options || '{"min_qty": 8, "qty_step": 8}'::jsonb
WHERE id = 'printedBanner';

-- Tam olarak 1 satir etkilenmis olmali; degilse COMMIT etme.
SELECT id, options->>'min_qty' AS min_qty, options->>'qty_step' AS qty_step
FROM products
WHERE id = 'printedBanner';

COMMIT;

-- Geri alma:
--   UPDATE products SET options = options - 'min_qty' - 'qty_step'
--   WHERE id = 'printedBanner';
