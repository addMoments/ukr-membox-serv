-- 6 — plus/premium paket limitlerinin onarimi (2026-09-10)
--
-- ############################################################################
-- BU SCRIPT ARTIK CALISTIRILMAZ. Yerine `7-package-storage-limits.sql` gecti.
-- Sebep: buradaki onarim storage_gb'yi -1 (sinirsiz) yaziyor. Musteri ayni gun
-- etkinlik basina gercek rakamlari verdi (Mini 5 / Classic 25 / Premium 100 GB),
-- yani 6 calistirilsa -1 yazilip hemen ustune 7 ile dogru deger yazilacakti.
-- 7 hem bu onarimi hem yeni limitleri tek transaction'da yapar.
-- Dosya, 9 Eylul arizasinin kaydi olarak duruyor.
-- ############################################################################
--
-- Ne oldu: 2026-09-09'da 13:04 ile 14:36 UTC arasinda admin panelinden paketler
-- kaydedilirken `plus` ve `premium` urunlerinin storage_gb, guest_media_count ve
-- guest_storage_gb degerleri 0 olarak yazildi. Panelin `readNumber` yardimcisi bos
-- birakilan alani Number('') === 0 ile 0'a ceviriyordu (bu deploy'da duzeltildi:
-- src/pages/admin/products.tsx).
--
-- Etkisi: 0, koda gore gercek bir limittir (Check_upload_limits, limits.go). Yani plus ve
-- premium satin alinmis TUM etkinliklerde misafir foto/video yuklemesi kapandi; misafir
-- 403 + STORAGE_LIMIT_REACHED / GUEST_MEDIA_LIMIT_REACHED goruyordu. Guestbook metni
-- PostgREST'e dogrudan yazildigi icin etkilenmedi, bu yuzden ariza kismi gorundu.
--
-- Ne yapar: iki paketi 4-upload-limits.sql'in yazdigi (ve `standard`ta bozulmadan duran)
-- degerlere geri cevirir. Musteri talebi 2026-09-08: "4 gb - 100 media for one user".
--
-- CANLI VERIYE YAZAR. Once yedek al: db-backups/products-pre-limit-restore-2026-09-10.csv

BEGIN;

UPDATE products
SET options = options || '{"storage_gb": -1, "guest_media_count": 100, "guest_storage_gb": 4}'::jsonb
WHERE is_add_on = FALSE
  AND id IN ('plus', 'premium');

-- Dogrulama: uc core pakette de per_guest_media=100, per_guest_gb=4, event_gb=-1 olmali.
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

-- Geri alma (sifirlara donmek icin — normalde gerekmez):
--   UPDATE products SET options = options || '{"storage_gb": 0, "guest_media_count": 0, "guest_storage_gb": 0}'::jsonb
--     WHERE is_add_on = FALSE AND id IN ('plus', 'premium');
