package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleUserInfo Google 用户信息结构
type GoogleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// getGoogleOAuthConfig 获取 Google OAuth 配置
func (s *Server) getGoogleOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.cfg.GoogleClientID,
		ClientSecret: s.cfg.GoogleClientSecret,
		RedirectURL:  s.cfg.GoogleRedirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}
}

// generateStateToken 生成随机状态令牌用于防止 CSRF 攻击
func generateStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// handleGoogleLogin 处理 Google OAuth 登录请求
// 重定向用户到 Google 授权页面
func (s *Server) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GoogleClientID == "" || s.cfg.GoogleClientSecret == "" {
		respondError(w, http.StatusServiceUnavailable, errors.New("Google OAuth not configured"))
		return
	}

	state, err := generateStateToken()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// 将 state 存储在 cookie 中，用于回调时验证
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   int(10 * time.Minute / time.Second),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	config := s.getGoogleOAuthConfig()
	url := config.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// handleGoogleCallback 处理 Google OAuth 回调
func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GoogleClientID == "" || s.cfg.GoogleClientSecret == "" {
		respondError(w, http.StatusServiceUnavailable, errors.New("Google OAuth not configured"))
		return
	}

	// 验证 state 参数以防止 CSRF 攻击
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != state {
		respondError(w, http.StatusBadRequest, errors.New("invalid state parameter"))
		return
	}

	// 清除 state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	// 检查是否有错误
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		respondError(w, http.StatusBadRequest, errors.New("OAuth error: "+errMsg))
		return
	}

	// 获取授权码
	code := r.URL.Query().Get("code")
	if code == "" {
		respondError(w, http.StatusBadRequest, errors.New("missing authorization code"))
		return
	}

	// 交换授权码获取访问令牌
	config := s.getGoogleOAuthConfig()
	token, err := config.Exchange(context.Background(), code)
	if err != nil {
		respondError(w, http.StatusInternalServerError, errors.New("failed to exchange token: "+err.Error()))
		return
	}

	// 获取用户信息
	userInfo, err := s.getGoogleUserInfo(token)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// 验证邮箱
	if !userInfo.VerifiedEmail {
		respondError(w, http.StatusBadRequest, errors.New("email not verified"))
		return
	}

	// 获取或创建用户
	user, isNewUser, err := s.svc.GetOrCreateUserByGoogleID(r.Context(), userInfo.ID, userInfo.Email)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	// 检查用户状态
	if user.Status != "active" {
		respondError(w, http.StatusForbidden, errors.New("user account is disabled"))
		return
	}

	// 生成 JWT Token
	jwtToken, err := s.generateJWT(user.ID, user.Email, user.Role)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"token":       jwtToken,
		"is_new_user": isNewUser,
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

// getGoogleUserInfo 使用访问令牌获取 Google 用户信息
func (s *Server) getGoogleUserInfo(token *oauth2.Token) (*GoogleUserInfo, error) {
	config := s.getGoogleOAuthConfig()
	client := config.Client(context.Background(), token)

	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, errors.New("failed to get user info: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed to get user info: unexpected status code")
	}

	var userInfo GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, errors.New("failed to decode user info: " + err.Error())
	}

	return &userInfo, nil
}
