-- 21 — Ozel paketler sayfasi: bolumler ve paket aciklamalari (2026-11-01)
--
-- Solomiia 29 Ekim (WhatsApp, /special-packages ekran goruntuleri):
--   2) "Please add Explanation Section to each of the packages in Special Packages (including Hero)":
--      fiyatin altindaki aciklama satiri (Mini'de "FOR SMALL GATHERINGS"). Alan display_description_en/uk,
--      ozel paketlerde bos girilmisti (18). Taslak metinler yazilir; admin urun listesinden degisir.
--   3) "Please divide section School and section Hero with a line-divider ... After the line ... text: bold
--      and regular": sayfa paketleri options.special_section'a gore bolumlere ayirir (Class, Grade, School ->
--      'school'; Hero -> 'hero'). Ilk bolum "Choose Your Package" basligi altinda, sonrakiler ince cizgi +
--      baslik + alt baslikla (metinler on yuzde).
--
-- Yalniz bos aciklama ve eksik bolum anahtari doldurulur: admin degisiklikleri ezilmez, tekrar calistirma guvenli.
-- SIRA: bu dosya -> frontend. Eski frontend bu dosyadan sonra da calisir (special_section'u okumaz, aciklama
-- satiri zaten gosteriliyor). Tek transaction; COMMIT'ten once kendini dogrular.

BEGIN;

UPDATE products SET options = options || '{"special_section": "school"}'
 WHERE id IN ('special_class', 'special_grade', 'special_school') AND NOT options ? 'special_section';
UPDATE products SET options = options || '{"special_section": "hero"}'
 WHERE id = 'special_hero' AND NOT options ? 'special_section';

-- Diger paketlerdeki gibi buyuk harf (Mini "FOR SMALL GATHERINGS" / "ДЛЯ НЕВЕЛИКИХ ЗУСТРІЧЕЙ").
UPDATE products p SET display_description_en = d.en
  FROM (VALUES ('special_class', 'FOR ONE CLASS'),
               ('special_grade', 'FOR A WHOLE GRADE'),
               ('special_school', 'FOR THE WHOLE SCHOOL'),
               ('special_hero', 'TO HONOUR YOUR HERO')) AS d(id, en)
 WHERE p.id = d.id AND COALESCE(TRIM(p.display_description_en), '') = '';
UPDATE products p SET display_description_uk = d.uk
  FROM (VALUES ('special_class', 'ДЛЯ ОДНОГО КЛАСУ'),
               ('special_grade', 'ДЛЯ ВСІЄЇ ПАРАЛЕЛІ'),
               ('special_school', 'ДЛЯ ВСІЄЇ ШКОЛИ'),
               ('special_hero', 'ЩОБ ВШАНУВАТИ ВАШОГО ГЕРОЯ')) AS d(id, uk)
 WHERE p.id = d.id AND COALESCE(TRIM(p.display_description_uk), '') = '';

DO $$
DECLARE
    v_sections INT;
    v_descs    INT;
BEGIN
    SELECT COUNT(*) INTO v_sections FROM products
     WHERE (id IN ('special_class', 'special_grade', 'special_school') AND options->>'special_section' = 'school')
        OR (id = 'special_hero' AND options->>'special_section' = 'hero');
    SELECT COUNT(*) INTO v_descs FROM products
     WHERE id IN ('special_class', 'special_grade', 'special_school', 'special_hero')
       AND COALESCE(TRIM(display_description_en), '') <> ''
       AND COALESCE(TRIM(display_description_uk), '') <> '';
    RAISE NOTICE 'special_sections=% special_descriptions=%', v_sections, v_descs;
    IF v_sections <> 4 OR v_descs <> 4 THEN
        RAISE EXCEPTION '21 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;

-- ============================================================================
-- GERI ALMA
-- ============================================================================
--   UPDATE products SET options = options - 'special_section' WHERE options ? 'special_section';
--   Aciklamalar admin urun listesinden degistirilir ya da bosaltilir.
