DROP TABLE IF EXISTS inbound_webhooks;
DROP TABLE IF EXISTS webhook_delivery_attempts;

DROP INDEX IF EXISTS webhook_deliveries_public_id_key;
DROP INDEX IF EXISTS idx_deliveries_created;
DROP INDEX IF EXISTS idx_deliveries_app_created;
DROP INDEX IF EXISTS idx_deliveries_endpoint_created;
DROP INDEX IF EXISTS idx_deliveries_event;
DROP INDEX IF EXISTS idx_deliveries_due;
CREATE INDEX IF NOT EXISTS idx_deliveries_unsent ON webhook_deliveries (created_at)
    WHERE status = 'PENDING' AND attempts = 0;

-- Test sends have no payment and cannot exist in the old shape.
DELETE FROM webhook_deliveries WHERE payment_id IS NULL;
ALTER TABLE webhook_deliveries ALTER COLUMN payment_id SET NOT NULL;
ALTER TABLE webhook_deliveries
    DROP COLUMN IF EXISTS public_id,
    DROP COLUMN IF EXISTS event_id,
    DROP COLUMN IF EXISTS endpoint_id,
    DROP COLUMN IF EXISTS max_attempts,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS last_attempt_at,
    DROP COLUMN IF EXISTS last_response_status;

DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS webhook_endpoints;

ALTER TABLE apps ALTER COLUMN webhook_secret_enc DROP DEFAULT;
