-- Excel 2.1d (musteri yorumu 14 Ekim 2026: "Admin need to update activation date. This is not working yet.")
--
-- Ne: Admin paneli aktivasyondan SONRA da bir etkinligin activation_date'ini degistirebilsin.
-- Nasil: Kilit (events_enforce_dates) hostlar ve PostgREST yolu icin aynen duruyor. Yalnizca
--        backend'in admin ucu (PATCH /api/admin/events/{uid}) ayni transaction icinde
--        set_config('membox.admin_date_override', 'on', true) cagirirsa trigger izin verir.
--        PostgREST istemcileri bu ayari kuramaz: yalnizca request.* ayarlarini kendisi yazar ve
--        pg_catalog'daki set_config RPC olarak acik degil.
--        Tarih tasininca active_until / storage_until eskisi gibi paketten yeniden hesaplanir; ek olarak
--        tek seferlik depolama uzatmasi (storage_extended_at, +30 gun) korunur ve eski storage_until
--        icin gonderilmis "14 gun kaldi" uyarisi sifirlanir, yeni tarihe gore tekrar gonderilsin.
-- Neden: 4 Eylul'de kilit "canli bir etkinligin suresini kazara degistirme" riskine karsi birakilmisti;
--        musteri admin icin bunu acikca istiyor. Risk adminle sinirli ve ekran degisiklikten once
--        yeni bitis tarihlerini gosterip onay istiyor.
--
-- Geriye uyumlu: eski backend ayari hic kurmadigi icin bu dosya backend'den ONCE calistirilir.
-- Geri alma: dosyanin sonundaki eski govdeyle CREATE OR REPLACE.

BEGIN;

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
        v_activation_days := get_purchase_activation_days(NEW.purchase_uid);
        NEW.active_until  := NEW.activation_date + make_interval(days => v_activation_days);

        v_storage_days    := get_purchase_storage_days(NEW.purchase_uid);
        NEW.storage_until := NEW.active_until + make_interval(days => v_storage_days);

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

COMMIT;

-- Eski govde (geri alma icin, 2026-10-16'ya kadar canlida olan):
--
-- CREATE OR REPLACE FUNCTION public.enforce_event_dates() RETURNS trigger
--     LANGUAGE plpgsql
--     AS $$
-- DECLARE
--     v_activation_days INT;
--     v_storage_days    INT;
-- BEGIN
--     IF OLD.activation_date <= NOW() THEN
--         IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
--             RAISE EXCEPTION 'Cannot modify activation_date after the event has been activated';
--         END IF;
--     END IF;
--
--     IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
--         v_activation_days := get_purchase_activation_days(NEW.purchase_uid);
--         NEW.active_until  := NEW.activation_date + make_interval(days => v_activation_days);
--
--         v_storage_days    := get_purchase_storage_days(NEW.purchase_uid);
--         NEW.storage_until := NEW.active_until + make_interval(days => v_storage_days);
--     END IF;
--
--     RETURN NEW;
-- END;
-- $$;
