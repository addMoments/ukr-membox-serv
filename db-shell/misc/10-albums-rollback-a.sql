-- 10a — Albumlerin geri alinmasi, BIRINCI adim (2026-09-11)
--
-- Musteri album ozelligini (3-albums.sql, 3b-albums-check.sql, 5-albums-guest-grant-fix.sql)
-- tamamen geri istedi. Kod tarafi git revert ile eski haline dondu; bu dosya DB'yi
-- album-oncesi semaya dondurmenin ilk yarisi.
--
-- NEDEN IKI PARCA: Geri alinan (albumsuz) backend photo/video'yu album_uid OLMADAN ekler.
-- 3b'deki uploads_album_by_type CHECK'i durdugu surece bu INSERT patlar, yani misafir
-- yuklemeleri kirilir. O yuzden CHECK ve trigger'lar backend deploy'undan ONCE dusuyor
-- (10a), tablo/kolon/politika ise deploy'dan SONRA (10b). 10a uygulanmis ama backend hala
-- albumlu calisirken hicbir sey bozulmaz: backend General'i kendisi atamaya devam eder.
--
-- SIRA: 10a -> frontend deploy -> backend deploy -> 10b.
--
-- CANLI VERIYE YAZAR. Once yedek (proje kokunden, psql ile):
--   \copy (SELECT * FROM albums ORDER BY created_at) TO 'db-backups/albums-pre-rollback-2026-09-11.csv' CSV HEADER
--   \copy (SELECT uid, event_uid, album_uid FROM uploads WHERE album_uid IS NOT NULL) TO 'db-backups/uploads-album-map-pre-rollback-2026-09-11.csv' CSV HEADER
--   \copy (SELECT uid, settings->'guest_gallery' AS guest_gallery FROM events WHERE settings ? 'guest_gallery') TO 'db-backups/events-guest-gallery-pre-rollback-2026-09-11.csv' CSV HEADER
-- Ayrica asagida ayni veri DB icinde *_backup_20260911 tablolarina kopyalaniyor
-- (webanon/auth'a grant yok, PostgREST'ten gorunmezler; ileride DROP TABLE ile temizlenir).

BEGIN;

-- ============================================================================
-- 1. DB ICI YEDEK
-- ============================================================================

CREATE TABLE IF NOT EXISTS albums_backup_20260911 AS
    SELECT * FROM albums;

CREATE TABLE IF NOT EXISTS uploads_album_backup_20260911 AS
    SELECT uid, event_uid, album_uid FROM uploads WHERE album_uid IS NOT NULL;

CREATE TABLE IF NOT EXISTS events_guest_gallery_backup_20260911 AS
    SELECT uid, settings->'guest_gallery' AS guest_gallery
    FROM events WHERE settings ? 'guest_gallery';

-- Album bazli zip isleri (host "Download album"); 10b bunlari jobs'tan siliyor.
CREATE TABLE IF NOT EXISTS jobs_album_backup_20260911 AS
    SELECT * FROM jobs WHERE name = 's3_export' AND input ? 'album_uid';

COMMENT ON TABLE albums_backup_20260911 IS
    'Album ozelligi geri alinmadan onceki albums tablosu (10-albums-rollback-a.sql, 2026-09-11)';

-- ============================================================================
-- 2. ESKI BACKEND'IN INSERT'INI ENGELLEYEN KISIT VE TRIGGER'LAR
-- ============================================================================

-- 3b: photo/video her zaman albumlu CHECK'i. Albumsuz backend icin dusmeli.
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_album_by_type;

-- 3: yeni event'e General acan, upload'u albume baglayan ve copten donuste
-- albumu General'e ceviren trigger'lar. Fonksiyonlar 10b'de dusuyor.
DROP TRIGGER IF EXISTS uploads_album_same_event ON uploads;
DROP TRIGGER IF EXISTS uploads_restore_album ON uploads;
DROP TRIGGER IF EXISTS events_create_default_album ON events;

-- ============================================================================
-- 3. DOGRULAMA (COMMIT oncesi goz at)
-- ============================================================================

SELECT
    (SELECT COUNT(*) FROM albums)                                    AS albums_total,
    (SELECT COUNT(*) FROM albums WHERE NOT is_default)               AS albums_user_created,
    (SELECT COUNT(*) FROM albums_backup_20260911)                    AS albums_backed_up,
    (SELECT COUNT(*) FROM uploads WHERE album_uid IS NOT NULL)       AS uploads_with_album,
    (SELECT COUNT(*) FROM uploads_album_backup_20260911)             AS uploads_map_backed_up,
    (SELECT COUNT(*) FROM jobs_album_backup_20260911)                AS album_zip_jobs,
    (SELECT COUNT(*) FROM pg_constraint WHERE conname = 'uploads_album_by_type') AS check_left,
    (SELECT COUNT(*) FROM pg_trigger
      WHERE tgname IN ('uploads_album_same_event','uploads_restore_album','events_create_default_album')
        AND NOT tgisinternal)                                        AS album_triggers_left;
-- albums_total = albums_backed_up, uploads_with_album = uploads_map_backed_up,
-- check_left = 0, album_triggers_left = 0 olmali.
-- albums_user_created > 0 ise hostlarin kendi actigi albumler var: 10b'de kaybolur
-- (fotograflar kalir, yalnizca album gruplamasi gider); yedek tabloda duruyor.

COMMIT;

-- Sema onbellegi: CHECK/trigger degisikligi PostgREST icin onemsiz, NOTIFY 10b'de.
--
-- Geri alma (10b calismadiysa albumler tamamen geri gelir):
--   BEGIN;
--   ALTER TABLE uploads ADD CONSTRAINT uploads_album_by_type
--       CHECK ((upload_type IN ('photo','video')) = (album_uid IS NOT NULL));
--   -- trigger'lar: 3-albums.sql §3'teki uc CREATE TRIGGER'i yeniden calistir
--   COMMIT;
