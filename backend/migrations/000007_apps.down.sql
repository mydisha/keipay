-- Lossy by design (a development tool): app changes are copied back to
-- tenants, apps created since the up migration get an unusable placeholder
-- api_key, and key revocations are lost.

UPDATE tenants t
SET name                 = a.name,
    webhook_url          = a.webhook_url,
    redirect_success_url = a.redirect_success_url,
    redirect_failure_url = a.redirect_failure_url,
    active               = (a.status = 'active'),
    updated_at           = a.updated_at
FROM apps a
WHERE a.id = t.id;

-- Slugs were unique per environment and are unique globally in tenants, so a
-- taken slug gets the environment as a suffix (and the id when even that is
-- taken).
WITH missing AS (
    SELECT a.*, row_number() OVER (PARTITION BY a.slug ORDER BY a.id) AS n
    FROM apps a
    WHERE NOT EXISTS (SELECT 1 FROM tenants t WHERE t.id = a.id)
)
INSERT INTO tenants (id, name, slug, api_key, api_secret_enc, webhook_url, redirect_success_url,
                     redirect_failure_url, active, created_at, updated_at)
SELECT m.id,
       m.name,
       CASE
           WHEN m.n = 1 AND NOT EXISTS (SELECT 1 FROM tenants t WHERE t.slug = m.slug) THEN m.slug
           WHEN NOT EXISTS (SELECT 1 FROM tenants t WHERE t.slug = m.slug || '-' || m.environment)
               THEN m.slug || '-' || m.environment
           ELSE m.slug || '-' || m.environment || '-' || m.id
       END,
       'kp_unavailable_' || lower(m.public_id),
       m.webhook_secret_enc,
       m.webhook_url,
       m.redirect_success_url,
       m.redirect_failure_url,
       m.status = 'active',
       m.created_at,
       m.updated_at
FROM missing m
ORDER BY m.id;

SELECT setval(pg_get_serial_sequence('tenants', 'id'), COALESCE((SELECT max(id) FROM tenants), 0) + 1, false);

UPDATE payments SET tenant_id = app_id WHERE tenant_id IS NULL;
UPDATE webhook_deliveries SET tenant_id = app_id WHERE tenant_id IS NULL;
UPDATE ledger_entries SET tenant_id = app_id WHERE tenant_id IS NULL;

ALTER TABLE payments ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE webhook_deliveries ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE ledger_entries ALTER COLUMN tenant_id SET NOT NULL;

DROP INDEX IF EXISTS idx_ledger_app_created;
ALTER TABLE ledger_entries DROP COLUMN IF EXISTS app_id;

DROP INDEX IF EXISTS idx_deliveries_app_status;
ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS app_id;

DROP INDEX IF EXISTS idx_payments_app_created;
DROP INDEX IF EXISTS idx_payments_created_public;
DROP INDEX IF EXISTS idx_payments_app_status;
DROP INDEX IF EXISTS payments_app_idempotency_key;
DROP INDEX IF EXISTS payments_app_order_key;
DROP INDEX IF EXISTS payments_public_id_key;
ALTER TABLE payments
    DROP COLUMN IF EXISTS idempotency_fingerprint,
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS public_id,
    DROP COLUMN IF EXISTS app_id;

DROP TABLE IF EXISTS app_api_keys;
DROP TABLE IF EXISTS apps;
