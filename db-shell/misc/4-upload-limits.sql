-- 4 — Yukleme limitleri: dosya boyutu kolonu + paket secenekleri (Excel madde 2.17)
--
-- Ne yapar:
--   * uploads.size_bytes: her medyanin boyutu. Misafir presign isteginde bildirilir,
--     yukleme satiriyla birlikte yazilir. Eski satirlar S3'ten geriye doldurulur
--     (backfill-upload-sizes.sh), o yuzden burada 0 ile baslar.
--   * event_uid indeksi: limit kontrolu her yuklemede COUNT/SUM calistiriyor.
--   * Paket secenekleri (products.options):
--       storage_gb        — etkinlik basina toplam depolama (GB). -1 = sinirsiz.
--       guest_media_count — TEK bir misafirin yukleyebilecegi medya adedi. -1 = sinirsiz.
--       guest_storage_gb  — TEK bir misafirin yukleyebilecegi toplam boyut (GB). -1 = sinirsiz.
--     media_count ve guest_count zaten vardi, dokunulmuyor.
--
-- Musteri talebi (8 Eylul 2026): "4 gb - 100 media for one user to upload" -> misafir basina
-- 100 medya ve 4 GB. Etkinlik basina depolama (storage_gb) sinirsiz birakiliyor: plus ve
-- premium paketleri "sinirsiz medya" ile satildi, bir deger yazmak satilani daraltirdi.
-- Uc deger de admin panelinden degistirilebilir, kod degisikligi gerekmez.
--
-- CANLI VERIYE YAZAR. Once yedek al (uploads, products).

BEGIN;

ALTER TABLE uploads ADD COLUMN IF NOT EXISTS size_bytes BIGINT NOT NULL DEFAULT 0;

-- Limit kontrolu event_uid uzerinden COUNT/SUM yapiyor; sequential scan olmasin.
CREATE INDEX IF NOT EXISTS uploads_event_idx ON uploads (event_uid);
-- Misafir basina limit (event_uid, client_uid) ile sorgulanir.
CREATE INDEX IF NOT EXISTS uploads_event_client_idx ON uploads (event_uid, client_uid);

-- Misafir basina limitler: uc core pakette de ayni (adil kullanim siniri, tier farki degil).
UPDATE products
SET options = options || '{"guest_media_count": 100, "guest_storage_gb": 4}'::jsonb
WHERE is_add_on = FALSE
  AND id IN ('standard', 'plus', 'premium');

-- Etkinlik basina depolama: acikca "sinirsiz" yazilir ki admin panelinde alan bos gorunmesin
-- ve limitin bilerek kapali oldugu belli olsun.
UPDATE products
SET options = options || '{"storage_gb": -1}'::jsonb
WHERE is_add_on = FALSE
  AND id IN ('standard', 'plus', 'premium');

SELECT id,
       options->>'guest_count'       AS guests,
       options->>'media_count'       AS media,
       options->>'guest_media_count' AS per_guest_media,
       options->>'guest_storage_gb'  AS per_guest_gb,
       options->>'storage_gb'        AS event_gb
FROM products
WHERE is_add_on = FALSE AND id IN ('standard', 'plus', 'premium')
ORDER BY id;

COMMIT;

-- PostgREST semayi onbellekler; yeni kolon yeniden yukleme sinyali olmadan gorunmez.
NOTIFY pgrst, 'reload schema';

-- Geri alma:
--   BEGIN;
--   UPDATE products SET options = options - 'guest_media_count' - 'guest_storage_gb' - 'storage_gb'
--     WHERE is_add_on = FALSE;
--   DROP INDEX IF EXISTS uploads_event_client_idx;
--   DROP INDEX IF EXISTS uploads_event_idx;
--   ALTER TABLE uploads DROP COLUMN IF EXISTS size_bytes;
--   COMMIT;
--   NOTIFY pgrst, 'reload schema';
