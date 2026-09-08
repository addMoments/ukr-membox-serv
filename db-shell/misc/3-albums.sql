-- 3 — Etkinlik ici albumler (PLAN-ALBUMLER.md, Excel madde 3)
--
-- Ne yapar:
--   * albums tablosu + uploads.album_uid
--   * her etkinlige silinemeyen "General" albumu (backfill + INSERT trigger'i)
--   * mevcut photo/video kayitlari General'e tasinir; text/voice albumsuz kalir (CHECK ile)
--   * host (auth) RLS'i mevcut kalipla; misafir (webanon) icin kolon bazli GRANT
--     (passcode disarida) + gorunurluk politikalari
--   * misafir token'indaki ev / al claim'lerini okuyan yardimci fonksiyonlar
--
-- CANLI VERIYE YAZAR. Calistirmadan once:
--   pg_dump -Fc -t albums -t uploads -t events > pre-albums.dump   (albums heniz yoksa -t albums'i cikar)
--   SELECT COUNT(*) FROM uploads WHERE upload_type IN ('photo','video');   -- backfill sonrasi
--   SELECT COUNT(*) FROM uploads WHERE album_uid IS NOT NULL;               -- ile ayni olmali
--
-- Tek transaction: bir adim patlarsa hicbir sey uygulanmaz.
-- Deploy sirasi: 3-albums.sql (sonunda NOTIFY pgrst) -> membox-serv deploy -> frontend deploy -> 3b-albums-check.sql

BEGIN;

-- ============================================================================
-- 1. TABLO
-- ============================================================================

CREATE TABLE IF NOT EXISTS albums (
    uid                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_uid          UUID NOT NULL REFERENCES events(uid),
    name               VARCHAR(120) NOT NULL,
    album_date         DATE,
    location           VARCHAR(255),
    description        TEXT,
    -- S3 yolu: /uload/<user>/album_cover/<uid>.<ext> (yuklenen) ya da bir upload'un value'su
    cover              VARCHAR(255),
    privacy            TEXT NOT NULL DEFAULT 'public'
                       CHECK (privacy IN ('public', 'private', 'protected')),
    -- yalnizca protected; duz metin saklanir, host paylasmak icin gorur (karar 2026-09-08)
    passcode           VARCHAR(32),
    -- kapaliysa album misafire hic gorunmez (liste, link, QR, zip)
    guest_upload       BOOLEAN NOT NULL DEFAULT TRUE,
    -- album duzeyi galeri anahtari; etkinlik duzeyi anahtar events.settings.guest_gallery
    guest_view         BOOLEAN NOT NULL DEFAULT TRUE,
    guest_download_all BOOLEAN NOT NULL DEFAULT FALSE,
    -- "General": silinemez, albumsuz yuklemeler buraya duser
    is_default         BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order         INT NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS albums_one_default_per_event
    ON albums (event_uid) WHERE is_default;

CREATE INDEX IF NOT EXISTS albums_event_idx ON albums (event_uid) WHERE deleted_at IS NULL;

ALTER TABLE uploads ADD COLUMN IF NOT EXISTS album_uid UUID REFERENCES albums(uid);

CREATE INDEX IF NOT EXISTS uploads_album_idx ON uploads (album_uid) WHERE album_uid IS NOT NULL;

-- ============================================================================
-- 2. BACKFILL — her etkinlige General, mevcut medya oraya
-- ============================================================================

INSERT INTO albums (event_uid, name, is_default)
SELECT e.uid, 'General', TRUE
FROM events e
WHERE NOT EXISTS (SELECT 1 FROM albums a WHERE a.event_uid = e.uid AND a.is_default);

UPDATE uploads u
SET album_uid = a.uid
FROM albums a
WHERE a.event_uid = u.event_uid
  AND a.is_default
  AND u.album_uid IS NULL
  AND u.upload_type IN ('photo', 'video');

-- NOT: "photo/video her zaman albumlu" CHECK kisiti BURADA DEGIL, 3b-albums-check.sql'de.
-- Sebep: bu dosya yeni backend deploy edilmeden once calisir; eski membox-serv photo
-- yuklemesini album_uid olmadan ekler ve kisit olsaydi misafir yuklemeleri backend
-- yenilenene kadar patlardi. Sira: 3 -> backend deploy -> frontend deploy -> 3b.

-- ============================================================================
-- 3. TRIGGER'LAR
-- ============================================================================

-- Yeni etkinlikte General otomatik. Event satiri signup_email.go'da iki ayri yerde
-- (Post ve Attach) aciliyor; trigger tek nokta.
CREATE OR REPLACE FUNCTION create_default_album()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO albums (event_uid, name, is_default)
    VALUES (NEW.uid, 'General', TRUE)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS events_create_default_album ON events;
CREATE TRIGGER events_create_default_album
    AFTER INSERT ON events
    FOR EACH ROW EXECUTE FUNCTION create_default_album();

-- Bir upload yalnizca kendi etkinliginin albumune baglanabilir. Host PostgREST'ten
-- PATCH ile tasidigi icin bu kural DB'de durmali (baska bir etkinligin albumune
-- tasimayi RLS engellemez: host iki etkinligin de admini olabilir).
CREATE OR REPLACE FUNCTION check_upload_album_event()
RETURNS TRIGGER AS $$
DECLARE
    v_album_event UUID;
    v_album_deleted TIMESTAMPTZ;
BEGIN
    IF NEW.album_uid IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT event_uid, deleted_at INTO v_album_event, v_album_deleted
    FROM albums WHERE uid = NEW.album_uid;

    IF v_album_event IS NULL THEN
        RAISE EXCEPTION 'Album % does not exist', NEW.album_uid;
    END IF;
    IF v_album_event <> NEW.event_uid THEN
        RAISE EXCEPTION 'Album belongs to a different event';
    END IF;
    -- Silinmis albume yeni medya baglanamaz; mevcut satirin album_uid'i
    -- degismiyorsa (ornegin cope tasima) dokunma.
    IF v_album_deleted IS NOT NULL
       AND (TG_OP = 'INSERT' OR NEW.album_uid IS DISTINCT FROM OLD.album_uid) THEN
        RAISE EXCEPTION 'Album has been deleted';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS uploads_album_same_event ON uploads;
CREATE TRIGGER uploads_album_same_event
    BEFORE INSERT OR UPDATE OF album_uid ON uploads
    FOR EACH ROW EXECUTE FUNCTION check_upload_album_event();

-- Copten geri alinan upload'un albumu silinmisse General'e doner (karar 3).
CREATE OR REPLACE FUNCTION reassign_album_on_restore()
RETURNS TRIGGER AS $$
DECLARE
    v_deleted TIMESTAMPTZ;
    v_default UUID;
BEGIN
    IF NOT (OLD.trashed_at IS NOT NULL AND NEW.trashed_at IS NULL) THEN
        RETURN NEW;
    END IF;
    IF NEW.album_uid IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT deleted_at INTO v_deleted FROM albums WHERE uid = NEW.album_uid;
    IF v_deleted IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT uid INTO v_default FROM albums
    WHERE event_uid = NEW.event_uid AND is_default AND deleted_at IS NULL;
    IF v_default IS NOT NULL THEN
        NEW.album_uid := v_default;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS uploads_restore_album ON uploads;
CREATE TRIGGER uploads_restore_album
    BEFORE UPDATE OF trashed_at ON uploads
    FOR EACH ROW EXECUTE FUNCTION reassign_album_on_restore();

-- ============================================================================
-- 4. MISAFIR TOKEN CLAIM'LERI
-- ============================================================================

-- Misafir token'inin etkinligi (claim "ev"). Eski token'larda yok -> NULL -> hicbir
-- album gorunmez; membox-serv eksik claim'li token'i ilk istekte yeniler.
CREATE OR REPLACE FUNCTION current_guest_event_uid()
RETURNS uuid AS $$
    SELECT NULLIF(coalesce(
        current_setting('request.jwt.claim.ev', true),
        current_setting('request.jwt.claims', true)::json->>'ev'
    ), '')::uuid;
$$ LANGUAGE sql STABLE;

-- Linkten acilmis private/protected albumler (claim "al", JSON dizi).
CREATE OR REPLACE FUNCTION current_guest_albums()
RETURNS uuid[] AS $$
    SELECT coalesce(
        (SELECT array_agg(value::uuid)
         FROM json_array_elements_text(
             coalesce(current_setting('request.jwt.claims', true)::json->'al', '[]'::json)
         )),
        '{}'::uuid[]
    );
$$ LANGUAGE sql STABLE;

GRANT EXECUTE ON FUNCTION current_guest_event_uid() TO auth;
GRANT EXECUTE ON FUNCTION current_guest_event_uid() TO webanon;
GRANT EXECUTE ON FUNCTION current_guest_albums() TO auth;
GRANT EXECUTE ON FUNCTION current_guest_albums() TO webanon;

-- ============================================================================
-- 5. YETKILER
-- ============================================================================

ALTER TABLE albums ENABLE ROW LEVEL SECURITY;

-- auth: "GRANT ... ON ALL TABLES" setup.sql'de calistigi anda var olan tablolari kapsar;
-- ALTER DEFAULT PRIVILEGES sadece ayni rolun yaratacagi tablolar icin gecerli. Acikca ver.
GRANT SELECT, INSERT, UPDATE, DELETE ON albums TO auth;

-- webanon: niyet passcode HARIC kolon bazli SELECT. DIKKAT: setup.sql'de "GRANT auth TO webanon"
-- oldugu icin webanon, auth'un tablo duzeyi SELECT'ini de miras alir ve bu kolon kisiti
-- pratikte baglayici DEGIL (yerel testte dogrulandi: webanon passcode kolonunu okuyabiliyor).
-- Gercek koruma RLS'ten geliyor: misafir yalnizca zaten actigi (passcode'unu bildigi)
-- protected albumu gorur; public/private albumlerde passcode yok. Yani sir sizmiyor.
-- Kisit yine de niyeti belgelemek icin duruyor; frontend misafir sorgulari kolon listesiyle gider.
GRANT SELECT (uid, event_uid, name, album_date, location, description, cover, privacy,
              guest_upload, guest_view, guest_download_all, is_default, sort_order, created_at)
    ON albums TO webanon;

-- Host: kendi etkinliklerinin albumleri
DROP POLICY IF EXISTS albums_event_admin_access ON albums;
CREATE POLICY albums_event_admin_access ON albums
    FOR ALL
    TO auth
    USING (
        event_uid IN (
            SELECT uid FROM events
            WHERE current_user_uid() = ANY(admins)
        )
    )
    WITH CHECK (
        event_uid IN (
            SELECT uid FROM events
            WHERE current_user_uid() = ANY(admins)
        )
    );

-- Misafir: token'in etkinligi + yukleme acik + (public | linkten acilmis)
DROP POLICY IF EXISTS albums_guest_select ON albums;
CREATE POLICY albums_guest_select ON albums
    FOR SELECT
    TO webanon
    USING (
        deleted_at IS NULL
        AND event_uid = current_guest_event_uid()
        AND guest_upload
        AND (privacy = 'public' OR uid = ANY(current_guest_albums()))
    );

-- Misafir: album icerigi. Iki galeri anahtari (etkinlik + album) da acik olmali.
-- Kendi yukledikleri icin mevcut uploads_participant_select ayrica gecerli kalir.
DROP POLICY IF EXISTS uploads_guest_album_select ON uploads;
CREATE POLICY uploads_guest_album_select ON uploads
    FOR SELECT
    TO webanon
    USING (
        trashed_at IS NULL
        AND upload_type IN ('photo', 'video')
        AND get_event_setting_bool(event_uid, 'guest_gallery')
        AND album_uid IN (
            SELECT a.uid FROM albums a
            WHERE a.deleted_at IS NULL
              AND a.event_uid = current_guest_event_uid()
              AND a.guest_upload
              AND a.guest_view
              AND (a.privacy = 'public' OR a.uid = ANY(current_guest_albums()))
        )
    );

-- Misafir galeri kartlari yukleyen adini gosteriyor (select=*,participants(name)).
-- participants_self_access yalnizca kendi kaydini aciyor; gorunur albumlerdeki
-- diger katilimcilarin adi icin ek politika. Ad disinda kolon zaten yok (uid, event_uid, created_at).
DROP POLICY IF EXISTS participants_guest_album_select ON participants;
CREATE POLICY participants_guest_album_select ON participants
    FOR SELECT
    TO webanon
    USING (
        event_uid = current_guest_event_uid()
        AND get_event_setting_bool(event_uid, 'guest_gallery')
    );

-- ============================================================================
-- 6. DOGRULAMA (COMMIT oncesi goz at)
-- ============================================================================

SELECT
    (SELECT COUNT(*) FROM events)                                              AS events,
    (SELECT COUNT(*) FROM albums WHERE is_default)                             AS default_albums,
    (SELECT COUNT(*) FROM uploads WHERE upload_type IN ('photo','video'))      AS media_uploads,
    (SELECT COUNT(*) FROM uploads WHERE album_uid IS NOT NULL)                 AS media_with_album;
-- events = default_albums ve media_uploads = media_with_album olmali.

COMMIT;

-- PostgREST semayi onbellekler; yeni tablo yeniden yukleme sinyali olmadan gorunmez
-- ("Could not find the table 'public.albums' in the schema cache", PGRST205).
-- Canlida 2026-09-08'de bu yuzden Albums sayfasi ilk dakikalarda calismadi.
NOTIFY pgrst, 'reload schema';

-- Geri alma (yalnizca hic kullanilmadiysa; 3b calistiysa once oradaki kisiti dusur):
--   BEGIN;
--   DROP TRIGGER IF EXISTS uploads_restore_album ON uploads;
--   DROP TRIGGER IF EXISTS uploads_album_same_event ON uploads;
--   DROP TRIGGER IF EXISTS events_create_default_album ON events;
--   DROP POLICY IF EXISTS uploads_guest_album_select ON uploads;
--   DROP POLICY IF EXISTS participants_guest_album_select ON participants;
--   ALTER TABLE uploads DROP COLUMN IF EXISTS album_uid;
--   DROP TABLE IF EXISTS albums;
--   DROP FUNCTION IF EXISTS create_default_album(), check_upload_album_event(),
--        reassign_album_on_restore(), current_guest_event_uid(), current_guest_albums();
--   COMMIT;
