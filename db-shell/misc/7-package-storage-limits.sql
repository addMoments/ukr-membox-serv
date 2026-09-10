-- 7 — Paket basina toplam depolama limiti + bozuk limitlerin onarimi (Excel madde 2.17)
--
-- BU SCRIPT `6-package-limits-restore.sql` YERINE GECER. 6'yi calistirma, bunu calistir.
-- 6 yalnizca plus/premium'u eski haline (storage_gb = -1) donduruyordu; musteri 10 Eylul'de
-- etkinlik basina gercek rakamlari verdi, yani -1 yazip hemen ustune yazmak anlamsiz.
-- 6 zaten calistirildiysa da sorun yok: asagidaki UPDATE mutlak deger yazar, sonuc ayni.
--
-- ---------------------------------------------------------------------------------------
-- Iki isi birden yapar
--
-- 1) ONARIM. 9 Eylul 2026'da 13:04-14:36 UTC arasinda admin panelinden paket kaydedilirken
--    `plus` ve `premium` urunlerinin storage_gb, guest_media_count ve guest_storage_gb
--    degerleri 0 olarak yazildi. 0 kodda GERCEK bir limittir (Check_upload_limits,
--    src/db_scripts/limits.go), yani bu iki paketle satin alinmis TUM etkinliklerde misafir
--    foto/video yuklemesi kapandi. Panelin `readNumber` yardimcisi bos birakilan alani
--    Number('') === 0 ile 0'a ceviriyordu; frontend tarafi duzeltildi
--    (ukr-membox/src/pages/admin/products.tsx).
--
-- 2) YENI LIMIT. Musteri 10 Eylul 2026'da etkinlik basina toplam yukleme rakamlarini verdi
--    (Excel 2.17 yorumu): Mini 5 GB, Classic 25 GB, Premium 100 GB. Bu, 4-upload-limits.sql
--    ile bilerek -1 (sinirsiz) birakilan `storage_gb` alaninin nihai cevabidir.
--    Misafir basina limitler degismiyor: 100 dosya / 4 GB, musteri "Done" diye onayladi.
--
-- ---------------------------------------------------------------------------------------
-- Paket ismi eslesmesi -- yanlis pakete yazmak canli etkinlikte yuklemeyi kapatir
--
-- Musterinin kullandigi isimler urun kimlikleri degil, products.display_name_en degerleri:
--
--     Musteri     display_name_en   display_name_uk   id          fiyat    yeni storage_gb
--     Mini        MINI              MINI              standard     790            5 GB
--     Classic     CLASSIC           KLASYCHNYI        plus        1790           25 GB
--     Premium     PREMIUM           PREMIUM           premium     2790          100 GB
--
-- Eslesme iki bagimsiz kanitla dogrulandi:
--   * db-backups/products-pre-limits-2026-09-08.csv -- display_name_en sutunu birebir MINI /
--     CLASSIC / PREMIUM, priority 1/2/3 ve fiyatlar artan sirada.
--   * Musterinin Excel'e gomdugu ekran goruntusu: uzerinde RECOMMENDED rozeti olan kart
--     "KLASICHNYI" ve 1790 UAH. Rozet options.tagText alanindan gelir ve yalnizca `plus`ta
--     doludur, yani ekran goruntusundeki Classic = plus.
--
-- ---------------------------------------------------------------------------------------
-- Once `7a-storage-impact-check.sql` calistirilir
--
-- Limit gecmise donuk uygulanir: kullanimi zaten limitin ustunde olan etkinlikte misafir
-- yuklemesi bu script biter bitmez 403 + STORAGE_LIMIT_REACHED alir. Mevcut medya SILINMEZ,
-- yalnizca yenisi eklenemez. 8 Eylul olcumunde tek bir etkinlik 38 GB'ti; o etkinlik Mini ya
-- da Classic ise limit yazilir yazilmaz kapanir. 7a bunu limit konmadan once listeler.
--
-- CANLI VERIYE YAZAR. Once yedek al:
--   \copy (SELECT uid, id, is_add_on, options FROM products) TO 'products-pre-storage-limits-2026-09-10.csv' CSV HEADER

BEGIN;

-- Misafir basina limitler: uc pakette de ayni. plus/premium'da 0'a dusmustu, geri yaziliyor;
-- standard'da zaten dogruydu, ayni deger tekrar yaziliyor (idempotent).
UPDATE products
SET options = options || '{"guest_media_count": 100, "guest_storage_gb": 4}'::jsonb
WHERE is_add_on = FALSE
  AND id IN ('standard', 'plus', 'premium');

-- Etkinlik basina toplam depolama: paket bazinda farkli (tier farki).
-- jsonb || sadece verilen anahtarlari ezer; guest_count, media_count, storage_days,
-- activation_days ve tagText oldugu gibi kalir.
UPDATE products SET options = options || '{"storage_gb": 5}'::jsonb
  WHERE is_add_on = FALSE AND id = 'standard';
UPDATE products SET options = options || '{"storage_gb": 25}'::jsonb
  WHERE is_add_on = FALSE AND id = 'plus';
UPDATE products SET options = options || '{"storage_gb": 100}'::jsonb
  WHERE is_add_on = FALSE AND id = 'premium';

-- Guard: uc core paketin ucu de beklenen degerde olmali. Bir tanesi bile tutmuyorsa
-- (id degismis, satir eksik, jsonb ezilmis) transaction geri alinir.
DO $$
DECLARE ok INT;
BEGIN
    SELECT COUNT(*) INTO ok
    FROM products
    WHERE is_add_on = FALSE
      AND (id, options->>'storage_gb', options->>'guest_media_count', options->>'guest_storage_gb')
          IN (('standard', '5', '100', '4'), ('plus', '25', '100', '4'), ('premium', '100', '100', '4'));

    IF ok <> 3 THEN
        RAISE EXCEPTION 'Beklenen 3 core paket dogrulanamadi, dogrulanan: %. Degisiklik geri alindi.', ok;
    END IF;
END $$;

-- Dogrulama: uc pakette de per_guest_media=100, per_guest_gb=4; event_gb 5/25/100 olmali.
SELECT id,
       options->>'guest_count'       AS guests,
       options->>'media_count'       AS media,
       options->>'guest_media_count' AS per_guest_media,
       options->>'guest_storage_gb'  AS per_guest_gb,
       options->>'storage_gb'        AS event_gb
FROM products
WHERE is_add_on = FALSE AND id IN ('standard', 'plus', 'premium')
ORDER BY id;

COMMIT;

-- PostgREST reload GEREKMEZ: yeni tablo/kolon yok, yalnizca satir icerigi degisti.
-- Backend limitleri her yukleme isteginde products.options'tan taze okur (Event_option_number),
-- yani restart de gerekmez; degisiklik COMMIT ile birlikte devreye girer.

-- Geri alma (limitleri tekrar sinirsiz yapmak icin):
--   BEGIN;
--   UPDATE products SET options = options || '{"storage_gb": -1}'::jsonb
--     WHERE is_add_on = FALSE AND id IN ('standard', 'plus', 'premium');
--   COMMIT;
