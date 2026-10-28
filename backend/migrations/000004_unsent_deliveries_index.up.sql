-- Supports the delivery sweeper, which re-enqueues forwards that were committed
-- but never attempted (e.g. Redis was down when the webhook was processed).
CREATE INDEX IF NOT EXISTS idx_deliveries_unsent ON webhook_deliveries (created_at)
    WHERE status = 'PENDING' AND attempts = 0;
