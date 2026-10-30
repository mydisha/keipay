-- When KeiPay last read a pending payment's checkout back from the gateway,
-- because the checkout expired and its callback never arrived.
ALTER TABLE payments ADD COLUMN status_checked_at TIMESTAMPTZ;

-- Finds pending payments whose checkout has expired.
CREATE INDEX idx_payments_pending_expiry ON payments (expires_at)
    WHERE status = 'PENDING' AND expires_at IS NOT NULL;

-- A checkout read back is logged next to the callbacks, as source 'poll'.
ALTER TABLE inbound_webhooks DROP CONSTRAINT inbound_webhooks_source_check;
ALTER TABLE inbound_webhooks ADD CONSTRAINT inbound_webhooks_source_check
    CHECK (source IN ('webhook', 'poll'));
