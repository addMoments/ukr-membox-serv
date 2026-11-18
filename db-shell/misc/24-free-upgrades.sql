-- 24 — Free paketten yukseltme (Fatih 18 Kasim 2026)
--
-- Fatih 14:23-14:25: free etkinligin panosunda kapagin altinda "Upgrade your free package" dugmesi; tiklayinca
--   "Upgrade your package" sayfasi, Events / School / Hero sekmeleri, paketler normal fiyatlariyla (Mini 790,
--   Classic 1790, Premium 2790 UAH). Yani free'den yukseltme = hedef paketin fiyati; etkinlik, link, QR ve
--   dosyalar ayni kalir.
--
-- Model 17 ve 20'deki gibi: yukseltme urunu products'ta (priority 0, is_add_on TRUE, upgrade_kind 'package',
--   upgrade_from 'free', upgrade_to hedef). Event_upgrade_options yalnizca gecerli paketi 'free' olan etkinlige
--   gosterir; normal /api/purchase yukseltme urununu reddeder. apply_event_upgrade tarihleri yeni paketin
--   gunleriyle yeniden hesaplar (asla kisaltmaz); limitler ve ozellikler event_core_product_uid'den gelir.
-- Fiyat bu dosya calistigi andaki hedef paketin fiyati; sonra admin > Urunler'den ayri ayri degistirilebilir
--   (hedef paketin fiyati degisirse yukseltme fiyati kendiliginden degismez).
-- Yalnizca var olan core paketler icin eklenir (ozel paketlerden biri silinmisse o yukseltme olmaz).
-- Tek transaction; COMMIT'ten once kendini dogrular. Tekrar calistirmak guvenli (ON CONFLICT DO NOTHING:
--   admin'in degistirdigi fiyat ezilmez).

BEGIN;

INSERT INTO products (id, price, options, fullfillment_type, priority, is_add_on, granted_features, is_enabled,
                      display_name_en, display_name_uk, display_description_en, display_description_uk)
SELECT 'upgrade_free_' || t.id,
       t.price,
       jsonb_build_object('upgrade_kind', 'package', 'upgrade_from', 'free', 'upgrade_to', t.id),
       'digital', 0, TRUE, '{}', TRUE,
       'Upgrade: FREE → ' || COALESCE(NULLIF(t.display_name_en, ''), upper(t.id)),
       'Перехід: БЕЗКОШТОВНИЙ → ' || COALESCE(NULLIF(t.display_name_uk, ''), NULLIF(t.display_name_en, ''), upper(t.id)),
       'Your event moves to the ' || COALESCE(NULLIF(t.display_name_en, ''), upper(t.id)) || ' package.',
       'Ваша подія переходить на пакет ' || COALESCE(NULLIF(t.display_name_uk, ''), NULLIF(t.display_name_en, ''), upper(t.id)) || '.'
  FROM products t
 WHERE t.id IN ('standard', 'plus', 'premium', 'special_class', 'special_grade', 'special_school', 'special_hero')
   AND t.is_add_on = FALSE
ON CONFLICT (id) DO NOTHING;

DO $$
DECLARE
    v_upgrades INT;
    v_targets  INT;
    v_core     INT;
    v_prices   INT;
BEGIN
    SELECT COUNT(*) INTO v_upgrades FROM products
     WHERE id LIKE 'upgrade_free_%' AND options->>'upgrade_kind' = 'package' AND options->>'upgrade_from' = 'free'
       AND is_add_on = TRUE;
    -- Event_upgrade_options ayni kosulu arar: hedef core paket olarak var.
    SELECT COUNT(*) INTO v_targets FROM products u
     WHERE u.id LIKE 'upgrade_free_%'
       AND EXISTS (SELECT 1 FROM products t WHERE t.id = u.options->>'upgrade_to' AND t.is_add_on = FALSE);
    SELECT COUNT(*) INTO v_core FROM products
     WHERE id IN ('standard', 'plus', 'premium', 'special_class', 'special_grade', 'special_school', 'special_hero')
       AND is_add_on = FALSE;
    -- Yeni eklenenler hedefin fiyatiyla basladi (ilk calistirmada hepsi; admin sonradan degistirdiyse az olabilir).
    SELECT COUNT(*) INTO v_prices FROM products u
      JOIN products t ON t.id = u.options->>'upgrade_to'
     WHERE u.id LIKE 'upgrade_free_%' AND u.price = t.price;
    RAISE NOTICE 'free_upgrades=% targets=% core_packages=% same_price=%', v_upgrades, v_targets, v_core, v_prices;
    IF v_upgrades <> v_core OR v_targets <> v_core OR v_core < 3
       OR NOT EXISTS (SELECT 1 FROM products WHERE id = 'free' AND is_add_on = FALSE) THEN
        RAISE EXCEPTION '24 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;

-- Geri alma (satilmis yukseltme yoksa):
--   DELETE FROM products u WHERE u.id LIKE 'upgrade_free_%' AND NOT EXISTS
--     (SELECT 1 FROM cart_items ci WHERE ci.product_uid = u.uid);
