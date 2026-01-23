-- Add password_hash column to users table if missing
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';

-- Remove the default after adding the column (new users must provide a password)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'users' AND column_name = 'password_hash'
    ) THEN
        ALTER TABLE users ALTER COLUMN password_hash DROP DEFAULT;
    END IF;
END
$$;
