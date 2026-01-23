package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"easyusersys/internal/config"
	"easyusersys/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound              = errors.New("not found")
	ErrInsufficientPoints    = errors.New("insufficient points")
	ErrSubscriptionRequired  = errors.New("active subscription required")
	ErrDuplicateRequest      = errors.New("duplicate request")
	ErrInvalidRequest        = errors.New("invalid request")
	ErrStripeNotConfigured   = errors.New("stripe not configured")
	ErrSubscriptionNotActive = errors.New("subscription not active")
	ErrInvalidCredentials    = errors.New("invalid email or password")
	ErrUnauthorized          = errors.New("unauthorized")
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidCode           = errors.New("invalid or expired verification code")
	ErrCodeAlreadyUsed       = errors.New("verification code already used")
	ErrTooManyRequests       = errors.New("too many requests, please try again later")
)

type Service struct {
	pool   *pgxpool.Pool
	config config.Config
}

func New(pool *pgxpool.Pool, cfg config.Config) *Service {
	return &Service{pool: pool, config: cfg}
}

func (s *Service) EnsureDefaultPlans(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plans (name, period_days, price_cents, grant_points, active)
		VALUES
			('monthly', 30, 2000, $1, true),
			('quarterly', 90, 5400, $2, true)
		ON CONFLICT (name)
		DO UPDATE SET period_days = EXCLUDED.period_days,
			price_cents = EXCLUDED.price_cents,
			grant_points = EXCLUDED.grant_points,
			active = EXCLUDED.active`, s.config.SubscriptionMonthlyPoints, s.config.SubscriptionQuarterlyPoints)
	return err
}

func (s *Service) CreateUser(ctx context.Context, systemCode, email, password string) (models.User, error) {
	if systemCode == "" || email == "" || password == "" {
		return models.User{}, ErrInvalidRequest
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return models.User{}, err
	}
	var user models.User
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (system_code, email, password_hash, status, role)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, system_code, email, password_hash, google_id, status, role, created_at, updated_at`,
		systemCode, email, string(passwordHash), models.UserStatusActive, models.UserRoleUser,
	).Scan(&user.ID, &user.SystemCode, &user.Email, &user.PasswordHash, &user.GoogleID, &user.Status, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return models.User{}, err
	}
	if s.config.FreeSignupPoints > 0 {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO balance_buckets (user_id, bucket_type, total_points, remaining_points)
			VALUES ($1, $2, $3, $3)`, user.ID, models.BucketFree, s.config.FreeSignupPoints)
		if err != nil {
			return models.User{}, err
		}
		_, err = s.pool.Exec(ctx, `
			INSERT INTO billing_ledger (user_id, delta_points, reason, reference_type)
			VALUES ($1, $2, $3, $4)`,
			user.ID, s.config.FreeSignupPoints, "signup_bonus", "user")
		if err != nil {
			return models.User{}, err
		}
	}
	return user, nil
}

func (s *Service) GetUserByID(ctx context.Context, id int64) (models.User, error) {
	var user models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, system_code, email, password_hash, google_id, status, role, created_at, updated_at
		FROM users WHERE id = $1`, id,
	).Scan(&user.ID, &user.SystemCode, &user.Email, &user.PasswordHash, &user.GoogleID, &user.Status, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, ErrNotFound
	}
	return user, err
}

