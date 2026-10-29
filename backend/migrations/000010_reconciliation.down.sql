-- Reports written since the up migration go back to ledger_entries, where
-- the previous release reads them; an unknown fee is 0 there.
INSERT INTO ledger_entries (app_id, payment_id, amount, xendit_fee, net_amount, type, settled_at, created_at)
SELECT app_id, payment_id, gross, COALESCE(fee, 0), COALESCE(net, gross), 'PAYMENT', settled_at, created_at
FROM payment_reports
ON CONFLICT (payment_id, type) DO NOTHING;

DROP TABLE IF EXISTS reconciliation_items;
DROP TABLE IF EXISTS reconciliation_runs;
DROP TABLE IF EXISTS payment_reports;
