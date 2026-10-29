-- Reconciliation: payment reports replace ledger_entries (expand: the old
-- table stays in place, read-only, until a later contract migration), and
-- reconciliation runs with their items.

CREATE TABLE payment_reports (
    id            BIGSERIAL   PRIMARY KEY,
    payment_id    BIGINT      NOT NULL UNIQUE REFERENCES payments (id),
    app_id        BIGINT      NOT NULL REFERENCES apps (id),
    connection_id BIGINT      REFERENCES connections (id),
    currency      TEXT        NOT NULL,
    gross         BIGINT      NOT NULL CHECK (gross >= 0),
    -- NULL when the gateway did not report a fee.
    fee           BIGINT      CHECK (fee >= 0),
    net           BIGINT,
    status        TEXT        NOT NULL CHECK (status IN ('PAID', 'SETTLED')),
    paid_at       TIMESTAMPTZ NOT NULL,
    settled_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_reports_net_known_check CHECK ((fee IS NULL) = (net IS NULL)),
    CONSTRAINT payment_reports_net_check CHECK (net IS NULL OR net = gross - fee)
);
CREATE INDEX idx_payment_reports_paid ON payment_reports (paid_at DESC);
CREATE INDEX idx_payment_reports_app_paid ON payment_reports (app_id, paid_at DESC);
CREATE INDEX idx_payment_reports_connection_paid ON payment_reports (connection_id, paid_at DESC);

-- The fee is unknown: old code copied the callback's fees_paid_amount into
-- xendit_fee, and that is the fee charged to the customer, not what Xendit
-- kept from the merchant.
INSERT INTO payment_reports (payment_id, app_id, connection_id, currency, gross, fee, net, status,
                             paid_at, settled_at, created_at, updated_at)
SELECT l.payment_id, l.app_id, p.connection_id, p.currency, l.amount, NULL, NULL,
       CASE WHEN p.status = 'SETTLED' OR l.settled_at IS NOT NULL THEN 'SETTLED' ELSE 'PAID' END,
       COALESCE(p.paid_at, l.created_at), COALESCE(l.settled_at, p.settled_at), l.created_at, now()
FROM ledger_entries l
JOIN payments p ON p.id = l.payment_id
WHERE l.type = 'PAYMENT'
ON CONFLICT (payment_id) DO NOTHING;

CREATE TABLE reconciliation_runs (
    id               BIGSERIAL   PRIMARY KEY,
    public_id        TEXT        NOT NULL UNIQUE,
    connection_id    BIGINT      NOT NULL REFERENCES connections (id),
    -- Half-open: range_start <= t < range_end.
    range_start      TIMESTAMPTZ NOT NULL,
    range_end        TIMESTAMPTZ NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'queued'
                                 CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    run_count        INT         NOT NULL DEFAULT 1 CHECK (run_count >= 1),
    -- {"total": n, "results": {result: n}} once the run succeeded.
    summary          JSONB,
    unresolved_count INT         NOT NULL DEFAULT 0 CHECK (unresolved_count >= 0),
    error_code       TEXT,
    error_message    TEXT,
    created_by       BIGINT      REFERENCES users (id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    CONSTRAINT reconciliation_runs_range_check CHECK (range_end > range_start)
);
CREATE INDEX idx_reconciliation_runs_created ON reconciliation_runs (created_at DESC, public_id DESC);
CREATE INDEX idx_reconciliation_runs_connection_created
    ON reconciliation_runs (connection_id, created_at DESC, public_id DESC);

CREATE TABLE reconciliation_items (
    id              BIGSERIAL   PRIMARY KEY,
    public_id       TEXT        NOT NULL UNIQUE,
    run_id          BIGINT      NOT NULL REFERENCES reconciliation_runs (id) ON DELETE CASCADE,
    reference       TEXT        NOT NULL,
    result          TEXT        NOT NULL CHECK (result IN ('matched', 'missing_in_keipay', 'missing_in_gateway',
                                                           'amount_mismatch', 'status_mismatch', 'fee_mismatch')),
    payment_id      BIGINT      REFERENCES payments (id),
    keipay          JSONB,
    gateway         JSONB,
    anchor_at       TIMESTAMPTZ NOT NULL,
    resolved_at     TIMESTAMPTZ,
    resolved_by     BIGINT      REFERENCES users (id),
    resolution_note TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT reconciliation_items_run_reference_key UNIQUE (run_id, reference),
    CONSTRAINT reconciliation_items_resolution_check CHECK ((resolved_at IS NULL) = (resolution_note IS NULL))
);
CREATE INDEX idx_reconciliation_items_run_created
    ON reconciliation_items (run_id, created_at DESC, public_id DESC);
CREATE INDEX idx_reconciliation_items_run_result_created
    ON reconciliation_items (run_id, result, created_at DESC, public_id DESC);
