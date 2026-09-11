-- 10b — Albumlerin geri alinmasi, IKINCI adim (2026-09-11)
--
-- ONKOSUL: 10-albums-rollback-a.sql uygulanmis, albumsuz backend VE frontend canlida.
-- Albumlu backend hala calisiyorsa bu dosyayi CALISTIRMA: GuestUpload General albumunu
-- arar, tablo yoksa her misafir yuklemesi 500 doner.
--
-- Ne yapar: album politikalari, uploads.album_uid kolonu, albums tablosu, yardimci
-- fonksiyonlar ve events.settings.guest_gallery anahtari gider; album bazli zip isleri
-- jobs'tan silinir (10a bunlarin hepsini *_backup_20260911 tablolarina kopyaladi).
--
-- Kalanlar (bilerek): S3'teki album kapaklari (/uload/<user>/album_cover/...), album QR
-- PNG'leri (/events/<ev>/albums/<al>/qr.png) ve hazirlanmis album zip'leri. Yalnizca depolama
-- kaplarlar, hicbir kod artik onlara bakmiyor; istenirse elle temizlenir.

BEGIN;

-- ============================================================================
-- 1. POLITIKALAR (kolon dusmeden once: uploads_guest_album_select album_uid'e bagli)
-- ============================================================================

DROP POLICY IF EXISTS uploads_guest_album_select ON uploads;
DROP POLICY IF EXISTS participants_guest_album_select ON participants;
-- albums uzerindekiler (albums_event_admin_access, albums_guest_select) tabloyla gider.

-- ============================================================================
-- 2. KOLON, TABLO, FONKSIYONLAR
-- ============================================================================

-- FK, uploads_album_idx ve kolon birlikte duser. Fotograf satirlari yerinde kalir.
ALTER TABLE uploads DROP COLUMN IF EXISTS album_uid;

-- albums_one_default_per_event, albums_event_idx ve RLS politikalari tabloyla duser.
DROP TABLE IF EXISTS albums;

DROP FUNCTION IF EXISTS create_default_album();
DROP FUNCTION IF EXISTS check_upload_album_event();
DROP FUNCTION IF EXISTS reassign_album_on_restore();
DROP FUNCTION IF EXISTS current_guest_event_uid();
DROP FUNCTION IF EXISTS current_guest_albums();

-- ============================================================================
-- 3. VERI ARTIKLARI
-- ============================================================================

-- Etkinlik duzeyi "misafir galeriyi gorebilir" anahtari: yalnizca dusen politika okuyordu,
-- eski kodda karsiligi yok. Yedek: events_guest_gallery_backup_20260911.
UPDATE events
SET settings = settings - 'guest_gallery'
WHERE settings ? 'guest_gallery';

-- Album zip isleri: geri alinan Ayarlar sayfasi "son export" sorgusunu album_uid'e gore
-- suzmuyor; bunlar kalirsa host albume ait yarim zip'i etkinlik export'u sanir.
-- Yedek: jobs_album_backup_20260911.
DELETE FROM jobs
WHERE name = 's3_export' AND input ? 'album_uid';

-- ============================================================================
-- 4. DOGRULAMA (COMMIT oncesi goz at) — hepsi 0 / false olmali
-- ============================================================================

SELECT
    to_regclass('public.albums') IS NOT NULL                                          AS albums_table_left,
    EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'uploads' AND column_name = 'album_uid')               AS album_column_left,
    (SELECT COUNT(*) FROM pg_policies
      WHERE policyname IN ('uploads_guest_album_select','participants_guest_album_select',
                           'albums_guest_select','albums_event_admin_access'))          AS album_policies_left,
    (SELECT COUNT(*) FROM pg_proc
      WHERE proname IN ('create_default_album','check_upload_album_event','reassign_album_on_restore',
                        'current_guest_event_uid','current_guest_albums'))              AS album_functions_left,
    (SELECT COUNT(*) FROM events WHERE settings ? 'guest_gallery')                     AS guest_gallery_keys_left,
    (SELECT COUNT(*) FROM jobs WHERE name = 's3_export' AND input ? 'album_uid')       AS album_zip_jobs_left,
    -- yedekler yerinde mi (bilgi amacli)
    (SELECT COUNT(*) FROM albums_backup_20260911)                                      AS albums_backed_up,
    (SELECT COUNT(*) FROM uploads_album_backup_20260911)                               AS uploads_map_backed_up;

COMMIT;

-- PostgREST semayi onbellekler: dusen tablo/kolon icin yeniden yukleme sart, yoksa
-- album_uid'i hala var sanir.
NOTIFY pgrst, 'reload schema';

-- Geri alma: 3-albums.sql'i yeniden calistir (General'leri yeniden acar), sonra
--   UPDATE uploads u SET album_uid = b.album_uid FROM uploads_album_backup_20260911 b WHERE b.uid = u.uid;
-- ile eslesmeyi geri yukle (host albumleri icin once albums_backup_20260911'den INSERT gerekir),
-- ardindan 3b-albums-check.sql ve 5-albums-guest-grant-fix.sql.
--
-- Yedek tablolar bir sure sonra istenirse:
--   DROP TABLE albums_backup_20260911, uploads_album_backup_20260911,
--              events_guest_gallery_backup_20260911, jobs_album_backup_20260911;
