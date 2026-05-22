
CREATE TABLE users (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    mail VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    surname VARCHAR(255) NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

-- Site panel admin rolleri. Super adminler simdilik env.admin_emails ile
-- calismaya devam eder; bu tablo order_admin icin kullanilir ve ileride
-- super_admin rolunun de DB'ye tasinmasini destekler.
CREATE TABLE panel_admins (
    user_uid UUID PRIMARY KEY REFERENCES users(uid) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('order_admin', 'super_admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by_uid UUID NULL REFERENCES users(uid) ON DELETE SET NULL,
    -- Admin yetkisi kaldirildiginda satiri silmeden revoke gecmisini korur.
    deleted_at TIMESTAMPTZ NULL,
    deleted_by_uid UUID NULL REFERENCES users(uid) ON DELETE SET NULL
);

CREATE TYPE CREDENTIAL_TYPE AS ENUM ('password', 'google');

CREATE TABLE credentials (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_uid UUID NOT NULL REFERENCES users(uid),
    type CREDENTIAL_TYPE NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TYPE EVENT_STATUS AS ENUM ('unpaid', 'paid', 'suspended');

CREATE TABLE events (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    admins UUID[],
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    activation_date TIMESTAMPTZ,
    active_until TIMESTAMPTZ,
    purchase_uid UUID REFERENCES purchases(uid),
    status EVENT_STATUS NOT NULL DEFAULT 'unpaid',

    name VARCHAR(255),
    event_type VARCHAR(255),
    description TEXT,
    welcome_message TEXT,
    image VARCHAR(255),
    settings JSONB DEFAULT '{}',
    -- Guest reklam alani ayarlari (layout + hucre bazli gorsel/link)
    -- bu alanda saklanir. Ilk olusumda bos obje ile baslar.
    advertorial_config JSONB NOT NULL DEFAULT '{}'
);

CREATE VIEW events_public AS
SELECT 
    uid,
    name,
    event_type,
    activation_date,
    active_until,
    description,
    welcome_message,
    image,
    settings
FROM events
WHERE status = 'paid';

-- Yardimci fonksiyon: purchase_uid uzerinden core paketin activation_days degerini bulur.
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

-- Trigger: activation_date degisince active_until degerini paketin activation_days suresine gore hesaplar.
CREATE OR REPLACE FUNCTION enforce_event_dates()
RETURNS TRIGGER AS $$
DECLARE
    v_activation_days INT;
BEGIN
    IF OLD.activation_date <= NOW() THEN
        IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
            RAISE EXCEPTION 'Cannot modify activation_date after the event has been activated';
        END IF;
    END IF;

    IF NEW.activation_date IS DISTINCT FROM OLD.activation_date THEN
        v_activation_days := get_purchase_activation_days(NEW.purchase_uid);
        NEW.active_until := NEW.activation_date + make_interval(days => v_activation_days);
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

-- INSERT aninda da ayni kurali uygular.
CREATE OR REPLACE FUNCTION enforce_event_dates_on_insert()
RETURNS TRIGGER AS $$
DECLARE
    v_activation_days INT;
BEGIN
    v_activation_days := get_purchase_activation_days(NEW.purchase_uid);
    NEW.active_until := NEW.activation_date + make_interval(days => v_activation_days);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS events_enforce_dates_insert ON events;
CREATE TRIGGER events_enforce_dates_insert
    BEFORE INSERT ON events
    FOR EACH ROW
    EXECUTE FUNCTION enforce_event_dates_on_insert();

CREATE TYPE FULLFILLMENT_TYPE AS ENUM ('digital', 'physical');

CREATE TABLE products (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    price DECIMAL(10, 2) NOT NULL,
    id TEXT NOT NULL UNIQUE,
    display_name_en TEXT NOT NULL DEFAULT '',
    display_name_uk TEXT NOT NULL DEFAULT '',
    display_description_en TEXT NOT NULL DEFAULT '',
    display_description_uk TEXT NOT NULL DEFAULT '',
    display_bullets_en TEXT NOT NULL DEFAULT '',
    display_bullets_uk TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    options JSONB NOT NULL DEFAULT '{}',
    priority INT NOT NULL DEFAULT 0,
    fullfillment_type FULLFILLMENT_TYPE NOT NULL,
    is_add_on BOOLEAN NOT NULL DEFAULT FALSE,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    granted_features INT[] NOT NULL DEFAULT ARRAY[]::INT[]
);

CREATE TABLE carts (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    note TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TYPE CART_ITEM_STATUS AS ENUM ('cart', 'pending', 'purchased', 'client-action', 'admin-action', 'fulfilled', 'cancelled');

CREATE TABLE cart_items (
    cart_uid UUID NOT NULL REFERENCES carts(uid),
    product_uid UUID NOT NULL REFERENCES products(uid),
    quantity INT NOT NULL,
    unit_price_snapshot DECIMAL(10, 2),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    note TEXT DEFAULT '',
    status CART_ITEM_STATUS NOT NULL DEFAULT 'cart',
    UNIQUE (cart_uid, product_uid)
);

-- Partnership kayitlari promo kodlarinin hangi kisi/kurum kanaliyla iliskili
-- oldugunu tutar. Soft delete gecmis promo ve rapor referanslarini korur.
CREATE TABLE partnerships (
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

-- Promo kodlari:
--   - MVP'de yalniz yuzde indirim desteklenir (`discount_type = 'percent'`).
--   - `valid_from` bos birakilirsa CURRENT_TIMESTAMP ile dolar; `valid_until` ve `usage_limit_total` opsiyoneldir.
--   - `is_expired` tutulmaz; pasiflik `is_active`, `deactivated_at`, `deactivated_reason` ile aciklanir.
CREATE TABLE promo_codes (
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (discount_type IN ('percent')),
    CHECK (discount_value > 0 AND discount_value <= 100),
    CHECK (usage_limit_total IS NULL OR usage_limit_total > 0),
    CHECK (usage_count >= 0),
    CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until >= valid_from),
    CHECK (
        deactivated_reason IS NULL
        OR deactivated_reason IN ('expired', 'usage_limit_reached', 'manual', 'deleted')
    )
);

CREATE UNIQUE INDEX promo_codes_upper_code_unique ON promo_codes (UPPER(BTRIM(code)));
CREATE INDEX promo_codes_partnership_uid_idx ON promo_codes (partnership_uid);

CREATE TABLE purchases (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id TEXT UNIQUE,
    provider VARCHAR(255) NOT NULL,
    buyer_uid UUID REFERENCES users(uid),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    purchase_info JSONB NOT NULL DEFAULT '{}',
    cart_uid UUID REFERENCES carts(uid),
    promo_code_uid UUID REFERENCES promo_codes(uid),
    promo_code_text_snapshot TEXT,
    promo_partnership_uid UUID REFERENCES partnerships(uid),
    promo_partnership_snapshot JSONB,
    gross_total DECIMAL(10, 2),
    discount_amount DECIMAL(10, 2),
    net_total DECIMAL(10, 2)
);

CREATE INDEX purchases_promo_partnership_uid_idx ON purchases (promo_partnership_uid);

CREATE TABLE participants (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    event_uid UUID REFERENCES events(uid) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name, event_uid)
);

CREATE TYPE UPLOAD_TYPE AS ENUM ('photo', 'video', 'voice', 'text');

CREATE TABLE uploads (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_type UPLOAD_TYPE NOT NULL,
    
    client_uid UUID REFERENCES participants(uid) NOT NULL,
    event_uid UUID REFERENCES events(uid) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    value TEXT NOT NULL,
    trashed_at TIMESTAMPTZ
);

CREATE TABLE event_upload_snapshots (
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

CREATE TABLE global_attributes (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_public BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TYPE JOB_STATUS AS ENUM ('queued','running','succeeded','failed');
CREATE TYPE DEFINED_JOB_NAMES AS ENUM ('s3_export');

CREATE TABLE jobs (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name DEFINED_JOB_NAMES NOT NULL,
    input JSONB NOT NULL DEFAULT '{}',
    output JSONB NOT NULL DEFAULT '{}',
    user_uid UUID REFERENCES users(uid) NOT NULL,
    status JOB_STATUS NOT NULL DEFAULT 'queued',
    locked_at TIMESTAMPTZ,
    locked_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);