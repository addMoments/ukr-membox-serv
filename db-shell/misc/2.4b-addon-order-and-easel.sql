-- Musterinin 2026-08-26 tarihli ekstra listesi, madde 9 ve 10.
--
-- 9) Welcome Board ile QR Card'in Services & Prices sayfasindaki yerleri degissin.
--    Siralama products.priority'den geliyor (public sorgu ORDER BY priority DESC),
--    frontend add-on listesini ayrica siralamiyor. Iki degeri takas etmek yeterli.
--    Not: priority artik admin panelinden de duzenlenebiliyor ("Display order",
--    d413a70), yani bundan sonraki siralama degisiklikleri SQL gerektirmez.
--
-- 10) Easel add-on'u satistan kalksin.
--     Silinmiyor, is_enabled = false yapiliyor: urun kaydi ve gecmis siparislerdeki
--     referanslar bozulmasin diye. Public urun sorgusu `is_enabled = true` filtreledigi
--     icin kart siteden hemen kayboluyor. Ayni yontem qrKit'te de kullanilmis.
--     Geri almak icin tek satir yeter (asagida).
--
-- Calistirmadan once mevcut hali gor:
--   SELECT id, priority, is_enabled FROM products WHERE is_add_on ORDER BY priority DESC;
--
-- Yedek: ukr-membox/deploy/backups/products-priority-2026-08-27-1050-pre.json

BEGIN;

-- 9) Takas. Ara deger kullanmaya gerek yok, priority'de unique kisit yok.
UPDATE products SET priority = 4 WHERE id = 'printedBanner';
UPDATE products SET priority = 3 WHERE id = 'welcome_board';

-- 10) Easel'i satistan kaldir.
UPDATE products SET is_enabled = false WHERE id = 'aesel';

-- Dogrulama: sira advertorial(90) > printedBanner(4) > welcome_board(3) >
-- audioGuestbook(2) olmali; aesel is_enabled = f oldugu icin sitede gorunmemeli.
SELECT id, priority, is_enabled,
       (priority > 0 AND is_enabled) AS sitede_gorunur
FROM products
WHERE is_add_on
ORDER BY priority DESC, id;

COMMIT;

-- Geri alma:
--   UPDATE products SET priority = 3 WHERE id = 'printedBanner';
--   UPDATE products SET priority = 4 WHERE id = 'welcome_board';
--   UPDATE products SET is_enabled = true WHERE id = 'aesel';
