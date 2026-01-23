-- 验证码表，用于邮箱验证和密码重置
CREATE TABLE IF NOT EXISTS verification_codes (
    id BIGSERIAL PRIMARY KEY,
    system_code TEXT NOT NULL,
    email TEXT NOT NULL,
    code VARCHAR(10) NOT NULL,
    code_type VARCHAR(20) NOT NULL, -- 'signup' | 'reset_password'
    expires_at TIMESTAMPTZ NOT NULL,
    verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 创建索引以加快查询
CREATE INDEX IF NOT EXISTS idx_verification_codes_lookup 
ON verification_codes (system_code, email, code_type, verified);

-- 创建索引以便清理过期验证码
CREATE INDEX IF NOT EXISTS idx_verification_codes_expires 
ON verification_codes (expires_at);

-- 添加注释
COMMENT ON TABLE verification_codes IS '邮箱验证码表，用于注册验证和密码重置';
COMMENT ON COLUMN verification_codes.code_type IS '验证码类型: signup-注册验证, reset_password-密码重置';
