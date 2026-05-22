-- ============================================================================
-- Database Roles and Permissions Setup
-- ============================================================================
-- This script sets up the security model for the membox database
-- Run this as the postgres superuser or database owner

-- ============================================================================
-- 1. CREATE ROLES
-- ============================================================================

-- Anonymous role (for unauthenticated API requests)
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'webanon') THEN
        CREATE ROLE webanon NOLOGIN;
    END IF;
END $$;

-- Make it a LOGIN role and set password
ALTER ROLE webanon LOGIN;
-- TODO: Replace with your actual password from postgrest.conf
-- ALTER ROLE webanon WITH PASSWORD 'redacted';

-- Authenticated role (for users with valid JWT tokens)
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'auth') THEN
        CREATE ROLE auth NOLOGIN;
    END IF;
END $$;

-- ============================================================================
-- 2. CONFIGURE ROLE RELATIONSHIPS
-- ============================================================================

-- Allow webanon (authenticator role) to switch to auth role via JWT
-- This is required for PostgREST to switch roles based on JWT claims
GRANT auth TO webanon;

-- ============================================================================
-- 3. GRANT SCHEMA ACCESS
-- ============================================================================

-- Both roles need USAGE on public schema (default is already granted)
GRANT USAGE ON SCHEMA public TO webanon;
GRANT USAGE ON SCHEMA public TO auth;

-- ============================================================================
-- 4. ANONYMOUS (webanon) PERMISSIONS
-- ============================================================================
-- Currently: NO permissions (locked down)
-- Uncomment lines below to grant specific permissions as needed:

-- Example: Allow browsing public events
-- GRANT SELECT ON events TO webanon;

-- Example: Allow reading public global attributes
-- GRANT SELECT ON global_attributes TO webanon;

-- Allow webanon to access uploads (RLS will restrict to their own uploads)
GRANT SELECT, INSERT, UPDATE, DELETE ON uploads TO webanon;

-- Allow webanon to access participants (needed for foreign key resolution and self-update)
GRANT SELECT, UPDATE ON participants TO webanon;

-- Allow anonymous users to view public event information (via view)
-- This view excludes sensitive fields like admins, purchase_uid, created_at
GRANT SELECT ON events_public TO webanon;
GRANT SELECT ON events_public TO auth;

-- Allow all users to view products (pricing/catalog information)
GRANT SELECT ON products TO webanon;
GRANT SELECT ON products TO auth;

