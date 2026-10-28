CREATE TABLE IF NOT EXISTS tenants (
    id                   BIGSERIAL PRIMARY KEY,
    name                 TEXT        NOT NULL,
    slug                 TEXT        NOT NULL UNIQUE,
    api_key              TEXT        NOT NULL UNIQUE,
    api_secret_enc       TEXT        NOT NULL,
    webhook_url          TEXT        NOT NULL DEFAULT '',
    redirect_success_url TEXT        NOT NULL DEFAULT '',
    redirect_failure_url TEXT        NOT NULL DEFAULT '',
    active               BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payments (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT      NOT NULL REFERENCES tenants(id),
    order_id          TEXT        NOT NULL,
    external_id       TEXT        NOT NULL UNIQUE,
    xendit_invoice_id TEXT        NOT NULL DEFAULT '',
    amount            BIGINT      NOT NULL,
    currency          TEXT        NOT NULL DEFAULT 'IDR',
    status            TEXT        NOT NULL DEFAULT 'PENDING',
    description       TEXT        NOT NULL DEFAULT '',
    invoice_url       TEXT        NOT NULL DEFAULT '',
    payer_email       TEXT        NOT NULL DEFAULT '',
    expires_at        TIMESTAMPTZ,
    paid_at           TIMESTAMPTZ,
    raw               JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, order_id)
);

CREATE INDEX IF NOT EXISTS idx_payments_tenant_status ON payments (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments (created_at);
CREATE INDEX IF NOT EXISTS idx_payments_xendit_invoice_id ON payments (xendit_invoice_id);

CREATE TABLE IF NOT EXISTS webhook_events (
    id          BIGSERIAL PRIMARY KEY,
    xendit_id   TEXT        NOT NULL,
    status      TEXT        NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (xendit_id, status)
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id         BIGSERIAL PRIMARY KEY,
    payment_id BIGINT      NOT NULL REFERENCES payments(id),
    tenant_id  BIGINT      NOT NULL REFERENCES tenants(id),
    event_type TEXT        NOT NULL,
    payload    JSONB       NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'PENDING',
    attempts   INT         NOT NULL DEFAULT 0,
    last_error TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_deliveries_tenant_status ON webhook_deliveries (tenant_id, status);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  BIGINT      NOT NULL REFERENCES tenants(id),
    payment_id BIGINT      NOT NULL REFERENCES payments(id),
    amount     BIGINT      NOT NULL,
    xendit_fee BIGINT      NOT NULL DEFAULT 0,
    net_amount BIGINT      NOT NULL,
    type       TEXT        NOT NULL DEFAULT 'PAYMENT',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (payment_id, type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_tenant_created ON ledger_entries (tenant_id, created_at);
