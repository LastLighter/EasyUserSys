CREATE TABLE IF NOT EXISTS users (
	id BIGSERIAL PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key_hash TEXT NOT NULL UNIQUE,
	key_prefix TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS plans (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	period_days INT NOT NULL,
	price_cents INT NOT NULL,
	grant_points INT NOT NULL,
	active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS subscriptions (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	plan_id BIGINT NOT NULL REFERENCES plans(id),
	status TEXT NOT NULL,
	started_at TIMESTAMPTZ NOT NULL,
	ends_at TIMESTAMPTZ NOT NULL,
	stripe_subscription_id TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS balance_buckets (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	bucket_type TEXT NOT NULL,
	total_points INT NOT NULL,
	remaining_points INT NOT NULL,
	expires_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS billing_ledger (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	bucket_id BIGINT REFERENCES balance_buckets(id) ON DELETE SET NULL,
	delta_points INT NOT NULL,
	reason TEXT NOT NULL,
	reference_type TEXT NOT NULL,
	reference_id BIGINT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS usage_records (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	units INT NOT NULL,
	cost_points INT NOT NULL,
	request_id TEXT NOT NULL,
	recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE(user_id, request_id)
);

CREATE TABLE IF NOT EXISTS orders (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	order_type TEXT NOT NULL,
	status TEXT NOT NULL,
	amount_cents INT NOT NULL,
	points INT NOT NULL,
	stripe_session_id TEXT,
	stripe_payment_intent_id TEXT,
	stripe_subscription_id TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_balance_buckets_user_id ON balance_buckets(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_records_user_time ON usage_records(user_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
