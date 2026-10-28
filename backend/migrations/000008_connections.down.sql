ALTER TABLE apps DROP CONSTRAINT IF EXISTS apps_default_connection_fkey;
DROP INDEX IF EXISTS idx_apps_default_connection;
ALTER TABLE apps
    DROP COLUMN IF EXISTS default_connection_id,
    DROP COLUMN IF EXISTS merchant_id;

ALTER TABLE payments
    DROP COLUMN IF EXISTS provider,
    DROP COLUMN IF EXISTS connection_id;

DROP TABLE IF EXISTS connection_channels;
ALTER TABLE merchants DROP CONSTRAINT IF EXISTS merchants_legacy_connection_fkey;
DROP TABLE IF EXISTS connections;
DROP TABLE IF EXISTS merchants;
