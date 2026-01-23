-- 为用户添加角色字段
ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'user';

-- 创建角色索引，便于查询管理员用户
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
