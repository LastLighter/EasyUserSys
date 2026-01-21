-- Add password_hash column to users table
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';

-- Remove the default after adding the column (new users must provide a password)
ALTER TABLE users ALTER COLUMN password_hash DROP DEFAULT;
