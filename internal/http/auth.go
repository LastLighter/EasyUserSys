package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"easyusersys/internal/models"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	contextKeyUserID contextKey = "user_id"
	contextKeyEmail  contextKey = "email"
	contextKeyRole   contextKey = "role"
	contextKeySystem contextKey = "system_code"
)

type JWTClaims struct {
	UserID     int64  `json:"user_id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	SystemCode string `json:"system_code"`
	jwt.RegisteredClaims
}

// generateJWT 生成 JWT Token
func (s *Server) generateJWT(userID int64, email string, role string, systemCode string) (string, error) {
	if s.cfg.JWTSecretKey == "" {
		return "", errors.New("JWT secret key not configured")
	}

	expiryDuration := time.Duration(s.cfg.JWTExpiryHours) * time.Hour
	claims := JWTClaims{
		UserID:     userID,
		Email:      email,
		Role:       role,
		SystemCode: systemCode,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiryDuration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "easyusersys",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecretKey))
}

// jwtMiddleware JWT 验证中间件
func (s *Server) jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondError(w, http.StatusUnauthorized, errors.New("missing authorization header"))
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			respondError(w, http.StatusUnauthorized, errors.New("invalid authorization header format"))
			return
		}

		tokenString := parts[1]
		if s.cfg.JWTSecretKey == "" {
			respondError(w, http.StatusInternalServerError, errors.New("JWT secret key not configured"))
			return
		}

		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(s.cfg.JWTSecretKey), nil
		})

		if err != nil {
			respondError(w, http.StatusUnauthorized, errors.New("invalid or expired token"))
			return
		}

		claims, ok := token.Claims.(*JWTClaims)
		if !ok || !token.Valid {
			respondError(w, http.StatusUnauthorized, errors.New("invalid token claims"))
			return
		}

		// 将用户信息存入 context
		ctx := r.Context()
		ctx = context.WithValue(ctx, contextKeyUserID, claims.UserID)
		ctx = context.WithValue(ctx, contextKeyEmail, claims.Email)
		ctx = context.WithValue(ctx, contextKeyRole, claims.Role)
	ctx = context.WithValue(ctx, contextKeySystem, claims.SystemCode)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// adminMiddleware 管理员权限验证中间件
func (s *Server) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := getRoleFromContext(r.Context())
		if role != models.UserRoleAdmin {
			respondError(w, http.StatusForbidden, errors.New("admin access required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// getUserIDFromContext 从 context 获取当前用户 ID
func getUserIDFromContext(ctx context.Context) int64 {
	if userID, ok := ctx.Value(contextKeyUserID).(int64); ok {
		return userID
	}
	return 0
}

// getEmailFromContext 从 context 获取当前用户邮箱
func getEmailFromContext(ctx context.Context) string {
	if email, ok := ctx.Value(contextKeyEmail).(string); ok {
		return email
	}
	return ""
}

// getRoleFromContext 从 context 获取当前用户角色
func getRoleFromContext(ctx context.Context) string {
	if role, ok := ctx.Value(contextKeyRole).(string); ok {
		return role
	}
	return ""
}

// getSystemCodeFromContext 从 context 获取系统标识
func getSystemCodeFromContext(ctx context.Context) string {
	if systemCode, ok := ctx.Value(contextKeySystem).(string); ok {
		return systemCode
	}
	return ""
}

// isAdmin 检查当前用户是否为管理员
func isAdmin(ctx context.Context) bool {
	return getRoleFromContext(ctx) == models.UserRoleAdmin
}

// resolveSystemCode 获取当前用户的系统标识（优先从 JWT 读取）
func (s *Server) resolveSystemCode(ctx context.Context) (string, error) {
	if systemCode := getSystemCodeFromContext(ctx); systemCode != "" {
		return systemCode, nil
	}
	userID := getUserIDFromContext(ctx)
	if userID == 0 {
		return "", nil
	}
	return s.svc.GetUserSystemCodeByID(ctx, userID)
}

// canAccessUser 检查当前用户是否可以访问目标用户的资源
// 管理员仅允许访问同一 system_code 的用户资源
func (s *Server) canAccessUser(ctx context.Context, targetUserID int64) (bool, error) {
	if !isAdmin(ctx) {
		return getUserIDFromContext(ctx) == targetUserID, nil
	}
	targetSystemCode, err := s.svc.GetUserSystemCodeByID(ctx, targetUserID)
	if err != nil {
		return false, err
	}
	requesterSystemCode, err := s.resolveSystemCode(ctx)
	if err != nil {
		return false, err
	}
	if requesterSystemCode == "" || targetSystemCode == "" {
		return true, nil
	}
	return requesterSystemCode == targetSystemCode, nil
}
