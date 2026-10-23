-- 17 — Host panelinden paket yukseltme ve ek depolama (Excel 2.18; 2026-10-22)
--
-- Musteri 22 Ekim: "Client can upgrade from his host panel"
--   Mini -> Classic 1000 UAH, Classic -> Premium 1000 UAH, Mini -> Premium 2000 UAH,
--   Premium: +6 ay galeri saklama 1000 UAH, Premium: +25 GB 1000 UAH;
--   fiyat ve GB/ay admin urun listesinden duzenlenebilir.
--
-- Model:
--   * Yukseltmeler products tablosunda bes urun (priority 0 = Services & Prices'ta gorunmez,
--     is_add_on TRUE = core paket sorgularina karismaz). Tur options'ta:
--       upgrade_kind 'package' + upgrade_from / upgrade_to  (paket id'leri)
--       upgrade_kind 'storage' + upgrade_for + extra_storage_gb
--       upgrade_kind 'time'    + upgrade_for + extra_storage_months
--   * Yukseltme siparisi mevcut etkinlige baglidir: purchases.event_uid. Normal siparislerde NULL.
--   * Odeme dogrulaninca apply_event_upgrade() tek transaction'da: sepet 'purchased',
--     purchases.upgrade_applied_at, etkinlik tarihleri. Admin "yeniden dene" de ayni fonksiyonu
--     cagirir; ikinci cagri hicbir sey yapmaz.
--   * Etkinligin gecerli paketi tek yerden: event_core_product_uid(). Uygulanmis bir paket
--     yukseltmesi varsa hedef paket (birden fazlaysa en pahalisi), yoksa ilk siparisteki paket.
--     Ek GB event_extra_storage_gb(), ek ay event_extra_storage_months(). Go (event_tier.go,
--     limits.go, features.go) ve asagidaki trigger'lar hep bunlari okur.
--
-- SIRA: bu dosya -> backend deploy -> frontend deploy. Eski backend bu dosyadan sonra da calisir
-- (yeni kolonlar NULL, yeni urunler satilamaz cunku eski backend'de yukseltme ucu yok).
-- Tek transaction; sonunda NOTIFY pgrst (purchases'a kolon eklendi).

BEGIN;

-- ============================================================================
-- 1. SIPARIS <-> ETKINLIK
-- ============================================================================

ALTER TABLE purchases ADD COLUMN IF NOT EXISTS event_uid UUID REFERENCES events(uid);
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS upgrade_applied_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS purchases_event_uid_idx ON purchases (event_uid) WHERE event_uid IS NOT NULL;

-- ============================================================================
-- 2. URUNLER (fiyat/GB/ay admin panelinden degisebilir; ON CONFLICT ile tekrar calistirma guvenli)
-- ============================================================================

INSERT INTO products (id, price, options, fullfillment_type, priority, is_add_on, granted_features, is_enabled,
                      display_name_en, display_name_uk, display_description_en, display_description_uk)
VALUES
    ('upgrade_standard_plus', 1000,
     '{"upgrade_kind": "package", "upgrade_from": "standard", "upgrade_to": "plus"}',
     'digital', 0, TRUE, '{}', TRUE,
     'Upgrade: MINI → CLASSIC', 'Перехід: МІНІ → КЛАСИЧНИЙ',
     'Your event moves to the CLASSIC package.', 'Ваша подія переходить на пакет КЛАСИЧНИЙ.'),
    ('upgrade_plus_premium', 1000,
     '{"upgrade_kind": "package", "upgrade_from": "plus", "upgrade_to": "premium"}',
     'digital', 0, TRUE, '{}', TRUE,
     'Upgrade: CLASSIC → PREMIUM', 'Перехід: КЛАСИЧНИЙ → ПРЕМІУМ',
     'Your event moves to the PREMIUM package.', 'Ваша подія переходить на пакет ПРЕМІУМ.'),
    ('upgrade_standard_premium', 2000,
     '{"upgrade_kind": "package", "upgrade_from": "standard", "upgrade_to": "premium"}',
     'digital', 0, TRUE, '{}', TRUE,
     'Upgrade: MINI → PREMIUM', 'Перехід: МІНІ → ПРЕМІУМ',
     'Your event moves to the PREMIUM package.', 'Ваша подія переходить на пакет ПРЕМІУМ.'),
    ('premium_extra_months', 1000,
     '{"upgrade_kind": "time", "upgrade_for": "premium", "extra_storage_months": 6}',
     'digital', 0, TRUE, '{}', TRUE,
     'PREMIUM: +6 months of gallery storage', 'ПРЕМІУМ: +6 місяців зберігання галереї',
     'Your gallery is kept 6 months longer.', 'Ваша галерея зберігається на 6 місяців довше.'),
    ('premium_extra_storage', 1000,
     '{"upgrade_kind": "storage", "upgrade_for": "premium", "extra_storage_gb": 25}',
     'digital', 0, TRUE, '{}', TRUE,
     'PREMIUM: +25 GB of storage', 'ПРЕМІУМ: +25 ГБ сховища',
     'Your event gets 25 GB more storage.', 'Ваша подія отримує на 25 ГБ більше сховища.')
ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- 3. GECERLI PAKET VE EKLER
-- ============================================================================

-- Uygulanmis yukseltme kalemleri (etkinlik basina). Yardimci; asagidaki fonksiyonlar okur.
CREATE OR REPLACE FUNCTION event_applied_upgrade_items(p_event_uid UUID)
RETURNS TABLE (product_id TEXT, options JSONB, quantity INT, applied_at TIMESTAMPTZ)
LANGUAGE sql STABLE AS $$
    SELECT p.id, p.options, ci.quantity, pu.upgrade_applied_at
      FROM purchases pu
      JOIN cart_items ci ON ci.cart_uid = pu.cart_uid
      JOIN products p    ON p.uid = ci.product_uid
     WHERE pu.event_uid = p_event_uid
       AND pu.upgrade_applied_at IS NOT NULL
       AND p.options ? 'upgrade_kind';
$$;

-- Etkinligin gecerli core paketi: uygulanmis paket yukseltmelerinin hedefleri (en pahalisi,
-- esitse en son uygulanan), yoksa ilk siparisteki paket (is_add_on = FALSE, Event_limit ile ayni).
CREATE OR REPLACE FUNCTION event_core_product_uid(p_event_uid UUID)
RETURNS UUID
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT tp.uid
           FROM event_applied_upgrade_items(p_event_uid) u
           JOIN products tp ON tp.id = u.options->>'upgrade_to' AND tp.is_add_on = FALSE
          WHERE u.options->>'upgrade_kind' = 'package'
          ORDER BY tp.price DESC, u.applied_at DESC
          LIMIT 1),
        (SELECT p.uid
           FROM events e
           JOIN purchases pu  ON pu.uid = e.purchase_uid
           JOIN cart_items ci ON ci.cart_uid = pu.cart_uid
           JOIN products p    ON p.uid = ci.product_uid AND p.is_add_on = FALSE
          WHERE e.uid = p_event_uid
          ORDER BY ci.created_at ASC
          LIMIT 1)
    );
$$;

CREATE OR REPLACE FUNCTION event_extra_storage_gb(p_event_uid UUID)
RETURNS NUMERIC
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(SUM(NULLIF(u.options->>'extra_storage_gb', '')::NUMERIC * u.quantity), 0)
      FROM event_applied_upgrade_items(p_event_uid) u
     WHERE u.options->>'upgrade_kind' = 'storage';
$$;

CREATE OR REPLACE FUNCTION event_extra_storage_months(p_event_uid UUID)
RETURNS INT
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(SUM(NULLIF(u.options->>'extra_storage_months', '')::INT * u.quantity), 0)::INT
      FROM event_applied_upgrade_items(p_event_uid) u
     WHERE u.options->>'upgrade_kind' = 'time';
$$;

-- get_purchase_activation_days / get_purchase_storage_days'in etkinlik karsiliklari:
-- yukseltilmis paketin gunleri. Varsayilan 14 gun, eskileriyle ayni.
CREATE OR REPLACE FUNCTION get_event_activation_days(p_event_uid UUID)
RETURNS INT
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT NULLIF(p.options->>'activation_days', '')::INT
           FROM products p WHERE p.uid = event_core_product_uid(p_event_uid)),
        14);
