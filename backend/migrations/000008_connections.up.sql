-- Gateway accounts become data (expand only): merchants with one hidden
-- default, connections with sealed credentials, their channel choices, and
-- the links from apps and payments. payments.xendit_invoice_id stays.

CREATE TABLE merchants (
    id                   BIGSERIAL   PRIMARY KEY,
    name                 TEXT        NOT NULL,
    is_default           BOOLEAN     NOT NULL DEFAULT false,
    -- The connection imported from XENDIT_*, the target of /webhooks/xendit.
    legacy_connection_id BIGINT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX merchants_default_key ON merchants (is_default) WHERE is_default;
INSERT INTO merchants (name, is_default) VALUES ('Default', true);

CREATE TABLE connections (
    id                  BIGSERIAL   PRIMARY KEY,
    public_id           TEXT        NOT NULL UNIQUE,
    merchant_id         BIGINT      NOT NULL REFERENCES merchants (id),
    provider            TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    environment         TEXT        NOT NULL CHECK (environment IN ('test', 'live')),
    status              TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    -- AES-256-GCM of the credentials JSON with a per-connection data key and
    -- public_id as additional data; the data key is encrypted with ENCRYPTION_KEY.
    credentials_enc     TEXT        NOT NULL,
    credentials_dek_enc TEXT        NOT NULL,
    key_version         INT         NOT NULL DEFAULT 1,
    credentials_last4   JSONB       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(credentials_last4) = 'object'),
    settings            JSONB       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(settings) = 'object'),
    last_tested_at      TIMESTAMPTZ,
    last_test_status    TEXT        CHECK (last_test_status IN ('ok', 'failed')),
    last_test_error     TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT connections_merchant_name_key UNIQUE (merchant_id, name),
    -- Target of the composite foreign key that keeps an app's default
    -- connection in the app's merchant and environment.
    CONSTRAINT connections_id_merchant_environment_key UNIQUE (id, merchant_id, environment)
);
CREATE INDEX idx_connections_created ON connections (created_at DESC, public_id DESC);
ALTER TABLE merchants ADD CONSTRAINT merchants_legacy_connection_fkey
    FOREIGN KEY (legacy_connection_id) REFERENCES connections (id);

CREATE TABLE connection_channels (
    connection_id BIGINT      NOT NULL REFERENCES connections (id) ON DELETE CASCADE,
    channel       TEXT        NOT NULL,
    enabled       BOOLEAN     NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, channel)
);

ALTER TABLE apps ADD COLUMN merchant_id BIGINT REFERENCES merchants (id);
UPDATE apps SET merchant_id = (SELECT id FROM merchants WHERE is_default);
ALTER TABLE apps ALTER COLUMN merchant_id SET NOT NULL;
ALTER TABLE apps ADD COLUMN default_connection_id BIGINT;
ALTER TABLE apps ADD CONSTRAINT apps_default_connection_fkey
    FOREIGN KEY (default_connection_id, merchant_id, environment)
    REFERENCES connections (id, merchant_id, environment);
CREATE INDEX idx_apps_default_connection ON apps (default_connection_id)
    WHERE default_connection_id IS NOT NULL;

ALTER TABLE payments
    ADD COLUMN connection_id BIGINT REFERENCES connections (id),
    ADD COLUMN provider      TEXT;
