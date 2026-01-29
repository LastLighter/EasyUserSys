package models

import "time"

type User struct {
	ID           int64
	SystemCode   string
	Email        string
	PasswordHash string  `json:"-"`
	GoogleID     *string `json:"-"` // Google OAuth 用户ID
	Status       string
	Role         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type APIKey struct {
	ID        int64
	UserID    int64
	KeyHash   string
	KeyPrefix string
	Status    string
	CreatedAt time.Time
	RevokedAt *time.Time
}

type Plan struct {
	ID          int64
	Name        string
	PeriodDays  int
	PriceCents  int
	GrantPoints float64
	Active      bool
}

type Subscription struct {
	ID                   int64
	UserID               int64
	PlanID               int64
	Status               string
	StartedAt            time.Time
	EndsAt               time.Time
	StripeSubscriptionID *string // 可能为 NULL（pending 状态时）
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type BalanceBucket struct {
	ID              int64
	UserID          int64
	BucketType      string
	TotalPoints     float64
	RemainingPoints float64
	ExpiresAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type UsageRecord struct {
	ID         int64
	UserID     int64
	Units      int
	CostPoints float64
	RequestID  string
	RecordedAt time.Time
}

type BillingLedger struct {
	ID            int64
	UserID        int64
	BucketID      *int64
	DeltaPoints   float64
	Reason        string
	ReferenceType string
	ReferenceID   *int64
	CreatedAt     time.Time
}

type Order struct {
	ID                     int64
	UserID                 int64
	OrderType              string
	Status                 string
	AmountCents            int
	Points                 float64
	SubscriptionID         *int64
	StripeSessionID        *string // 可能为 NULL（创建后才关联）
	StripePaymentIntentID  *string // 可能为 NULL（支付完成后才有）
	StripeSubscriptionID   *string // 可能为 NULL（订阅类型才有）
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

const (
	UserStatusActive              = "active"
	UserStatusDisabled            = "disabled"
	UserStatusPendingVerification = "pending_verification"
)

const (
	UserRoleUser  = "user"
	UserRoleAdmin = "admin"
)

const (
	APIKeyStatusActive  = "active"
	APIKeyStatusRevoked = "revoked"
)

const (
	BucketFree        = "free"
	BucketSubscription = "subscription"
	BucketPrepaid     = "prepaid"
)

const (
	SubscriptionActive   = "active"
	SubscriptionCanceled = "canceled"
	SubscriptionExpired  = "expired"
	SubscriptionPending  = "pending"
)

const (
	OrderTypeSubscription = "subscription"
	OrderTypePrepaid      = "prepaid"
)

const (
	OrderStatusPending = "pending"
	OrderStatusPaid    = "paid"
	OrderStatusFailed  = "failed"
)

// VerificationCode 验证码模型
type VerificationCode struct {
	ID         int64
	SystemCode string
	Email      string
	Code       string
	CodeType   string // signup | reset_password
	ExpiresAt  time.Time
	Verified   bool
	CreatedAt  time.Time
}

const (
	CodeTypeSignup        = "signup"
	CodeTypeResetPassword = "reset_password"
)