$$;

CREATE OR REPLACE FUNCTION get_event_storage_days(p_event_uid UUID)
RETURNS INT
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT NULLIF(p.options->>'storage_days', '')::INT
           FROM products p WHERE p.uid = event_core_product_uid(p_event_uid)),
        14);
$$;

-- ============================================================================
-- 4. YUKSELTMEYI UYGULA (odeme dogrulaninca; admin "yeniden dene" de bunu cagirir)
-- ============================================================================

-- Tarih kurali:
--   paket: yukleme ve galeri suresi yeni paketin gunleriyle yeniden hesaplanir, hicbir zaman
--          kisalmaz (GREATEST). Tek seferlik +30 gun ve alinmis ek aylar korunur.
--   time : galeri bitis tarihine ay eklenir.
--   storage: tarih degismez (ek GB event_extra_storage_gb ile okunur).
-- activation_date bossa (host tarihi henuz secmedi) tarihlere dokunulmaz; tarih secilince
-- enforce_event_dates yukseltilmis paketle hesaplar.
CREATE OR REPLACE FUNCTION apply_event_upgrade(p_purchase_uid UUID)
RETURNS UUID
LANGUAGE plpgsql AS $$
DECLARE
    v_event_uid     UUID;
    v_cart_uid      UUID;
    v_applied_at    TIMESTAMPTZ;
    v_items         INT;
    v_has_package   BOOLEAN;
    v_months        INT;
    v_activation    TIMESTAMPTZ;
    v_active_until  TIMESTAMPTZ;
    v_storage_until TIMESTAMPTZ;
    v_extended_at   TIMESTAMPTZ;
    v_new_active    TIMESTAMPTZ;
    v_new_storage   TIMESTAMPTZ;
