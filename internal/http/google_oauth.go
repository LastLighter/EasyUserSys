package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

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

// oauthState OAuth state 参数结构，包含 CSRF token 和 system_code
type oauthState struct {
	CSRFToken  string `json:"csrf_token"`
	SystemCode string `json:"system_code"`
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

// generateCSRFToken 生成随机 CSRF 令牌
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// encodeOAuthState 将 state 结构编码为字符串
func encodeOAuthState(state oauthState) (string, error) {
	jsonData, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(jsonData), nil
}

// decodeOAuthState 从字符串解码 state 结构
func decodeOAuthState(encoded string) (oauthState, error) {
	var state oauthState
	jsonData, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(jsonData, &state)
	return state, err
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

	// 生成 CSRF token
	csrfToken, err := generateCSRFToken()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// 将 CSRF token 和 system_code 一起编码到 state 参数中
	// state 参数会被 Google 原样返回，避免了跨域 cookie 问题
	state := oauthState{
		CSRFToken:  csrfToken,
		SystemCode: systemCode,
	}
	encodedState, err := encodeOAuthState(state)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	url := config.AuthCodeURL(encodedState, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// handleGoogleCallback 处理 Google OAuth 回调
func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	// 从 state 参数中解码 CSRF token 和 system_code
	encodedState := r.URL.Query().Get("state")
	if encodedState == "" {
		respondError(w, http.StatusBadRequest, errors.New("missing state parameter"))
		return
	}

	state, err := decodeOAuthState(encodedState)
	if err != nil {
		respondError(w, http.StatusBadRequest, errors.New("invalid state parameter"))
		return
	}

	// 验证 state 中的数据
	if state.CSRFToken == "" || state.SystemCode == "" {
		respondError(w, http.StatusBadRequest, errors.New("invalid state parameter"))
		return
	}

	systemCode := state.SystemCode

	// 获取 Google OAuth 配置（包含前端回调地址）
	googleCfg, ok := s.cfg.GoogleOAuthFor(systemCode)
	if !ok || googleCfg.ClientID == "" || googleCfg.ClientSecret == "" || googleCfg.RedirectURL == "" {
		respondError(w, http.StatusServiceUnavailable, errors.New("Google OAuth not configured"))
		return
	}

	config := &oauth2.Config{
		ClientID:     googleCfg.ClientID,
		ClientSecret: googleCfg.ClientSecret,
		RedirectURL:  googleCfg.RedirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}

	frontendCallbackURL := googleCfg.FrontendCallbackURL

	// 辅助函数：重定向到前端并带上错误信息
	redirectWithError := func(errMsg string) {
		if frontendCallbackURL != "" {
			redirectURL := frontendCallbackURL + "?error=" + errMsg
			http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		} else {
			respondError(w, http.StatusBadRequest, errors.New(errMsg))
		}
	}

	// 检查是否有错误
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		redirectWithError("oauth_error")
		return
	}

	// 获取授权码
	code := r.URL.Query().Get("code")
	if code == "" {
		redirectWithError("missing_code")
		return
	}

	// 交换授权码获取访问令牌
	token, err := config.Exchange(context.Background(), code)
	if err != nil {
		redirectWithError("token_exchange_failed")
		return
	}

	// 获取用户信息
	userInfo, err := s.getGoogleUserInfo(config, token)
	if err != nil {
		redirectWithError("get_user_info_failed")
		return
	}

	// 验证邮箱
	if !userInfo.VerifiedEmail {
		redirectWithError("email_not_verified")
		return
	}

	// 获取或创建用户
	user, isNewUser, err := s.svc.GetOrCreateUserByGoogleID(r.Context(), systemCode, userInfo.ID, userInfo.Email)
	if err != nil {
		redirectWithError("create_user_failed")
		return
	}

	// 检查用户状态
	if user.Status != "active" {
		redirectWithError("user_disabled")
		return
	}

	// 生成 JWT Token
	jwtToken, err := s.generateJWT(user.ID, user.Email, user.Role)
	if err != nil {
		redirectWithError("token_generation_failed")
		return
	}

	// 如果配置了前端回调地址，重定向到前端
	if frontendCallbackURL != "" {
		isNewUserStr := "false"
		if isNewUser {
			isNewUserStr = "true"
		}
		redirectURL := frontendCallbackURL + "?token=" + jwtToken + "&is_new_user=" + isNewUserStr
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	// 如果没有配置前端回调地址，返回 JSON（用于测试或 API 调用）
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
