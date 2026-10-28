ALTER TABLE ledger_entries
    DROP COLUMN IF EXISTS settled_at;

ALTER TABLE payments
    DROP COLUMN IF EXISTS expected_settlement_at,
    DROP COLUMN IF EXISTS settled_at,
    DROP COLUMN IF EXISTS items;