-- Site panel admin rolleri:
--   - order_admin : sadece admin order ekranlarini gorecek kisiler
--   - super_admin : ileride super adminleri DB'ye tasimak icin hazir rol
-- Mevcut super admin kaynagi env.admin_emails olarak kalir; bu tablo bosken
-- mevcut sistem davranisi degismez.
CREATE TABLE IF NOT EXISTS panel_admins (
    user_uid UUID PRIMARY KEY REFERENCES users(uid) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('order_admin', 'super_admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by_uid UUID NULL REFERENCES users(uid) ON DELETE SET NULL,
    deleted_at TIMESTAMPTZ NULL,
    deleted_by_uid UUID NULL REFERENCES users(uid) ON DELETE SET NULL
);

-- Panel adminleri hard-delete etmek yerine revoke bilgisini saklar.
-- IF NOT EXISTS, mevcut ortamlarda setup tekrar calistiginda deploy'u kirmaz.
ALTER TABLE panel_admins ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;
ALTER TABLE panel_admins ADD COLUMN IF NOT EXISTS deleted_by_uid UUID NULL REFERENCES users(uid) ON DELETE SET NULL;

-- Partnership kayitlari promo kodlarinin hangi kisi/kurum kanaliyla iliskili
-- oldugunu tutar. Kayitlar editlenebilir; eski raporlar purchase snapshotlariyla korunur.
CREATE TABLE IF NOT EXISTS partnerships (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    surname TEXT NOT NULL,
    company_name TEXT,
    phone TEXT,
    email TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

-- Tablo onceden daha dar olusturulduysa setup tekrar calistiginda tamamlanir.
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS name TEXT;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS surname TEXT;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS company_name TEXT;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS phone TEXT;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE partnerships ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
UPDATE partnerships
SET name = COALESCE(NULLIF(BTRIM(name), ''), 'Unknown')
WHERE name IS NULL OR BTRIM(name) = '';
UPDATE partnerships
SET surname = COALESCE(NULLIF(BTRIM(surname), ''), 'Unknown')
WHERE surname IS NULL OR BTRIM(surname) = '';
ALTER TABLE partnerships ALTER COLUMN name SET NOT NULL;
ALTER TABLE partnerships ALTER COLUMN surname SET NOT NULL;

-- Promo kodlari:
--   - MVP'de yalniz yuzde indirim desteklenir (`discount_type = 'percent'`).
--   - `valid_from` bos birakilirsa CURRENT_TIMESTAMP ile dolar; `valid_until` ve `usage_limit_total` opsiyoneldir.
--   - `is_expired` tutulmaz; pasiflik `is_active`, `deactivated_at`, `deactivated_reason` ile aciklanir.
CREATE TABLE IF NOT EXISTS promo_codes (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    partnership_uid UUID REFERENCES partnerships(uid),
    code TEXT NOT NULL,
    discount_type TEXT NOT NULL DEFAULT 'percent',
    discount_value DECIMAL(10, 2) NOT NULL,
    valid_from TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    valid_until TIMESTAMPTZ,
    usage_limit_total INT,
    usage_count INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    deactivated_at TIMESTAMPTZ,
    deactivated_reason TEXT,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Tablo onceden eksik kolonlarla olusmussa migration tekrar calistiginda tamamlanir.
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS partnership_uid UUID REFERENCES partnerships(uid);
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS code TEXT NOT NULL;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS discount_type TEXT NOT NULL DEFAULT 'percent';
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS discount_value DECIMAL(10, 2) NOT NULL;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS usage_limit_total INT;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS usage_count INT NOT NULL DEFAULT 0;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS deactivated_at TIMESTAMPTZ;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS deactivated_reason TEXT;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE promo_codes ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE promo_codes ALTER COLUMN valid_from SET DEFAULT CURRENT_TIMESTAMP;
UPDATE promo_codes
SET valid_from = COALESCE(created_at, CURRENT_TIMESTAMP)
WHERE valid_from IS NULL;
ALTER TABLE promo_codes ALTER COLUMN valid_from SET NOT NULL;

-- Promo veri kurallari DB tarafinda da korunur; backend validation bunun uzerine ek guvenlik katmanidir.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'promo_codes_discount_type_check') THEN
        ALTER TABLE promo_codes
            ADD CONSTRAINT promo_codes_discount_type_check
            CHECK (discount_type IN ('percent'));
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'promo_codes_discount_value_check') THEN
        ALTER TABLE promo_codes
            ADD CONSTRAINT promo_codes_discount_value_check
            CHECK (discount_value > 0 AND discount_value <= 100);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'promo_codes_usage_limit_total_check') THEN
        ALTER TABLE promo_codes
            ADD CONSTRAINT promo_codes_usage_limit_total_check
            CHECK (usage_limit_total IS NULL OR usage_limit_total > 0);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'promo_codes_usage_count_check') THEN
        ALTER TABLE promo_codes
            ADD CONSTRAINT promo_codes_usage_count_check
            CHECK (usage_count >= 0);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'promo_codes_valid_dates_check') THEN
        ALTER TABLE promo_codes
            ADD CONSTRAINT promo_codes_valid_dates_check
            CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until >= valid_from);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'promo_codes_deactivated_reason_check') THEN
        ALTER TABLE promo_codes
            ADD CONSTRAINT promo_codes_deactivated_reason_check
            CHECK (
                deactivated_reason IS NULL
                OR deactivated_reason IN ('expired', 'usage_limit_reached', 'manual', 'deleted')
            );
    END IF;
END $$;

-- Kod karsilastirmasi case-insensitive ve bastaki/sondaki bosluklardan bagimsizdir.
CREATE UNIQUE INDEX IF NOT EXISTS promo_codes_upper_code_unique
ON promo_codes (UPPER(BTRIM(code)));
CREATE INDEX IF NOT EXISTS promo_codes_partnership_uid_idx
ON promo_codes (partnership_uid);

