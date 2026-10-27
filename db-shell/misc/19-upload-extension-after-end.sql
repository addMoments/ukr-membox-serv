-- 19 — +1 ay yukleme (link uzatma) yukleme suresi bittikten sonra da (2026-10-27)
--
-- Musteri 27 Ekim 14:38 (suresi dolmus Premium etkinlik ekran goruntusu):
--   "-6 month storage: visible ✅  -extra 25 GB: not visible ✅
--    -1 month Link extension: ❌ This is not visible, but should be."
-- 17'de +1 ay yukleme de +25 GB gibi yalniz yukleme suresi acikken satiliyordu (Event_upgrade_options).
-- Artik galeri saklanirken (storage_until gelecekte) de satilir; +25 GB eskisi gibi yukleme bitince gizli.
--
-- Tarih kurali (upgrade_extra_dates):
--   yukleme suresi aciksa: eskisi gibi, yukleme ve galeri bitisi alinan ay kadar kayar.
--   yukleme suresi bittiyse: link satin alindigi andan itibaren ay kadar acilir; galeri bitisi de
--     ayni toplam kadar kayar (bitis ile satin alma arasindaki bosluk + ay), yukleme sonrasi saklama
--     suresi kisalmaz. Eski bitise ay eklemek, aylar once bitmis bir etkinlikte linki kapali birakirdi.
--   +6 ay galeri (time) degismedi: yalniz galeri bitisine eklenir.
-- Paket yukseltmesi dali degismedi.
--
-- SIRA: bu dosya -> backend. Eski backend bu dosyadan sonra da calisir (suresi gecmis etkinlige
-- +1 ay yukleme satmadigi icin yeni dal hic calismaz). Tek transaction; COMMIT'ten once kendini dogrular.

BEGIN;

-- Ek ay kalemlerinin (upload/time) yeni tarihleri. p_now parametre: dogrulama sabit tarihle cagirir.
CREATE OR REPLACE FUNCTION upgrade_extra_dates(p_active_until TIMESTAMPTZ, p_storage_until TIMESTAMPTZ,
                                               p_upload_months INT, p_storage_months INT, p_now TIMESTAMPTZ,
                                               OUT new_active TIMESTAMPTZ, OUT new_storage TIMESTAMPTZ)
LANGUAGE plpgsql STABLE AS $$
DECLARE
    v_gap INTERVAL := INTERVAL '0';
BEGIN
    new_active := p_active_until;
    IF p_upload_months > 0 THEN
        IF p_active_until < p_now THEN
            v_gap := p_now - p_active_until;
        END IF;
        new_active := p_active_until + v_gap + make_interval(months => p_upload_months);
    END IF;
    new_storage := p_storage_until + v_gap + make_interval(months => p_upload_months + p_storage_months);
END;
$$;

CREATE OR REPLACE FUNCTION apply_event_upgrade(p_purchase_uid UUID)
RETURNS UUID
LANGUAGE plpgsql AS $$
DECLARE
    v_event_uid     UUID;
    v_cart_uid      UUID;
    v_applied_at    TIMESTAMPTZ;
    v_items         INT;
    v_has_package   BOOLEAN;
    v_months        INT;
    v_upload_months INT;
    v_activation    TIMESTAMPTZ;
    v_active_until  TIMESTAMPTZ;
    v_storage_until TIMESTAMPTZ;
    v_extended_at   TIMESTAMPTZ;
    v_new_active    TIMESTAMPTZ;
    v_new_storage   TIMESTAMPTZ;
