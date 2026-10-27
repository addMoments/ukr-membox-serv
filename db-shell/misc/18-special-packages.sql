-- 18 — Ozel musterilere ozel paketler (Excel madde 6; 2026-10-27)
--
-- Musteri 26 Ekim (tablo + mail): Class 2500 UAH 5 GB, Grade 4000 UAH 10 GB, School 7500 UAH 20 GB
--   (yukleme 12 ay, saklama 12 ay), Hero 390 UAH 10 GB (yukleme 1 ay, saklama 3 ay). Hepsinde:
--   Unlimited Event Gallery, Unlimited Guests & Co-Hosts, QR templates, Configurable activation date,
--   Collaborators, Ads by Addmoments. Ayarlar admin urun listesinden.
-- Solomiia 26 Ekim 17:50: saklama "12 months in total"; reklam alani host'a kapali, AddMoments admini
--   yonetir; sesli/video mesaj dahil. Kim alabilir ve yukseltme: konusuluyor.
--
-- Model:
--   * Dort urun products'ta, is_add_on FALSE (core paket gibi limit/tarih kaynagi), options.special_package
--     TRUE (on yuz bunlari Mini/Classic/Premium sirasindan ayri tutar). is_enabled FALSE: yerlesim
--     kararlasana kadar hicbir yerde gorunmez ve satilamaz (backend kapali urunu reddeder); admin acar.
--   * options.storage_from_activation TRUE: storage_days aktivasyondan itibaren TOPLAM sure. Diger
--     paketlerde storage_days yukleme bittikten sonra sayilir (Premium 31 + 180). Iki gun fonksiyonu
--     ortak package_storage_after_upload() ile yukleme sonrasini hesaplar; trigger'lar degismez.
--   * granted_features {2, 3, 5}: 2 Poster (QR sablonlari), 3 sesli mesaj, 5 AddMoments reklami.
--     5'li etkinlikte misafir sayfasi site_settings.addmoments_ads reklamini gosterir, host duzenleyemez.
--   * Yukseltme urunu bu paketlere bagli degil: yukseltme karti bu etkinliklerde cikmaz.
--
-- SIRA: bu dosya -> backend -> frontend. Eski backend bu dosyadan sonra da calisir (urunler kapali,
-- yeni tablo okunmaz, gun fonksiyonlari ozel olmayan paketlerde ayni sonucu verir).
-- Tek transaction; COMMIT'ten once kendini dogrular.

BEGIN;

-- ============================================================================
-- 1. URUNLER (ON CONFLICT ile tekrar calistirma guvenli; admin degisiklikleri ezilmez)
-- ============================================================================

INSERT INTO products (id, price, options, fullfillment_type, priority, is_add_on, granted_features, is_enabled,
                      display_name_en, display_name_uk, display_description_en, display_description_uk,
                      display_bullets_en, display_bullets_uk)
