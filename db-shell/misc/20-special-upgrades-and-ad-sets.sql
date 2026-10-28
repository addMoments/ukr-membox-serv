-- 20 — Ozel paket yukseltmeleri ve paket gruplarina gore AddMoments reklami (2026-10-28)
--
-- Musteri 28 Ekim 12:15 (5. Upgrades):
--   Class -> Grade 2500 UAH, Grade -> School 2500 UAH, Class -> School 5000 UAH;
--   Class +12 ay (storage + link) 2500, Grade +12 ay 5000, School +12 ay 7500; School +20 GB 2500 UAH.
--   Hero icin yukseltme yok.
-- Musteri 28 Ekim 12:20 (Ads Section): "Below items need separate Ads settings":
--   Ads for School, Ads for Hero, Ads for Mini ("we plan to place our ads to Mini packages also").
--
-- Model:
--   * Yukseltmeler 17'deki gibi products'ta (priority 0, is_add_on TRUE). "+12 ay (storage + link)"
--     upgrade_kind 'upload': yukleme ve galeri bitisi ayni ay kadar kayar (upgrade_extra_dates, 19).
--     Ozel paketlerde saklama toplam sure oldugu icin galeri yukleme bitince biter; +12 ay ikisini de uzatir.
--     +20 GB upgrade_kind 'storage', +25 GB gibi yalniz yukleme acikken satilir.
--   * Reklam grubu urunde: options.addmoments_ads_set. Class, Grade, School -> 'school', Hero -> 'hero',
--     Mini -> 'mini'. Her grubun reklami site_settings 'addmoments_ads:<grup>'. Grubu degistirmek ya da
--     yeni grup acmak veri isi: urunun addmoments_ads_set'i degisir, admin listesi grubu kendiliginden gosterir.
--   * Ozellik 5'li paketlerde (ozel paketler) alan her zaman AddMoments'in. Ozellik 5'siz ama grubu olan
--     pakette (Mini) AddMoments reklami yalniz admin o grubun reklamini kurduysa ve etkinlik kendi reklam
--     alanini (ozellik 4, "Digital Ads Zone") almadiysa gosterilir: parasini odeyen hostun reklami onde.
--   * Eski tek satir 'addmoments_ads' school ve hero'ya kopyalanir (admin'in kurdugu reklam kaybolmaz) ve
--     geri alma icin yerinde birakilir. Mini bos baslar: admin kurana kadar Mini etkinliklerinde hicbir sey degismez.
--
-- SIRA: bu dosya -> backend -> frontend. Eski backend bu dosyadan sonra da calisir: 'addmoments_ads'
-- satiri duruyor, yeni urunler yalniz ozel paketli etkinliklere satilir (canlida henuz yok).
-- Tek transaction; COMMIT'ten once kendini dogrular.

BEGIN;

-- ============================================================================
-- 1. YUKSELTME URUNLERI (ON CONFLICT ile tekrar calistirma guvenli; admin degisiklikleri ezilmez)
-- ============================================================================

INSERT INTO products (id, price, options, fullfillment_type, priority, is_add_on, granted_features, is_enabled,
                      display_name_en, display_name_uk, display_description_en, display_description_uk)
VALUES
    ('upgrade_special_class_grade', 2500,
     '{"upgrade_kind": "package", "upgrade_from": "special_class", "upgrade_to": "special_grade"}',
     'digital', 0, TRUE, '{}', TRUE,
     'Upgrade: CLASS → GRADE', 'Перехід: КЛАС → ПАРАЛЕЛЬ',
     'Your event moves to the GRADE package.', 'Ваша подія переходить на пакет ПАРАЛЕЛЬ.'),
    ('upgrade_special_grade_school', 2500,
     '{"upgrade_kind": "package", "upgrade_from": "special_grade", "upgrade_to": "special_school"}',
     'digital', 0, TRUE, '{}', TRUE,
     'Upgrade: GRADE → SCHOOL', 'Перехід: ПАРАЛЕЛЬ → ШКОЛА',
     'Your event moves to the SCHOOL package.', 'Ваша подія переходить на пакет ШКОЛА.'),
    ('upgrade_special_class_school', 5000,
     '{"upgrade_kind": "package", "upgrade_from": "special_class", "upgrade_to": "special_school"}',
     'digital', 0, TRUE, '{}', TRUE,
     'Upgrade: CLASS → SCHOOL', 'Перехід: КЛАС → ШКОЛА',
     'Your event moves to the SCHOOL package.', 'Ваша подія переходить на пакет ШКОЛА.'),
    ('special_class_extra_year', 2500,
     '{"upgrade_kind": "upload", "upgrade_for": "special_class", "extra_upload_months": 12}',
     'digital', 0, TRUE, '{}', TRUE,
     'CLASS: +12 months of uploads and storage', 'КЛАС: +12 місяців завантажень і зберігання',
     'Guests can upload for 12 months longer and the gallery is kept 12 months longer.',
     'Гості можуть завантажувати на 12 місяців довше, і галерея зберігається на 12 місяців довше.'),
    ('special_grade_extra_year', 5000,
     '{"upgrade_kind": "upload", "upgrade_for": "special_grade", "extra_upload_months": 12}',
     'digital', 0, TRUE, '{}', TRUE,
     'GRADE: +12 months of uploads and storage', 'ПАРАЛЕЛЬ: +12 місяців завантажень і зберігання',
     'Guests can upload for 12 months longer and the gallery is kept 12 months longer.',
     'Гості можуть завантажувати на 12 місяців довше, і галерея зберігається на 12 місяців довше.'),
    ('special_school_extra_year', 7500,
     '{"upgrade_kind": "upload", "upgrade_for": "special_school", "extra_upload_months": 12}',
     'digital', 0, TRUE, '{}', TRUE,
     'SCHOOL: +12 months of uploads and storage', 'ШКОЛА: +12 місяців завантажень і зберігання',
     'Guests can upload for 12 months longer and the gallery is kept 12 months longer.',
     'Гості можуть завантажувати на 12 місяців довше, і галерея зберігається на 12 місяців довше.'),
    ('special_school_extra_storage', 2500,
     '{"upgrade_kind": "storage", "upgrade_for": "special_school", "extra_storage_gb": 20}',
     'digital', 0, TRUE, '{}', TRUE,
     'SCHOOL: +20 GB of storage', 'ШКОЛА: +20 ГБ сховища',
     'Your event gets 20 GB more storage.', 'Ваша подія отримує на 20 ГБ більше сховища.')
ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- 2. REKLAM GRUPLARI
-- ============================================================================