func (s *Service) GetUserByEmail(ctx context.Context, systemCode, email string) (models.User, error) {
	var user models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, system_code, email, password_hash, google_id, status, role, created_at, updated_at
		FROM users WHERE system_code = $1 AND email = $2`, systemCode, email,
	).Scan(&user.ID, &user.SystemCode, &user.Email, &user.PasswordHash, &user.GoogleID, &user.Status, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, ErrNotFound
	}
	return user, err
}

func (s *Service) UpdateUserStatus(ctx context.Context, id int64, status string) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE users SET status = $1, updated_at = NOW()
		WHERE id = $2`, status, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) CreateAPIKey(ctx context.Context, userID int64) (string, models.APIKey, error) {
	if userID == 0 {
		return "", models.APIKey{}, ErrInvalidRequest
	}
	raw, prefix, hash, err := generateKey()
	if err != nil {
		return "", models.APIKey{}, err
	}
	var apiKey models.APIKey
	err = s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (user_id, key_hash, key_prefix, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, key_hash, key_prefix, status, created_at, revoked_at`,
		userID, hash, prefix, models.APIKeyStatusActive,
	).Scan(&apiKey.ID, &apiKey.UserID, &apiKey.KeyHash, &apiKey.KeyPrefix, &apiKey.Status, &apiKey.CreatedAt, &apiKey.RevokedAt)
	if err != nil {
		return "", models.APIKey{}, err
	}
	return raw, apiKey, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, userID int64) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, key_hash, key_prefix, status, created_at, revoked_at
		FROM api_keys WHERE user_id = $1
		ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []models.APIKey
	for rows.Next() {
		var item models.APIKey
		if err := rows.Scan(&item.ID, &item.UserID, &item.KeyHash, &item.KeyPrefix, &item.Status, &item.CreatedAt, &item.RevokedAt); err != nil {
			return nil, err
		}
		keys = append(keys, item)
	}
	return keys, rows.Err()
}

func (s *Service) RevokeAPIKey(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET status = $1, revoked_at = NOW()
		WHERE id = $2`, models.APIKeyStatusRevoked, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ListPlans(ctx context.Context) ([]models.Plan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, period_days, price_cents, grant_points, active
		FROM plans WHERE active = true ORDER BY period_days`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plans []models.Plan
	for rows.Next() {
		var p models.Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.PeriodDays, &p.PriceCents, &p.GrantPoints, &p.Active); err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

func (s *Service) GetPlanByID(ctx context.Context, planID int64) (models.Plan, error) {
	var p models.Plan
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, period_days, price_cents, grant_points, active
		FROM plans WHERE id = $1`, planID).Scan(&p.ID, &p.Name, &p.PeriodDays, &p.PriceCents, &p.GrantPoints, &p.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Plan{}, ErrNotFound
	}
	return p, err
}

func (s *Service) CreatePendingSubscription(ctx context.Context, userID, planID int64, periodDays int) (models.Subscription, error) {
	now := time.Now().UTC()
	sub := models.Subscription{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO subscriptions (user_id, plan_id, status, started_at, ends_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, plan_id, status, started_at, ends_at, stripe_subscription_id, created_at, updated_at`,
		userID, planID, models.SubscriptionPending, now, now.Add(time.Duration(periodDays)*24*time.Hour),
	).Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.Status, &sub.StartedAt, &sub.EndsAt, &sub.StripeSubscriptionID, &sub.CreatedAt, &sub.UpdatedAt)
	return sub, err
}

func (s *Service) ActivateSubscription(ctx context.Context, subscriptionID int64, stripeSubscriptionID string, grantPoints int, periodDays int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	endsAt := now.Add(time.Duration(periodDays) * 24 * time.Hour)

	ct, err := tx.Exec(ctx, `
		UPDATE subscriptions
		SET status = $1, stripe_subscription_id = $2, started_at = $3, ends_at = $4, updated_at = NOW()
		WHERE id = $5`, models.SubscriptionActive, stripeSubscriptionID, now, endsAt, subscriptionID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}

	var bucketID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO balance_buckets (user_id, bucket_type, total_points, remaining_points, expires_at)
		SELECT user_id, $1, $2, $2, $3 FROM subscriptions WHERE id = $4
		RETURNING id`,
		models.BucketSubscription, grantPoints, endsAt, subscriptionID).Scan(&bucketID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO billing_ledger (user_id, bucket_id, delta_points, reason, reference_type, reference_id)
		SELECT user_id, $1, $2, $3, $4, id FROM subscriptions WHERE id = $5`,
		bucketID, grantPoints, "subscription_grant", "subscription", subscriptionID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) CancelSubscription(ctx context.Context, userID int64) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE subscriptions
		SET status = $1, updated_at = NOW()
		WHERE user_id = $2 AND status = $3`, models.SubscriptionCanceled, userID, models.SubscriptionActive)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrSubscriptionNotActive
	}
	return nil
}

func (s *Service) GetActiveSubscription(ctx context.Context, userID int64) (models.Subscription, error) {
	var sub models.Subscription
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, plan_id, status, started_at, ends_at, stripe_subscription_id, created_at, updated_at
		FROM subscriptions
		WHERE user_id = $1 AND status = $2 AND ends_at > NOW()
		ORDER BY id DESC LIMIT 1`, userID, models.SubscriptionActive,
	).Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.Status, &sub.StartedAt, &sub.EndsAt, &sub.StripeSubscriptionID, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Subscription{}, ErrNotFound
	}
	return sub, err
}

func (s *Service) GetSubscriptionByID(ctx context.Context, subscriptionID int64) (models.Subscription, error) {
	var sub models.Subscription
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, plan_id, status, started_at, ends_at, stripe_subscription_id, created_at, updated_at
		FROM subscriptions WHERE id = $1`, subscriptionID,
	).Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.Status, &sub.StartedAt, &sub.EndsAt, &sub.StripeSubscriptionID, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Subscription{}, ErrNotFound
	}
	return sub, err
}