BEGIN
    SELECT event_uid, cart_uid, upgrade_applied_at
      INTO v_event_uid, v_cart_uid, v_applied_at
      FROM purchases WHERE uid = p_purchase_uid
       FOR UPDATE;
    IF NOT FOUND OR v_event_uid IS NULL THEN
        RAISE EXCEPTION 'NOT_AN_UPGRADE: purchase % is not an event upgrade', p_purchase_uid;
    END IF;
    IF v_applied_at IS NOT NULL THEN
        RETURN v_event_uid;
    END IF;

    -- Bir sipariste ya tek paket kalemi ya da storage/time kalemleri (adetli) olur (upgrade.go).
    -- Ay sayisi tum time kalemlerinin toplami.
    SELECT COUNT(*),
           COALESCE(bool_or(p.options->>'upgrade_kind' = 'package'), FALSE),
           COALESCE(SUM(CASE WHEN p.options->>'upgrade_kind' = 'time'
                             THEN COALESCE(NULLIF(p.options->>'extra_storage_months', '')::INT, 0) * ci.quantity
                        END), 0)::INT
      INTO v_items, v_has_package, v_months
      FROM cart_items ci JOIN products p ON p.uid = ci.product_uid
     WHERE ci.cart_uid = v_cart_uid AND p.options ? 'upgrade_kind';
    IF v_items = 0 THEN
        RAISE EXCEPTION 'NOT_AN_UPGRADE: purchase % has no upgrade item', p_purchase_uid;
    END IF;

    UPDATE cart_items SET status = 'purchased' WHERE cart_uid = v_cart_uid;
    UPDATE purchases SET upgrade_applied_at = NOW() WHERE uid = p_purchase_uid;

    SELECT activation_date, active_until, storage_until, storage_extended_at
      INTO v_activation, v_active_until, v_storage_until, v_extended_at
      FROM events WHERE uid = v_event_uid
       FOR UPDATE;
    IF v_activation IS NULL OR v_active_until IS NULL OR v_storage_until IS NULL THEN
        RETURN v_event_uid;
    END IF;

    v_new_active  := v_active_until;
    v_new_storage := v_storage_until;
    IF v_has_package THEN
        -- upgrade_applied_at yukarida yazildi: get_event_* artik yeni paketi okur.
        v_new_active  := GREATEST(v_active_until,
                                  v_activation + make_interval(days => get_event_activation_days(v_event_uid)));
        v_new_storage := GREATEST(v_storage_until,
                                  v_new_active
                                  + make_interval(days => get_event_storage_days(v_event_uid))
                                  + CASE WHEN v_extended_at IS NOT NULL THEN INTERVAL '30 days' ELSE INTERVAL '0' END
                                  + make_interval(months => event_extra_storage_months(v_event_uid)));
    ELSIF v_months > 0 THEN
        v_new_storage := v_storage_until + make_interval(months => v_months);
    END IF;

    IF v_new_active IS DISTINCT FROM v_active_until OR v_new_storage IS DISTINCT FROM v_storage_until THEN
        UPDATE events
           SET active_until  = v_new_active,
               storage_until = v_new_storage,
               -- "14 gun icinde silinecek" uyarisi eski tarihe gore gitmis olabilir.
               storage_warning_mail_sent_at = CASE WHEN v_new_storage IS DISTINCT FROM v_storage_until
                                                   THEN NULL ELSE storage_warning_mail_sent_at END
         WHERE uid = v_event_uid;
    END IF;

    RETURN v_event_uid;
