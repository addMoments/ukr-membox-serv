-- 7a — Etkinlik basina depolama limiti: ON KONTROL (Excel madde 2.17, ikinci yari)
--
-- SALT OKUNUR. Hicbir sey yazmaz. `7-package-storage-limits.sql` CALISTIRILMADAN ONCE bu
-- calistirilir; amaci "limiti koyarsak kim aninda kapanir" sorusunu limit konmadan once
-- cevaplamak.
--
-- Neden onemli: storage_gb backend'de GERCEK bir limittir (Check_upload_limits,
-- src/db_scripts/limits.go). Mevcut kullanimi zaten limitin ustunde olan bir etkinlikte
-- misafir yuklemesi limit yazilir yazilmaz 403 + STORAGE_LIMIT_REACHED verir. Mevcut icerik
-- silinmez, sadece yenisi eklenemez. 8 Eylul olcumunde tek bir etkinlik tek basina 38 GB'ti,
-- yani bu senaryo teorik degil.
--
-- Dikkat: sayim cope ATILMAMIS medyayi kapsar (trashed_at IS NULL), yani backend'in
-- Event_storage_bytes fonksiyonuyla birebir ayni. Cope atilan dosya S3'te durmaya devam
-- eder ama kotaya girmez (bkz. db-shell/misc/8-trash-quota.sql). Filtre unutulursa rapor
-- backend'in gordugunden YUKSEK cikar ve olmayan bir ariza gosterir.
--
-- Onerilen limitler (musteri, 10 Eylul 2026): Mini 5 GB, Classic 25 GB, Premium 100 GB.
-- Isim eslesmesi products.display_name_en uzerinden: MINI=standard, CLASSIC=plus, PREMIUM=premium.

\echo ''
\echo '=== 1. Paketlerin su anki limitleri ==='
SELECT id,
       options->>'guest_count'       AS guests,
       options->>'media_count'       AS media,
       options->>'guest_media_count' AS per_guest_media,
       options->>'guest_storage_gb'  AS per_guest_gb,
       options->>'storage_gb'        AS event_gb
FROM products
WHERE is_add_on = FALSE AND id IN ('standard', 'plus', 'premium')
ORDER BY id;

\echo ''
\echo '=== 2. Paket bazinda kullanim ve yeni limitin etkisi ==='
WITH proposed (id, gb) AS (
    VALUES ('standard', 5), ('plus', 25), ('premium', 100)
),
event_usage AS (
    SELECT e.uid,
           p.id AS package_id,
           COALESCE((
               SELECT SUM(u.size_bytes) FROM uploads u
               WHERE u.event_uid = e.uid
                 AND u.upload_type IN ('photo', 'video', 'voice')
                 AND u.trashed_at IS NULL
           ), 0) AS bytes
    FROM events e
    JOIN purchases pu  ON e.purchase_uid = pu.uid
    JOIN cart_items ci ON pu.cart_uid = ci.cart_uid
    JOIN products p    ON ci.product_uid = p.uid AND p.is_add_on = FALSE
    WHERE e.deleted_at IS NULL
)
SELECT pr.id                                                   AS package,
       pr.gb                                                   AS new_limit_gb,
       COUNT(eu.uid)                                           AS events,
       ROUND((COALESCE(SUM(eu.bytes), 0) / 1073741824.0)::numeric, 2) AS total_gb,
       ROUND((COALESCE(MAX(eu.bytes), 0) / 1073741824.0)::numeric, 2) AS biggest_event_gb,
       COUNT(*) FILTER (WHERE eu.bytes > pr.gb::bigint * 1073741824) AS would_be_blocked
FROM proposed pr
LEFT JOIN event_usage eu ON eu.package_id = pr.id
GROUP BY pr.id, pr.gb
ORDER BY pr.id;

\echo ''
\echo '=== 3. Yeni limiti ASAN etkinlikler (bu etkinliklerde yukleme aninda kapanir) ==='
WITH proposed (id, gb) AS (
    VALUES ('standard', 5), ('plus', 25), ('premium', 100)
),
event_usage AS (
    SELECT e.uid,
           e.name,
           e.active_until,
           p.id AS package_id,
           COALESCE((
               SELECT SUM(u.size_bytes) FROM uploads u
               WHERE u.event_uid = e.uid
                 AND u.upload_type IN ('photo', 'video', 'voice')
                 AND u.trashed_at IS NULL
           ), 0) AS bytes
    FROM events e
    JOIN purchases pu  ON e.purchase_uid = pu.uid
    JOIN cart_items ci ON pu.cart_uid = ci.cart_uid
    JOIN products p    ON ci.product_uid = p.uid AND p.is_add_on = FALSE
    WHERE e.deleted_at IS NULL
)
SELECT eu.uid,
       eu.name,
       eu.package_id                                     AS package,
       ROUND((eu.bytes / 1073741824.0)::numeric, 2)      AS used_gb,
       pr.gb                                             AS new_limit_gb,
       eu.active_until,
       (eu.active_until > NOW())                         AS still_open
FROM event_usage eu
JOIN proposed pr ON pr.id = eu.package_id
WHERE eu.bytes > pr.gb::bigint * 1073741824
ORDER BY eu.bytes DESC;

\echo ''
\echo '=== 4. Limite yaklasan etkinlikler (yeni limitin %80 ustu, henuz asmamis) ==='
WITH proposed (id, gb) AS (
    VALUES ('standard', 5), ('plus', 25), ('premium', 100)
),
event_usage AS (
    SELECT e.uid,
           e.name,
           p.id AS package_id,
           COALESCE((
               SELECT SUM(u.size_bytes) FROM uploads u
               WHERE u.event_uid = e.uid
                 AND u.upload_type IN ('photo', 'video', 'voice')
                 AND u.trashed_at IS NULL
           ), 0) AS bytes
    FROM events e
    JOIN purchases pu  ON e.purchase_uid = pu.uid
    JOIN cart_items ci ON pu.cart_uid = ci.cart_uid
    JOIN products p    ON ci.product_uid = p.uid AND p.is_add_on = FALSE
    WHERE e.deleted_at IS NULL
)
SELECT eu.uid,
       eu.name,
       eu.package_id                                AS package,
       ROUND((eu.bytes / 1073741824.0)::numeric, 2) AS used_gb,
       pr.gb                                        AS new_limit_gb
FROM event_usage eu
JOIN proposed pr ON pr.id = eu.package_id
WHERE eu.bytes > (pr.gb::bigint * 1073741824) * 0.8
  AND eu.bytes <= pr.gb::bigint * 1073741824
ORDER BY eu.bytes DESC;
