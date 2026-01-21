-- 添加 Google OAuth 登录支持
ALTER TABLE users ADD COLUMN IF NOT EXISTS google_id TEXT UNIQUE;

-- 为 google_id 创建索引以加速查询
CREATE INDEX IF NOT EXISTS idx_users_google_id ON users(google_id) WHERE google_id IS NOT NULL;