VALUES
    ('special_class', 2500,
     '{"corePackage": true, "special_package": true, "storage_from_activation": true,
       "activation_days": 365, "storage_days": 365, "storage_gb": 5,
       "guest_count": -1, "media_count": -1, "guest_media_count": -1, "guest_storage_gb": -1}',
     'digital', 4, FALSE, '{2,3,5}', FALSE,
     'CLASS', 'КЛАС', '', '',
     E'5 GB for Pictures / Videos\n12 Months Upload Duration\n12 Months Storage Access\nUnlimited Event Gallery\nUnlimited Guests & Co-Hosts\nQR Templates\nConfigurable Activation Date\nCollaborators\nVoice/Video Message\nOriginal Resolution\nAds by AddMoments',
     E'5 ГБ для фото / відео\n12 місяців для завантаження медіа\n12 місяців доступу до медіа\nНеобмежена галерея події\nНеобмежена кількість гостей і співорганізаторів\nQR-шаблони\nДата активації на ваш вибір\nСпіворганізатори\nГолосові повідомлення\nОригінальна якість\nРеклама від AddMoments'),
    ('special_grade', 4000,
     '{"corePackage": true, "special_package": true, "storage_from_activation": true,
       "activation_days": 365, "storage_days": 365, "storage_gb": 10,
       "guest_count": -1, "media_count": -1, "guest_media_count": -1, "guest_storage_gb": -1}',
     'digital', 3, FALSE, '{2,3,5}', FALSE,
     'GRADE', 'ПАРАЛЕЛЬ', '', '',
     E'10 GB for Pictures / Videos\n12 Months Upload Duration\n12 Months Storage Access\nUnlimited Event Gallery\nUnlimited Guests & Co-Hosts\nQR Templates\nConfigurable Activation Date\nCollaborators\nVoice/Video Message\nOriginal Resolution\nAds by AddMoments',
     E'10 ГБ для фото / відео\n12 місяців для завантаження медіа\n12 місяців доступу до медіа\nНеобмежена галерея події\nНеобмежена кількість гостей і співорганізаторів\nQR-шаблони\nДата активації на ваш вибір\nСпіворганізатори\nГолосові повідомлення\nОригінальна якість\nРеклама від AddMoments'),
    ('special_school', 7500,
     '{"corePackage": true, "special_package": true, "storage_from_activation": true,
       "activation_days": 365, "storage_days": 365, "storage_gb": 20,
       "guest_count": -1, "media_count": -1, "guest_media_count": -1, "guest_storage_gb": -1}',
     'digital', 2, FALSE, '{2,3,5}', FALSE,
     'SCHOOL', 'ШКОЛА', '', '',
     E'20 GB for Pictures / Videos\n12 Months Upload Duration\n12 Months Storage Access\nUnlimited Event Gallery\nUnlimited Guests & Co-Hosts\nQR Templates\nConfigurable Activation Date\nCollaborators\nVoice/Video Message\nOriginal Resolution\nAds by AddMoments',
     E'20 ГБ для фото / відео\n12 місяців для завантаження медіа\n12 місяців доступу до медіа\nНеобмежена галерея події\nНеобмежена кількість гостей і співорганізаторів\nQR-шаблони\nДата активації на ваш вибір\nСпіворганізатори\nГолосові повідомлення\nОригінальна якість\nРеклама від AddMoments'),
    ('special_hero', 390,
     '{"corePackage": true, "special_package": true, "storage_from_activation": true,
       "activation_days": 30, "storage_days": 90, "storage_gb": 10,
       "guest_count": -1, "media_count": -1, "guest_media_count": -1, "guest_storage_gb": -1}',
     'digital', 1, FALSE, '{2,3,5}', FALSE,
     'HERO', 'ГЕРОЙ', '', '',
     E'10 GB for Pictures / Videos\n1 Month Upload Duration\n3 Months Storage Access\nUnlimited Event Gallery\nUnlimited Guests & Co-Hosts\nQR Templates\nConfigurable Activation Date\nCollaborators\nVoice/Video Message\nOriginal Resolution\nAds by AddMoments',
     E'10 ГБ для фото / відео\n1 місяць для завантаження медіа\n3 місяці доступу до медіа\nНеобмежена галерея події\nНеобмежена кількість гостей і співорганізаторів\nQR-шаблони\nДата активації на ваш вибір\nСпіворганізатори\nГолосові повідомлення\nОригінальна якість\nРеклама від AddMoments')
ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- 2. SAKLAMA SURESI: yukleme bittikten sonraki gun sayisi tek yerden
-- ============================================================================

-- storage_from_activation paketlerinde storage_days toplam sure: yukleme sonrasi = toplam - yukleme
-- (0'in altina inmez). Digerlerinde storage_days aynen doner. storage_days yoksa NULL (cagiran 14'e duser).
CREATE OR REPLACE FUNCTION package_storage_after_upload(p_options JSONB)
RETURNS INT
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN COALESCE((p_options->>'storage_from_activation')::BOOLEAN, FALSE)
            THEN GREATEST(NULLIF(p_options->>'storage_days', '')::INT
                          - COALESCE(NULLIF(p_options->>'activation_days', '')::INT, 0), 0)
        ELSE NULLIF(p_options->>'storage_days', '')::INT
    END;
$$;

