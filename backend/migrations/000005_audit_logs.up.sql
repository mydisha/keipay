CREATE TABLE IF NOT EXISTS audit_logs (
    id          BIGSERIAL   PRIMARY KEY,
    public_id   TEXT        NOT NULL UNIQUE,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_type  TEXT        NOT NULL,
    actor_id    TEXT        NOT NULL,
    action      TEXT        NOT NULL,
    target_type TEXT        NOT NULL,
    target_id   TEXT        NOT NULL,
    before      JSONB,
    after       JSONB,
    ip          TEXT        NOT NULL DEFAULT '',
    user_agent  TEXT        NOT NULL DEFAULT '',
    request_id  TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_occurred ON audit_logs (occurred_at DESC, public_id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_target   ON audit_logs (target_type, target_id, occurred_at DESC);
