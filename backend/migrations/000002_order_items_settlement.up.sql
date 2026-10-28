ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS items                  JSONB,
    ADD COLUMN IF NOT EXISTS settled_at             TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS expected_settlement_at TIMESTAMPTZ;

ALTER TABLE ledger_entries
    ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ;