-- Product display fields: admin editlenebilir ad/aciklama metinleri.
-- Her kolon ayri ALTER olarak tutulur; boylece migration parca parca
-- calistirildiginda ADD COLUMN tek basina kalip syntax hatasi uretmez.
ALTER TABLE products ADD COLUMN IF NOT EXISTS display_name_en TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS display_name_uk TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS display_description_en TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS display_description_uk TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS display_bullets_en TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS display_bullets_uk TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS is_add_on BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS is_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS granted_features INT[] NOT NULL DEFAULT ARRAY[]::INT[];

-- Advertorial reklam alani add-on'u:
-- Feature id 4, backend tarafindaki FeatureAdvertorial sabitiyle eslesir.
-- ON CONFLICT bolumu fiyat/metin/gorsel gibi admin tarafindan sonradan
-- duzenlenecek alanlari ezmez; yalniz urunun add-on ve feature bagini garanti eder.
INSERT INTO products (
    id,
    price,
    fullfillment_type,
    is_add_on,
    is_enabled,
    granted_features,
    options,
    priority,
    display_name_en,
    display_name_uk,
    display_description_en,
    display_description_uk,
    display_bullets_en,
    display_bullets_uk
) VALUES (
    'advertorial',
    300.00,
    'digital',
    TRUE,
    TRUE,
    ARRAY[4]::INT[],
    '{"image":"https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/addon_banner/advertorial.png"}'::JSONB,
    90,
    'Advertising Area',
    'Advertising Area',
    'Show promotional banners on the guest page.',
    'Show promotional banners on the guest page.',
    'Guest page banner area',
    'Guest page banner area'
) ON CONFLICT (id) DO UPDATE SET
    is_add_on = TRUE,
    granted_features = ARRAY(
        SELECT DISTINCT feature_id
        FROM unnest(COALESCE(products.granted_features, ARRAY[]::INT[]) || ARRAY[4]::INT[]) AS f(feature_id)
        ORDER BY feature_id
    );

-- Premium pakette reklam alani hakki admin panelden granted_features
-- uzerinden yonetilir; setup tekrar calistiginda bu karar ezilmez.

-- Order price snapshot: satin alim anindaki birim fiyati korur
ALTER TABLE cart_items
    ADD COLUMN IF NOT EXISTS unit_price_snapshot DECIMAL(10, 2);

-- Promo purchase snapshotlari:
-- Odeme anindaki promo ve tutar bilgileri burada saklanir; promo sonradan degisse
-- veya silinse bile eski satis raporlari ayni kalir.
ALTER TABLE purchases
    ADD COLUMN IF NOT EXISTS promo_code_uid UUID REFERENCES promo_codes(uid),
    ADD COLUMN IF NOT EXISTS promo_code_text_snapshot TEXT,
    ADD COLUMN IF NOT EXISTS promo_partnership_uid UUID REFERENCES partnerships(uid),
    ADD COLUMN IF NOT EXISTS promo_partnership_snapshot JSONB,
    ADD COLUMN IF NOT EXISTS gross_total DECIMAL(10, 2),
    ADD COLUMN IF NOT EXISTS discount_amount DECIMAL(10, 2),
    ADD COLUMN IF NOT EXISTS net_total DECIMAL(10, 2);
CREATE INDEX IF NOT EXISTS purchases_promo_partnership_uid_idx
ON purchases (promo_partnership_uid);

-- Event storage expiration sistemi:
--   storage_until                : paket bazli icerik saklama bitis tarihi
--                                  (= active_until + paket.options.storage_days)
--   storage_warning_mail_sent_at : T-14 uyari mailinin gonderildigi an
--                                  cron job idempotent: NULL ise mail at + flag'i doldur (tek sefer).
--   storage_extended_at          : kullanicinin +30 gun uzatma hakkini kullandigi an
--                                  event yasam boyunca tek seferlik hak.
--                                  NULL ise hak duruyor; doluysa endpoint 409, modal gozukmez.
ALTER TABLE events
    ADD COLUMN IF NOT EXISTS storage_until                TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS storage_warning_mail_sent_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS storage_extended_at          TIMESTAMPTZ;

-- Reklam alani ayari:
-- Event bazli layout ve hucre icerikleri JSONB olarak tutulur.
-- Bos obje default'u sayesinde eski eventlerde geriye donuk uyumluluk korunur.
ALTER TABLE events
    ADD COLUMN IF NOT EXISTS advertorial_config JSONB NOT NULL DEFAULT '{}';

