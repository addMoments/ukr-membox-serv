-- 16 — Yeni etkinligin General albumu misafire acik baslar (2026-10-22)
--
-- 15-album-decides-gallery.sql General'i guest_view=FALSE aciyordu (eski etkinlik anahtarinin
-- "yeni etkinlikte kapali" varsayilanini korumak icin). Kullanici karari ayni gun: General
-- misafire acik baslasin. Trigger kolon varsayilanina (guest_view DEFAULT TRUE) doner.
--
-- 15 ile bu dosya arasinda acilan etkinliklerin General'i kapali olustu; 15'in yedeginde
-- (albums_guest_view_backup_20261022) olmayan, yani 15'ten sonra olusan kapali General'lar acilir.
-- Eski etkinliklere dokunulmaz.
--
-- Tek transaction. Politika/sema degismedigi icin NOTIFY gerekmez.

BEGIN;

CREATE OR REPLACE FUNCTION create_default_album()
RETURNS TRIGGER AS $$
BEGIN
    -- guest_view kolon varsayilani (TRUE): misafir General'in foto/videolarini gorur;
    -- host albumun ayarlarindan kapatabilir.
    INSERT INTO albums (event_uid, name, is_default)
    VALUES (NEW.uid, 'General', TRUE)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 15'ten sonra kapali olusan General'lar (acilacaklar):
SELECT e.name AS event, a.created_at
FROM albums a JOIN events e ON e.uid = a.event_uid
WHERE a.is_default AND NOT a.guest_view
  AND NOT EXISTS (SELECT 1 FROM albums_guest_view_backup_20261022 b WHERE b.uid = a.uid);

UPDATE albums a
SET guest_view = TRUE
WHERE a.is_default AND NOT a.guest_view
  AND NOT EXISTS (SELECT 1 FROM albums_guest_view_backup_20261022 b WHERE b.uid = a.uid);

-- DOGRULAMA: trigger guest_view yazmiyor (TRUE varsayilani) ve 15'ten sonra kapali General kalmadi.
SELECT
    (SELECT prosrc NOT LIKE '%guest_view)%' FROM pg_proc WHERE proname = 'create_default_album') AS trigger_uses_default,
    (SELECT COUNT(*) FROM albums a WHERE a.is_default AND NOT a.guest_view
       AND NOT EXISTS (SELECT 1 FROM albums_guest_view_backup_20261022 b WHERE b.uid = a.uid)) AS new_generals_closed;
-- trigger_uses_default = t, new_generals_closed = 0 olmali.

COMMIT;
