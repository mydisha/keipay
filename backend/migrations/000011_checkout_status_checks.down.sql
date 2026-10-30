-- The previous release knows only callbacks; rows read back are kept as
-- such so the constraint can return.
UPDATE inbound_webhooks SET source = 'webhook' WHERE source = 'poll';
ALTER TABLE inbound_webhooks DROP CONSTRAINT inbound_webhooks_source_check;
ALTER TABLE inbound_webhooks ADD CONSTRAINT inbound_webhooks_source_check
    CHECK (source IN ('webhook'));

DROP INDEX IF EXISTS idx_payments_pending_expiry;
ALTER TABLE payments DROP COLUMN IF EXISTS status_checked_at;