END;
$$;

-- ============================================================================
-- 5. TRIGGER'LAR — gecerli paket ve ekler
-- ============================================================================

-- 13-admin-activation-date.sql'deki govde; tek fark gunlerin etkinligin gecerli paketinden
-- (get_event_*) gelmesi ve alinmis ek aylarin eklenmesi. INSERT trigger'i degismedi: yeni
-- etkinligin yukseltmesi olamaz.
CREATE OR REPLACE FUNCTION public.enforce_event_dates() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_activation_days INT;
    v_storage_days    INT;
BEGIN
    -- Aktivasyon sonrasi tarih degistirilemez (kullanici icerik yuklemis olabilir);
    -- yalnizca admin ucu transaction icinde acik bir istisna kurabilir.
    IF OLD.activation_date <= NOW()
       AND COALESCE(current_setting('membox.admin_date_override', true), '') <> 'on' THEN
        IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
            RAISE EXCEPTION 'Cannot modify activation_date after the event has been activated';
        END IF;
    END IF;
    -- activation_date degisince hem active_until hem storage_until pakete gore yeniden hesaplanir.
    IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
        v_activation_days := get_event_activation_days(NEW.uid);
        NEW.active_until  := NEW.activation_date + make_interval(days => v_activation_days);
        v_storage_days    := get_event_storage_days(NEW.uid);
        NEW.storage_until := NEW.active_until + make_interval(days => v_storage_days)
                             + make_interval(months => event_extra_storage_months(NEW.uid));
        -- Tek seferlik depolama uzatmasi kullanildiysa (event.go ExtendStorage, +30 gun) korunur.
        IF NEW.storage_extended_at IS NOT NULL THEN
            NEW.storage_until := NEW.storage_until + interval '30 days';
        END IF;
        -- "Etkinliginiz 14 gun icinde silinecek" uyarisi eski tarihe gore gitmis olabilir.
        IF NEW.storage_until IS DISTINCT FROM OLD.storage_until THEN
            NEW.storage_warning_mail_sent_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- 12-upload-received.sql'deki govde; fark: paket event_core_product_uid'den, depolama
-- limitine alinmis ek GB eklenir (limits.go Event_option_number ile ayni kural).
CREATE OR REPLACE FUNCTION public.check_upload_restore_quota()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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

    -- Etkinligin gecerli paketindeki limitler (yukseltme dahil). limits.go ile ayni.
    SELECT (p.options->>'media_count')::NUMERIC,
           (p.options->>'storage_gb')::NUMERIC
      INTO v_media_limit, v_storage_limit
      FROM events e
      JOIN products p ON p.uid = event_core_product_uid(e.uid)
     WHERE e.uid = NEW.event_uid
       AND e.deleted_at IS NULL
     LIMIT 1;

    -- Sinirli depolamaya alinmis ek GB eklenir; sinirsiz (-1) sinirsiz kalir.
    IF v_storage_limit IS NOT NULL AND v_storage_limit >= 0 THEN
        v_storage_limit := v_storage_limit + event_extra_storage_gb(NEW.event_uid);
    END IF;

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
$function$;

