-- Webhook router: per-app endpoints, events fanned out to deliveries, an
-- attempt log, and an inbound log that is also the durable callback dedupe.
-- webhook_events, apps.webhook_url and apps.webhook_secret_enc stay in place
-- unread until a later contract migration.

-- Same generator as 000007: millisecond timestamp, then 16 random characters.
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

ALTER TABLE apps ALTER COLUMN webhook_secret_enc SET DEFAULT '';

CREATE TABLE webhook_endpoints (
    id                          BIGSERIAL   PRIMARY KEY,
    public_id                   TEXT        NOT NULL UNIQUE,
    app_id                      BIGINT      NOT NULL REFERENCES apps (id),
    url                         TEXT        NOT NULL,
    description                 TEXT        NOT NULL DEFAULT '',
    events                      TEXT[]      NOT NULL CHECK (cardinality(events) BETWEEN 1 AND 20),
    api_version                 TEXT        NOT NULL DEFAULT 'v2' CHECK (api_version IN ('v1', 'v2')),
    legacy_signature            BOOLEAN     NOT NULL DEFAULT false,
    -- crypto.Cipher ciphertext, no additional data.
    secret_enc                  TEXT        NOT NULL,
    -- NULL for secrets carried over from apps.webhook_secret_enc.
    secret_last4                TEXT,
    previous_secret_enc         TEXT,
    previous_secret_expires_at  TIMESTAMPTZ,
    -- Cipher(JSON [{name, value}]); NULL means no custom headers.
    headers_enc                 TEXT,
    headers_hint                JSONB       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(headers_hint) = 'array'),
    status                      TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    disabled_reason             TEXT        CHECK (disabled_reason IN ('manual', 'consecutive_failures')),
    disabled_at                 TIMESTAMPTZ,
    consecutive_failures        INT         NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    retry_max_attempts          INT         NOT NULL DEFAULT 8 CHECK (retry_max_attempts BETWEEN 1 AND 20),
    retry_backoff               TEXT        NOT NULL DEFAULT 'exponential' CHECK (retry_backoff IN ('exponential', 'fixed')),
    retry_interval_seconds      INT         NOT NULL DEFAULT 30 CHECK (retry_interval_seconds BETWEEN 10 AND 3600),
    migrated                    BOOLEAN     NOT NULL DEFAULT false,
    deleted_at                  TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT webhook_endpoints_disabled_check
        CHECK ((status = 'disabled') = (disabled_reason IS NOT NULL AND disabled_at IS NOT NULL))
);
CREATE INDEX idx_webhook_endpoints_app_created ON webhook_endpoints (app_id, created_at DESC, public_id DESC)
    WHERE deleted_at IS NULL;

-- One subscribe-all v1 endpoint per app with a webhook_url, keeping its secret
-- and the X-Kei-Signature header, so existing receivers keep working.
INSERT INTO webhook_endpoints (public_id, app_id, url, events, api_version, legacy_signature,
                               secret_enc, migrated, created_at, updated_at)
SELECT 'whe_' || pg_temp.keipay_ulid(created_at), id, webhook_url, ARRAY['*'], 'v1', true,
       webhook_secret_enc, true, created_at, now()
FROM apps WHERE webhook_url <> '' AND webhook_secret_enc <> '';

