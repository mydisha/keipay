-- Apps replace tenants (expand step). tenants and the tenant_id columns stay
-- as they are, read-only, and are dropped by a later contract migration.

-- A ULID in Crockford base32: 10 characters of millisecond timestamp, then 16
-- random characters. Public ids are not secrets, so random() is enough.
CREATE OR REPLACE FUNCTION pg_temp.keipay_ulid(ts TIMESTAMPTZ) RETURNS TEXT
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    alphabet CONSTANT TEXT := '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
    ms BIGINT := floor(extract(epoch FROM ts) * 1000)::BIGINT;
    ulid TEXT := '';
BEGIN
    FOR i IN 1..10 LOOP
        ulid := substr(alphabet, (ms % 32)::INT + 1, 1) || ulid;
        ms := ms / 32;
    END LOOP;
    FOR i IN 1..16 LOOP
        ulid := ulid || substr(alphabet, floor(random() * 32)::INT + 1, 1);
    END LOOP;
    RETURN ulid;
END
$$;

CREATE TABLE apps (
    id                   BIGSERIAL   PRIMARY KEY,
    public_id            TEXT        NOT NULL UNIQUE,
    slug                 TEXT        NOT NULL,
    name                 TEXT        NOT NULL,
    environment          TEXT        NOT NULL CHECK (environment IN ('test', 'live')),
    status               TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    metadata             JSONB       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object'),
    webhook_url          TEXT        NOT NULL DEFAULT '',
    redirect_success_url TEXT        NOT NULL DEFAULT '',
    redirect_failure_url TEXT        NOT NULL DEFAULT '',
    webhook_secret_enc   TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT apps_environment_slug_key UNIQUE (environment, slug),
    -- Target of the composite foreign key that pins a key to its app's environment.
    CONSTRAINT apps_id_environment_key UNIQUE (id, environment)
);

CREATE INDEX idx_apps_created ON apps (created_at DESC, public_id DESC);
CREATE INDEX idx_apps_environment_created ON apps (environment, created_at DESC, public_id DESC);

-- Tenants keep their ids, so every tenant_id is already a valid app_id. They
-- become live apps whose webhook secret is the old API secret, which keeps
-- outbound signatures unchanged.
INSERT INTO apps (id, public_id, slug, name, environment, status, metadata, webhook_url,
                  redirect_success_url, redirect_failure_url, webhook_secret_enc, created_at, updated_at)
SELECT id,
       'app_' || pg_temp.keipay_ulid(created_at),
       slug,
       name,
       'live',
       CASE WHEN active THEN 'active' ELSE 'disabled' END,
       '{}',
       webhook_url,
       redirect_success_url,
       redirect_failure_url,
       api_secret_enc,
       created_at,
       updated_at
FROM tenants
ORDER BY id;

SELECT setval(pg_get_serial_sequence('apps', 'id'), COALESCE((SELECT max(id) FROM apps), 0) + 1, false);

CREATE TABLE app_api_keys (
    id           BIGSERIAL   PRIMARY KEY,
    public_id    TEXT        NOT NULL UNIQUE,
    app_id       BIGINT      NOT NULL,
    environment  TEXT        NOT NULL CHECK (environment IN ('test', 'live')),
    label        TEXT        NOT NULL,
    key_prefix   TEXT        NOT NULL,
    key_last4    TEXT        NOT NULL,
    -- Lower-case hex SHA-256 of the full key; the key itself is never stored.
    key_hash     TEXT        NOT NULL UNIQUE,
    secret_enc   TEXT        NOT NULL,
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT app_api_keys_app_environment_fkey FOREIGN KEY (app_id, environment) REFERENCES apps (id, environment),
    CONSTRAINT app_api_keys_prefix_check CHECK (key_prefix = 'kp_' OR key_prefix = 'kp_' || environment || '_')
);

CREATE INDEX idx_app_api_keys_app_created ON app_api_keys (app_id, created_at DESC, public_id DESC);

-- Each tenant's credentials become its app's first key, so existing
-- integrations keep signing with the same key and secret.
INSERT INTO app_api_keys (public_id, app_id, environment, label, key_prefix, key_last4, key_hash,
                          secret_enc, created_at, updated_at)
SELECT 'key_' || pg_temp.keipay_ulid(created_at),
       id,
       'live',
       'Migrated key',
       'kp_',
       right(api_key, 4),
       encode(sha256(convert_to(api_key, 'UTF8')), 'hex'),
       api_secret_enc,
       created_at,
       updated_at
FROM tenants
ORDER BY id;

ALTER TABLE payments
    ADD COLUMN app_id                  BIGINT REFERENCES apps (id),
    ADD COLUMN public_id               TEXT,
    ADD COLUMN metadata                JSONB NOT NULL DEFAULT '{}'
        CONSTRAINT payments_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    ADD COLUMN idempotency_key         TEXT,
    ADD COLUMN idempotency_fingerprint TEXT;

UPDATE payments SET app_id = tenant_id, public_id = 'pay_' || pg_temp.keipay_ulid(created_at);

ALTER TABLE payments
    ALTER COLUMN app_id SET NOT NULL,
    ALTER COLUMN public_id SET NOT NULL,
    ALTER COLUMN tenant_id DROP NOT NULL;

-- The service tells duplicate orders and duplicate idempotency keys apart by
-- these index names.
CREATE UNIQUE INDEX payments_public_id_key ON payments (public_id);
CREATE UNIQUE INDEX payments_app_order_key ON payments (app_id, order_id);
CREATE UNIQUE INDEX payments_app_idempotency_key ON payments (app_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_payments_app_status ON payments (app_id, status);
CREATE INDEX idx_payments_created_public ON payments (created_at DESC, public_id DESC);
CREATE INDEX idx_payments_app_created ON payments (app_id, created_at DESC, public_id DESC);

ALTER TABLE webhook_deliveries ADD COLUMN app_id BIGINT REFERENCES apps (id);
UPDATE webhook_deliveries SET app_id = tenant_id;
ALTER TABLE webhook_deliveries
    ALTER COLUMN app_id SET NOT NULL,
    ALTER COLUMN tenant_id DROP NOT NULL;
CREATE INDEX idx_deliveries_app_status ON webhook_deliveries (app_id, status);

ALTER TABLE ledger_entries ADD COLUMN app_id BIGINT REFERENCES apps (id);
UPDATE ledger_entries SET app_id = tenant_id;
ALTER TABLE ledger_entries
    ALTER COLUMN app_id SET NOT NULL,
    ALTER COLUMN tenant_id DROP NOT NULL;
CREATE INDEX idx_ledger_app_created ON ledger_entries (app_id, created_at);

DROP FUNCTION pg_temp.keipay_ulid(TIMESTAMPTZ);
