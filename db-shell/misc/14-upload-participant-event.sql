-- Excel 2.15 takibi (musteri 19 Ekim 2026: "as an admin i cant delete events which i created before")
--
-- Ne: Bir paylasim (uploads) yalnizca kendi event'inin participant'ina yazilabilsin.
-- Nasil: 1) uploads'a trigger: client_uid'in participant kaydi NEW.event_uid'de degilse satir
--           reddedilir. Hem backend (presign) hem PostgREST (ani defteri) yolunu kapsar; yalnizca
--           client_uid / event_uid yazilinca calisir, eski satirlara dokunmaz.
--        2) uploads_participant_text_insert canlida WITH CHECK (true) idi; misafir mesaji baska
--           bir participant adina ve baska event'e yazabiliyordu. setup.sql'deki kurala donuyor:
--           yalnizca kendi adina, yalnizca text.
--        3) webanon participants'ta tum kolonlari guncelleyebiliyordu (event_uid dahil);
--           frontend yalnizca name gonderiyor, yetki name'e indiriliyor.
-- Neden: Baska event'in participant'ina yazilmis paylasimlar uploads_client_uid_fkey yuzunden
--        kalici silmeyi durduruyordu ("3 people", "OLENA BM PARTY"). Mevcut 18 satir 21 Agustos
--        (444fada) oncesinden; kalici silme onlari artik tasiyor (159fe19). Bu dosya yenilerini
--        engeller.
--
-- Sira: frontend (core.ts) -> backend (AuthMiddleware participant kontrolu) -> bu dosya.
-- Geri alma: dosyanin sonundaki blok.

BEGIN;

CREATE OR REPLACE FUNCTION public.check_upload_participant_event() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = public, pg_temp
    AS $$
BEGIN
    -- SECURITY DEFINER: misafir rolu RLS yuzunden participant satirini goremeyebilir;
    -- kontrol tablo sahibi olarak yapilir.
    IF NOT EXISTS (
        SELECT 1 FROM participants WHERE uid = NEW.client_uid AND event_uid = NEW.event_uid
    ) THEN
        RAISE EXCEPTION 'upload participant does not belong to this event'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS uploads_participant_same_event ON uploads;
CREATE TRIGGER uploads_participant_same_event
    BEFORE INSERT OR UPDATE OF client_uid, event_uid ON uploads
    FOR EACH ROW EXECUTE FUNCTION public.check_upload_participant_event();

DROP POLICY IF EXISTS uploads_participant_text_insert ON uploads;
CREATE POLICY uploads_participant_text_insert ON uploads
    FOR INSERT
    TO webanon
    WITH CHECK (
        client_uid = current_user_uid()
        AND upload_type = 'text'
    );

REVOKE UPDATE ON participants FROM webanon;
GRANT UPDATE (name) ON participants TO webanon;

COMMIT;

NOTIFY pgrst, 'reload schema';

-- Geri alma:
-- BEGIN;
-- DROP TRIGGER IF EXISTS uploads_participant_same_event ON uploads;
-- DROP FUNCTION IF EXISTS public.check_upload_participant_event();
-- DROP POLICY IF EXISTS uploads_participant_text_insert ON uploads;
-- CREATE POLICY uploads_participant_text_insert ON uploads FOR INSERT TO webanon WITH CHECK (true);
-- REVOKE UPDATE (name) ON participants FROM webanon;
-- GRANT UPDATE ON participants TO webanon;
-- COMMIT;
-- NOTIFY pgrst, 'reload schema';
