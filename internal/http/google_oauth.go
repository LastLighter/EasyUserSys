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
func (s *Server) getGoogleOAuthConfig(systemCode string) (*oauth2.Config, error) {
	googleCfg, ok := s.cfg.GoogleOAuthFor(systemCode)
	if !ok || googleCfg.ClientID == "" || googleCfg.ClientSecret == "" || googleCfg.RedirectURL == "" {
		return nil, errors.New("Google OAuth not configured")
	}
	return &oauth2.Config{
		ClientID:     googleCfg.ClientID,
		ClientSecret: googleCfg.ClientSecret,
		RedirectURL:  googleCfg.RedirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}, nil
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
	systemCode := r.URL.Query().Get("system_code")
	if systemCode == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code is required"))
		return
	}
	config, err := s.getGoogleOAuthConfig(systemCode)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
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
	// 存储 system_code，用于回调时确定租户
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_system_code",
		Value:    systemCode,
		Path:     "/",
		MaxAge:   int(10 * time.Minute / time.Second),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	url := config.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// handleGoogleCallback 处理 Google OAuth 回调
func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	// 验证 state 参数以防止 CSRF 攻击
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != state {
		respondError(w, http.StatusBadRequest, errors.New("invalid state parameter"))
		return
	}
	systemCookie, err := r.Cookie("oauth_system_code")
	if err != nil || systemCookie.Value == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code missing"))
		return
	}
	systemCode := systemCookie.Value
	config, err := s.getGoogleOAuthConfig(systemCode)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
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
	// 清除 system_code cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_system_code",
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
	token, err := config.Exchange(context.Background(), code)
	if err != nil {
		respondError(w, http.StatusInternalServerError, errors.New("failed to exchange token: "+err.Error()))
		return
	}

	// 获取用户信息
	userInfo, err := s.getGoogleUserInfo(config, token)
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
	user, isNewUser, err := s.svc.GetOrCreateUserByGoogleID(r.Context(), systemCode, userInfo.ID, userInfo.Email)
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
			"id":          user.ID,
			"system_code": user.SystemCode,
			"email":       user.Email,
			"role":        user.Role,
		},
	})
}

// getGoogleUserInfo 使用访问令牌获取 Google 用户信息
func (s *Server) getGoogleUserInfo(config *oauth2.Config, token *oauth2.Token) (*GoogleUserInfo, error) {
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
