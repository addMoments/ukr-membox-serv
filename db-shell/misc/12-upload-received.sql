-- 12 — Yukleme, dosya S3'e gercekten ulasinca gorunur (2026-09-25).
--
-- Belirti: "Подорож 6-А класу у Кам'янець-Подільський" etkinliginde misafirler otobusten, mobil
-- agla yukledi; fotograflarin bir kismi galeride gri kutu olarak kaldi. 15 medya satirindan
-- 4'unun S3'te dosyasi yoktu.
-- Kok neden: routes/upload.go satiri presign aninda yaziyor; tarayicidan S3'e giden PUT yarida
-- kalirsa satir kaliyor ve galeri olmayan dosyayi gosteriyor. Canlida medya satirlarinin %11'inin
-- dosyasi yok; ayrica bir iPhone misafirinin 100 dosyasi 0 bayt olarak gitmis (21 Eylul).
--
-- Ne yapiyor:
--   1. uploads.received_at: dosyanin S3'te dogrulandigi an; NULL = henuz dogrulanmadi.
--      Varsayilan now(): mevcut satirlar ve PostgREST'ten dogrudan yazilan guestbook metinleri
--      gorunur kalir. Yalnizca membox-serv'in presign'da yazdigi medya NULL baslar ve
--      /api/guest/upload/{event}/confirm ya da dakikalik tarama (upload_receipt) onu doldurur.
--   2. Kolon bazli GRANT: auth/webanon kolonu yalnizca OKUR. select=* her kolonu istedigi icin
--      SELECT sart (yoksa tum galeri sorgulari 42501 verir); INSERT/UPDATE bilerek yok, misafir
--      kendi satirini "ulasti" diye isaretleyemez.
--   3. RESTRICTIVE politika: PostgREST rolleri received_at NULL satiri hic gormez. membox-serv
--      tablo sahibi oldugu icin RLS'ten muaf; kendi sorgulari filtreyi acikca yaziyor.
--   4. check_upload_restore_quota: copten geri alma da Go'daki kota kuraliyla ayni satirlari sayar
--      (db_scripts.UploadCountsSQL). Dogrulanmamis satir yalnizca ilk bir saat -- yukleme suruyor
--      olabilir -- kotaya girer; sonrasinda yarim kalmis sayilir ve kimseyi engellemez.
--
-- Sira: bu dosya backend'den ONCE. Tek basina hicbir seyi gizlemez (tum satirlar dolu baslar).
-- Eski satirlardan dosyasi olmayanlari gizlemek ayri adim: upload-received-migrate.sh b.

BEGIN;

ALTER TABLE uploads ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ DEFAULT now();

GRANT SELECT (received_at) ON uploads TO auth, webanon;

DROP POLICY IF EXISTS uploads_received_only ON uploads;
CREATE POLICY uploads_received_only ON uploads
    AS RESTRICTIVE
    FOR SELECT
    TO auth, webanon
    USING (received_at IS NOT NULL);

-- Govde canlidaki 8-trash-quota.sql surumuyle ayni; yalnizca iki sayimin WHERE'ine
-- received_at kosulu eklendi.
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
           AND trashed_at IS NULL
           AND (received_at IS NOT NULL OR created_at > LOCALTIMESTAMP - INTERVAL '1 hour');

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
           AND trashed_at IS NULL
           AND (received_at IS NOT NULL OR created_at > LOCALTIMESTAMP - INTERVAL '1 hour');

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

-- Dakikalik tarama "dogrulanmamis ve son 24 saatte yazilmis" satirlari okur.
CREATE INDEX IF NOT EXISTS uploads_pending_idx ON uploads (created_at) WHERE received_at IS NULL;

COMMIT;

-- Yeni kolon: PostgREST sema onbellegi yenilenmezse select=* yanitlari eski kolon listesiyle kalir.
NOTIFY pgrst, 'reload schema';

-- Dogrulama:
--   SELECT COUNT(*) FILTER (WHERE received_at IS NULL) AS hidden, COUNT(*) AS total FROM uploads;
--   -- 12'den hemen sonra hidden = 0 olmali.
--
-- Geri alma (once backend'i onceki surume dondur; yoksa yeni yuklemeler NULL kalir ve gorunmez):
--   BEGIN;
--   DROP POLICY IF EXISTS uploads_received_only ON uploads;
--   REVOKE SELECT (received_at) ON uploads FROM auth, webanon;
--   DROP INDEX IF EXISTS uploads_pending_idx;
--   ALTER TABLE uploads DROP COLUMN IF EXISTS received_at;
--   -- ve 8-trash-quota.sql'deki check_upload_restore_quota govdesini yeniden calistir.
--   COMMIT;
--   NOTIFY pgrst, 'reload schema';
