-- 8b — 8-trash-quota.sql testleri
--
-- ############################################################################
-- CANLI VERITABANINDA CALISTIRILMAZ. Kendi semasini kurar ve sahte veri yazar.
-- Bos, tek kullanimlik bir veritabani ister. Asagidaki guard uretim gibi gorunen
-- bir veritabaninda calismayi reddeder ama guvenlik yalnizca ona birakilmaz.
-- ############################################################################
--
-- Nasil calistirilir (tek kullanimlik konteyner):
--
--   cd ukr-membox-serv/db-shell/misc
--   docker run -d --name pgtest -e POSTGRES_PASSWORD=test -e POSTGRES_DB=testdb \
--     -v "$PWD:/sql:ro" postgres:16-alpine
--   # konteynerin ilk kurulumu birkac saniye surer; testdb hazir olana kadar bekle:
--   until docker exec pgtest psql -U postgres -d testdb -c 'SELECT 1' >/dev/null 2>&1; do :; done
--   docker exec pgtest psql -U postgres -d testdb -q -t -A -f /sql/8b-trash-quota-test.sql
--   docker rm -f pgtest
--
-- Klasor -v ile baglanir ve -f kullanilir; dosya stdin'den verilirse asagidaki
-- \ir satiri 8-trash-quota.sql'i bulamaz (psql konteynerin icinde calisiyor).
--
-- Beklenen: 17 satirin tamami PASS. Tek bir FAIL bile trigger'in davranisinin
-- degistigi anlamina gelir.
--
-- Neden test dosyasi var: kural iki yerde birden yasiyor (Go tarafinda sayaclar,
-- burada geri alma korumasi) ve ikisinin sayma kurallari birebir ayni olmak zorunda.
-- Sessizce ayrisirlarsa kota bir yerde bosalir, digerinde dolu kalir.

-- Ilk hatada dur: asagidaki guard tetiklenirse geri kalani hic calismamali.
\set ON_ERROR_STOP on

-- Uretim guard'i: canli semada bulunan ama bu testin kurmadigi bir tablo varsa dur.
DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = 'public' AND table_name IN ('users', 'credentials')) THEN
        RAISE EXCEPTION 'Bu veritabani uretim gibi gorunuyor (users/credentials tablosu var). Test calistirilmadi.';
    END IF;
END $guard$;

-- --------------------------------------------------------------------------
-- Gercek semanin trigger icin gereken en kucuk parcasi
-- --------------------------------------------------------------------------
-- Gercek semanin trigger icin gereken en kucuk parcasi.
CREATE TYPE UPLOAD_TYPE AS ENUM ('photo', 'video', 'voice', 'text');