-- Event upload snapshotlari:
-- Event soft-delete edilmeden once upload bazli analitikler ve silinecek S3
-- path'leri burada saklanir. Medya DB'den silinse bile raporlama ve retry icin
-- son durum kaybolmaz.
CREATE TABLE IF NOT EXISTS event_upload_snapshots (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_uid UUID REFERENCES events(uid) NOT NULL UNIQUE,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reason TEXT NOT NULL CHECK (reason IN ('manual_delete', 'storage_expired')),

    guest_count INT NOT NULL DEFAULT 0,
    contributor_count INT NOT NULL DEFAULT 0,
    upload_count_total INT NOT NULL DEFAULT 0,
    photo_count INT NOT NULL DEFAULT 0,
    video_count INT NOT NULL DEFAULT 0,
    voice_count INT NOT NULL DEFAULT 0,
    text_count INT NOT NULL DEFAULT 0,

    total_upload_size_mb DECIMAL(12, 2),
    first_upload_at TIMESTAMPTZ,
    last_upload_at TIMESTAMPTZ,

    media_paths JSONB NOT NULL DEFAULT '[]'::jsonb,
    purge_started_at TIMESTAMPTZ,
    purge_finished_at TIMESTAMPTZ,
    purge_error TEXT
);

-- Explicitly ensure anonymous users cannot access the events table directly
-- (This is already the default, but explicit is better)
REVOKE ALL ON events FROM webanon;

-- ============================================================================
-- 5. AUTHENTICATED USER PERMISSIONS
-- ============================================================================

-- Grant full access to all tables for authenticated users
-- Row-Level Security (RLS) will restrict what they can actually see/modify
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO auth;

-- Explicitly grant on cart tables (in case they were created after the above) TODO: CHANGE
GRANT SELECT, INSERT, UPDATE, DELETE ON cart_items TO auth;
GRANT SELECT, INSERT, UPDATE, DELETE ON carts TO auth;

-- Grant sequence usage (for auto-incrementing IDs)
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO auth;

-- Make sure future tables also get these permissions
ALTER DEFAULT PRIVILEGES IN SCHEMA public 
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO auth;

ALTER DEFAULT PRIVILEGES IN SCHEMA public 
    GRANT USAGE, SELECT ON SEQUENCES TO auth;

-- ============================================================================
-- 6. HELPER FUNCTIONS
-- ============================================================================

-- Function to get current user's role from JWT claims
CREATE OR REPLACE FUNCTION current_user_role()
RETURNS text AS $$
    SELECT coalesce(
        current_setting('request.jwt.claim.role', true),
        current_setting('request.jwt.claims', true)::json->>'role'
    );
$$ LANGUAGE sql STABLE;

-- Function to get current user's UID from JWT claims
-- Works for both authenticated users and anonymous users (tracked by cookies)
-- Compatible with PostgREST v12+ (per-claim settings) and older versions (json claims)
CREATE OR REPLACE FUNCTION current_user_uid()
RETURNS uuid AS $$
    SELECT coalesce(
        current_setting('request.jwt.claim.ui', true),
        current_setting('request.jwt.claims', true)::json->>'ui'
    )::uuid;
$$ LANGUAGE sql STABLE;

-- Grant execute permission to both roles
GRANT EXECUTE ON FUNCTION current_user_role() TO auth;
GRANT EXECUTE ON FUNCTION current_user_role() TO webanon;
GRANT EXECUTE ON FUNCTION current_user_uid() TO auth;
GRANT EXECUTE ON FUNCTION current_user_uid() TO webanon;

-- ============================================================================
-- 7. ENABLE ROW-LEVEL SECURITY (RLS)
-- ============================================================================

-- Enable RLS on sensitive tables
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE events ENABLE ROW LEVEL SECURITY;
ALTER TABLE uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE participants ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchases ENABLE ROW LEVEL SECURITY;
ALTER TABLE cart_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE carts ENABLE ROW LEVEL SECURITY;
ALTER TABLE event_upload_snapshots ENABLE ROW LEVEL SECURITY;

-- global_attributes can use RLS based on is_public flag
ALTER TABLE global_attributes ENABLE ROW LEVEL SECURITY;

-- Jobs table RLS
ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;

-- ============================================================================
-- 8. CREATE RLS POLICIES
-- ============================================================================

