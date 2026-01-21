ALTER TABLE orders
	ADD COLUMN IF NOT EXISTS subscription_id BIGINT REFERENCES subscriptions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_orders_subscription_id ON orders(subscription_id);
