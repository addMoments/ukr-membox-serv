-- 23 — Demo etkinligi (Fatih 14 Kasim 2026, 15:14)
--
-- Fatih: "https://addmoments.com.ua/l/qdfKM38bpSv-KvP1rDjMvzA  We can make this event as demo. Link is always
--   for upload and download. Limits can be: - Uploaded photos except us will be deleted after 1 hour.
--   - Cant upload more than 30 photos."
-- Etkinlik: 75f28cdf-c6e9-4aff-8abc-fd6b0e332fcc "DEMO", partnership@addmoments.com.ua, Premium.
--
-- Bu dosya:
--   * site_settings 'demo_events' = [uid]: backend (demo.go) bu listeye bakar; misafir yuklemelerini bir saat sonra
--     siler, sahip olmayan ziyaretciyi 30 dosyayla sinirlar, public-features'a 6 (demo) ekler. Anahtar PostgREST'e
--     kapali, host degistiremez. Eski backend anahtari okumaz; bu dosya backend'den once calisabilir.
--   * active_until / storage_until 2099-12-31: link hep acik, galeri hic silinmez, uyari maili gitmez.
--     enforce_event_dates bu tarihleri yalniz activation_date degisince yeniden hesaplar; aktive olmus etkinlikte
--     o tarih degistirilemez, host da bu kolonlari yazamaz. Yani tarih kalici.
--
-- Etkinlik uid'i psql degiskeniyle verilir (yerel test baska etkinlikle calisir):
--   psql -v demo_uid=75f28cdf-c6e9-4aff-8abc-fd6b0e332fcc -f 23-demo-event.sql
-- Tek transaction; COMMIT'ten once kendini dogrular. Tekrar calistirmak guvenli.
-- Geri alma: DELETE FROM site_settings WHERE key = 'demo_events'; tarihleri eski degerlerine (2026-12-15 / 2027-06-13)
--   UPDATE ile geri yazmak (aktivasyon sonrasi trigger yeniden hesaplamaz).

\if :{?demo_uid}
\else
  \echo 'demo_uid verilmedi: psql -v demo_uid=<event uid> ...'
  \quit
\endif

BEGIN;

SELECT set_config('demo.uid', :'demo_uid', true);

-- Etkinlik var, silinmemis ve aktive olmus olmali; yoksa dur.
DO $$
DECLARE
    v_uid UUID := current_setting('demo.uid')::uuid;
    v_ok  BOOLEAN;
BEGIN
    SELECT deleted_at IS NULL AND activation_date IS NOT NULL INTO v_ok FROM events WHERE uid = v_uid;
    IF v_ok IS NOT TRUE THEN
        RAISE EXCEPTION 'demo event % not found, deleted or not activated', v_uid;
    END IF;
END $$;

SELECT uid, name, activation_date, active_until AS active_until_before, storage_until AS storage_until_before
  FROM events WHERE uid = current_setting('demo.uid')::uuid;

INSERT INTO site_settings (key, value, updated_at)
VALUES ('demo_events', jsonb_build_array(current_setting('demo.uid')), NOW())
ON CONFLICT (key) DO UPDATE
   SET value = CASE WHEN site_settings.value ? current_setting('demo.uid') THEN site_settings.value
                    ELSE site_settings.value || jsonb_build_array(current_setting('demo.uid')) END,
       updated_at = NOW();

UPDATE events
   SET active_until  = '2099-12-31 00:00:00+00',
       storage_until = '2099-12-31 00:00:00+00',
       storage_warning_mail_sent_at = NULL
 WHERE uid = current_setting('demo.uid')::uuid;

DO $$
DECLARE
    v_uid     TEXT := current_setting('demo.uid');
    v_flag    BOOLEAN;
    v_active  TIMESTAMPTZ;
    v_storage TIMESTAMPTZ;
BEGIN
    SELECT value ? v_uid INTO v_flag FROM site_settings WHERE key = 'demo_events';
    SELECT active_until, storage_until INTO v_active, v_storage FROM events WHERE uid = v_uid::uuid;
    IF v_flag IS NOT TRUE OR v_active <> '2099-12-31 00:00:00+00' OR v_storage <> '2099-12-31 00:00:00+00' THEN
        RAISE EXCEPTION 'verification failed: flag=% active_until=% storage_until=%', v_flag, v_active, v_storage;
    END IF;
    RAISE NOTICE 'demo_event=% flag=% active_until=% storage_until=%', v_uid, v_flag, v_active, v_storage;
END $$;

COMMIT;
