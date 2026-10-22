-- 15 — Misafir galerisine yalnizca album karar verir (AM-08 secenek B; 2026-10-22)
--
-- Ayarlar'daki etkinlik duzeyi "Guests can view the gallery" anahtari (events.settings.guest_gallery)
-- kaldiriliyor. Misafirin bir albumun foto/videolarini gorup goremeyecegine artik yalnizca albumun
-- "Guests can view this album" anahtari (albums.guest_view) karar verir. Musteri, 20 Ekim:
-- "we discussed to remove this from general settings to prevent duplication, because already in
-- album settings, we have this".
--
-- Misafirin gordugu hicbir sey degismez:
--   * Anahtari kapali (ya da hic yazilmamis) etkinliklerin albumlerinde guest_view kapatilir;
--     bugun gorulmeyen hicbir foto acilmaz. Anahtari acik etkinliklere dokunulmaz.
--   * Yeni etkinligin General albumu misafire kapali acilir (bugun yeni etkinlikte anahtar yok = kapali).
-- events.settings.guest_gallery degeri silinmez, yalnizca artik okunmaz (events'e UPDATE yok:
-- events tetikleyicileri tum satirlarda calismasin).
--
-- SIRA: bu dosya -> backend deploy -> frontend deploy. Arada eski backend anahtari okumaya devam
-- eder; anahtari kapali etkinliklerde guest_view de artik kapali oldugu icin ikisi ayni sonucu verir.
-- Tek transaction; sonunda NOTIFY pgrst (politikalar degisti).
--
-- Once etkiyi gormek icin (salt okunur): ./album-gallery-migrate.sh check

BEGIN;

-- ============================================================================
-- 1. YEDEK (geri alma icin; asagidaki GERI ALMA bolumu bunlari kullanir)
-- ============================================================================

CREATE TABLE IF NOT EXISTS albums_guest_view_backup_20261022 AS
    SELECT uid, guest_view FROM albums;

CREATE TABLE IF NOT EXISTS events_guest_gallery_backup_20261022 AS
    SELECT uid, settings->'guest_gallery' AS guest_gallery FROM events;

-- ============================================================================
-- 2. VERI — anahtari kapali etkinliklerin albumleri misafire kapali kalsin
-- ============================================================================

UPDATE albums a
SET guest_view = FALSE
FROM events e
WHERE e.uid = a.event_uid
  AND a.guest_view
  AND NOT COALESCE((e.settings->>'guest_gallery')::boolean, FALSE);

-- ============================================================================
-- 3. YENI ETKINLIK — General misafire kapali acilir
-- ============================================================================

CREATE OR REPLACE FUNCTION create_default_album()
RETURNS TRIGGER AS $$
BEGIN
    -- guest_view FALSE: etkinlik anahtari varken yeni etkinlik galerisi kapali basliyordu;
    -- host acmadikca misafir baskalarinin fotolarini gormesin (15-album-decides-gallery.sql).
    INSERT INTO albums (event_uid, name, is_default, guest_view)
    VALUES (NEW.uid, 'General', TRUE, FALSE)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- 4. POLITIKALAR — etkinlik anahtari kosulu kalkar
-- ============================================================================

-- Misafir album listesi: misafire acik bir yani olan album (yukleme acik YA DA gorunur)
-- + public / protected (kilitli gosterilir) / token'da acilmis (private linkten).
DROP POLICY IF EXISTS albums_guest_select ON albums;
CREATE POLICY albums_guest_select ON albums
    FOR SELECT
    TO webanon
    USING (
        deleted_at IS NULL
        AND event_uid = current_guest_event_uid()
        AND (guest_upload OR guest_view)
        AND (privacy IN ('public', 'protected') OR uid = ANY(current_guest_albums()))
    );

-- Misafir album icerigi: album guest_view acik; protected/private icerik yalnizca token'da
-- acilmis albumlerde. Kendi yukledikleri icin uploads_participant_select ayrica gecerli.
DROP POLICY IF EXISTS uploads_guest_album_select ON uploads;
CREATE POLICY uploads_guest_album_select ON uploads
    FOR SELECT
    TO webanon
    USING (
        trashed_at IS NULL
        AND upload_type IN ('photo', 'video')
        AND album_uid IN (
            SELECT a.uid FROM albums a
            WHERE a.deleted_at IS NULL
              AND a.event_uid = current_guest_event_uid()
              AND a.guest_view
              AND (a.privacy = 'public' OR a.uid = ANY(current_guest_albums()))
        )
    );

-- Galeri kartlarindaki yukleyen adlari: etkinlikte misafirin gorebildigi bir album varsa.
-- Eskiden etkinlik anahtarina bagliydi; artik "gorunur album var mi" sorusu. Alt sorgu
-- albums_guest_select'e tabi; okudugu kolonlar (event_uid, deleted_at, guest_view) webanon
-- GRANT listesinde (11-albums-v2.sql).
DROP POLICY IF EXISTS participants_guest_album_select ON participants;
CREATE POLICY participants_guest_album_select ON participants
    FOR SELECT
    TO webanon
    USING (
        event_uid = current_guest_event_uid()
        AND EXISTS (
            SELECT 1 FROM albums a
            WHERE a.event_uid = participants.event_uid
              AND a.deleted_at IS NULL
              AND a.guest_view
        )
    );

-- ============================================================================
-- 5. DOGRULAMA (COMMIT oncesi goz at)
-- ============================================================================

SELECT
    (SELECT COUNT(*) FROM albums_guest_view_backup_20261022)                          AS albums_backed_up,
    (SELECT COUNT(*) FROM albums_guest_view_backup_20261022 b
       JOIN albums a ON a.uid = b.uid WHERE b.guest_view AND NOT a.guest_view)         AS albums_view_turned_off,
    (SELECT COUNT(*) FROM albums a JOIN events e ON e.uid = a.event_uid
      WHERE a.deleted_at IS NULL AND a.guest_view
        AND NOT COALESCE((e.settings->>'guest_gallery')::boolean, FALSE))              AS visible_in_gallery_off_events,
    (SELECT COUNT(*) FROM pg_policies WHERE policyname IN
        ('albums_guest_select', 'uploads_guest_album_select', 'participants_guest_album_select')
        AND qual NOT LIKE '%guest_gallery%')                                           AS policies_without_gallery;
-- visible_in_gallery_off_events = 0, policies_without_gallery = 3 olmali;
-- albums_view_turned_off, check adimindaki "albums_to_turn_off" ile ayni olmali.

COMMIT;

NOTIFY pgrst, 'reload schema';

-- ============================================================================
-- GERI ALMA (gerekirse; once frontend + backend eski surume, sonra bu blok)
-- ============================================================================
-- BEGIN;
-- UPDATE albums a SET guest_view = b.guest_view
--   FROM albums_guest_view_backup_20261022 b WHERE b.uid = a.uid AND a.guest_view <> b.guest_view;
-- CREATE OR REPLACE FUNCTION create_default_album() ... (11-albums-v2.sql §3, guest_view'siz INSERT)
-- albums_guest_select / uploads_guest_album_select / participants_guest_album_select:
--   11-albums-v2.sql §5'teki tanimlar (get_event_setting_bool(event_uid, 'guest_gallery') kosullu)
-- COMMIT;
-- NOTIFY pgrst, 'reload schema';
-- Yedekler artik gerekmeyince: DROP TABLE albums_guest_view_backup_20261022, events_guest_gallery_backup_20261022;
