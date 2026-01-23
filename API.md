# API 文档

## 概述

### 基础地址

```
http://localhost:8080
```

### 认证

本系统使用 JWT（JSON Web Token）进行身份认证。

**获取 Token**：通过以下方式获取 JWT Token：
- 邮箱密码登录：`POST /auth/login`
- Google 账号登录：`GET /auth/google` → `GET /auth/google/callback`

**携带 Token**：在需要认证的接口请求头中添加：

```
Authorization: Bearer <your_jwt_token>
```

**Token 有效期**：默认 7 天（168 小时），可通过环境变量 `JWT_EXPIRY_HOURS` 配置。

**接口权限说明**：
| 标记 | 说明 |
|------|------|
| 公开 | 无需认证即可访问 |
| 需要认证 | 需要有效的 JWT Token |
| 仅限本人 | 只能操作自己的资源，管理员可操作任何人 |
| 仅限管理员 | 仅管理员角色可访问 |

### 用户角色

| 角色 | 说明 |
|------|------|
| `user` | 普通用户（默认） |
| `admin` | 管理员，可访问所有用户数据和管理接口 |

### 统一响应格式

**成功响应**：返回 JSON 对象或数组

**错误响应**：
```json
{"error": "错误描述信息"}
```

### HTTP 状态码

| 状态码 | 含义 |
|--------|------|
| 200 | 请求成功 |
| 201 | 创建成功 |
| 400 | 请求参数错误 |
| 401 | 未认证（Token 缺失或无效） |
| 403 | 禁止访问（无权限或无有效订阅） |
| 404 | 资源不存在 |
| 409 | 冲突（如重复请求、积分不足） |
| 429 | 请求过于频繁（如验证码发送频率限制） |
| 503 | 服务不可用（如 Stripe/邮件服务 未配置） |

---

## 认证模块

### 用户登录

`POST /auth/login` **公开**

验证用户凭证，返回 JWT Token。

**请求**：
```json
{
  "system_code": "demo",
  "email": "user@example.com",
  "password": "your_password"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| system_code | string | 是 | 系统标识（租户） |
| email | string | 是 | 用户邮箱 |
| password | string | 是 | 用户密码 |

**响应**（200）：
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": 1,
    "system_code": "demo",
    "email": "user@example.com",
    "role": "user"
  }
}
```

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 401 | 邮箱或密码错误 |
| 400 | 缺少必要参数 |

---

### Google OAuth 登录

支持使用 Google 账号授权登录。首次登录会自动创建用户并赠送免费积分。

#### 发起 Google 登录

`GET /auth/google` **公开**

重定向用户到 Google 授权页面。

**使用方式**：
```javascript
// 前端直接跳转
window.location.href = '/auth/google?system_code=demo';
```
**查询参数**：
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| system_code | string | 是 | 系统标识（租户） |


**流程说明**：
1. 用户访问此接口
2. 服务端生成 CSRF state token 并设置 cookie
3. 用户被重定向到 Google 授权页面
4. 用户授权后，Google 重定向回 `/auth/google/callback`

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 503 | Google OAuth 未配置 |

---

#### Google 登录回调

`GET /auth/google/callback` **公开**

处理 Google OAuth 回调，完成登录并返回 JWT Token。

**查询参数**（由 Google 自动附加）：
| 参数 | 类型 | 说明 |
|------|------|------|
| code | string | 授权码 |
| state | string | CSRF 防护令牌 |

