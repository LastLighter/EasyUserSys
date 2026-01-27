package config

import (
	"encoding/json"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL                 string
	ServerAddr                  string
	CostPerUnit                 int
	FreeSignupPoints            int
	StripeSecretKey             string
	StripeWebhookSecret         string
	StripePriceMonthly          string
	StripePriceQuarterly        string
	StripeCurrency              string
	SubscriptionMonthlyPoints   int
	SubscriptionQuarterlyPoints int
	PrepaidExpiryDays           int
	JWTSecretKey                string
	JWTExpiryHours              int
	UsageAPIKey                 string
	// Google OAuth 配置（支持多应用）
	GoogleOAuthConfigs map[string]GoogleOAuthConfig
	// 兼容旧配置
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	// Resend 邮件服务配置（API Key 和过期时间共享，发件人支持多应用）
	ResendAPIKey                  string
	ResendFromEmail               string                       // 兼容旧配置（单应用）
	ResendEmailConfigs            map[string]ResendEmailConfig // 多应用配置
	VerificationCodeExpiryMinutes int
}

type GoogleOAuthConfig struct {
	ClientID            string `json:"client_id"`
	ClientSecret        string `json:"client_secret"`
	RedirectURL         string `json:"redirect_url"`
	FrontendCallbackURL string `json:"frontend_callback_url"` // 前端回调地址，OAuth 成功后重定向到此地址
}

type ResendEmailConfig struct {
	FromEmail string `json:"from_email"`
}

func Load() Config {
	googleConfigs := parseGoogleOAuthConfigs(env("GOOGLE_OAUTH_CONFIGS", ""))
	legacyGoogle := GoogleOAuthConfig{
		ClientID:     env("GOOGLE_CLIENT_ID", ""),
		ClientSecret: env("GOOGLE_CLIENT_SECRET", ""),
		RedirectURL:  env("GOOGLE_REDIRECT_URL", ""),
	}
	if len(googleConfigs) == 0 && legacyGoogle.ClientID != "" && legacyGoogle.ClientSecret != "" && legacyGoogle.RedirectURL != "" {
		googleConfigs = map[string]GoogleOAuthConfig{
			"default": legacyGoogle,
		}
	}

	// 解析多应用邮件配置
	resendEmailConfigs := parseResendEmailConfigs(env("RESEND_EMAIL_CONFIGS", ""))
	legacyFromEmail := env("RESEND_FROM_EMAIL", "")
	if len(resendEmailConfigs) == 0 && legacyFromEmail != "" {
		resendEmailConfigs = map[string]ResendEmailConfig{
			"default": {FromEmail: legacyFromEmail},
		}
	}

	return Config{
		DatabaseURL:                   env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/easyusersys?sslmode=disable"),
		ServerAddr:                    env("SERVER_ADDR", ":8080"),
		CostPerUnit:                   envInt("COST_PER_UNIT", 1),
		FreeSignupPoints:              envInt("FREE_SIGNUP_POINTS", 10),
		StripeSecretKey:               env("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:           env("STRIPE_WEBHOOK_SECRET", ""),
		StripePriceMonthly:            env("STRIPE_PRICE_MONTHLY", ""),
		StripePriceQuarterly:          env("STRIPE_PRICE_QUARTERLY", ""),
		StripeCurrency:                env("STRIPE_CURRENCY", "usd"),
		SubscriptionMonthlyPoints:     envInt("SUBSCRIPTION_MONTHLY_POINTS", 200),
		SubscriptionQuarterlyPoints:   envInt("SUBSCRIPTION_QUARTERLY_POINTS", 600),
		PrepaidExpiryDays:             envInt("PREPAID_EXPIRY_DAYS", 30),
		JWTSecretKey:                  env("JWT_SECRET_KEY", ""),
		JWTExpiryHours:                envInt("JWT_EXPIRY_HOURS", 168),
		UsageAPIKey:                   env("USAGE_API_KEY", ""),
		GoogleOAuthConfigs:            googleConfigs,
		GoogleClientID:                legacyGoogle.ClientID,
		GoogleClientSecret:            legacyGoogle.ClientSecret,
		GoogleRedirectURL:             legacyGoogle.RedirectURL,
		ResendAPIKey:                  env("RESEND_API_KEY", ""),
		ResendFromEmail:               legacyFromEmail,
		ResendEmailConfigs:            resendEmailConfigs,
		VerificationCodeExpiryMinutes: envInt("VERIFICATION_CODE_EXPIRY_MINUTES", 10),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return def
}

func parseGoogleOAuthConfigs(raw string) map[string]GoogleOAuthConfig {
	if raw == "" {
		return nil
	}
	var parsed map[string]GoogleOAuthConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	return parsed
}

func parseResendEmailConfigs(raw string) map[string]ResendEmailConfig {
	if raw == "" {
		return nil
	}
	var parsed map[string]ResendEmailConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	return parsed
}

func (c Config) PrepaidExpiry() time.Duration {
	return time.Duration(c.PrepaidExpiryDays) * 24 * time.Hour
}

func (c Config) VerificationCodeExpiry() time.Duration {
	return time.Duration(c.VerificationCodeExpiryMinutes) * time.Minute
}

func (c Config) GoogleOAuthFor(systemCode string) (GoogleOAuthConfig, bool) {
	if systemCode != "" {
		if cfg, ok := c.GoogleOAuthConfigs[systemCode]; ok {
			return cfg, true
		}
	}
	if cfg, ok := c.GoogleOAuthConfigs["default"]; ok {
		return cfg, true
	}
	return GoogleOAuthConfig{}, false
}

func (c Config) ResendEmailFor(systemCode string) (ResendEmailConfig, bool) {
	if systemCode != "" {
		if cfg, ok := c.ResendEmailConfigs[systemCode]; ok {
			return cfg, true
		}
	}
	if cfg, ok := c.ResendEmailConfigs["default"]; ok {
		return cfg, true
	}
	return ResendEmailConfig{}, false
}