func (s *Service) GetSubscriptionByStripeID(ctx context.Context, stripeSubscriptionID string) (models.Subscription, error) {
	var sub models.Subscription
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, plan_id, status, started_at, ends_at, stripe_subscription_id, created_at, updated_at
		FROM subscriptions WHERE stripe_subscription_id = $1`, stripeSubscriptionID,
	).Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.Status, &sub.StartedAt, &sub.EndsAt, &sub.StripeSubscriptionID, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Subscription{}, ErrNotFound
	}
	return sub, err
}

func (s *Service) ReportUsage(ctx context.Context, userID int64, units int, requestID string) (models.UsageRecord, error) {
	if userID == 0 || units <= 0 || requestID == "" {
		return models.UsageRecord{}, ErrInvalidRequest
	}
	costPoints := units * s.config.CostPerUnit
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.UsageRecord{}, err
	}
	defer tx.Rollback(ctx)

	var activeCount int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(1)
		FROM subscriptions
		WHERE user_id = $1 AND status = $2 AND ends_at > NOW()`,
		userID, models.SubscriptionActive,
	).Scan(&activeCount)
	if err != nil {
		return models.UsageRecord{}, err
	}
	if activeCount == 0 {
		return models.UsageRecord{}, ErrSubscriptionRequired
	}

	usage := models.UsageRecord{}
	err = tx.QueryRow(ctx, `
		INSERT INTO usage_records (user_id, units, cost_points, request_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, units, cost_points, request_id, recorded_at`,
		userID, units, costPoints, requestID).Scan(&usage.ID, &usage.UserID, &usage.Units, &usage.CostPoints, &usage.RequestID, &usage.RecordedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.UsageRecord{}, ErrDuplicateRequest
		}
		return models.UsageRecord{}, err
	}

	buckets, err := s.lockBuckets(ctx, tx, userID)
	if err != nil {
		return models.UsageRecord{}, err
	}
	remaining := costPoints
	for i := range buckets {
		if remaining == 0 {
			break
		}
		available := buckets[i].RemainingPoints
		if available == 0 {
			continue
		}
		toDeduct := minInt(available, remaining)
		remaining -= toDeduct
		newRemaining := available - toDeduct
		_, err = tx.Exec(ctx, `
			UPDATE balance_buckets
			SET remaining_points = $1, updated_at = NOW()
			WHERE id = $2`, newRemaining, buckets[i].ID)
		if err != nil {
			return models.UsageRecord{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_ledger (user_id, bucket_id, delta_points, reason, reference_type, reference_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			userID, buckets[i].ID, -toDeduct, "usage_deduction", "usage", usage.ID)
		if err != nil {
			return models.UsageRecord{}, err
		}
	}
	if remaining > 0 {
		return models.UsageRecord{}, ErrInsufficientPoints
	}
	if err := tx.Commit(ctx); err != nil {
		return models.UsageRecord{}, err
	}
	return usage, nil
}

func (s *Service) lockBuckets(ctx context.Context, tx pgx.Tx, userID int64) ([]models.BalanceBucket, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, user_id, bucket_type, total_points, remaining_points, expires_at, created_at, updated_at
		FROM balance_buckets
		WHERE user_id = $1
			AND remaining_points > 0
			AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY
			CASE bucket_type
				WHEN 'subscription' THEN 1
				WHEN 'prepaid' THEN 2
				WHEN 'free' THEN 3
				ELSE 4
			END,
			expires_at NULLS LAST,
			id
		FOR UPDATE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buckets []models.BalanceBucket
	for rows.Next() {
		var b models.BalanceBucket
		if err := rows.Scan(&b.ID, &b.UserID, &b.BucketType, &b.TotalPoints, &b.RemainingPoints, &b.ExpiresAt, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

func (s *Service) ListUsage(ctx context.Context, userID int64, from, to time.Time) ([]models.UsageRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, units, cost_points, request_id, recorded_at
		FROM usage_records
		WHERE user_id = $1 AND recorded_at >= $2 AND recorded_at <= $3
		ORDER BY recorded_at DESC`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []models.UsageRecord
	for rows.Next() {
		var r models.UsageRecord
		if err := rows.Scan(&r.ID, &r.UserID, &r.Units, &r.CostPoints, &r.RequestID, &r.RecordedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *Service) CreatePrepaidOrder(ctx context.Context, userID int64, amountCents int) (models.Order, error) {
	if userID == 0 || amountCents <= 0 {
		return models.Order{}, ErrInvalidRequest
	}
	points := amountCents / 10
	var order models.Order
	err := s.pool.QueryRow(ctx, `
		INSERT INTO orders (user_id, order_type, status, amount_cents, points)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, order_type, status, amount_cents, points, subscription_id,
			stripe_session_id, stripe_payment_intent_id, stripe_subscription_id, created_at, updated_at`,
		userID, models.OrderTypePrepaid, models.OrderStatusPending, amountCents, points,
	).Scan(&order.ID, &order.UserID, &order.OrderType, &order.Status, &order.AmountCents, &order.Points, &order.SubscriptionID, &order.StripeSessionID, &order.StripePaymentIntentID, &order.StripeSubscriptionID, &order.CreatedAt, &order.UpdatedAt)
	return order, err
}

func (s *Service) MarkOrderPaid(ctx context.Context, orderID int64, stripeSessionID, stripePaymentIntentID, stripeSubscriptionID string) (models.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.Order{}, err
	}
	defer tx.Rollback(ctx)

	var order models.Order
	err = tx.QueryRow(ctx, `
		UPDATE orders
		SET status = $1, stripe_session_id = $2, stripe_payment_intent_id = $3, stripe_subscription_id = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING id, user_id, order_type, status, amount_cents, points, subscription_id,
			stripe_session_id, stripe_payment_intent_id, stripe_subscription_id, created_at, updated_at`,
		models.OrderStatusPaid, stripeSessionID, stripePaymentIntentID, stripeSubscriptionID, orderID,
	).Scan(&order.ID, &order.UserID, &order.OrderType, &order.Status, &order.AmountCents, &order.Points, &order.SubscriptionID, &order.StripeSessionID, &order.StripePaymentIntentID, &order.StripeSubscriptionID, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return models.Order{}, err
	}

	if order.OrderType == models.OrderTypePrepaid {
		expiresAt := time.Now().UTC().Add(s.config.PrepaidExpiry())
		var bucketID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO balance_buckets (user_id, bucket_type, total_points, remaining_points, expires_at)
			VALUES ($1, $2, $3, $3, $4)
			RETURNING id`, order.UserID, models.BucketPrepaid, order.Points, expiresAt).Scan(&bucketID)
		if err != nil {
			return models.Order{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_ledger (user_id, bucket_id, delta_points, reason, reference_type, reference_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			order.UserID, bucketID, order.Points, "prepaid_grant", "order", order.ID)
		if err != nil {
			return models.Order{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return models.Order{}, err
	}
	return order, nil
}

func (s *Service) GetOrder(ctx context.Context, orderID int64) (models.Order, error) {
	var order models.Order
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, order_type, status, amount_cents, points, subscription_id,
			stripe_session_id, stripe_payment_intent_id, stripe_subscription_id, created_at, updated_at
		FROM orders WHERE id = $1`, orderID,
	).Scan(&order.ID, &order.UserID, &order.OrderType, &order.Status, &order.AmountCents, &order.Points, &order.SubscriptionID, &order.StripeSessionID, &order.StripePaymentIntentID, &order.StripeSubscriptionID, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Order{}, ErrNotFound
	}
	return order, err
}

func (s *Service) ListBalances(ctx context.Context, userID int64) ([]models.BalanceBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, bucket_type, total_points, remaining_points, expires_at, created_at, updated_at
		FROM balance_buckets
		WHERE user_id = $1
		ORDER BY bucket_type, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buckets []models.BalanceBucket
	for rows.Next() {
		var b models.BalanceBucket
		if err := rows.Scan(&b.ID, &b.UserID, &b.BucketType, &b.TotalPoints, &b.RemainingPoints, &b.ExpiresAt, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

func generateKey() (raw, prefix, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}
	raw = hex.EncodeToString(buf)
	if len(raw) < 6 {
		return "", "", "", errors.New("key too short")
	}
	prefix = raw[:6]
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, prefix, hash, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) GrantSubscriptionPoints(ctx context.Context, userID int64, points int, expiresAt time.Time, subscriptionID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO balance_buckets (user_id, bucket_type, total_points, remaining_points, expires_at)
		VALUES ($1, $2, $3, $3, $4)`, userID, models.BucketSubscription, points, expiresAt)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO billing_ledger (user_id, delta_points, reason, reference_type, reference_id)
		VALUES ($1, $2, $3, $4, $5)`,
		userID, points, "subscription_grant", "subscription", subscriptionID)
	return err
}

func (s *Service) UpdateSubscriptionFromStripe(ctx context.Context, subscriptionID int64, stripeSubscriptionID string, periodDays, grantPoints int) error {
	now := time.Now().UTC()
	endsAt := now.Add(time.Duration(periodDays) * 24 * time.Hour)
	ct, err := s.pool.Exec(ctx, `
		UPDATE subscriptions
		SET status = $1, stripe_subscription_id = $2, started_at = $3, ends_at = $4, updated_at = NOW()
		WHERE id = $5`, models.SubscriptionActive, stripeSubscriptionID, now, endsAt, subscriptionID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	userID, err := s.subscriptionUser(ctx, subscriptionID)
	if err != nil {
		return err
	}
	return s.GrantSubscriptionPoints(ctx, userID, grantPoints, endsAt, subscriptionID)
}

func (s *Service) subscriptionUser(ctx context.Context, subscriptionID int64) (int64, error) {
	var userID int64
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM subscriptions WHERE id = $1`, subscriptionID).Scan(&userID)
	if err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Service) CreateSubscriptionOrder(ctx context.Context, userID int64, subscriptionID int64, amountCents int, points int) (models.Order, error) {
	var order models.Order
	err := s.pool.QueryRow(ctx, `
		INSERT INTO orders (user_id, order_type, status, amount_cents, points, subscription_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, order_type, status, amount_cents, points, subscription_id,
			stripe_session_id, stripe_payment_intent_id, stripe_subscription_id, created_at, updated_at`,
		userID, models.OrderTypeSubscription, models.OrderStatusPending, amountCents, points, subscriptionID,
	).Scan(&order.ID, &order.UserID, &order.OrderType, &order.Status, &order.AmountCents, &order.Points, &order.SubscriptionID, &order.StripeSessionID, &order.StripePaymentIntentID, &order.StripeSubscriptionID, &order.CreatedAt, &order.UpdatedAt)
	return order, err
}

func (s *Service) GetOrderByStripeSessionID(ctx context.Context, sessionID string) (models.Order, error) {
	var order models.Order
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, order_type, status, amount_cents, points, subscription_id,
			stripe_session_id, stripe_payment_intent_id, stripe_subscription_id, created_at, updated_at
		FROM orders WHERE stripe_session_id = $1`, sessionID,
	).Scan(&order.ID, &order.UserID, &order.OrderType, &order.Status, &order.AmountCents, &order.Points, &order.SubscriptionID, &order.StripeSessionID, &order.StripePaymentIntentID, &order.StripeSubscriptionID, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Order{}, ErrNotFound
	}
	return order, err
}

func (s *Service) LinkOrderSession(ctx context.Context, orderID int64, sessionID string) error {
	ct, err := s.pool.Exec(ctx, `UPDATE orders SET stripe_session_id = $1, updated_at = NOW() WHERE id = $2`, sessionID, orderID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) StringifyPoints(points int) string {
	return fmt.Sprintf("%d", points)
}

// AuthenticateUser 验证用户凭证
func (s *Service) AuthenticateUser(ctx context.Context, systemCode, email, password string) (models.User, error) {
	if systemCode == "" || email == "" || password == "" {
		return models.User{}, ErrInvalidCredentials
	}

	user, err := s.GetUserByEmail(ctx, systemCode, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return models.User{}, ErrInvalidCredentials
		}
		return models.User{}, err
	}

	if user.Status != models.UserStatusActive {
		return models.User{}, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return models.User{}, ErrInvalidCredentials
	}

	return user, nil
}

// ListUsers 分页列出所有用户（管理员功能）
func (s *Service) ListUsers(ctx context.Context, page, pageSize int) ([]models.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, system_code, email, password_hash, google_id, status, role, created_at, updated_at
		FROM users
		ORDER BY id DESC
		LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.SystemCode, &u.Email, &u.PasswordHash, &u.GoogleID, &u.Status, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

// GetUserSubscriptions 获取用户的所有订阅记录
func (s *Service) GetUserSubscriptions(ctx context.Context, userID int64) ([]models.Subscription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, plan_id, status, started_at, ends_at, stripe_subscription_id, created_at, updated_at
		FROM subscriptions
		WHERE user_id = $1
		ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var sub models.Subscription
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.Status, &sub.StartedAt, &sub.EndsAt, &sub.StripeSubscriptionID, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// UpdateUserRole 更新用户角色
func (s *Service) UpdateUserRole(ctx context.Context, userID int64, role string) error {
	if role != models.UserRoleUser && role != models.UserRoleAdmin {
		return ErrInvalidRequest
	}

	ct, err := s.pool.Exec(ctx, `
		UPDATE users SET role = $1, updated_at = NOW()
		WHERE id = $2`, role, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Stats 系统统计数据
type Stats struct {
	TotalUsers          int64 `json:"total_users"`
	ActiveSubscriptions int64 `json:"active_subscriptions"`
	TotalRevenueCents   int64 `json:"total_revenue_cents"`
	PeriodRevenueCents  int64 `json:"period_revenue_cents"`
	NewUsersInPeriod    int64 `json:"new_users_in_period"`
}

// GetStats 获取系统统计数据
func (s *Service) GetStats(ctx context.Context, from, to time.Time) (Stats, error) {
	var stats Stats

	// 总用户数
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&stats.TotalUsers)
	if err != nil {
		return Stats{}, err
	}

	// 活跃订阅数
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM subscriptions
		WHERE status = $1 AND ends_at > NOW()`, models.SubscriptionActive).Scan(&stats.ActiveSubscriptions)
	if err != nil {
		return Stats{}, err
	}

	// 总收入（所有已支付订单）
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents), 0) FROM orders
		WHERE status = $1`, models.OrderStatusPaid).Scan(&stats.TotalRevenueCents)
	if err != nil {
		return Stats{}, err
	}

	// 指定时段收入
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents), 0) FROM orders
		WHERE status = $1 AND created_at >= $2 AND created_at <= $3`,
		models.OrderStatusPaid, from, to).Scan(&stats.PeriodRevenueCents)
	if err != nil {
		return Stats{}, err
	}

	// 时段内新增用户
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users
		WHERE created_at >= $1 AND created_at <= $2`, from, to).Scan(&stats.NewUsersInPeriod)
	if err != nil {
		return Stats{}, err
	}

	return stats, nil
}

// GetAPIKeyByID 根据 ID 获取 API Key
func (s *Service) GetAPIKeyByID(ctx context.Context, id int64) (models.APIKey, error) {
	var apiKey models.APIKey
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, key_hash, key_prefix, status, created_at, revoked_at
		FROM api_keys WHERE id = $1`, id,
	).Scan(&apiKey.ID, &apiKey.UserID, &apiKey.KeyHash, &apiKey.KeyPrefix, &apiKey.Status, &apiKey.CreatedAt, &apiKey.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.APIKey{}, ErrNotFound
	}
	return apiKey, err
}