-- Users Table: Users can only see/modify their own user record
DROP POLICY IF EXISTS users_self_access ON users;
CREATE POLICY users_self_access ON users
    FOR ALL
    TO auth
    USING (uid = current_user_uid())
    WITH CHECK (uid = current_user_uid());

-- Users Table: Allow users to see other users who are co-admins on the same events
DROP POLICY IF EXISTS users_co_admin_access ON users;
CREATE POLICY users_co_admin_access ON users
    FOR SELECT
    TO auth
    USING (
        uid IN (
            SELECT UNNEST(admins) FROM events 
            WHERE current_user_uid() = ANY(admins)
        )
    );

-- Credentials Table: Users can only manage their own credentials
DROP POLICY IF EXISTS credentials_self_access ON credentials;
CREATE POLICY credentials_self_access ON credentials
    FOR ALL
    TO auth
    USING (user_uid = current_user_uid())
    WITH CHECK (user_uid = current_user_uid());

-- Events Table: Users can only access events where they are admins
DROP POLICY IF EXISTS events_admin_access ON events;
CREATE POLICY events_admin_access ON events
    FOR ALL
    TO auth
    USING (current_user_uid() = ANY(admins))
    WITH CHECK (current_user_uid() = ANY(admins));

-- Events Table: Restrict column-level UPDATE access
-- Admins can only modify content fields, not system/payment fields
-- Note: active_until is not editable - it's always activation_date + 2 weeks
REVOKE UPDATE ON events FROM auth;
GRANT UPDATE (name, event_type, description, welcome_message, image, settings, activation_date) ON events TO auth;

-- Yardimci fonksiyon: purchase_uid uzerinden core paketin activation_days degerini bulur.
-- Not: Bu adimda sadece fonksiyonu hazirliyoruz; trigger degisikligi bir sonraki adimda yapilacak.
CREATE OR REPLACE FUNCTION get_purchase_activation_days(p_purchase_uid UUID)
RETURNS INT AS $$
DECLARE
    v_activation_days INT;
BEGIN
    SELECT COALESCE(NULLIF(p.options->>'activation_days', '')::INT, 14)
    INTO v_activation_days
    FROM purchases pu
    JOIN carts c ON c.uid = pu.cart_uid
    JOIN cart_items ci ON ci.cart_uid = c.uid
    JOIN products p ON p.uid = ci.product_uid
    WHERE pu.uid = p_purchase_uid
      AND p.is_add_on = FALSE
    ORDER BY ci.created_at ASC
    LIMIT 1;

    RETURN COALESCE(v_activation_days, 14);
END;
$$ LANGUAGE plpgsql STABLE;

-- Yardimci fonksiyon: purchase_uid uzerinden core paketin storage_days degerini bulur.
-- Paket options'inda storage_days yoksa varsayilan 14 gun.
-- Trigger bu degeri active_until uzerine ekleyerek storage_until alanini doldurur.
CREATE OR REPLACE FUNCTION get_purchase_storage_days(p_purchase_uid UUID)
RETURNS INT AS $$
DECLARE
    v_storage_days INT;
BEGIN
    SELECT COALESCE(NULLIF(p.options->>'storage_days', '')::INT, 14)
    INTO v_storage_days
    FROM purchases pu
    JOIN carts c ON c.uid = pu.cart_uid
    JOIN cart_items ci ON ci.cart_uid = c.uid
    JOIN products p ON p.uid = ci.product_uid
    WHERE pu.uid = p_purchase_uid
      AND p.is_add_on = FALSE
    ORDER BY ci.created_at ASC
    LIMIT 1;

    RETURN COALESCE(v_storage_days, 14);
END;
$$ LANGUAGE plpgsql STABLE;

-- Trigger: activation_date degisirse hem active_until hem storage_until tazelenir.
-- active_until = activation_date + paket.options.activation_days
-- storage_until = active_until + paket.options.storage_days
CREATE OR REPLACE FUNCTION enforce_event_dates()
RETURNS TRIGGER AS $$
DECLARE
    v_activation_days INT;
    v_storage_days    INT;
