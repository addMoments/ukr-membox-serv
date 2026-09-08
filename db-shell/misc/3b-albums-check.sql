-- 3b — Albumler, ikinci adim: "photo/video her zaman albumlu" kisiti.
--
-- 3-albums.sql'den AYRI cunku yeni membox-serv deploy edilmeden once calisirsa eski backend
-- photo yuklemelerini album_uid olmadan ekler ve kisit misafir yuklemelerini patlatirdi.
-- Bu dosya backend ve frontend canlida yenilendikten SONRA calistirilir.
--
-- Kontrol (0 olmali; degilse asagidaki UPDATE zaten General'e tasir):
--   SELECT COUNT(*) FROM uploads WHERE upload_type IN ('photo','video') AND album_uid IS NULL;

BEGIN;

-- Aradaki surede eski backend'in albumsuz ekledigi medya varsa General'e tasi.
UPDATE uploads u
SET album_uid = a.uid
FROM albums a
WHERE a.event_uid = u.event_uid
  AND a.is_default
  AND u.album_uid IS NULL
  AND u.upload_type IN ('photo', 'video');

-- Guestbook (text/voice) albumsuz, medya (photo/video) albumlu — baska kombinasyon yok.
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_album_by_type;
ALTER TABLE uploads ADD CONSTRAINT uploads_album_by_type
    CHECK ((upload_type IN ('photo', 'video')) = (album_uid IS NOT NULL));

SELECT
    (SELECT COUNT(*) FROM uploads WHERE upload_type IN ('photo','video'))  AS media_uploads,
    (SELECT COUNT(*) FROM uploads WHERE album_uid IS NOT NULL)             AS media_with_album;
-- Ikisi esit olmali.

COMMIT;

-- Geri alma:
--   ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_album_by_type;