BEGIN
    SELECT event_uid, cart_uid, upgrade_applied_at
      INTO v_event_uid, v_cart_uid, v_applied_at
      FROM purchases WHERE uid = p_purchase_uid
       FOR UPDATE;
    IF NOT FOUND OR v_event_uid IS NULL THEN
        RAISE EXCEPTION 'NOT_AN_UPGRADE: purchase % is not an event upgrade', p_purchase_uid;
    END IF;
    IF v_applied_at IS NOT NULL THEN
        RETURN v_event_uid;
    END IF;

    -- Bir sipariste ya tek paket kalemi ya da storage/time/upload kalemleri (adetli) olur
    -- (upgrade.go). Ay sayilari ayni turdeki kalemlerin toplami.
    SELECT COUNT(*),
           COALESCE(bool_or(p.options->>'upgrade_kind' = 'package'), FALSE),
           COALESCE(SUM(CASE WHEN p.options->>'upgrade_kind' = 'time'
                             THEN COALESCE(NULLIF(p.options->>'extra_storage_months', '')::INT, 0) * ci.quantity
                        END), 0)::INT,
           COALESCE(SUM(CASE WHEN p.options->>'upgrade_kind' = 'upload'
                             THEN COALESCE(NULLIF(p.options->>'extra_upload_months', '')::INT, 0) * ci.quantity
                        END), 0)::INT
      INTO v_items, v_has_package, v_months, v_upload_months
      FROM cart_items ci JOIN products p ON p.uid = ci.product_uid
     WHERE ci.cart_uid = v_cart_uid AND p.options ? 'upgrade_kind';
    IF v_items = 0 THEN
        RAISE EXCEPTION 'NOT_AN_UPGRADE: purchase % has no upgrade item', p_purchase_uid;
    END IF;

    UPDATE cart_items SET status = 'purchased' WHERE cart_uid = v_cart_uid;
    UPDATE purchases SET upgrade_applied_at = NOW() WHERE uid = p_purchase_uid;

    SELECT activation_date, active_until, storage_until, storage_extended_at
      INTO v_activation, v_active_until, v_storage_until, v_extended_at
      FROM events WHERE uid = v_event_uid
       FOR UPDATE;
    IF v_activation IS NULL OR v_active_until IS NULL OR v_storage_until IS NULL THEN
        RETURN v_event_uid;
    END IF;

    v_new_active  := v_active_until;
    v_new_storage := v_storage_until;
    IF v_has_package THEN
        -- upgrade_applied_at yukarida yazildi: get_event_* artik yeni paketi okur.
        v_new_active  := GREATEST(v_active_until,
                                  v_activation + make_interval(days => get_event_activation_days(v_event_uid))
                                  + make_interval(months => event_extra_upload_months(v_event_uid)));
        v_new_storage := GREATEST(v_storage_until,
                                  v_new_active
                                  + make_interval(days => get_event_storage_days(v_event_uid))
                                  + CASE WHEN v_extended_at IS NOT NULL THEN INTERVAL '30 days' ELSE INTERVAL '0' END
                                  + make_interval(months => event_extra_storage_months(v_event_uid)));
    ELSE
        SELECT d.new_active, d.new_storage INTO v_new_active, v_new_storage
          FROM upgrade_extra_dates(v_active_until, v_storage_until, v_upload_months, v_months, NOW()) d;
    END IF;

    IF v_new_active IS DISTINCT FROM v_active_until OR v_new_storage IS DISTINCT FROM v_storage_until THEN
        UPDATE events
           SET active_until  = v_new_active,
               storage_until = v_new_storage,
               -- "14 gun icinde silinecek" uyarisi eski tarihe gore gitmis olabilir.
               storage_warning_mail_sent_at = CASE WHEN v_new_storage IS DISTINCT FROM v_storage_until
                                                   THEN NULL ELSE storage_warning_mail_sent_at END
         WHERE uid = v_event_uid;
    END IF;

    RETURN v_event_uid;
END;
$$;

-- Dogrulama tarihleri UTC'de; DST gecisi gun ekleyen aritmetigi oturum saat dilimine gore kaydirmasin.
SET LOCAL TIME ZONE 'UTC';

DO $$
DECLARE
    v_open_a  TIMESTAMPTZ; v_open_s  TIMESTAMPTZ;
    v_ended_a TIMESTAMPTZ; v_ended_s TIMESTAMPTZ;
    v_time_a  TIMESTAMPTZ; v_time_s  TIMESTAMPTZ;
    v_uses    BOOLEAN;
BEGIN
    -- Yukleme acik: 10 Kasim'e kadar, galeri 10 Mayis. +1 ay -> 10 Aralik / 10 Haziran (eskisi gibi).
    SELECT new_active, new_storage INTO v_open_a, v_open_s
      FROM upgrade_extra_dates('2026-11-10 12:00+00', '2027-05-10 12:00+00', 1, 0, '2026-10-27 12:00+00');
    -- Yukleme 26 Eylul'de bitti, galeri 26 Mart. 27 Ekim'de +1 ay -> 27 Kasim / 26 Mayis (+31 gun bosluk +1 ay).
    SELECT new_active, new_storage INTO v_ended_a, v_ended_s
      FROM upgrade_extra_dates('2026-09-26 12:00+00', '2027-03-26 12:00+00', 1, 0, '2026-10-27 12:00+00');
    -- Yalniz +6 ay galeri, yukleme bitmis: yukleme bitisi degismez.
    SELECT new_active, new_storage INTO v_time_a, v_time_s
      FROM upgrade_extra_dates('2026-09-26 12:00+00', '2027-03-26 12:00+00', 0, 6, '2026-10-27 12:00+00');
    SELECT pg_get_functiondef('apply_event_upgrade(uuid)'::regprocedure) LIKE '%upgrade_extra_dates%' INTO v_uses;
    RAISE NOTICE 'open=% / % ended=% / % time=% / % apply_uses_helper=%',
        v_open_a, v_open_s, v_ended_a, v_ended_s, v_time_a, v_time_s, v_uses;
    IF v_open_a <> '2026-12-10 12:00+00' OR v_open_s <> '2027-06-10 12:00+00'
       OR v_ended_a <> '2026-11-27 12:00+00' OR v_ended_s <> '2027-05-26 12:00+00'
       OR v_time_a <> '2026-09-26 12:00+00' OR v_time_s <> '2027-09-26 12:00+00'
       OR NOT v_uses THEN
        RAISE EXCEPTION '19 dogrulama tutmadi, geri aliniyor';
    END IF;
END;
$$;

COMMIT;