// GetOrCreateUserByGoogleID 通过 Google ID 获取或创建用户
// 首次登录时会自动创建用户并赠送免费积分
func (s *Service) GetOrCreateUserByGoogleID(ctx context.Context, systemCode, googleID, email string) (models.User, bool, error) {
	if systemCode == "" || googleID == "" || email == "" {
		return models.User{}, false, ErrInvalidRequest
	}

	// 先尝试通过 google_id 查找用户
	var user models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, system_code, email, password_hash, google_id, status, role, created_at, updated_at
		FROM users WHERE system_code = $1 AND google_id = $2`, systemCode, googleID,
	).Scan(&user.ID, &user.SystemCode, &user.Email, &user.PasswordHash, &user.GoogleID, &user.Status, &user.Role, &user.CreatedAt, &user.UpdatedAt)

	if err == nil {
		// 用户已存在
		return user, false, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, false, err
	}

	// 检查是否有相同邮箱的用户（可能是之前用密码注册的）
	existingUser, err := s.GetUserByEmail(ctx, systemCode, email)
	if err == nil {
		// 用户存在但没有绑定 Google ID，更新绑定
		_, err = s.pool.Exec(ctx, `
			UPDATE users SET google_id = $1, updated_at = NOW()
			WHERE id = $2`, googleID, existingUser.ID)
		if err != nil {
			return models.User{}, false, err
		}
		existingUser.GoogleID = &googleID
		return existingUser, false, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return models.User{}, false, err
	}

	// 用户不存在，创建新用户
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (system_code, email, google_id, status, role)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, system_code, email, password_hash, google_id, status, role, created_at, updated_at`,
		systemCode, email, googleID, models.UserStatusActive, models.UserRoleUser,
	).Scan(&user.ID, &user.SystemCode, &user.Email, &user.PasswordHash, &user.GoogleID, &user.Status, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return models.User{}, false, err
	}

	// 赠送免费积分
	if s.config.FreeSignupPoints > 0 {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO balance_buckets (user_id, bucket_type, total_points, remaining_points)
			VALUES ($1, $2, $3, $3)`, user.ID, models.BucketFree, s.config.FreeSignupPoints)
		if err != nil {
			return models.User{}, false, err
		}
		_, err = s.pool.Exec(ctx, `
			INSERT INTO billing_ledger (user_id, delta_points, reason, reference_type)
			VALUES ($1, $2, $3, $4)`,
			user.ID, s.config.FreeSignupPoints, "signup_bonus", "user")
		if err != nil {
			return models.User{}, false, err
		}
	}

	return user, true, nil
}

// ========== 验证码相关方法 ==========

// generateVerificationCode 生成6位数字验证码
func generateVerificationCode() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// 生成6位数字验证码
	code := fmt.Sprintf("%06d", (int(buf[0])<<16|int(buf[1])<<8|int(buf[2]))%1000000)
	return code, nil
}

// CreateVerificationCode 创建验证码
// 限制：每个邮箱每分钟最多发送1次
func (s *Service) CreateVerificationCode(ctx context.Context, systemCode, email, codeType string) (string, error) {
	if systemCode == "" || email == "" || codeType == "" {
		return "", ErrInvalidRequest
	}

	// 验证 codeType
	if codeType != models.CodeTypeSignup && codeType != models.CodeTypeResetPassword {
		return "", ErrInvalidRequest
	}

	// 检查是否在1分钟内已发送过验证码（防止滥用）
	var recentCount int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM verification_codes 
		WHERE system_code = $1 AND email = $2 AND code_type = $3 
		AND created_at > NOW() - INTERVAL '1 minute'`,
		systemCode, email, codeType,
	).Scan(&recentCount)
	if err != nil {
		return "", err
	}
	if recentCount > 0 {
		return "", ErrTooManyRequests
	}

	// 生成验证码
	code, err := generateVerificationCode()
	if err != nil {
		return "", err
	}

	// 设置过期时间
	expiresAt := time.Now().UTC().Add(s.config.VerificationCodeExpiry())

	// 保存验证码
	_, err = s.pool.Exec(ctx, `
		INSERT INTO verification_codes (system_code, email, code, code_type, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		systemCode, email, code, codeType, expiresAt,
	)
	if err != nil {
		return "", err
	}

	return code, nil
}

// VerifyCode 验证验证码
func (s *Service) VerifyCode(ctx context.Context, systemCode, email, code, codeType string) error {
	if systemCode == "" || email == "" || code == "" || codeType == "" {
		return ErrInvalidRequest
	}

	// 查找最新的未使用且未过期的验证码
	var vc models.VerificationCode
	err := s.pool.QueryRow(ctx, `
		SELECT id, system_code, email, code, code_type, expires_at, verified, created_at
		FROM verification_codes
		WHERE system_code = $1 AND email = $2 AND code_type = $3 AND verified = false
		ORDER BY created_at DESC
		LIMIT 1`,
		systemCode, email, codeType,
	).Scan(&vc.ID, &vc.SystemCode, &vc.Email, &vc.Code, &vc.CodeType, &vc.ExpiresAt, &vc.Verified, &vc.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidCode
		}
		return err
	}

	// 检查是否过期
	if time.Now().UTC().After(vc.ExpiresAt) {
		return ErrInvalidCode
	}

	// 检查验证码是否匹配
	if vc.Code != code {
		return ErrInvalidCode
	}

	// 标记验证码为已使用
	_, err = s.pool.Exec(ctx, `
		UPDATE verification_codes SET verified = true WHERE id = $1`, vc.ID)
	if err != nil {
		return err
	}

	return nil
}

// ResetPassword 重置密码
// 需要先调用 VerifyCode 验证验证码
func (s *Service) ResetPassword(ctx context.Context, systemCode, email, newPassword string) error {
	if systemCode == "" || email == "" || newPassword == "" {
		return ErrInvalidRequest
	}

	// 验证用户存在
	user, err := s.GetUserByEmail(ctx, systemCode, email)
	if err != nil {
		return err
	}

	// 生成新密码的哈希
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 更新密码
	ct, err := s.pool.Exec(ctx, `
		UPDATE users SET password_hash = $1, updated_at = NOW()
		WHERE id = $2`, string(passwordHash), user.ID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// CleanupExpiredCodes 清理过期的验证码
func (s *Service) CleanupExpiredCodes(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM verification_codes 
		WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return err
}
