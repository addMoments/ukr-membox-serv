-- 25 — Yukleme ve saklama ayni gun baslar (musteri 19 Kasim 2026, 12:22)
--
-- Istek 2.5: "Calculation of upload and storage time both start on the same date. Say 1 November would serve as
--   beginning of upload and beginning of storage. Accordingly, the 'death' of package will be Nov 7. In all other
--   packages the same logic is applicable."
--
-- Ozel paketlerde (18-special-packages.sql) bu kural zaten options.storage_from_activation ile calisiyor:
--   storage_days aktivasyondan itibaren TOPLAM sure, galeri yukleme bitince degil storage_days dolunca kapanir.
--   Bu dosya ayni bayragi free, Mini (standard), Classic (plus) ve Premium'a acar. Trigger ve gun fonksiyonlari
--   degismez (package_storage_after_upload bayragi zaten okuyor).
--
-- Etki (yalniz bundan sonra tarih hesaplanan etkinliklerde):
--   free    7 + 7   ->  7 gun toplam (aktivasyon + 7)
--   standard 30 + 31  ->  31 gun toplam
--   plus    31 + 90  ->  90 gun toplam
--   premium 31 + 180 -> 180 gun toplam
-- Var olan etkinliklerin tarihleri DEGISMEZ: trigger yalnizca INSERT'te ve activation_date degisince hesaplar,
--   apply_event_upgrade GREATEST ile asla kisaltmaz. Bu dosya events tablosuna dokunmaz.
--
-- Kosul: bayrak aciksa storage_days >= activation_days olmali (yoksa galeri yukleme bitmeden kapanir; admin
--   urun ekrani ve product.go da bunu reddeder). Dogrulama tutmazsa geri alinir.
--
-- Geri alma: UPDATE products SET options = options - 'storage_from_activation'
--            WHERE id IN ('free','standard','plus','premium');

BEGIN;

UPDATE products
   SET options = options || '{"storage_from_activation": true}'::jsonb
 WHERE id IN ('free', 'standard', 'plus', 'premium')
   AND is_add_on = FALSE
   AND COALESCE((options->>'storage_from_activation')::BOOLEAN, FALSE) = FALSE;

DO $$
DECLARE
    v_flagged INT;
    v_bad     INT;
    v_free    INT;
    v_events  INT;
BEGIN
    SELECT COUNT(*) INTO v_flagged FROM products
     WHERE id IN ('free', 'standard', 'plus', 'premium')
       AND (options->>'storage_from_activation')::BOOLEAN;
    -- Toplam sure yukleme suresinden kisa olamaz.
    SELECT COUNT(*) INTO v_bad FROM products
     WHERE COALESCE((options->>'storage_from_activation')::BOOLEAN, FALSE)
       AND is_add_on = FALSE
       AND NULLIF(options->>'storage_days', '')::INT < COALESCE(NULLIF(options->>'activation_days', '')::INT, 0);
    -- free: yukleme bitince saklama da biter (7 - 7 = 0 gun yukleme sonrasi).
    SELECT package_storage_after_upload(options) INTO v_free FROM products WHERE id = 'free';
    SELECT COUNT(*) INTO v_events FROM events WHERE deleted_at IS NULL;
    RAISE NOTICE 'flagged=% bad=% free_after_upload=% live_events_untouched=%', v_flagged, v_bad, v_free, v_events;
    IF v_flagged <> 4 OR v_bad <> 0 OR v_free IS DISTINCT FROM 0 THEN
        RAISE EXCEPTION '25 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;
