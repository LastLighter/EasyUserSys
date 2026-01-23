-- Add system_code to users and adjust uniqueness for multi-system usage
ALTER TABLE users ADD COLUMN IF NOT EXISTS system_code TEXT NOT NULL DEFAULT 'default';

-- Remove default after backfill so new users must supply system_code
ALTER TABLE users ALTER COLUMN system_code DROP DEFAULT;

-- Drop old unique constraints to allow same email/google_id across systems
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_google_id_key;

-- Drop old index on google_id if present
DROP INDEX IF EXISTS idx_users_google_id;

-- Create per-system uniqueness and lookup indexes
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_system_email ON users(system_code, email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_system_google_id ON users(system_code, google_id) WHERE google_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_system_code ON users(system_code);
