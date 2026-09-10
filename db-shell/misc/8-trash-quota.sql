-- 8 — Copten geri almada kota kontrolu (Excel madde 2.17'nin devami)
--
-- Baglam: bu surumle birlikte cope atilan medya kotadan DUSUYOR (limits.go,
-- Event_storage_bytes + Guest_upload_usage artik trashed_at IS NULL suzuyor). Kota artik
-- host'un galeride gordugu icerigi olcuyor.
--
-- Acilan delik: kota cope atinca bosaldigi icin
--     cope at -> yenisini yukle -> copten geri al
-- dizisi paketin limitini kalici olarak asmanin yolu olurdu. 5 GB'lik Mini pakette host
-- 5 GB'i cope atip 5 GB daha yukleyip sonra ilkini geri alabilir, sonuc 10 GB olurdu.
--
-- Neden kontrol BURADA, Go'da degil: copten geri alma backend'den gecmiyor. Trash sayfasi
-- (ukr-membox/src/pages/event-detail/trash.tsx) PostgREST'e dogrudan
--     PATCH /uploads?uid=eq.<uid>  {"trashed_at": null}
-- gonderiyor. Go tarafina hic ugramayan bir yolu Go'da korumak mumkun degil, o yuzden
-- kural veritabaninda duruyor.
--
-- Ne yapar: bir upload copten cikarilirken (trashed_at dolu -> NULL) etkinligin paket
-- limitlerini yeniden hesaplar ve limit asilacaksa UPDATE'i reddeder. Cope ATMA yonu
-- (NULL -> dolu) hicbir zaman engellenmez; kalici silme de engellenmez.
--
-- Hangi limitler: yalnizca ETKINLIK bazli olanlar, yani media_count ve storage_gb.
-- Misafir bazli limitler (guest_media_count, guest_storage_gb) bilerek disarida: onlar tek
-- bir misafirin tek seferde tasmasini engelleyen adil kullanim siniri, host'un kendi
-- arsivini geri almasini engellemeleri anlamsiz olurdu.
--
-- Sayma kurallari limits.go ile birebir ayni olmak zorunda:
--   media_count  -> photo + video, trashed_at IS NULL
--   storage_gb   -> photo + video + voice, trashed_at IS NULL, SUM(size_bytes)
--   -1 ya da tanimsiz = sinirsiz
--
-- Hata mesaji: PostgREST P0001'i HTTP 400'e cevirir ve govdeye {"message": ...} koyar;
-- frontend fetch sarmalayicisi bu mesaji firlatir. Mesaj makine tarafindan okunabilsin diye
-- RESTORE_QUOTA_EXCEEDED onekiyle basliyor.
--
-- CANLI VERIYE YAZAR (yalnizca fonksiyon + trigger; satir verisine dokunmaz).

BEGIN;

CREATE OR REPLACE FUNCTION check_upload_restore_quota()
RETURNS TRIGGER AS $$
DECLARE
    v_media_limit   NUMERIC;
    v_storage_limit NUMERIC;
    v_media_used    BIGINT;
    v_storage_used  NUMERIC;
    v_adds_media    BOOLEAN;
    v_adds_bytes    BOOLEAN;
BEGIN
    -- Yalnizca geri alma yonu. Cope atma ve diger UPDATE'ler dokunulmadan gecer.
    IF NOT (OLD.trashed_at IS NOT NULL AND NEW.trashed_at IS NULL) THEN
        RETURN NEW;
    END IF;

    -- Metin (guestbook) kayitlari hicbir kotaya girmez.
    IF NEW.upload_type NOT IN ('photo', 'video', 'voice') THEN
        RETURN NEW;
    END IF;

    v_adds_media := NEW.upload_type IN ('photo', 'video');
    v_adds_bytes := TRUE;

    -- Etkinligin core paketindeki limitler. Join, limits.go'daki Event_option_number ile ayni.
    SELECT (p.options->>'media_count')::NUMERIC,
           (p.options->>'storage_gb')::NUMERIC
      INTO v_media_limit, v_storage_limit
      FROM events e
      JOIN purchases pu  ON e.purchase_uid = pu.uid
      JOIN cart_items ci ON pu.cart_uid = ci.cart_uid
      JOIN products p    ON ci.product_uid = p.uid AND p.is_add_on = FALSE
     WHERE e.uid = NEW.event_uid
       AND e.deleted_at IS NULL
     LIMIT 1;

    -- Paket bulunamadi ya da deger sayiya cevrilemedi -> sinirsiz say, engelleme.
    -- Eski paketlerde bu anahtarlar hic yok; limits.go da ayni sekilde davraniyor.

    IF v_adds_media AND v_media_limit IS NOT NULL AND v_media_limit >= 0 THEN
        SELECT COUNT(*) INTO v_media_used
          FROM uploads
         WHERE event_uid = NEW.event_uid
           AND upload_type IN ('photo', 'video')
           AND trashed_at IS NULL;

        IF v_media_used + 1 > v_media_limit THEN
            RAISE EXCEPTION
                'RESTORE_QUOTA_EXCEEDED: restoring this file would exceed the event media limit (% of % files already in the gallery)',
                v_media_used, v_media_limit;
        END IF;
    END IF;

    IF v_adds_bytes AND v_storage_limit IS NOT NULL AND v_storage_limit >= 0 THEN
        SELECT COALESCE(SUM(size_bytes), 0) INTO v_storage_used
          FROM uploads
         WHERE event_uid = NEW.event_uid
           AND upload_type IN ('photo', 'video', 'voice')
           AND trashed_at IS NULL;

        -- 1 GB = 1024^3, limits.go'daki bytesPerGB ile ayni.
        IF v_storage_used + COALESCE(NEW.size_bytes, 0) > v_storage_limit * 1073741824 THEN
            RAISE EXCEPTION
                'RESTORE_QUOTA_EXCEEDED: restoring this file would exceed the event storage limit (% GB)',
                v_storage_limit;
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql
   SECURITY DEFINER
   SET search_path = public, pg_temp;

-- SECURITY DEFINER sebebi: geri almayi yapan rol (auth) products/purchases/cart_items
-- satirlarini RLS yuzunden gormeyebilir. Goremezse limit NULL kalir ve kontrol sessizce
-- "sinirsiz" diye gecerdi, yani koruma hic calismazdi. Fonksiyon yalnizca limitleri okur,
-- disaridan parametre almaz ve hicbir sey yazmaz.

DROP TRIGGER IF EXISTS uploads_restore_quota ON uploads;
CREATE TRIGGER uploads_restore_quota
    BEFORE UPDATE OF trashed_at ON uploads
    FOR EACH ROW EXECUTE FUNCTION check_upload_restore_quota();

COMMIT;

-- PostgREST reload gerekmez: yeni tablo/kolon yok.

-- Dogrulama (canlida, gercek bir upload'u bozmadan):
--   BEGIN;
--   -- limiti gecici olarak 0 GB yapip bir geri almayi denemek:
--   UPDATE products SET options = options || '{"storage_gb": 0}'::jsonb
--     WHERE is_add_on = FALSE AND id = 'standard';
--   UPDATE uploads SET trashed_at = NULL WHERE uid = '<copteki-bir-upload-uid>';
--   -- RESTORE_QUOTA_EXCEEDED bekleniyor
--   ROLLBACK;

-- Geri alma:
--   DROP TRIGGER IF EXISTS uploads_restore_quota ON uploads;
--   DROP FUNCTION IF EXISTS check_upload_restore_quota();
