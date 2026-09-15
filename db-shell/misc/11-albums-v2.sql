-- 11 — Etkinlik ici albumler, IKINCI KURULUM (2026-09-15; PLAN-ALBUMLER.md §9)
--
-- Gecmis: 3-albums.sql + 3b + 5 ile 2026-09-08'de kuruldu, 10a/10b ile 2026-09-11'de
-- musteri istegiyle tamamen geri alindi. Bu dosya albumleri yeniden kurar; 3/3b/5 tarihsel
-- kayittir, YENIDEN CALISTIRILMAZ. Icerik 3-albums.sql ile ayni sema, farklar:
--
--   * TEMIZ BASLANGIC: *_backup_20260911 tablolarindaki eski host albumleri geri
--     yuklenmez (kullanici karari 2026-09-15). Her etkinlige yeni bir General acilir,
--     tum medya oraya duser. Yedek tablolar dokunulmadan kalir; en sonda opsiyonel DROP var.
--   * GORUNURLUK v2 (musterinin "gizleme / passcode duzgun calismiyor" geri bildirimi):
--       - protected album misafir listesinde KILITLI olarak cikar (eskiden hic cikmiyordu,
--         host passcode koyunca album misafir sayfasindan "kayboluyordu");
--       - guest_upload=false artik albumu gizlemez, yalnizca yuklemeyi kapatir (eski karar
--         12 kaldirildi). Album, misafire acik hicbir yani kalmayinca gizlenir:
--         NOT guest_upload AND NOT (guest_view AND etkinlik guest_gallery);
--       - private: degismedi — listede yok, linki/QR'i bilen acar.
--     Ayni kural Go'da dbscripts.Guest_album_access icinde; ikisi birlikte degismeli.
--   * webanon GRANT listesinde deleted_at var (5-albums-guest-grant-fix.sql'in dersi:
--     RLS alt sorgusu kolon yetkisine tabidir).
--
-- CANLI VERIYE YAZAR. Once yedek (proje kokunden, psql ile):
--   \copy (SELECT uid, event_uid, upload_type, trashed_at FROM uploads) TO 'db-backups/uploads-pre-albums-v2-2026-09-15.csv' CSV HEADER
--   \copy (SELECT uid, settings FROM events) TO 'db-backups/events-pre-albums-v2-2026-09-15.csv' CSV HEADER
--
-- SIRA: 11-albums-v2.sql -> backend deploy -> frontend deploy -> 11b-albums-v2-check.sql
-- (11b'deki CHECK, eski backend albumsuz photo eklerken kosulamaz; 3b ile ayni sebep.)
-- Tek transaction; sonunda NOTIFY pgrst (yeni tablo/kolon sema onbellegine girsin).

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
    -- misafir bu albume yukleyebilir mi; kapaliysa album gorunur kalir (v2), yukleme kapanir
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

-- Linkten / passcode ile acilmis private-protected albumler (claim "al", JSON dizi).
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

-- webanon: passcode HARIC kolon bazli SELECT. Canlida kolon listesi BAGLAYICI (2026-09-09
-- dersi): politikalarin okudugu her kolon listede olmali, deleted_at dahil. Listede olmayan
-- bir kolona dokunan alt sorgu 42501 verir ve PostgREST bunu 401'e cevirir -> misafir
-- sayfasi hic acilmaz.
GRANT SELECT (uid, event_uid, name, album_date, location, description, cover, privacy,
              guest_upload, guest_view, guest_download_all, is_default, sort_order,
              created_at, deleted_at)
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

-- Misafir album listesi (v2):
--   token'in etkinligi
--   + misafire acik bir yani var (yukleme acik YA DA album gorunur ve etkinlik galerisi acik)
--   + public / protected (kilitli gosterilir) / token'da acilmis (private linkten)
DROP POLICY IF EXISTS albums_guest_select ON albums;
CREATE POLICY albums_guest_select ON albums
    FOR SELECT
    TO webanon
    USING (
        deleted_at IS NULL
        AND event_uid = current_guest_event_uid()
        AND (guest_upload OR (guest_view AND get_event_setting_bool(event_uid, 'guest_gallery')))
        AND (privacy IN ('public', 'protected') OR uid = ANY(current_guest_albums()))
    );

-- Misafir album icerigi (v2): etkinlik galerisi + album guest_view acik; protected/private
-- icerik yalnizca token'da acilmis albumlerde. guest_upload icerige etki etmez.
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
    (SELECT COUNT(*) FROM albums WHERE NOT is_default)                         AS other_albums,
    (SELECT COUNT(*) FROM uploads WHERE upload_type IN ('photo','video'))      AS media_uploads,
    (SELECT COUNT(*) FROM uploads WHERE album_uid IS NOT NULL)                 AS media_with_album,
    (SELECT COUNT(*) FROM pg_policies WHERE policyname IN
        ('albums_event_admin_access','albums_guest_select',
         'uploads_guest_album_select','participants_guest_album_select'))      AS album_policies;
-- events = default_albums, other_albums = 0 (temiz baslangic), media_uploads = media_with_album,
-- album_policies = 4 olmali.

COMMIT;

-- PostgREST semayi onbellekler; yeni tablo yeniden yukleme sinyali olmadan gorunmez
-- (PGRST205). 2026-09-08'de canlida bu yuzden Albums sayfasi ilk dakikalarda calismadi.
NOTIFY pgrst, 'reload schema';

-- Opsiyonel temizlik (kullanici "temiz baslangic" dedi, yedekler artik kullanilmayacak;
-- yine de aceleye gerek yok, istenirse ayri bir adimda):
--   DROP TABLE IF EXISTS albums_backup_20260911, uploads_album_backup_20260911,
--                        events_guest_gallery_backup_20260911, jobs_album_backup_20260911;
--
-- Geri alma: 10-albums-rollback-a.sql + 10-albums-rollback-b.sql ayni sirayla
-- (a backend'den once, b sonra); yedek tablo adlarindaki tarihi degistirmeyi unutma.