-- ============================================================================
-- 6. DOGRULAMA (COMMIT oncesi goz at)
-- ============================================================================

SELECT
    (SELECT COUNT(*) FROM products WHERE options ? 'upgrade_kind')                      AS upgrade_products,
    (SELECT COUNT(*) FROM information_schema.columns
      WHERE table_name = 'purchases' AND column_name IN ('event_uid', 'upgrade_applied_at')) AS new_columns,
    -- Hic yukseltme yokken gecerli paket = ilk siparisteki paket olmali (0 fark).
    (SELECT COUNT(*) FROM events e
      WHERE e.deleted_at IS NULL AND e.purchase_uid IS NOT NULL
        AND event_core_product_uid(e.uid) IS DISTINCT FROM (
            SELECT p.uid FROM purchases pu
              JOIN cart_items ci ON ci.cart_uid = pu.cart_uid
              JOIN products p ON p.uid = ci.product_uid AND p.is_add_on = FALSE
             WHERE pu.uid = e.purchase_uid
             ORDER BY ci.created_at ASC LIMIT 1))                                        AS package_mismatches;
-- upgrade_products = 5, new_columns = 2, package_mismatches = 0 olmali.

-- Ayni kontrol + yeni gun fonksiyonlari eskileriyle ayni sonucu vermeli (hic yukseltme yok).
-- Biri tutmazsa hata -> ON_ERROR_STOP ile transaction geri alinir, canliya hicbir sey yazilmaz.
DO $$
DECLARE
    v_products   INT;
    v_columns    INT;
    v_package    INT;
    v_days       INT;
BEGIN
    SELECT COUNT(*) INTO v_products FROM products WHERE options ? 'upgrade_kind';
    SELECT COUNT(*) INTO v_columns FROM information_schema.columns
     WHERE table_name = 'purchases' AND column_name IN ('event_uid', 'upgrade_applied_at');
    SELECT COUNT(*) INTO v_package FROM events e
     WHERE e.deleted_at IS NULL AND e.purchase_uid IS NOT NULL
       AND event_core_product_uid(e.uid) IS DISTINCT FROM (
           SELECT p.uid FROM purchases pu
             JOIN cart_items ci ON ci.cart_uid = pu.cart_uid
             JOIN products p ON p.uid = ci.product_uid AND p.is_add_on = FALSE
            WHERE pu.uid = e.purchase_uid
            ORDER BY ci.created_at ASC LIMIT 1);
    SELECT COUNT(*) INTO v_days FROM events e
     WHERE e.deleted_at IS NULL AND e.purchase_uid IS NOT NULL
       AND (get_event_activation_days(e.uid) IS DISTINCT FROM get_purchase_activation_days(e.purchase_uid)
            OR get_event_storage_days(e.uid) IS DISTINCT FROM get_purchase_storage_days(e.purchase_uid));
    RAISE NOTICE 'upgrade_products=% new_columns=% package_mismatches=% day_mismatches=%',
        v_products, v_columns, v_package, v_days;
    IF v_products <> 5 OR v_columns <> 2 OR v_package <> 0 OR v_days <> 0 THEN
        RAISE EXCEPTION '17 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;

NOTIFY pgrst, 'reload schema';

-- ============================================================================
-- GERI ALMA (once backend eski surume)
-- ============================================================================
-- enforce_event_dates: 13-admin-activation-date.sql'deki govde; check_upload_restore_quota:
-- 12-upload-received.sql'deki govde. Sonra:
--   DROP FUNCTION apply_event_upgrade(UUID), get_event_storage_days(UUID), get_event_activation_days(UUID),
--     event_extra_storage_months(UUID), event_extra_storage_gb(UUID), event_core_product_uid(UUID),
--     event_applied_upgrade_items(UUID);
--   Satilmis yukseltme yoksa: DELETE FROM products WHERE options ? 'upgrade_kind';
--   ALTER TABLE purchases DROP COLUMN upgrade_applied_at, DROP COLUMN event_uid;
