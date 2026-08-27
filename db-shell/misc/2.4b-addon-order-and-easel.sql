-- Musterinin 2026-08-26 tarihli ekstra listesi: madde 9, 10 ve 11'in ilk adimi.
--
-- Siralama products.priority'den geliyor (public sorgu ORDER BY priority DESC),
-- frontend add-on listesini ayrica siralamiyor. Priority artik admin panelinden de
-- duzenlenebiliyor ("Display order", d413a70), yani bundan sonraki siralama
-- degisiklikleri SQL gerektirmez -- bu script sadece ilk kurulumu yapiyor.
--
--  9) Welcome Board ile QR Card yer degistirsin  -> printedBanner 4, welcome_board 3
-- 10) Easel satistan kalksin                     -> aesel is_enabled = false
-- 11) Reklam Zone en asagi insin (simdilik sadece bu) -> advertorial 90 -> 1
--
-- advertorial neden 1: en dusuk deger, boylece listenin sonunda kaliyor. aesel de 1
-- ama kapatildigi icin listeye hic girmiyor, yani beraberlik sorun degil. Yine de
-- ileride karisiklik olmasin diye aesel 0'a CEKILMIYOR -- public sorgu priority > 0
-- filtreliyor ama gizleme isi is_enabled'in, priority'nin degil.
--
-- Easel siliniyor degil, kapatiliyor: urun kaydi ve gecmis siparislerdeki referanslar
-- bozulmasin diye. Ayni yontem qrKit'te de kullanilmis.
--
-- Calistirmadan once mevcut hali gor:
--   SELECT id, priority, is_enabled FROM products WHERE is_add_on ORDER BY priority DESC;
--
-- Yedek: ukr-membox/deploy/backups/products-priority-2026-08-27-1050-pre.json

BEGIN;

-- 9) Takas. Ara deger gerekmez, priority'de unique kisit yok.
UPDATE products SET priority = 4 WHERE id = 'printedBanner';
UPDATE products SET priority = 3 WHERE id = 'welcome_board';

-- 10) Easel'i satistan kaldir.
UPDATE products SET is_enabled = false WHERE id = 'aesel';

-- 11) Reklam Zone'u listenin sonuna al.
UPDATE products SET priority = 1 WHERE id = 'advertorial';

-- Dogrulama: sitede gorunen sira QR Card > Welcome Board > Audio Guestbook > Реклама
-- olmali; aesel sitede_gorunur = false olmali.
SELECT id, priority, is_enabled,
       (priority > 0 AND is_enabled) AS sitede_gorunur
FROM products
WHERE is_add_on
ORDER BY priority DESC, id;

COMMIT;

-- Geri alma:
--   UPDATE products SET priority = 3  WHERE id = 'printedBanner';
--   UPDATE products SET priority = 4  WHERE id = 'welcome_board';
--   UPDATE products SET priority = 90 WHERE id = 'advertorial';
--   UPDATE products SET is_enabled = true WHERE id = 'aesel';