CREATE TABLE events (
    id          BIGSERIAL   PRIMARY KEY,
    public_id   TEXT        NOT NULL UNIQUE,
    type        TEXT        NOT NULL,
    api_version TEXT        NOT NULL DEFAULT 'v2' CHECK (api_version IN ('v2')),
    app_id      BIGINT      NOT NULL REFERENCES apps (id),
    -- NULL for test events.
    payment_id  BIGINT      REFERENCES payments (id),
    data        JSONB       NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    is_test     BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_app_created ON events (app_id, created_at DESC);
CREATE INDEX idx_events_payment ON events (payment_id) WHERE payment_id IS NOT NULL;

ALTER TABLE webhook_deliveries
    ADD COLUMN public_id            TEXT,
    ADD COLUMN event_id             BIGINT REFERENCES events (id),
    ADD COLUMN endpoint_id          BIGINT REFERENCES webhook_endpoints (id),
    ADD COLUMN max_attempts         INT,
    ADD COLUMN next_attempt_at      TIMESTAMPTZ,
    ADD COLUMN last_attempt_at      TIMESTAMPTZ,
    ADD COLUMN last_response_status INT,
    ALTER COLUMN payment_id DROP NOT NULL;
UPDATE webhook_deliveries SET public_id = 'dlv_' || pg_temp.keipay_ulid(created_at);
UPDATE webhook_deliveries d SET endpoint_id = e.id
    FROM webhook_endpoints e WHERE e.app_id = d.app_id AND e.migrated;
-- Pending legacy deliveries get the attempts asynq would still have made.
UPDATE webhook_deliveries SET max_attempts = CASE WHEN status = 'PENDING' THEN attempts + 6 ELSE GREATEST(attempts, 1) END,
                              next_attempt_at = CASE WHEN status = 'PENDING' THEN now() END;
ALTER TABLE webhook_deliveries
    ALTER COLUMN public_id SET NOT NULL,
    ALTER COLUMN max_attempts SET NOT NULL,
    ALTER COLUMN max_attempts SET DEFAULT 1;
CREATE UNIQUE INDEX webhook_deliveries_public_id_key ON webhook_deliveries (public_id);
CREATE INDEX idx_deliveries_created ON webhook_deliveries (created_at DESC, public_id DESC);
CREATE INDEX idx_deliveries_app_created ON webhook_deliveries (app_id, created_at DESC, public_id DESC);
CREATE INDEX idx_deliveries_endpoint_created ON webhook_deliveries (endpoint_id, created_at DESC, public_id DESC);
CREATE INDEX idx_deliveries_event ON webhook_deliveries (event_id) WHERE event_id IS NOT NULL;
CREATE INDEX idx_deliveries_due ON webhook_deliveries (next_attempt_at) WHERE status = 'PENDING';
DROP INDEX IF EXISTS idx_deliveries_unsent;

CREATE TABLE webhook_delivery_attempts (
    id                      BIGSERIAL   PRIMARY KEY,
    delivery_id             BIGINT      NOT NULL REFERENCES webhook_deliveries (id),
    attempt_no              INT         NOT NULL CHECK (attempt_no >= 1),
    manual                  BOOLEAN     NOT NULL DEFAULT false,
    url                     TEXT        NOT NULL,
    request_headers         JSONB       NOT NULL CHECK (jsonb_typeof(request_headers) = 'object'),
    response_status         INT,
    -- At most 4096 bytes, valid UTF-8, no NUL.
    response_body           TEXT        NOT NULL DEFAULT '',
    response_body_truncated BOOLEAN     NOT NULL DEFAULT false,
    error                   TEXT        NOT NULL DEFAULT '',
    duration_ms             INT         NOT NULL CHECK (duration_ms >= 0),
    started_at              TIMESTAMPTZ NOT NULL,
    CONSTRAINT webhook_delivery_attempts_no_key UNIQUE (delivery_id, attempt_no)
);

CREATE TABLE inbound_webhooks (
    id              BIGSERIAL   PRIMARY KEY,
    public_id       TEXT        NOT NULL UNIQUE,
    connection_id   BIGINT      NOT NULL REFERENCES connections (id),
    provider        TEXT        NOT NULL,
    source          TEXT        NOT NULL DEFAULT 'webhook' CHECK (source IN ('webhook')),
    signature_valid BOOLEAN     NOT NULL,
    result          TEXT        NOT NULL CHECK (result IN ('applied', 'stale', 'duplicate', 'mismatch',
                                    'unmapped', 'invalid_signature', 'invalid_payload', 'error')),
    result_detail   TEXT        NOT NULL DEFAULT '',
    event_key       TEXT,
    provider_status TEXT,
    reference       TEXT,
    payment_id      BIGINT      REFERENCES payments (id),
    event_id        BIGINT      REFERENCES events (id),
    raw_body        BYTEA       NOT NULL,
    body_truncated  BOOLEAN     NOT NULL DEFAULT false,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- The durable dedupe claim: one claimed outcome per (connection, event key, gateway status).
CREATE UNIQUE INDEX inbound_webhooks_dedupe_key ON inbound_webhooks (connection_id, event_key, provider_status)
    WHERE result IN ('applied', 'stale', 'mismatch', 'unmapped');
CREATE INDEX idx_inbound_received ON inbound_webhooks (received_at DESC, public_id DESC);
CREATE INDEX idx_inbound_connection_received ON inbound_webhooks (connection_id, received_at DESC, public_id DESC);

DROP FUNCTION pg_temp.keipay_ulid(TIMESTAMPTZ);