-- Grubu zaten olan urune dokunulmaz (tekrar calistirma ve admin/el degisikligi korunur).
UPDATE products SET options = options || '{"addmoments_ads_set": "school"}'
 WHERE id IN ('special_class', 'special_grade', 'special_school') AND NOT options ? 'addmoments_ads_set';
UPDATE products SET options = options || '{"addmoments_ads_set": "hero"}'
 WHERE id = 'special_hero' AND NOT options ? 'addmoments_ads_set';
UPDATE products SET options = options || '{"addmoments_ads_set": "mini"}'
 WHERE id = 'standard' AND NOT options ? 'addmoments_ads_set';

INSERT INTO site_settings (key, value)
SELECT k, COALESCE((SELECT value FROM site_settings WHERE key = 'addmoments_ads'), '{"layout": "none", "cells": []}')
  FROM unnest(ARRAY['addmoments_ads:school', 'addmoments_ads:hero']) AS k
ON CONFLICT (key) DO NOTHING;

INSERT INTO site_settings (key, value)
VALUES ('addmoments_ads:mini', '{"layout": "none", "cells": []}')
ON CONFLICT (key) DO NOTHING;

-- ============================================================================
-- 3. DOGRULAMA (tutmazsa hata -> ON_ERROR_STOP ile geri alinir)
-- ============================================================================

DO $$
DECLARE
    v_upgrades   INT;
    v_targets    INT;
    v_sets       INT;
    v_rows       INT;
    v_copied     INT;
BEGIN
    SELECT COUNT(*) INTO v_upgrades FROM products
     WHERE id IN ('upgrade_special_class_grade', 'upgrade_special_grade_school', 'upgrade_special_class_school',
                  'special_class_extra_year', 'special_grade_extra_year', 'special_school_extra_year',
                  'special_school_extra_storage')
       AND options ? 'upgrade_kind';
    -- Kaynak ve hedef paketler var ve core paket (Event_upgrade_options ayni kosulu arar).
    SELECT COUNT(*) INTO v_targets FROM products u
     WHERE u.id LIKE 'upgrade_special_%'
       AND EXISTS (SELECT 1 FROM products f WHERE f.id = u.options->>'upgrade_from' AND f.is_add_on = FALSE)
       AND EXISTS (SELECT 1 FROM products t WHERE t.id = u.options->>'upgrade_to' AND t.is_add_on = FALSE);
    SELECT COUNT(*) INTO v_sets FROM products
     WHERE (id IN ('special_class', 'special_grade', 'special_school') AND options->>'addmoments_ads_set' = 'school')
        OR (id = 'special_hero' AND options->>'addmoments_ads_set' = 'hero')
        OR (id = 'standard' AND options->>'addmoments_ads_set' = 'mini');
    SELECT COUNT(*) INTO v_rows FROM site_settings
     WHERE key IN ('addmoments_ads:school', 'addmoments_ads:hero', 'addmoments_ads:mini');
    -- Eski satir varsa school ve hero onunla ayni baslamali.
    SELECT COUNT(*) INTO v_copied FROM site_settings s
     WHERE s.key IN ('addmoments_ads:school', 'addmoments_ads:hero')
       AND s.value IS NOT DISTINCT FROM COALESCE((SELECT value FROM site_settings WHERE key = 'addmoments_ads'), s.value);
    RAISE NOTICE 'special_upgrades=% package_targets=% ad_set_products=% ad_set_rows=% copied=%',
        v_upgrades, v_targets, v_sets, v_rows, v_copied;
    IF v_upgrades <> 7 OR v_targets <> 3 OR v_sets <> 5 OR v_rows <> 3 OR v_copied <> 2 THEN
        RAISE EXCEPTION '20 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;

-- ============================================================================
-- GERI ALMA (once backend eski surume; eski backend 'addmoments_ads' satirini okur)
-- ============================================================================
--   Satilmis yukseltme yoksa: DELETE FROM products WHERE id IN ('upgrade_special_class_grade',
--     'upgrade_special_grade_school', 'upgrade_special_class_school', 'special_class_extra_year',
--     'special_grade_extra_year', 'special_school_extra_year', 'special_school_extra_storage');
--   UPDATE products SET options = options - 'addmoments_ads_set' WHERE options ? 'addmoments_ads_set';
--   DELETE FROM site_settings WHERE key LIKE 'addmoments_ads:%';