**响应**（200）：
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "is_new_user": true,
  "user": {
    "id": 1,
    "system_code": "demo",
    "email": "user@gmail.com",
    "role": "user"
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| token | string | JWT Token，用于后续 API 调用 |
| is_new_user | boolean | 是否为首次登录（新创建的用户） |
| user | object | 用户基本信息 |

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 400 | state 参数无效（可能是 CSRF 攻击）|
| 400 | Google 邮箱未验证 |
| 400 | 缺少授权码 |
| 403 | 用户账号已被禁用 |
| 500 | Token 交换失败或获取用户信息失败 |
| 503 | Google OAuth 未配置 |

**特殊说明**：
- 首次登录会自动创建用户，并赠送免费积分（与邮箱注册相同）
- 如果邮箱已存在（之前用密码注册），会自动绑定 Google 账号
- 已禁用的用户无法通过 Google 登录

---

### 发送验证码

`POST /auth/send-verification-code` **公开**

发送邮箱验证码，用于注册验证或找回密码。每个邮箱每分钟最多发送 1 次。

**请求**：
```json
{
  "system_code": "demo",
  "email": "user@example.com",
  "code_type": "reset_password"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| system_code | string | 是 | 系统标识（租户） |
| email | string | 是 | 用户邮箱 |
| code_type | string | 是 | 验证码类型 |

**验证码类型**：
| 类型 | 说明 |
|------|------|
| `signup` | 注册验证（可用于验证邮箱真实性） |
| `reset_password` | 密码重置（用户必须已存在） |

**响应**（200）：
```json
{
  "status": "ok",
  "message": "verification code sent"
}
```

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 400 | 缺少必要参数或 code_type 无效 |
| 404 | `reset_password` 类型时用户不存在 |
| 429 | 请求过于频繁（1分钟内只能发送1次） |
| 503 | 邮件服务未配置（API Key 或该 system_code 的发件人未配置） |

**多应用邮件配置**：

邮件发送支持多应用配置，每个 `system_code` 可以有不同的发件人地址。配置方式如下：

```bash
# API Key 和过期时间是共享的
RESEND_API_KEY=re_xxx
VERIFICATION_CODE_EXPIRY_MINUTES=10

# 多应用邮件配置（JSON 格式，按 system_code 区分）
RESEND_EMAIL_CONFIGS={"appA":{"from_email":"noreply@appA.com"},"appB":{"from_email":"noreply@appB.com"}}

# 兼容旧配置（会作为 "default" 配置，当 system_code 无对应配置时使用）
RESEND_FROM_EMAIL=noreply@yourdomain.com
```

---

### 验证验证码

`POST /auth/verify-code` **公开**

验证邮箱验证码是否正确有效。

**请求**：
```json
{
  "system_code": "demo",
  "email": "user@example.com",
  "code": "123456",
  "code_type": "reset_password"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| system_code | string | 是 | 系统标识（租户） |
| email | string | 是 | 用户邮箱 |
| code | string | 是 | 6位数字验证码 |
| code_type | string | 是 | 验证码类型 |

**响应**（200）：
```json
{
  "status": "ok",
  "message": "code verified"
}
```

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 400 | 验证码错误、已过期或已使用 |

---

### 重置密码

`POST /auth/reset-password` **公开**

使用验证码重置用户密码。需要先获取验证码。

**请求**：
```json
{
  "system_code": "demo",
  "email": "user@example.com",
  "code": "123456",
  "new_password": "your_new_secure_password"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| system_code | string | 是 | 系统标识（租户） |
| email | string | 是 | 用户邮箱 |
| code | string | 是 | 6位数字验证码 |
| new_password | string | 是 | 新密码 |

**响应**（200）：
```json
{
  "status": "ok",
  "message": "password reset successfully"
}
```

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 400 | 验证码错误、已过期或已使用 |
| 404 | 用户不存在 |

**找回密码流程**：
```
┌─────────────────────────────────────────────────────────────────┐
│  1. 用户输入邮箱，点击「发送验证码」                               │
│                              ↓                                  │
│  2. 前端调用 POST /auth/send-verification-code                  │
│     { code_type: "reset_password" }                            │
│                              ↓                                  │
│  3. 用户收到验证码邮件                                            │
│                              ↓                                  │
│  4. 用户输入验证码和新密码                                         │
│                              ↓                                  │
│  5. 前端调用 POST /auth/reset-password                          │
│                              ↓                                  │
│  6. 密码重置成功，引导用户登录                                      │
└─────────────────────────────────────────────────────────────────┘
```

---

## 用户模块

### 创建用户

`POST /users` **公开**

创建新用户（注册）。新用户会自动获得免费注册积分（默认 10 点）。

**请求**：
```json
{
  "system_code": "demo",
  "email": "user@example.com",
  "password": "your_secure_password"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| system_code | string | 是 | 系统标识（租户） |
| email | string | 是 | 用户邮箱，系统内需唯一 |
| password | string | 是 | 用户密码 |

**响应**（201）：
```json
{
  "ID": 1,
  "SystemCode": "demo",
  "Email": "user@example.com",
  "Status": "active",
  "Role": "user",
  "CreatedAt": "2025-01-21T10:00:00Z",
  "UpdatedAt": "2025-01-21T10:00:00Z"
}
```

---

### 查询用户

`GET /users/{id}` **需要认证** **仅限本人**

**路径参数**：
| 参数 | 类型 | 说明 |
|------|------|------|
| id | int64 | 用户 ID |

**响应**（200）：
```json
{
  "ID": 1,
  "SystemCode": "demo",
  "Email": "user@example.com",
  "Status": "active",
  "Role": "user",
  "CreatedAt": "2025-01-21T10:00:00Z",
  "UpdatedAt": "2025-01-21T10:00:00Z"
}
```

---

### 通过邮箱查询用户

`GET /users/by-email?system_code={system_code}&email={email}` **公开**

**查询参数**：
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| system_code | string | 是 | 系统标识（租户） |
| email | string | 是 | 用户邮箱 |

**响应**（200）：与「查询用户」相同

---

### 更新用户状态

`PATCH /users/{id}/status` **需要认证** **仅限管理员**

**请求**：
```json
{
  "status": "disabled"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| status | string | 是 | 用户状态 |

**用户状态值**：
| 状态 | 说明 |
|------|------|
| `active` | 正常 |
| `disabled` | 已禁用 |

**响应**（200）：
```json
{"status": "ok"}
```

---

### 查询余额桶

`GET /users/{id}/balances` **需要认证** **仅限本人**

查询用户的积分余额。系统支持多种类型的积分桶，按优先级扣减。

**响应**（200）：
```json
[
  {
    "ID": 1,
    "UserID": 1,
    "BucketType": "free",
    "TotalPoints": 10,
    "RemainingPoints": 5,
    "ExpiresAt": null,
    "CreatedAt": "2025-01-21T10:00:00Z",
    "UpdatedAt": "2025-01-21T10:00:00Z"
  },
  {
    "ID": 2,
    "UserID": 1,
    "BucketType": "subscription",
    "TotalPoints": 200,
    "RemainingPoints": 150,
    "ExpiresAt": "2025-02-21T10:00:00Z",
    "CreatedAt": "2025-01-21T10:00:00Z",
    "UpdatedAt": "2025-01-21T10:00:00Z"
  }
]
```

**积分桶类型**：
| 类型 | 说明 |
|------|------|
| `free` | 免费注册赠送积分，无过期时间 |
| `subscription` | 订阅发放积分，周期内有效 |
| `prepaid` | 预充值购买积分，有过期时间 |

---

## API Key 模块

API Key 用于标识和验证应用程序的 API 调用身份。

### 创建 API Key

`POST /users/{id}/api-keys` **需要认证** **仅限本人**

为指定用户创建新的 API Key。

> ⚠️ `raw_key` 是完整密钥，**仅在创建时返回一次**，请妥善保存。

**响应**（201）：
```json
{
  "raw_key": "eus_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "api_key": {
    "ID": 1,
    "UserID": 1,
    "KeyHash": "...",
    "KeyPrefix": "eus_a1b2",
    "Status": "active",
    "CreatedAt": "2025-01-21T10:00:00Z",
    "RevokedAt": null
  }
}
```

---

### 列出 API Keys

`GET /users/{id}/api-keys` **需要认证** **仅限本人**

列出指定用户的所有 API Key。

**响应**（200）：
```json
[
  {
    "ID": 1,
    "UserID": 1,
    "KeyHash": "...",
    "KeyPrefix": "eus_a1b2",
    "Status": "active",
    "CreatedAt": "2025-01-21T10:00:00Z",
    "RevokedAt": null
  }
]
```

---

### 吊销 API Key

`POST /api-keys/{id}/revoke` **需要认证** **仅限本人**

吊销指定的 API Key，吊销后不可恢复。只能吊销自己的 API Key。

**响应**（200）：
```json
{"status": "ok"}
```

**API Key 状态值**：
| 状态 | 说明 |
|------|------|
| `active` | 有效 |
| `revoked` | 已吊销 |

---

## 订阅模块

### 查询订阅计划

`GET /plans` **公开**

获取所有可用的订阅计划。

**响应**（200）：
```json
[
  {
    "ID": 1,
    "Name": "monthly",
    "PeriodDays": 30,
    "PriceCents": 999,
    "GrantPoints": 200,
    "Active": true
  },
  {
    "ID": 2,
    "Name": "quarterly",
    "PeriodDays": 90,
    "PriceCents": 2499,
    "GrantPoints": 600,
    "Active": true
  }
]
```

| 字段 | 说明 |
|------|------|
| PeriodDays | 订阅周期天数 |
| PriceCents | 价格（美分） |
| GrantPoints | 订阅发放的积分数量 |
| Active | 是否上架销售 |

---

### 创建订阅 Checkout

`POST /subscriptions/checkout` **需要认证** **仅限本人**

创建 Stripe 订阅支付会话，返回支付链接。只能为自己创建订阅。

**请求**：
```json
{
  "user_id": 1,
  "plan_id": 1,
  "success_url": "https://example.com/payment/success?order_id={order_id}",
  "cancel_url": "https://example.com/payment/cancel"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | int64 | 是 | 用户 ID |
| plan_id | int64 | 是 | 订阅计划 ID |
| success_url | string | 是 | 支付成功后跳转地址 |
| cancel_url | string | 是 | 用户取消支付后跳转地址 |

**响应**（201）：
```json
{
  "order_id": 1,
  "subscription_id": 1,
  "stripe_session": "cs_test_xxx",
  "checkout_url": "https://checkout.stripe.com/c/pay/cs_test_xxx"
}
```

| 字段 | 说明 |
|------|------|
| order_id | 订单 ID，用于后续查询支付状态 |
| subscription_id | 订阅 ID |
| stripe_session | Stripe 会话 ID |
| checkout_url | **跳转此链接完成支付** |

---

### 取消订阅

`POST /subscriptions/{id}/cancel` **需要认证** **仅限本人**

取消指定订阅。只能取消自己的订阅。

**响应**（200）：
```json
{"status": "ok"}
```

---

### 查询订阅

`GET /subscriptions/{id}` **需要认证** **仅限本人**

**响应**（200）：
```json
{
  "ID": 1,
  "UserID": 1,
  "PlanID": 1,
  "Status": "active",
  "StartedAt": "2025-01-21T10:00:00Z",
  "EndsAt": "2025-02-20T10:00:00Z",
  "StripeSubscriptionID": "sub_xxx",
  "CreatedAt": "2025-01-21T10:00:00Z",
  "UpdatedAt": "2025-01-21T10:00:00Z"
}
```

**订阅状态值**：
| 状态 | 说明 |
|------|------|
| `pending` | 待支付 |
| `active` | 生效中 |
| `canceled` | 已取消 |
| `expired` | 已过期 |

---

## 预充值模块（按量积分包）

### 创建预充值 Checkout

`POST /prepaid/checkout` **需要认证** **仅限本人**

创建一次性积分充值支付会话。积分数量 = 金额（美分）/ 10，例如充值 $20.00 获得 200 积分。只能为自己充值。

**请求**：
```json
{
  "user_id": 1,
  "amount_cents": 2000,
  "success_url": "https://example.com/payment/success?order_id={order_id}",
  "cancel_url": "https://example.com/payment/cancel"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | int64 | 是 | 用户 ID |
| amount_cents | int | 是 | 金额（美分），同时也是获得的积分数 |
| success_url | string | 是 | 支付成功后跳转地址 |
| cancel_url | string | 是 | 用户取消支付后跳转地址 |

**响应**（201）：
```json
{
  "order_id": 2,
  "stripe_session": "cs_test_xxx",
  "checkout_url": "https://checkout.stripe.com/c/pay/cs_test_xxx"
}
```

---

## 用量模块

### 上报用量

`POST /usage` **服务间接口**

上报 API 调用用量，系统自动扣减积分。此接口供内部微服务调用，使用 API Key 认证。

**请求头**：
| 头部 | 必填 | 说明 |
|------|------|------|
| X-API-Key | 是 | 服务间认证密钥（环境变量 `USAGE_API_KEY`） |

**请求**：
```json
{
  "user_id": 1,
  "units": 10,
  "request_id": "req-unique-123"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | int64 | 是 | 用户 ID |
| units | int | 是 | 使用单位数 |
| request_id | string | 否 | 幂等性 ID，防止重复扣费 |

**响应**（201）：
```json
{
  "ID": 1,
  "UserID": 1,
  "Units": 10,
  "CostPoints": 10,
  "RequestID": "req-unique-123",
  "RecordedAt": "2025-01-21T10:00:00Z"
}
```

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 403 | 用户无有效订阅或积分不足 |
| 409 | 相同 `request_id` 已提交过 |

---

### 查询用量

`GET /usage?user_id={user_id}&from={from}&to={to}` **需要认证** **仅限本人**

查询指定时间范围内的用量记录。只能查询自己的用量。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | int64 | 是 | 用户 ID |
| from | RFC3339 | 否 | 开始时间 |
| to | RFC3339 | 否 | 结束时间 |

> 若不传 `from` 和 `to`，默认返回最近 30 天的记录。

**响应**（200）：
```json
[
  {
    "ID": 1,
    "UserID": 1,
    "Units": 10,
    "CostPoints": 10,
    "RequestID": "req-unique-123",
    "RecordedAt": "2025-01-21T10:00:00Z"
  }
]
```

---

## 订单模块

### 查询订单

`GET /orders/{id}` **需要认证** **仅限本人**

查询订单详情，可用于确认支付状态。只能查询自己的订单。

**响应**（200）：
```json
{
  "ID": 1,
  "UserID": 1,
  "OrderType": "subscription",
  "Status": "paid",
  "AmountCents": 999,
  "Points": 200,
  "SubscriptionID": 1,
  "StripeSessionID": "cs_test_xxx",
  "StripePaymentIntentID": "pi_xxx",
  "StripeSubscriptionID": "sub_xxx",
  "CreatedAt": "2025-01-21T10:00:00Z",
  "UpdatedAt": "2025-01-21T10:05:00Z"
}
```

**订单类型**：
| 类型 | 说明 |
|------|------|
| `subscription` | 订阅付款 |
| `prepaid` | 预充值付款 |

**订单状态值**：
| 状态 | 说明 |
|------|------|
| `pending` | 待支付 |
| `paid` | 已支付 |
| `failed` | 支付失败 |

---

## Webhook（仅后端）

### Stripe Webhook

`POST /webhooks/stripe`

接收 Stripe 支付事件回调，由后端自动处理。

**处理的事件类型**：
- `checkout.session.completed` - 支付完成
- `invoice.paid` - 发票支付（订阅续费）

> 前端无需关心此接口，支付结果通过查询订单状态获取。

---

## 管理员模块

以下接口仅管理员角色可访问。

### 列出所有用户

`GET /admin/users` **仅限管理员**

分页列出所有用户，支持按系统标识筛选，并可同时查询用户积分余额。

**查询参数**：
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| page | int | 否 | 页码，默认 1 |
| page_size | int | 否 | 每页数量，默认 20，最大 100 |
| system_code | string | 否 | 按系统标识（租户）筛选 |
| include_balances | bool | 否 | 是否包含积分余额信息，设为 `true` 时返回余额详情 |

**响应**（200）：

基础响应（不含余额）：
```json
{
  "users": [
    {
      "ID": 1,
      "SystemCode": "demo",
      "Email": "user@example.com",
      "Status": "active",
      "Role": "user",
      "CreatedAt": "2025-01-21T10:00:00Z",
      "UpdatedAt": "2025-01-21T10:00:00Z",
      "total_balance": 0
    }
  ],
  "total": 150,
  "page": 1,
  "page_size": 20
}
```

包含余额的响应（`include_balances=true`）：
```json
{
  "users": [
    {
      "ID": 1,
      "SystemCode": "demo",
      "Email": "user@example.com",
      "Status": "active",
      "Role": "user",
      "CreatedAt": "2025-01-21T10:00:00Z",
      "UpdatedAt": "2025-01-21T10:00:00Z",
      "total_balance": 155,
      "balance_buckets": [
        {
          "ID": 1,
          "UserID": 1,
          "BucketType": "free",
          "TotalPoints": 10,
          "RemainingPoints": 5,
          "ExpiresAt": null,
          "CreatedAt": "2025-01-21T10:00:00Z",
          "UpdatedAt": "2025-01-21T10:00:00Z"
        },
        {
          "ID": 2,
          "UserID": 1,
          "BucketType": "subscription",
          "TotalPoints": 200,
          "RemainingPoints": 150,
          "ExpiresAt": "2025-02-21T10:00:00Z",
          "CreatedAt": "2025-01-21T10:00:00Z",
          "UpdatedAt": "2025-01-21T10:00:00Z"
        }
      ]
    }
  ],
  "total": 150,
  "page": 1,
  "page_size": 20
}
```

| 字段 | 说明 |
|------|------|
| total_balance | 用户当前可用的总剩余积分（仅统计未过期的积分桶） |
| balance_buckets | 详细的积分桶列表（仅当 `include_balances=true` 时返回） |

**使用示例**：

```bash
# 列出所有用户
curl -X GET "http://localhost:8080/admin/users" \
  -H "Authorization: Bearer <admin_token>"

# 按 system_code 筛选并包含余额
curl -X GET "http://localhost:8080/admin/users?system_code=demo&include_balances=true" \
  -H "Authorization: Bearer <admin_token>"
```
```

---

### 更新用户角色

`PATCH /admin/users/{id}/role` **仅限管理员**

更新指定用户的角色。

**请求**：
```json
{
  "role": "admin"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| role | string | 是 | 用户角色（`user` 或 `admin`） |

**响应**（200）：
```json
{"status": "ok"}
```

---

### 查询用户用量

`GET /admin/users/{id}/usage` **仅限管理员**

查询指定用户的用量记录。

**查询参数**：
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| from | RFC3339 | 否 | 开始时间 |
| to | RFC3339 | 否 | 结束时间 |

> 若不传 `from` 和 `to`，默认返回最近 30 天的记录。

**响应**（200）：与用量查询接口相同。

---

### 查询用户订阅

`GET /admin/users/{id}/subscriptions` **仅限管理员**

查询指定用户的所有订阅记录。

**响应**（200）：
```json
[
  {
    "ID": 1,
    "UserID": 1,
    "PlanID": 1,
    "Status": "active",
    "StartedAt": "2025-01-21T10:00:00Z",
    "EndsAt": "2025-02-20T10:00:00Z",
    "StripeSubscriptionID": "sub_xxx",
    "CreatedAt": "2025-01-21T10:00:00Z",
    "UpdatedAt": "2025-01-21T10:00:00Z"
  }
]
```

---

### 查询用户余额（管理员）

`GET /admin/users/{id}/balances` **仅限管理员**

查询指定用户的积分余额。

**路径参数**：
| 参数 | 类型 | 说明 |
|------|------|------|
| id | int64 | 用户 ID |

**响应**（200）：
```json
[
  {
    "ID": 1,
    "UserID": 1,
    "BucketType": "free",
    "TotalPoints": 10,
    "RemainingPoints": 5,
    "ExpiresAt": null,
    "CreatedAt": "2025-01-21T10:00:00Z",
    "UpdatedAt": "2025-01-21T10:00:00Z"
  },
  {
    "ID": 2,
    "UserID": 1,
    "BucketType": "subscription",
    "TotalPoints": 200,
    "RemainingPoints": 150,
    "ExpiresAt": "2025-02-21T10:00:00Z",
    "CreatedAt": "2025-01-21T10:00:00Z",
    "UpdatedAt": "2025-01-21T10:00:00Z"
  }
]
```

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 403 | 非管理员访问 |
| 404 | 用户不存在 |

---

### 系统统计

`GET /admin/stats` **仅限管理员**

获取系统统计数据。

**查询参数**：
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| from | RFC3339 | 否 | 开始时间 |
| to | RFC3339 | 否 | 结束时间 |

> 若不传 `from` 和 `to`，默认统计最近 30 天。

**响应**（200）：
```json
{
  "total_users": 1500,
  "active_subscriptions": 230,
  "total_revenue_cents": 5000000,
  "period_revenue_cents": 150000,
  "new_users_in_period": 45
}
```

| 字段 | 说明 |
|------|------|
| total_users | 系统总用户数 |
| active_subscriptions | 当前活跃订阅数 |
| total_revenue_cents | 历史总收入（美分） |
| period_revenue_cents | 指定时段收入（美分） |
| new_users_in_period | 时段内新增用户数 |

---

## 内部服务接口

以下接口供内部微服务调用，使用 `X-API-Key` 头部认证（环境变量 `USAGE_API_KEY`）。

### 查询用户余额（内部服务）

`GET /internal/users/{id}/balances` **服务间接口**

查询指定用户的积分余额。供内部服务调用，用于在扣费前检查用户余额等场景。

**请求头**：
| 头部 | 必填 | 说明 |
|------|------|------|
| X-API-Key | 是 | 服务间认证密钥（环境变量 `USAGE_API_KEY`） |

**路径参数**：
| 参数 | 类型 | 说明 |
|------|------|------|
| id | int64 | 用户 ID |

**响应**（200）：
```json
[
  {
    "ID": 1,
    "UserID": 1,
    "BucketType": "free",
    "TotalPoints": 10,
    "RemainingPoints": 5,
    "ExpiresAt": null,
    "CreatedAt": "2025-01-21T10:00:00Z",
    "UpdatedAt": "2025-01-21T10:00:00Z"
  },
  {
    "ID": 2,
    "UserID": 1,
    "BucketType": "subscription",
    "TotalPoints": 200,
    "RemainingPoints": 150,
    "ExpiresAt": "2025-02-21T10:00:00Z",
    "CreatedAt": "2025-01-21T10:00:00Z",
    "UpdatedAt": "2025-01-21T10:00:00Z"
  }
]
```

**积分桶类型**：
| 类型 | 说明 |
|------|------|
| `free` | 免费注册赠送积分，无过期时间 |
| `subscription` | 订阅发放积分，周期内有效 |
| `prepaid` | 预充值购买积分，有过期时间 |

**错误情况**：
| 状态码 | 场景 |
|--------|------|
| 401 | X-API-Key 缺失或无效 |
| 404 | 用户不存在 |
| 503 | 服务间 API Key 未配置 |

**使用示例**：
```bash
curl -X GET "http://localhost:8080/internal/users/1/balances" \
  -H "X-API-Key: your_usage_api_key"
```

---

## 前端集成指南

### 典型用户流程

```
注册/登录 → 获取Token → 选择订阅计划 → 支付 → 使用 API → 查看余额/用量
```

### 认证流程

#### 方式一：邮箱密码登录

```
┌─────────────────────────────────────────────────────────────────┐
│  1. 用户注册：POST /users                                        │
│                              ↓                                  │
│  2. 用户登录：POST /auth/login                                   │
│                              ↓                                  │
│  3. 保存返回的 token                                             │
│                              ↓                                  │
│  4. 后续请求携带 Authorization: Bearer <token>                   │
│                              ↓                                  │
│  5. Token 过期后重新登录                                          │
└─────────────────────────────────────────────────────────────────┘
```

#### 方式二：Google OAuth 登录

```
┌─────────────────────────────────────────────────────────────────┐
│  1. 用户点击「使用 Google 登录」                                   │
│                              ↓                                  │
│  2. 前端跳转到 GET /auth/google                                  │
│                              ↓                                  │
│  3. 用户在 Google 页面授权                                        │
│                              ↓                                  │
│  4. Google 回调 GET /auth/google/callback                        │
│                              ↓                                  │
│  5. 后端返回 JWT Token（首次登录自动创建用户）                       │
│                              ↓                                  │
│  6. 前端保存 token，后续请求携带 Authorization: Bearer <token>     │
└─────────────────────────────────────────────────────────────────┘
```

### 支付流程（订阅 / 预充值通用）

```
┌─────────────────────────────────────────────────────────────────┐
│  1. 前端调用 POST /subscriptions/checkout 或 /prepaid/checkout  │
│                              ↓                                  │
│  2. 获取响应中的 order_id 和 checkout_url                        │
│                              ↓                                  │
│  3. 重定向用户到 checkout_url（或新窗口打开）                      │
│                              ↓                                  │
│  4. 用户在 Stripe 页面完成支付                                   │
│                              ↓                                  │
│  5. 支付完成后，用户被重定向回 success_url                        │
│                              ↓                                  │
│  6. 前端调用 GET /orders/{order_id} 确认订单状态                  │
│                              ↓                                  │
│  7. 若 status = "paid"，支付成功；否则继续轮询                    │
└─────────────────────────────────────────────────────────────────┘
```

### 前端示例代码

```javascript
// Token 管理
function getToken() {
  return localStorage.getItem('jwt_token');
}

function setToken(token) {
  localStorage.setItem('jwt_token', token);
}

function getAuthHeaders() {
  const token = getToken();
  return token ? { 'Authorization': `Bearer ${token}` } : {};
}

// 0a. 邮箱密码登录
async function login(systemCode, email, password) {
  const response = await fetch('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ system_code: systemCode, email, password })
  });
  
  if (!response.ok) {
    throw new Error('登录失败');
  }
  
  const data = await response.json();
  setToken(data.token);
  return data.user;
}

// 0b. Google OAuth 登录
function loginWithGoogle() {
  // 直接跳转到 Google 授权页面
  window.location.href = '/auth/google?system_code=demo';
}

// 0c. Google 登录回调处理（在回调页面调用）
// 注意：回调 URL 会直接返回 JSON，前端需要处理响应
async function handleGoogleCallback() {
  // 如果使用弹窗方式，可以用以下方法
  // 但推荐直接跳转方式，回调页面处理 JSON 响应
  
  const urlParams = new URLSearchParams(window.location.search);
  const code = urlParams.get('code');
  const state = urlParams.get('state');
  
  if (code && state) {
    // 回调会自动处理，响应为 JSON
    // 前端需要从响应中提取 token
    const response = await fetch(`/auth/google/callback${window.location.search}`, {
      credentials: 'include'  // 携带 cookie 用于 state 验证
    });
    
    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Google 登录失败');
    }
    
    const data = await response.json();
    setToken(data.token);
    
    if (data.is_new_user) {
      console.log('欢迎新用户！');
    }
    
    return data.user;
  }
}

// 1. 创建订阅支付
async function createSubscription(userId, planId) {
  const response = await fetch('/subscriptions/checkout', {
    method: 'POST',
    headers: { 
      'Content-Type': 'application/json',
      ...getAuthHeaders()
    },
    body: JSON.stringify({
      user_id: userId,
      plan_id: planId,
      success_url: `${window.location.origin}/payment/success?order_id={order_id}`,
      cancel_url: `${window.location.origin}/payment/cancel`
    })
  });
  
  if (response.status === 401) {
    // Token 过期，需要重新登录
    window.location.href = '/login';
    return;
  }
  
  const data = await response.json();
  
  // 保存 order_id 用于后续查询
  localStorage.setItem('pending_order_id', data.order_id);
  
  // 跳转到 Stripe 支付页面
  window.location.href = data.checkout_url;
}

// 2. 支付成功页面：确认订单状态
async function confirmPayment(orderId) {
  const maxAttempts = 15;
  const interval = 2000; // 2秒
  
  for (let i = 0; i < maxAttempts; i++) {
    const response = await fetch(`/orders/${orderId}`, {
      headers: getAuthHeaders()
    });
    const order = await response.json();
    
    if (order.Status === 'paid') {
      // 支付成功
      return { success: true, order };
    }
    
    if (order.Status === 'failed') {
      // 支付失败
      return { success: false, order };
    }
    
    // 等待后重试
    await new Promise(resolve => setTimeout(resolve, interval));
  }
  
  // 超时，建议用户稍后刷新
  return { success: false, timeout: true };
}

// 3. 查询用户余额
async function getUserBalance(userId) {
  const response = await fetch(`/users/${userId}/balances`, {
    headers: getAuthHeaders()
  });
  
  if (response.status === 401) {
    window.location.href = '/login';
    return 0;
  }
  
  const balances = await response.json();
  
  // 计算总剩余积分
  const totalRemaining = balances.reduce((sum, b) => sum + b.RemainingPoints, 0);
  return totalRemaining;
}
```

### 重要注意事项

1. **不要仅凭跳转判断支付结果**
   - 用户可能手动访问 `success_url`
   - 必须调用 `GET /orders/{id}` 确认 `status` 为 `paid`

2. **建议的轮询策略**
   - 间隔：2 秒
   - 最大次数：15 次（共 30 秒）
   - 超时后提示用户刷新页面

3. **处理用户取消**
   - 用户点击「返回」会跳转到 `cancel_url`
   - 此时订单状态仍为 `pending`
   - 可引导用户重新发起支付

4. **幂等性处理**
   - 上报用量时建议传入唯一的 `request_id`
   - 相同 `request_id` 不会重复扣费

5. **Google OAuth 登录**
   - 回调接口返回 JSON 响应，前端需要处理
   - 首次登录自动创建账号并赠送免费积分
   - 如果用户已用邮箱注册，会自动绑定 Google 账号
   - 需要在 [Google Cloud Console](https://console.cloud.google.com/apis/credentials) 配置 OAuth 2.0 凭据

6. **邮件验证码**
   - 验证码有效期默认 10 分钟（可配置）
   - 同一邮箱每分钟只能发送 1 次验证码
   - 找回密码时需要用户已注册

### 找回密码示例代码

```javascript
// 发送重置密码验证码
async function sendResetCode(systemCode, email) {
  const response = await fetch('/auth/send-verification-code', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      system_code: systemCode,
      email: email,
      code_type: 'reset_password'
    })
  });
  
  if (response.status === 429) {
    throw new Error('请求过于频繁，请稍后再试');
  }
  
  if (response.status === 404) {
    throw new Error('该邮箱尚未注册');
  }
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || '发送验证码失败');
  }
  
  return true;
}

// 重置密码
async function resetPassword(systemCode, email, code, newPassword) {
  const response = await fetch('/auth/reset-password', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      system_code: systemCode,
      email: email,
      code: code,
      new_password: newPassword
    })
  });
  
  if (response.status === 400) {
    throw new Error('验证码错误或已过期');
  }
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || '重置密码失败');
  }
  
  return true;
}
```

---

## 错误码速查

| HTTP 状态码 | 错误场景 |
|-------------|----------|
| 400 | 参数缺失或格式错误、验证码无效/已过期 |
| 403 | 无有效订阅或积分不足 |
| 404 | 用户/订单/订阅不存在 |
| 409 | 重复请求（如相同 request_id） |
| 429 | 请求过于频繁（验证码1分钟内限发1次） |
| 503 | Stripe/邮件服务未配置 |
