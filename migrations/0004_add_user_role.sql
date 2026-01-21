-- 为用户添加角色字段
ALTER TABLE users ADD COLUMN role VARCHAR(20) NOT NULL DEFAULT 'user';

-- 创建角色索引，便于查询管理员用户
CREATE INDEX idx_users_role ON users(role);