BEGIN
    -- Aktivasyon sonrasi tarih degistirilemez (kullanici icerik yuklemis olabilir).
    IF OLD.activation_date <= NOW() THEN
        IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
            RAISE EXCEPTION 'Cannot modify activation_date after the event has been activated';
        END IF;
    END IF;

    -- activation_date degisince hem active_until hem storage_until pakete gore yeniden hesaplanir.
    IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
        v_activation_days := get_purchase_activation_days(NEW.purchase_uid);
        NEW.active_until  := NEW.activation_date + make_interval(days => v_activation_days);

        v_storage_days    := get_purchase_storage_days(NEW.purchase_uid);
        NEW.storage_until := NEW.active_until + make_interval(days => v_storage_days);
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS events_lock_dates_after_activation ON events;
DROP TRIGGER IF EXISTS events_enforce_dates ON events;
CREATE TRIGGER events_enforce_dates
    BEFORE UPDATE ON events
    FOR EACH ROW
    EXECUTE FUNCTION enforce_event_dates();

-- INSERT trigger'i: yeni event olusurken active_until ve storage_until pakete gore set edilir.
CREATE OR REPLACE FUNCTION enforce_event_dates_on_insert()
RETURNS TRIGGER AS $$
DECLARE
    v_activation_days INT;
    v_storage_days    INT;
BEGIN
    -- Ilk olusumda active_until paketin activation_days degerine gore hesaplanir.
    v_activation_days := get_purchase_activation_days(NEW.purchase_uid);
    NEW.active_until  := NEW.activation_date + make_interval(days => v_activation_days);

    -- storage_until = active_until uzerine paketin storage_days degeri eklenerek hesaplanir.
    v_storage_days    := get_purchase_storage_days(NEW.purchase_uid);
    NEW.storage_until := NEW.active_until + make_interval(days => v_storage_days);

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS events_enforce_dates_insert ON events;
CREATE TRIGGER events_enforce_dates_insert
    BEFORE INSERT ON events
    FOR EACH ROW
    EXECUTE FUNCTION enforce_event_dates_on_insert();

-- PostgREST computed field: should_show_extend_prompt(events) -> boolean
-- Frontend events tablosunu PostgREST uzerinden direkt okuyor.
-- Bu fonksiyon, function adi tablo adina argument olarak match ettigi icin
-- PostgREST tarafindan "virtual column" gibi sunulur:
--   GET /events?select=*,should_show_extend_prompt
--
-- Modal acilma kurallari (tek satirda hepsi saglanmali):
--   1. event soft-delete edilmemis        (deleted_at IS NULL)
--   2. tek seferlik uzatma hakki kullanilmamis (storage_extended_at IS NULL)
--   3. storage_until tanimli              (NOT NULL)
--   4. henuz bitmemis                     (storage_until > NOW())
--   5. son 14 gune girmis                 (storage_until - 14 gun <= NOW())
--
-- STABLE: NOW()'a bagli oldugu icin IMMUTABLE olamaz; PostgREST STABLE
-- fonksiyonlari computed column olarak kabul eder.
CREATE OR REPLACE FUNCTION should_show_extend_prompt(events)
RETURNS boolean AS $$
    SELECT $1.deleted_at          IS NULL
       AND $1.storage_extended_at IS NULL
       AND $1.storage_until       IS NOT NULL
       AND $1.storage_until        > NOW()
       AND ($1.storage_until - interval '14 days') <= NOW();
$$ LANGUAGE sql STABLE;

-- PostgREST'in computed field'i cagirabilmesi icin auth ve webanon rollerinin
-- EXECUTE yetkisi olmali. RLS hala satir bazinda calisir; bu fonksiyon
-- sadece zaten erisim hakki olan satirlar icin hesaplanir.
GRANT EXECUTE ON FUNCTION should_show_extend_prompt(events) TO auth;
GRANT EXECUTE ON FUNCTION should_show_extend_prompt(events) TO webanon;

-- Uploads Table: Users can access uploads for events they're admins of
DROP POLICY IF EXISTS uploads_event_admin_access ON uploads;
CREATE POLICY uploads_event_admin_access ON uploads
    FOR ALL
    TO auth
    USING (
        event_uid IN (
            SELECT uid FROM events 
            WHERE current_user_uid() = ANY(admins)
        )
    )
    WITH CHECK (
        event_uid IN (
            SELECT uid FROM events 
            WHERE current_user_uid() = ANY(admins)
        )
    );

