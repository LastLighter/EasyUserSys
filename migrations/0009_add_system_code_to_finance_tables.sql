-- Add system_code to finance and access-related tables for multi-system isolation

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS system_code TEXT;
UPDATE api_keys ak
SET system_code = u.system_code
FROM users u
WHERE ak.user_id = u.id
  AND ak.system_code IS NULL;
ALTER TABLE api_keys ALTER COLUMN system_code SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_system_code ON api_keys(system_code);
CREATE INDEX IF NOT EXISTS idx_api_keys_system_user ON api_keys(system_code, user_id);

ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS system_code TEXT;
UPDATE subscriptions s
SET system_code = u.system_code
FROM users u
WHERE s.user_id = u.id
  AND s.system_code IS NULL;
ALTER TABLE subscriptions ALTER COLUMN system_code SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_subscriptions_system_code ON subscriptions(system_code);
CREATE INDEX IF NOT EXISTS idx_subscriptions_system_user ON subscriptions(system_code, user_id);

ALTER TABLE balance_buckets ADD COLUMN IF NOT EXISTS system_code TEXT;
UPDATE balance_buckets b
SET system_code = u.system_code
FROM users u
WHERE b.user_id = u.id
  AND b.system_code IS NULL;
ALTER TABLE balance_buckets ALTER COLUMN system_code SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_balance_buckets_system_code ON balance_buckets(system_code);
CREATE INDEX IF NOT EXISTS idx_balance_buckets_system_user ON balance_buckets(system_code, user_id);

ALTER TABLE billing_ledger ADD COLUMN IF NOT EXISTS system_code TEXT;
UPDATE billing_ledger bl
SET system_code = u.system_code
FROM users u
WHERE bl.user_id = u.id
  AND bl.system_code IS NULL;
ALTER TABLE billing_ledger ALTER COLUMN system_code SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_billing_ledger_system_code ON billing_ledger(system_code);
CREATE INDEX IF NOT EXISTS idx_billing_ledger_system_user ON billing_ledger(system_code, user_id);

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS system_code TEXT;
UPDATE usage_records ur
SET system_code = u.system_code
FROM users u
WHERE ur.user_id = u.id
  AND ur.system_code IS NULL;
ALTER TABLE usage_records ALTER COLUMN system_code SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_usage_records_system_code ON usage_records(system_code);
CREATE INDEX IF NOT EXISTS idx_usage_records_system_user_time ON usage_records(system_code, user_id, recorded_at DESC);

ALTER TABLE orders ADD COLUMN IF NOT EXISTS system_code TEXT;
UPDATE orders o
SET system_code = u.system_code
FROM users u
WHERE o.user_id = u.id
  AND o.system_code IS NULL;
ALTER TABLE orders ALTER COLUMN system_code SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orders_system_code ON orders(system_code);
CREATE INDEX IF NOT EXISTS idx_orders_system_user ON orders(system_code, user_id);
