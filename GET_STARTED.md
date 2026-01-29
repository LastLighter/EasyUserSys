# Get Started

本文档描述如何部署和测试该项目（后端 HTTP API，无前端）。

## 前置条件

- Go 1.22+
- PostgreSQL 13+
- 可选：Stripe 账号（订阅与支付）

## 安装与启动

1. 创建数据库

```sql
CREATE DATABASE easyusersys;
```

2. 配置环境变量（支持 `.env`）

推荐：使用 `.env` 文件（不会提交到仓库）

```bash
cp env.example .env
```

`.env` 内容示例：

```bash
DATABASE_URL=postgres://postgres:postgres@localhost:5432/easyusersys?sslmode=disable
SERVER_ADDR=:8080
COST_PER_UNIT=1
FREE_SIGNUP_POINTS=5
FREE_SIGNUP_EXPIRY_DAYS=30
PREPAID_EXPIRY_DAYS=30
STRIPE_SECRET_KEY=sk_test_xxx
STRIPE_WEBHOOK_SECRET=whsec_xxx
STRIPE_PRICE_MONTHLY=price_monthly_xxx
STRIPE_PRICE_QUARTERLY=price_quarterly_xxx
STRIPE_CURRENCY=usd
SUBSCRIPTION_MONTHLY_POINTS=200
SUBSCRIPTION_QUARTERLY_POINTS=600

# JWT 认证配置（必填）
JWT_SECRET_KEY=your-secret-key-at-least-32-characters-long
JWT_EXPIRY_HOURS=168

# 服务间认证（用量上报）
USAGE_API_KEY=your-usage-api-key-for-internal-services
```

备用方案：配置系统环境变量（Windows）

```bash
setx DATABASE_URL "postgres://postgres:postgres@localhost:5432/easyusersys?sslmode=disable"
setx SERVER_ADDR ":8080"
setx COST_PER_UNIT "1"
setx FREE_SIGNUP_POINTS "5"
setx FREE_SIGNUP_EXPIRY_DAYS "30"
setx PREPAID_EXPIRY_DAYS "30"
setx STRIPE_SECRET_KEY "sk_test_xxx"
setx STRIPE_WEBHOOK_SECRET "whsec_xxx"
setx STRIPE_PRICE_MONTHLY "price_monthly_xxx"
setx STRIPE_PRICE_QUARTERLY "price_quarterly_xxx"
setx STRIPE_CURRENCY "usd"
setx SUBSCRIPTION_MONTHLY_POINTS "200"
setx SUBSCRIPTION_QUARTERLY_POINTS "600"
setx JWT_SECRET_KEY "your-secret-key-at-least-32-characters-long"
setx JWT_EXPIRY_HOURS "168"
setx USAGE_API_KEY "your-usage-api-key-for-internal-services"
```

说明：
- `COST_PER_UNIT` 为每次用量扣除积分（默认 1），支持浮点数用于按量计费。
- `FREE_SIGNUP_POINTS` 为注册赠送积分（默认 5），支持浮点数。
- `FREE_SIGNUP_EXPIRY_DAYS` 为免费积分过期天数（默认 30 天，即每月刷新）。
- `SUBSCRIPTION_*_POINTS` 为订阅发放积分额度，支持浮点数。
- `JWT_SECRET_KEY` **必须配置**，用于签名 JWT Token，建议使用至少 32 字符的随机字符串。
- `JWT_EXPIRY_HOURS` Token 有效期，默认 168 小时（7 天）。
- `USAGE_API_KEY` 用量上报接口的服务间认证密钥，供内部微服务调用。

3. 执行数据库迁移

项目自带 SQL 迁移文件，按顺序执行：

```bash
psql "%DATABASE_URL%" -f migrations/0001_init.sql
psql "%DATABASE_URL%" -f migrations/0002_add_order_subscription_id.sql
psql "%DATABASE_URL%" -f migrations/0003_add_user_password.sql
psql "%DATABASE_URL%" -f migrations/0004_add_user_role.sql
psql "%DATABASE_URL%" -f migrations/0005_add_user_google_id.sql
psql "%DATABASE_URL%" -f migrations/0006_add_user_system_code.sql
psql "%DATABASE_URL%" -f migrations/0007_add_verification_codes.sql
psql "%DATABASE_URL%" -f migrations/0008_points_to_float.sql
psql "%DATABASE_URL%" -f migrations/0009_add_system_code_to_finance_tables.sql
```

如果本地没有 `psql`，可以用 Python 脚本（需要安装依赖）：

```bash
python -m pip install "psycopg[binary]"
python tools/migrate.py
```

4. 启动服务

```bash
go run .
```

启动后默认监听 `:8080`。

## 测试

运行单元测试：

```bash
go test ./...
```

## 创建管理员账户

系统部署后，需要手动将第一个用户设置为管理员。

1. 先注册一个普通用户：

```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@example.com", "password": "your_secure_password"}'
```

2. 直接在数据库中将该用户设置为管理员：

```sql
UPDATE users SET role = 'admin' WHERE email = 'admin@example.com';
```

3. 之后可以使用该管理员账户登录，通过 API 设置其他管理员：

```bash
# 先登录获取 Token
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@example.com", "password": "your_secure_password"}'

# 使用 Token 设置其他用户为管理员
curl -X PATCH http://localhost:8080/admin/users/2/role \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your_token>" \
  -d '{"role": "admin"}'
```

## 常见问题

- 未配置 Stripe 相关环境变量时，订阅/支付相关接口会返回 `503`。
- 未配置 `JWT_SECRET_KEY` 时，登录接口会返回错误。
- 所有涉及用户隐私数据的接口都需要 JWT Token 认证。
- 管理员接口（`/admin/*`）仅限管理员角色访问。