-- Helper function to get a boolean setting from an event
-- Returns: the setting value, or FALSE if key doesn't exist
-- Raises: exception if event doesn't exist
CREATE OR REPLACE FUNCTION get_event_setting_bool(
    p_event_uid UUID,
    p_key TEXT
) RETURNS BOOLEAN AS $$
    SELECT COALESCE((settings->>p_key)::boolean, FALSE)
    FROM events
    WHERE uid = p_event_uid;
$$ LANGUAGE sql STABLE SECURITY DEFINER;

-- Grant execute to both roles
GRANT EXECUTE ON FUNCTION get_event_setting_bool(UUID, TEXT) TO auth;
GRANT EXECUTE ON FUNCTION get_event_setting_bool(UUID, TEXT) TO webanon;

-- Uploads Table: Webanon can always SELECT their own uploads
DROP POLICY IF EXISTS uploads_participant_select ON uploads;
DROP POLICY IF EXISTS uploads_participant_own_text_select ON uploads;
CREATE POLICY uploads_participant_select ON uploads
    FOR SELECT
    TO webanon
    USING (client_uid = current_user_uid());

-- Uploads Table: Webanon can INSERT text-type uploads (guestbook entries)
DROP POLICY IF EXISTS uploads_participant_text_insert ON uploads;
CREATE POLICY uploads_participant_text_insert ON uploads
    FOR INSERT
    TO webanon
    WITH CHECK (
        client_uid = current_user_uid()
        AND upload_type = 'text'
    );

DROP POLICY IF EXISTS uploads_participant_text_update ON uploads;
CREATE POLICY uploads_participant_text_update ON uploads
    FOR UPDATE
    TO webanon
    USING (client_uid = current_user_uid() AND upload_type = 'text')
    WITH CHECK (client_uid = current_user_uid() AND upload_type = 'text');

-- Uploads Table: Webanon can DELETE their own uploads if remove_uploads is enabled
DROP POLICY IF EXISTS uploads_participant_delete ON uploads;
CREATE POLICY uploads_participant_delete ON uploads
    FOR DELETE
    TO webanon
    USING (
        client_uid = current_user_uid()
        AND get_event_setting_bool(event_uid, 'remove_uploads')
    );

-- Event Upload Snapshots Table: event adminleri kendi eventlerinin silme oncesi
-- upload analitik snapshot'larini gorebilir. Webanon'a erisim verilmez.
DROP POLICY IF EXISTS event_upload_snapshots_admin_access ON event_upload_snapshots;
CREATE POLICY event_upload_snapshots_admin_access ON event_upload_snapshots
    FOR ALL
    TO auth
    USING (
        event_uid IN (
            SELECT uid FROM events
            WHERE current_user_uid() = ANY(admins)
        )
    )
    WITH CHECK (
        event_uid IN (
            SELECT uid FROM events
            WHERE current_user_uid() = ANY(admins)
        )
    );

-- Participants Table: Can access participants for events they're admins of
DROP POLICY IF EXISTS participants_event_admin_access ON participants;
CREATE POLICY participants_event_admin_access ON participants
    FOR ALL
    TO auth
    USING (
        event_uid IN (
            SELECT uid FROM events 
            WHERE current_user_uid() = ANY(admins)
        )
    )
    WITH CHECK (
        event_uid IN (
            SELECT uid FROM events 
            WHERE current_user_uid() = ANY(admins)
        )
    );

-- Participants Table: Webanon can see and update their own participant record
DROP POLICY IF EXISTS participants_self_access ON participants;
CREATE POLICY participants_self_access ON participants
    FOR ALL
    TO webanon
    USING (uid = current_user_uid())
    WITH CHECK (uid = current_user_uid());

-- Purchases Table: Users can only see their own purchases
DROP POLICY IF EXISTS purchases_self_access ON purchases;
CREATE POLICY purchases_self_access ON purchases
    FOR ALL
    TO auth
    USING (buyer_uid = current_user_uid())
    WITH CHECK (buyer_uid = current_user_uid());

-- Cart Items: Users can access cart_items for carts they've purchased
DROP POLICY IF EXISTS cart_items_purchase_access ON cart_items;
CREATE POLICY cart_items_purchase_access ON cart_items
    FOR ALL
    TO auth
    USING (
        cart_uid IN (
            SELECT cart_uid FROM purchases 
            WHERE buyer_uid = current_user_uid()
        )
    )
    WITH CHECK (
        cart_uid IN (
            SELECT cart_uid FROM purchases 
            WHERE buyer_uid = current_user_uid()
        )
    );