-- Etkinlik olusurken (enforce_event_dates_on_insert) okunur; govde canlidakiyle ayni, yalniz gun hesabi ortak.
CREATE OR REPLACE FUNCTION get_purchase_storage_days(p_purchase_uid UUID)
RETURNS INT
LANGUAGE plpgsql STABLE AS $$
DECLARE
    v_storage_days INT;
BEGIN
    SELECT COALESCE(package_storage_after_upload(p.options), 14)
    INTO v_storage_days
    FROM purchases pu
    JOIN carts c ON c.uid = pu.cart_uid
    JOIN cart_items ci ON ci.cart_uid = c.uid
    JOIN products p ON p.uid = ci.product_uid
    WHERE pu.uid = p_purchase_uid
      AND p.is_add_on = FALSE
    ORDER BY ci.created_at ASC
    LIMIT 1;

    RETURN COALESCE(v_storage_days, 14);
END;
$$;

-- Tarih degisince (enforce_event_dates) ve yukseltme uygulanirken (apply_event_upgrade) okunur.
CREATE OR REPLACE FUNCTION get_event_storage_days(p_event_uid UUID)
RETURNS INT
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT package_storage_after_upload(p.options)
           FROM products p WHERE p.uid = event_core_product_uid(p_event_uid)),
        14);
$$;

-- ============================================================================
-- 3. ADDMOMENTS REKLAMI (tum ozel paket etkinliklerinde ayni; super admin duzenler)
-- ============================================================================

-- Backend sahibi (membox) okur/yazar; PostgREST rollerine acik degil.
CREATE TABLE IF NOT EXISTS site_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE site_settings ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON site_settings FROM PUBLIC;
REVOKE ALL ON site_settings FROM auth;
REVOKE ALL ON site_settings FROM webanon;

INSERT INTO site_settings (key, value)
VALUES ('addmoments_ads', '{"layout": "none", "cells": []}')
ON CONFLICT (key) DO NOTHING;

-- ============================================================================
-- 4. DOGRULAMA (tutmazsa hata -> ON_ERROR_STOP ile geri alinir)
-- ============================================================================

DO $$
DECLARE
    v_products     INT;
    v_class_after  INT;
    v_hero_after   INT;
    v_day_changes  INT;
    v_ads          INT;
BEGIN
    SELECT COUNT(*) INTO v_products FROM products WHERE options->>'special_package' = 'true';
    SELECT package_storage_after_upload(options) INTO v_class_after FROM products WHERE id = 'special_class';
    SELECT package_storage_after_upload(options) INTO v_hero_after FROM products WHERE id = 'special_hero';
    -- Ozel olmayan paketlerde sonuc storage_days'in kendisi olmali: mevcut hicbir etkinligin tarihi degismez.
    SELECT COUNT(*) INTO v_day_changes FROM products
     WHERE is_add_on = FALSE
       AND COALESCE((options->>'storage_from_activation')::BOOLEAN, FALSE) = FALSE
       AND package_storage_after_upload(options) IS DISTINCT FROM NULLIF(options->>'storage_days', '')::INT;
    SELECT COUNT(*) INTO v_ads FROM site_settings WHERE key = 'addmoments_ads';
    RAISE NOTICE 'special_packages=% class_after_upload=% hero_after_upload=% day_changes=% ads_row=%',
        v_products, v_class_after, v_hero_after, v_day_changes, v_ads;
    IF v_products <> 4 OR v_class_after <> 0 OR v_hero_after <> 60 OR v_day_changes <> 0 OR v_ads <> 1 THEN
        RAISE EXCEPTION '18 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;

NOTIFY pgrst, 'reload schema';

-- ============================================================================
-- GERI ALMA (once backend eski surume)
-- ============================================================================
--   get_event_storage_days: 17-event-upgrades.sql'deki govde (NULLIF(p.options->>'storage_days', '')::INT);
--   get_purchase_storage_days: ayni ifade, govdesi yukaridakiyle ayni.
--   DROP FUNCTION package_storage_after_upload(JSONB); DROP TABLE site_settings;
--   Satilmis ozel paket yoksa: DELETE FROM products WHERE options->>'special_package' = 'true';
