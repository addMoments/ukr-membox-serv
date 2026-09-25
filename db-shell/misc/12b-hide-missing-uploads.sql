-- 12b — Dosyasi S3'te olmayan ya da 0 bayt olan eski yuklemeleri gizler (2026-09-25).
--
-- 12-upload-received.sql mevcut satirlarin hepsini "ulasti" sayarak baslatir. Oysa canlida
-- medya satirlarinin %11'inin dosyasi yok (yarida kalan PUT'lar) ve bir etkinlikte 104 dosya
-- 0 bayt; bunlar galeride gri kutu olarak duruyor. Bu adim onlari received_at NULL yapar:
-- galeriden, export'tan ve kotalardan duserler. Satir silinmez, geri alinabilir.
--
-- Calistirma: upload-received-migrate.sh b. Betik bucket'in tam listesini CSV olarak
-- (:s3csv, "path,size" ve path basinda "/") verir ve gizlenen satirlari :backup'a yazar.
-- :min_objects -- listenin en az bu kadar nesne icermesi beklenir; bos ya da yarim bir liste
-- butun galeriyi gizlerdi.
--
-- Sira: backend deploy'undan SONRA; yeni satirlar zaten gizli basliyor. Su an yuklemesi suren
-- bir dosya da (eski backend'in yazdigi satir) gizlenir ama kaybolmaz: son 24 saatteki gizli
-- satirlari upload_receipt taramasi dakikada bir kontrol eder ve dosya gelince geri acar.
-- Taramasi olmayan eski backend'le calistirma; o zaman boyle bir dosya gizli kalir.

\set ON_ERROR_STOP on

BEGIN;

CREATE TEMP TABLE s3_objects (path TEXT PRIMARY KEY, size_bytes BIGINT NOT NULL) ON COMMIT DROP;
-- psql \copy satirinda degisken acmaz; komut \set ile kurulup oyle calistirilir.
\set copy_in '\\copy s3_objects FROM ' :'s3csv' ' CSV'
:copy_in

SELECT set_config('upload_received.min_objects', :'min_objects', true);
DO $$
DECLARE
    n BIGINT := (SELECT COUNT(*) FROM s3_objects);
BEGIN
    IF n < current_setting('upload_received.min_objects')::BIGINT THEN
        RAISE EXCEPTION 'S3 listesi % nesne, beklenen en az %: liste eksik, hicbir sey degismedi',
            n, current_setting('upload_received.min_objects');
    END IF;
END $$;

CREATE TEMP TABLE hidden_uploads ON COMMIT DROP AS
SELECT u.uid, u.event_uid, e.name AS event_name, u.upload_type, u.client_uid, u.created_at,
       u.trashed_at, u.size_bytes, s.size_bytes AS s3_size_bytes, u.value
FROM uploads u
LEFT JOIN s3_objects s ON s.path = u.value
LEFT JOIN events e ON e.uid = u.event_uid
WHERE u.upload_type IN ('photo', 'video', 'voice')
  AND u.received_at IS NOT NULL
  AND (s.path IS NULL OR s.size_bytes = 0);

\set copy_out '\\copy hidden_uploads TO ' :'backup' ' CSV HEADER'
:copy_out

UPDATE uploads u
SET received_at = NULL
FROM hidden_uploads h
WHERE u.uid = h.uid;

SELECT COUNT(*)                                              AS hidden,
       COUNT(*) FILTER (WHERE s3_size_bytes = 0)             AS empty_objects,
       COUNT(*) FILTER (WHERE trashed_at IS NULL)            AS were_visible_in_gallery,
       COUNT(DISTINCT event_uid)                             AS events
FROM hidden_uploads;

COMMIT;

-- Geri alma (yedek CSV'deki uid'lerle):
--   CREATE TEMP TABLE h (uid UUID);  \copy h FROM PROGRAM 'cut -d, -f1 <yedek.csv> | tail -n +2' CSV
--   UPDATE uploads SET received_at = created_at WHERE uid IN (SELECT uid FROM h);
