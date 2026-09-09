-- 5 — Misafir sayfasi "Could not load the event" duzeltmesi (2026-09-09)
--
-- BELIRTI: /guest/<packed> acilmiyor, ekranda "Could not load the event".
--   db.addmoments.com.ua/uploads?...  ->  401
--   {"code":"42501","message":"permission denied for table albums"}
--
-- KOK NEDEN: 3-albums.sql webanon'a albums uzerinde KOLON BAZLI SELECT verdi ve listeye
--   deleted_at'i koymadi (passcode'u bilerek disarida biraktik, deleted_at ise gozden kacti).
--   3-albums.sql'deki "webanon zaten auth'u miras aliyor, kolon kisiti baglayici degil"
--   notu CANLIDA GECERSIZ: prod'da webanon auth'un tablo duzeyi SELECT'ini almiyor
--   (2026-09-09'da dogrulandi: /albums?select=deleted_at -> 42501, ?select=privacy -> 200).
--
--   uploads uzerindeki uploads_guest_album_select politikasi
--       album_uid IN (SELECT a.uid FROM albums a WHERE a.deleted_at IS NULL AND ...)
--   diyor. Politika ifadesindeki bu ALT SORGU ayri bir rangetable girdisi oldugu icin
--   normal kolon yetki denetimine tabi (tablonun kendi politikasindaki qual'lar muaftir --
--   bu yuzden /albums sorgusu 200 donerken /uploads patliyordu). Misafirin KENDI
--   yuklemelerini soran sorgu bile bu politikadan gecmek zorunda (permissive politikalarin
--   hepsi OR'lanmadan once degerlendirilir), dolayisiyla misafir sayfasinin acilis
--   bootstrap'i tamamen kiriliyordu.
--
--   Zincirin geri kalani: PostgREST anon rolde 42501'i 401'e ceviriyor -> core.ts'teki
--   "401 ise token'i sil, tokensiz tekrar dene" dusumu devreye giriyor -> proxy bu kez
--   "authorization header required" ile 401 donuyor -> bootstrap catch'i bootstrapFailed
--   yapiyor -> "Could not load the event" ekrani.
--
-- COZUM: deleted_at'i webanon'un SELECT kolon listesine ekle. passcode disarida kalmaya
--   devam ediyor. deleted_at hassas degil; satir gorunurlugunu zaten RLS belirliyor.

BEGIN;

GRANT SELECT (deleted_at) ON albums TO webanon;

-- Dogrulama: asagidaki iki satir da 't' olmali.
SELECT
    has_column_privilege('webanon', 'albums', 'deleted_at', 'SELECT') AS deleted_at_ok,
    NOT has_column_privilege('webanon', 'albums', 'passcode', 'SELECT') AS passcode_still_hidden;

COMMIT;

-- Kolon yetkileri sema onbelleginde tutulmuyor; NOTIFY gerekmez ama zararsiz.
--
-- Canli dogrulama (misafir token'i alip uploads'i sor):
--   T=$(curl -s -D - -o /dev/null -H 'X-Event: <packedUid>' \
--        https://serv.addmoments.com.ua/api/guest/whoami | awk '/^x-auth-token/{print $2}' | tr -d '\r')
--   curl -s -w ' [%{http_code}]\n' -H "Authorization: Bearer $T" -H 'X-Event: <packedUid>' \
--     "https://db.addmoments.com.ua/uploads?event_uid=eq.<eventUid>&upload_type=in.(photo,video)&trashed_at=is.null&limit=3"
--   -> 200 ve JSON dizi donmeli.
--
-- Geri alma:
--   REVOKE SELECT (deleted_at) ON albums FROM webanon;