CREATE TABLE products (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    id TEXT UNIQUE NOT NULL,
    is_add_on BOOLEAN NOT NULL DEFAULT FALSE,
    options JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE carts (uid UUID PRIMARY KEY DEFAULT gen_random_uuid());

CREATE TABLE cart_items (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_uid UUID REFERENCES carts(uid) NOT NULL,
    product_uid UUID REFERENCES products(uid) NOT NULL
);

CREATE TABLE purchases (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_uid UUID REFERENCES carts(uid) NOT NULL
);

CREATE TABLE events (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL DEFAULT '',
    purchase_uid UUID REFERENCES purchases(uid),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE participants (uid UUID PRIMARY KEY DEFAULT gen_random_uuid());

CREATE TABLE uploads (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_type UPLOAD_TYPE NOT NULL,
    client_uid UUID REFERENCES participants(uid) NOT NULL,
    event_uid UUID REFERENCES events(uid) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    value TEXT NOT NULL DEFAULT '',
    trashed_at TIMESTAMPTZ,
    size_bytes BIGINT NOT NULL DEFAULT 0
);

-- --------------------------------------------------------------------------
-- Test edilen migration
-- --------------------------------------------------------------------------
\ir 8-trash-quota.sql

-- uploads_restore_quota trigger'inin davranis testleri.
--
-- Not: kurulum ve geri alma AYNI ifadede olamaz. Tek bir SELECT icindeki CTE'lerin
-- ekledigi satirlar ayni ifadedeki trigger'a gorunmez, o yuzden her test kendi
-- plpgsql fonksiyonunda sirayla calisir.

\pset pager off

CREATE OR REPLACE FUNCTION mk_event(p_options JSONB) RETURNS UUID AS $$
DECLARE v_prod UUID; v_cart UUID; v_pur UUID; v_ev UUID;
BEGIN
    INSERT INTO products (id, is_add_on, options)
        VALUES (gen_random_uuid()::text, FALSE, p_options) RETURNING uid INTO v_prod;
    INSERT INTO carts DEFAULT VALUES RETURNING uid INTO v_cart;
    INSERT INTO cart_items (cart_uid, product_uid) VALUES (v_cart, v_prod);
    INSERT INTO purchases (cart_uid) VALUES (v_cart) RETURNING uid INTO v_pur;
    INSERT INTO events (purchase_uid) VALUES (v_pur) RETURNING uid INTO v_ev;
    RETURN v_ev;
END $$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION mk_upload(p_ev UUID, p_spec TEXT, p_trashed BOOLEAN)
RETURNS UUID AS $$
DECLARE v_pt UUID; v_up UUID; parts TEXT[];
BEGIN
    parts := string_to_array(p_spec, ':');
    INSERT INTO participants DEFAULT VALUES RETURNING uid INTO v_pt;
    INSERT INTO uploads (upload_type, client_uid, event_uid, size_bytes, trashed_at)
        VALUES (parts[1]::UPLOAD_TYPE, v_pt, p_ev, parts[2]::BIGINT,
                CASE WHEN p_trashed THEN NOW() ELSE NULL END)
        RETURNING uid INTO v_up;
    RETURN v_up;
END $$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION assert_result(p_name TEXT, p_got TEXT, p_want TEXT) RETURNS TEXT AS $$
BEGIN
    IF p_want = 'OK' THEN
        IF p_got = 'OK' THEN RETURN 'PASS  ' || p_name; END IF;
        RETURN 'FAIL  ' || p_name || '  --> beklenen OK, gelen: ' || p_got;
    END IF;
    IF p_got LIKE 'RESTORE_QUOTA_EXCEEDED%' THEN RETURN 'PASS  ' || p_name; END IF;
    RETURN 'FAIL  ' || p_name || '  --> beklenen RED, gelen: ' || p_got;
END $$ LANGUAGE plpgsql;

-- t_case: paketi kurar, galeriye ve cope dosyalari koyar, sonra hedefi geri almayi dener.
CREATE OR REPLACE FUNCTION t_case(
    p_name TEXT, p_options JSONB,
    p_gallery TEXT[],       -- galeride duran dosyalar
    p_trash_extra TEXT[],   -- copte duran DIGER dosyalar
    p_target TEXT,          -- geri alinacak dosya (copte)
    p_want TEXT
) RETURNS TEXT AS $$
DECLARE v_ev UUID; v_up UUID; v_got TEXT; item TEXT;
BEGIN
    v_ev := mk_event(p_options);
    FOREACH item IN ARRAY COALESCE(p_gallery, ARRAY[]::TEXT[]) LOOP
        PERFORM mk_upload(v_ev, item, FALSE);
    END LOOP;
    FOREACH item IN ARRAY COALESCE(p_trash_extra, ARRAY[]::TEXT[]) LOOP
        PERFORM mk_upload(v_ev, item, TRUE);
    END LOOP;
    v_up := mk_upload(v_ev, p_target, TRUE);

    BEGIN
        UPDATE uploads SET trashed_at = NULL WHERE uid = v_up;
        v_got := 'OK';
    EXCEPTION WHEN OTHERS THEN
        v_got := SQLERRM;
    END;
    RETURN assert_result(p_name, v_got, p_want);
END $$ LANGUAGE plpgsql;

-- 1 GB = 1073741824 bayt. 900 MB = 943718400, 500 MB = 524288000, 300 MB = 314572800.

SELECT t_case('T1  depolama limiti asiliyor -> RED',
    '{"storage_gb": 1, "media_count": -1}', ARRAY['photo:943718400'], NULL, 'photo:314572800', 'RED');

SELECT t_case('T2  depolama limiti icinde -> OK',
    '{"storage_gb": 1, "media_count": -1}', ARRAY['photo:524288000'], NULL, 'photo:314572800', 'OK');

SELECT t_case('T3  tam sinirda esitlik -> OK',
    '{"storage_gb": 1, "media_count": -1}', ARRAY['photo:536870912'], NULL, 'photo:536870912', 'OK');

SELECT t_case('T3b bir bayt fazlasi -> RED',
    '{"storage_gb": 1, "media_count": -1}', ARRAY['photo:536870913'], NULL, 'photo:536870912', 'RED');

SELECT t_case('T4  adet limiti asiliyor -> RED',
    '{"storage_gb": -1, "media_count": 2}', ARRAY['photo:10','photo:10'], NULL, 'photo:10', 'RED');

SELECT t_case('T4b adet limiti tam doluyor -> OK',
    '{"storage_gb": -1, "media_count": 2}', ARRAY['photo:10'], NULL, 'photo:10', 'OK');

SELECT t_case('T5  sinirsiz paket -> OK',
    '{"storage_gb": -1, "media_count": -1}', NULL, NULL, 'photo:999999999999', 'OK');

SELECT t_case('T6  limit anahtari yok -> OK',
    '{"guest_count": 30}', NULL, NULL, 'photo:999999999999', 'OK');

SELECT t_case('T7  voice depolamaya sayilir -> RED',
    '{"storage_gb": 1, "media_count": -1}', ARRAY['photo:943718400'], NULL, 'voice:314572800', 'RED');

SELECT t_case('T8  voice adet limitine girmez -> OK',
    '{"storage_gb": -1, "media_count": 1}', ARRAY['photo:10'], NULL, 'voice:10', 'OK');

SELECT t_case('T9  text kotadan muaf -> OK',
    '{"storage_gb": 0, "media_count": 0}', NULL, NULL, 'text:999999999', 'OK');

SELECT t_case('T13 copteki digerleri sayilmaz -> OK',
    '{"storage_gb": 1, "media_count": -1}', ARRAY['photo:838860800'],
    ARRAY['photo:5368709120'], 'photo:104857600', 'OK');

SELECT t_case('T14 metin kayitlari depolamaya girmez -> OK',
    '{"storage_gb": 1, "media_count": -1}', ARRAY['text:999999999999','photo:524288000'],
    NULL, 'photo:314572800', 'OK');

-- T10: cope ATMA yonu asla engellenmez.
CREATE OR REPLACE FUNCTION t10() RETURNS TEXT AS $$
DECLARE v_ev UUID; v_up UUID;
BEGIN
    v_ev := mk_event('{"storage_gb": 0, "media_count": 0}');
    v_up := mk_upload(v_ev, 'photo:500000000', FALSE);
    UPDATE uploads SET trashed_at = NOW() WHERE uid = v_up;
    RETURN 'OK';
EXCEPTION WHEN OTHERS THEN RETURN SQLERRM;
END $$ LANGUAGE plpgsql;
SELECT assert_result('T10 cope atma engellenmez -> OK', t10(), 'OK');

-- T11: trashed_at disindaki UPDATE'ler etkilenmez.
CREATE OR REPLACE FUNCTION t11() RETURNS TEXT AS $$
DECLARE v_ev UUID; v_up UUID;
BEGIN
    v_ev := mk_event('{"storage_gb": 0, "media_count": 0}');
    v_up := mk_upload(v_ev, 'photo:500000000', FALSE);
    UPDATE uploads SET value = 'yeni-deger' WHERE uid = v_up;
    RETURN 'OK';
EXCEPTION WHEN OTHERS THEN RETURN SQLERRM;
END $$ LANGUAGE plpgsql;
SELECT assert_result('T11 ilgisiz UPDATE etkilenmez -> OK', t11(), 'OK');

-- T12: baska etkinligin dolulugu bu etkinligi etkilemez.
CREATE OR REPLACE FUNCTION t12() RETURNS TEXT AS $$
DECLARE v_a UUID; v_b UUID; v_up UUID;
BEGIN
    v_b := mk_event('{"storage_gb": 1, "media_count": -1}');
    PERFORM mk_upload(v_b, 'photo:1073741800', FALSE);   -- komsu etkinlik neredeyse dolu
    v_a := mk_event('{"storage_gb": 1, "media_count": -1}');
    v_up := mk_upload(v_a, 'photo:1000', TRUE);
    UPDATE uploads SET trashed_at = NULL WHERE uid = v_up;
    RETURN 'OK';
EXCEPTION WHEN OTHERS THEN RETURN SQLERRM;
END $$ LANGUAGE plpgsql;
SELECT assert_result('T12 etkinlikler birbirini etkilemez -> OK', t12(), 'OK');

-- T15: geri alma reddedildiginde satir GERCEKTEN copte kalmali (rollback dogrulamasi).
CREATE OR REPLACE FUNCTION t15() RETURNS TEXT AS $$
DECLARE v_ev UUID; v_up UUID; v_still BOOLEAN;
BEGIN
    v_ev := mk_event('{"storage_gb": 1, "media_count": -1}');
    PERFORM mk_upload(v_ev, 'photo:943718400', FALSE);
    v_up := mk_upload(v_ev, 'photo:314572800', TRUE);
    BEGIN
        UPDATE uploads SET trashed_at = NULL WHERE uid = v_up;
    EXCEPTION WHEN OTHERS THEN NULL;
    END;
    SELECT trashed_at IS NOT NULL INTO v_still FROM uploads WHERE uid = v_up;
    IF v_still THEN RETURN 'OK'; END IF;
    RETURN 'satir copten cikmis, reddedilen guncelleme yazilmis';
END $$ LANGUAGE plpgsql;
SELECT assert_result('T15 reddedilen geri alma yazilmaz -> OK', t15(), 'OK');