-- Carts: Users can access carts they've purchased
DROP POLICY IF EXISTS carts_purchase_access ON carts;
CREATE POLICY carts_purchase_access ON carts
    FOR ALL
    TO auth
    USING (
        uid IN (
            SELECT cart_uid FROM purchases 
            WHERE buyer_uid = current_user_uid()
        )
    )
    WITH CHECK (
        uid IN (
            SELECT cart_uid FROM purchases 
            WHERE buyer_uid = current_user_uid()
        )
    );

-- Global Attributes: Authenticated users see all, anonymous see only public
DROP POLICY IF EXISTS global_attributes_authenticated ON global_attributes;
CREATE POLICY global_attributes_authenticated ON global_attributes
    FOR SELECT
    TO auth
    USING (true);

DROP POLICY IF EXISTS global_attributes_anonymous ON global_attributes;
CREATE POLICY global_attributes_anonymous ON global_attributes
    FOR SELECT
    TO webanon
    USING (is_public = true);

-- Jobs Table: Only auth can access, no webanon access
-- Grant SELECT on all columns, INSERT only on allowed columns (no created_at/updated_at)
GRANT SELECT ON jobs TO auth;
GRANT INSERT (name, input, user_uid) ON jobs TO auth;

-- Jobs Table: Auth can only see their own jobs
DROP POLICY IF EXISTS jobs_self_select ON jobs;
CREATE POLICY jobs_self_select ON jobs
    FOR SELECT
    TO auth
    USING (user_uid = current_user_uid());

-- Jobs Table: Auth can only insert jobs for themselves with 'queued' status
DROP POLICY IF EXISTS jobs_self_insert ON jobs;
CREATE POLICY jobs_self_insert ON jobs
    FOR INSERT
    TO auth
    WITH CHECK (
        user_uid = current_user_uid()
    );

-- Only one queued/running job per (user_uid, name) combination
DROP INDEX IF EXISTS jobs_one_active_per_user_name;
CREATE UNIQUE INDEX jobs_one_active_per_user_name 
ON jobs (user_uid, name) 
WHERE status IN ('queued', 'running');

CREATE OR REPLACE FUNCTION notify_job_insert()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('job_insert', NEW.uid::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_insert_trigger
AFTER INSERT ON jobs
FOR EACH ROW EXECUTE FUNCTION notify_job_insert();

CREATE ROLE adm_admin NOLOGIN BYPASSRLS;
GRANT ALL ON ALL TABLES IN SCHEMA public TO adm_admin;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO adm_admin;
GRANT adm_admin TO webanon;  -- allow PostgREST to switch to it

-- ============================================================================
-- 9. VERIFICATION QUERIES
-- ============================================================================

-- Uncomment to verify the setup:

-- Check roles exist
-- \du webanon
-- \du auth

-- Check table permissions
-- SELECT 
--     tablename,
--     has_table_privilege('webanon', schemaname||'.'||tablename, 'SELECT') as webanon_select,
--     has_table_privilege('auth', schemaname||'.'||tablename, 'SELECT') as auth_select
-- FROM pg_tables 
-- WHERE schemaname = 'public';

-- Check RLS is enabled
-- SELECT tablename, rowsecurity FROM pg_tables WHERE schemaname = 'public';

-- Check policies
-- SELECT schemaname, tablename, policyname, roles FROM pg_policies WHERE schemaname = 'public';

-- ============================================================================
-- NOTES:
-- ============================================================================
-- 
-- JWT Token Format for Authenticated Users:
-- {
--   "role": "auth",
--   "user_uid": "123e4567-e89b-12d3-a456-426614174000",
--   "exp": 1702492800
-- }
--
-- JWT Token Format for Anonymous Users (tracked by cookies):
-- {
--   "role": "webanon",  -- or omit role to use db-anon-role
--   "user_uid": "123e4567-e89b-12d3-a456-426614174000",  -- from cookie
--   "exp": 1702492800
-- }
--
-- PostgREST Config Required:
-- db-anon-role = "webanon"
-- jwt-secret = "your-secret-key-here"
--
-- ============================================================================
