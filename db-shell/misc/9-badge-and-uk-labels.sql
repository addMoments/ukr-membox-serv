-- 9 — Paket rozeti ve Ukraynaca kalan iki etiket (musterinin 10 Eylul yorumlari)
--
-- Iki ayri sikayet, ayni kok: gorunen metin products tablosunda VERI olarak duruyor ve
-- yalnizca Ingilizce yazilmis. Cozum 2.13'teki kalibin aynisi -- mekanizma kodda, ceviri
-- veride.
--
-- 1) ROZET (Excel 2.4, musteri yorumu: "Instead of 'recommended': Popular / Популярний").
--    Kartin sag ust kosesindeki rozet options.tagText alanindan geliyordu ve TEK DILLIYDI,
--    yani Ukraynaca fiyat sayfasinda "RECOMMENDED" diye Ingilizce cikiyordu. Musterinin
--    ekran goruntusunde de aynen boyle gorunuyor.
--    Frontend artik options.tagText_uk alanini okuyor (localizedTagText,
--    ukr-membox/src/utils/product_i18n.ts); alan bossa Ingilizceye duser, yani bu script
--    calismadan once de hicbir sey bozulmaz.
--    Rozet yalnizca `plus` pakette dolu; digerlerinde rozet hic gosterilmiyor.
--
-- 2) REKLAM ALANI MADDESI (musterinin ekran goruntusunde gorunen ayri bir hata).
--    advertorial urununun display_bullets_uk alani Ingilizce "Guest page banner area"
--    yaziyordu, yani Ukraynaca sayfada kartin basligi ve aciklamasi Ukraynaca, alt maddesi
--    Ingilizce cikiyordu. Ayni kaydin display_name_uk ve display_description_uk alanlari
--    zaten Ukraynacaydi, yalnizca bu satir atlanmis.
--
-- Ukraynaca metinler bizim yazdigimiz; musteri kendi metnini gonderirse degisir
-- (durum raporunun "Ukrainian copy" notu).
--
-- CANLI VERIYE YAZAR. Once yedek al:
--   \copy (SELECT uid, id, is_add_on, options, display_bullets_en, display_bullets_uk FROM products) TO 'products-pre-badge-2026-09-11.csv' CSV HEADER

BEGIN;

-- 1) Rozet: Ingilizce "Popular", Ukraynaca "Популярний".
UPDATE products
SET options = options || '{"tagText": "Popular", "tagText_uk": "Популярний"}'::jsonb
WHERE is_add_on = FALSE
  AND id = 'plus';

-- 2) Reklam alani maddesinin Ukraynacasi.
UPDATE products
SET display_bullets_uk = 'Банерна зона на сторінці для гостей'
WHERE id = 'advertorial';

-- Guard: iki guncelleme de tutmali, yoksa islem geri alinir.
DO $$
DECLARE v_badge INT; v_bullet INT;
BEGIN
    SELECT COUNT(*) INTO v_badge FROM products
    WHERE is_add_on = FALSE AND id = 'plus'
      AND options->>'tagText' = 'Popular' AND options->>'tagText_uk' = 'Популярний';

    SELECT COUNT(*) INTO v_bullet FROM products
    WHERE id = 'advertorial' AND display_bullets_uk = 'Банерна зона на сторінці для гостей';

    IF v_badge <> 1 OR v_bullet <> 1 THEN
        RAISE EXCEPTION 'Dogrulama basarisiz (rozet=%, madde=%). Degisiklik geri alindi.', v_badge, v_bullet;
    END IF;
END $$;

-- Dogrulama ciktisi.
SELECT id, options->>'tagText' AS tag_en, options->>'tagText_uk' AS tag_uk
FROM products WHERE is_add_on = FALSE AND id IN ('standard', 'plus', 'premium') ORDER BY id;

SELECT id, display_bullets_en, display_bullets_uk FROM products WHERE id = 'advertorial';

COMMIT;

-- PostgREST reload gerekmez: yeni tablo/kolon yok.

-- Geri alma:
--   BEGIN;
--   UPDATE products SET options = (options - 'tagText_uk') || '{"tagText": "Recommended"}'::jsonb
--     WHERE is_add_on = FALSE AND id = 'plus';
--   UPDATE products SET display_bullets_uk = 'Guest page banner area' WHERE id = 'advertorial';
--   COMMIT;
