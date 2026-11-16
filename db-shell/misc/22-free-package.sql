-- 22 — Free paket (Fatih 13-14 Kasim 2026)
--
-- Fatih: 30 fotograf (etkinligin toplami, 14 Kasim 10:41), 1 hafta yukleme + 1 hafta indirme, misafir siniri yok,
--   Services & Prices'ta herkese acik, Ads by AddMoments, yukseltilebilir. 2 hafta sonunda bugunku akis
--   (kapanir, hatirlatma maili, dosyalar silinir) free icin de OK. E-posta basina bir free paket (backend 9b2c099).
--
-- Model:
--   * price 0: Purchase LiqPay acmaz, completeFreePurchase siparisi 'free' olarak kapatir (9b2c099).
--   * storage_days 7: ozel olmayan pakette yukleme bittikten SONRA sayilir -> toplam 14 gun.
--   * media_count 30 etkinlik toplami; guest_media_count 30 (tek misafir toplamdan fazlasini zaten yukleyemez).
--   * storage_gb 2: 30 foto/kisa video icin yeterli; R2 maliyetine ust sinir.
--   * granted_features {5}: reklam alani AddMoments'in (host kendi reklamini koyamaz), reklam grubu 'mini'
--     (Mini ile ayni reklam; admin istersen ayri grup acar).
--   * is_enabled FALSE: kart yerlesimi (Option A/B) 16 Kasim gorusmesinde konusulacak; o zamana kadar gorunmez,
--     satilamaz. Admin urun listesinden acilir.
--   * Yukseltme urunleri (free -> Mini/Classic/Premium) fiyatlari belli olunca ayri eklenir.
-- Tek transaction; COMMIT'ten once kendini dogrular. Tekrar calistirmak guvenli (ON CONFLICT DO NOTHING).

BEGIN;

INSERT INTO products (id, price, options, fullfillment_type, priority, is_add_on, granted_features, is_enabled,
                      display_name_en, display_name_uk, display_description_en, display_description_uk,
                      display_bullets_en, display_bullets_uk)
VALUES
    ('free', 0,
     '{"corePackage": true, "name_i18n": {"en": "Free", "uk": "Безкоштовний"},
       "activation_days": 7, "storage_days": 7, "storage_gb": 2,
       "guest_count": -1, "media_count": 30, "guest_media_count": 30, "guest_storage_gb": 2,
       "addmoments_ads_set": "mini"}',
     'digital', 0, FALSE, '{5}', FALSE,
     'FREE', 'БЕЗКОШТОВНИЙ', '', '',
     E'30 Pictures / Videos\n1 Week Upload Duration\n1 Week Storage Access\nUnlimited Guests\nGuests Can See Gallery\nAds by AddMoments',
     E'30 фото / відео\n1 тиждень для завантаження медіа\n1 тиждень доступу до медіа\nНеобмежена кількість гостей\nГості бачать галерею\nРеклама від AddMoments')
ON CONFLICT (id) DO NOTHING;

DO $$
DECLARE
    v_ok INT;
BEGIN
    SELECT COUNT(*) INTO v_ok FROM products
     WHERE id = 'free' AND price = 0 AND is_add_on = FALSE AND is_enabled = FALSE
       AND (options->>'media_count')::INT = 30 AND (options->>'activation_days')::INT = 7
       AND (options->>'storage_days')::INT = 7 AND (options->>'guest_count')::INT = -1
       AND package_storage_after_upload(options) = 7;
    RAISE NOTICE 'free_product=%', v_ok;
    IF v_ok <> 1 THEN
        RAISE EXCEPTION '22 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;

NOTIFY pgrst, 'reload schema';

-- Geri alma (siparis yoksa):  DELETE FROM products WHERE id = 'free' AND NOT EXISTS
--   (SELECT 1 FROM cart_items ci JOIN products p ON p.uid = ci.product_uid WHERE p.id = 'free');
