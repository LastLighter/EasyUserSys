package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"easyusersys/internal/config"
	"easyusersys/internal/email"
	"easyusersys/internal/models"
	"easyusersys/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
)

type Server struct {
	svc         *services.Service
	cfg         config.Config
	emailClient *email.ResendClient
}

func NewServer(svc *services.Service, cfg config.Config) *Server {
	emailClient := email.NewResendClient(cfg.ResendAPIKey)
	return &Server{svc: svc, cfg: cfg, emailClient: emailClient}
}

// loggingRecoverer 自定义的 panic 恢复中间件，记录详细的错误信息
func loggingRecoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				reqID := middleware.GetReqID(r.Context())
				log.Printf("[ERROR] [%s] Panic recovered in %s %s: %v\n%s",
					reqID, r.Method, r.URL.Path, rvr, debug.Stack())

				if r.Header.Get("Connection") != "Upgrade" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					errMsg := fmt.Sprintf("internal server error: %v", rvr)
					_ = json.NewEncoder(w).Encode(ErrorResponse{Error: errMsg})
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLogger 记录请求日志的中间件
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			reqID := middleware.GetReqID(r.Context())
			log.Printf("[%s] %s %s %d %s",
				reqID, r.Method, r.URL.Path, ww.Status(), time.Since(start))
		}()
		next.ServeHTTP(ww, r)
	})
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(loggingRecoverer)
	r.Use(requestLogger)
	r.Use(s.corsMiddleware)

	// 所有 API 路由都在 /api 前缀下
	r.Route("/api", func(r chi.Router) {
		// 公开接口
		r.Post("/auth/login", s.handleLogin)
		r.Get("/auth/google", s.handleGoogleLogin)
		r.Get("/auth/google/callback", s.handleGoogleCallback)
		r.Post("/auth/send-verification-code", s.handleSendVerificationCode)
		r.Post("/auth/verify-code", s.handleVerifyCode)
		r.Post("/auth/reset-password", s.handleResetPassword)
		r.Post("/users", s.handleCreateUser)
		r.Get("/users/by-email", s.handleGetUserByEmail)
		r.Get("/plans", s.handleListPlans)
		r.Post("/webhooks/stripe", s.handleStripeWebhook)

		// 服务间接口（使用 API Key 验证）
		r.Post("/usage", s.handleReportUsage)

		// 需要认证的用户接口
		r.Group(func(r chi.Router) {
			r.Use(s.jwtMiddleware)

			r.Get("/users/{id}", s.handleGetUser)
			r.Patch("/users/{id}/status", s.handleUpdateUserStatus)
			r.Get("/users/{id}/balances", s.handleListBalances)
			r.Post("/users/{id}/api-keys", s.handleCreateAPIKey)
			r.Get("/users/{id}/api-keys", s.handleListAPIKeys)
			r.Post("/api-keys/{id}/revoke", s.handleRevokeAPIKey)

			r.Post("/subscriptions/checkout", s.handleCreateSubscriptionCheckout)
			r.Post("/subscriptions/{id}/cancel", s.handleCancelSubscription)
			r.Get("/subscriptions/{id}", s.handleGetSubscription)

			r.Post("/prepaid/checkout", s.handleCreatePrepaidCheckout)

			r.Get("/usage", s.handleListUsage)

			r.Get("/orders/{id}", s.handleGetOrder)
		})

		// 管理员接口
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.jwtMiddleware)
			r.Use(s.adminMiddleware)

			r.Get("/users", s.handleAdminListUsers)
			r.Patch("/users/{id}/role", s.handleAdminUpdateUserRole)
			r.Get("/users/{id}/usage", s.handleAdminGetUserUsage)
			r.Get("/users/{id}/subscriptions", s.handleAdminGetUserSubscriptions)
			r.Get("/users/{id}/balances", s.handleAdminGetUserBalances)
			r.Get("/stats", s.handleAdminGetStats)
		})

		// 内部服务接口（使用 X-API-Key 验证）
		r.Route("/internal", func(r chi.Router) {
			r.Use(s.internalAPIKeyMiddleware)

			r.Get("/users/{id}/balances", s.handleInternalGetUserBalances)
		})
	})

	return r
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-API-Key")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type loginRequest struct {
	SystemCode string `json:"system_code"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.SystemCode == "" || req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code, email and password are required"))
		return
	}

	user, err := s.svc.AuthenticateUser(r.Context(), req.SystemCode, req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentials):
			respondError(w, http.StatusUnauthorized, err)
		case errors.Is(err, services.ErrEmailNotVerified):
			respondError(w, http.StatusForbidden, err)
		case errors.Is(err, services.ErrUserDisabled):
			respondError(w, http.StatusForbidden, err)
		default:
			s.respondServiceError(w, err)
		}
		return
	}

	token, err := s.generateJWT(user.ID, user.Email, user.Role)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]any{
			"id":          user.ID,
			"system_code": user.SystemCode,
			"email":       user.Email,
			"role":        user.Role,
		},
	})
}

type createUserRequest struct {
	SystemCode string `json:"system_code"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.SystemCode == "" || req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code, email and password are required"))
		return
	}
	user, err := s.svc.CreateUser(r.Context(), req.SystemCode, req.Email, req.Password)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, user)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	// 权限验证：只能查看自己的信息，管理员可以查看任何人
	if !canAccessUser(r.Context(), id) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	user, err := s.svc.GetUserByID(r.Context(), id)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, user)
}

func (s *Server) handleGetUserByEmail(w http.ResponseWriter, r *http.Request) {
	systemCode := r.URL.Query().Get("system_code")
	email := r.URL.Query().Get("email")
	if systemCode == "" || email == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code and email are required"))
		return
	}
	user, err := s.svc.GetUserByEmail(r.Context(), systemCode, email)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, user)
}

type updateUserStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	// 权限验证：只有管理员可以更新用户状态
	if !isAdmin(r.Context()) {
		respondError(w, http.StatusForbidden, errors.New("admin access required"))
		return
	}
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	var req updateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.Status == "" {
		respondError(w, http.StatusBadRequest, errors.New("status is required"))
		return
	}
	if err := s.svc.UpdateUserStatus(r.Context(), id, req.Status); err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListBalances(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	// 权限验证：只能查看自己的余额，管理员可以查看任何人
	if !canAccessUser(r.Context(), userID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	balances, err := s.svc.ListBalances(r.Context(), userID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, balances)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	// 权限验证：只能为自己创建 API Key，管理员可以为任何人创建
	if !canAccessUser(r.Context(), userID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	raw, key, err := s.svc.CreateAPIKey(r.Context(), userID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"raw_key": raw,
		"api_key": key,
	})
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	// 权限验证：只能查看自己的 API Keys，管理员可以查看任何人
	if !canAccessUser(r.Context(), userID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	keys, err := s.svc.ListAPIKeys(r.Context(), userID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, keys)
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	// 权限验证：检查 API Key 是否属于当前用户
	apiKey, err := s.svc.GetAPIKeyByID(r.Context(), id)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	if !canAccessUser(r.Context(), apiKey.UserID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	if err := s.svc.RevokeAPIKey(r.Context(), id); err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.svc.ListPlans(r.Context())
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, plans)
}

type createSubscriptionCheckoutRequest struct {
	UserID     int64  `json:"user_id"`
	PlanID     int64  `json:"plan_id"`
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
}

func (s *Server) handleCreateSubscriptionCheckout(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	log.Printf("[INFO] [%s] Starting subscription checkout", reqID)

	if s.cfg.StripeSecretKey == "" {
		log.Printf("[ERROR] [%s] Stripe not configured", reqID)
		s.respondServiceErrorWithContext(w, r, services.ErrStripeNotConfigured, "stripe_not_configured")
		return
	}
	var req createSubscriptionCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] [%s] Failed to decode request: %v", reqID, err)
		respondErrorWithLog(w, r, http.StatusBadRequest, err, "decode_request")
		return
	}
	log.Printf("[INFO] [%s] Checkout request: user_id=%d, plan_id=%d", reqID, req.UserID, req.PlanID)

	if req.UserID == 0 || req.PlanID == 0 || req.SuccessURL == "" || req.CancelURL == "" {
		respondErrorWithLog(w, r, http.StatusBadRequest, errors.New("user_id, plan_id, success_url, cancel_url are required"), "validation")
		return
	}
	// 权限验证：只能为自己创建订阅，管理员可以为任何人创建
	if !canAccessUser(r.Context(), req.UserID) {
		respondErrorWithLog(w, r, http.StatusForbidden, errors.New("access denied"), "access_denied")
		return
	}

	plan, err := s.svc.GetPlanByID(r.Context(), req.PlanID)
	if err != nil {
		log.Printf("[ERROR] [%s] Failed to get plan %d: %v", reqID, req.PlanID, err)
		s.respondServiceErrorWithContext(w, r, err, fmt.Sprintf("get_plan_%d", req.PlanID))
		return
	}
	log.Printf("[INFO] [%s] Found plan: name=%s, price=%d cents", reqID, plan.Name, plan.PriceCents)

	priceID, err := s.stripePriceForPlan(plan.Name)
	if err != nil {
		log.Printf("[ERROR] [%s] Failed to get stripe price for plan %s: %v", reqID, plan.Name, err)
		respondErrorWithLog(w, r, http.StatusBadRequest, err, fmt.Sprintf("stripe_price_for_%s", plan.Name))
		return
	}
	log.Printf("[INFO] [%s] Stripe price ID: %s", reqID, priceID)

	sub, err := s.svc.CreatePendingSubscription(r.Context(), req.UserID, plan.ID, plan.PeriodDays)
	if err != nil {
		log.Printf("[ERROR] [%s] Failed to create pending subscription: %v", reqID, err)
		s.respondServiceErrorWithContext(w, r, err, "create_pending_subscription")
		return
	}
	log.Printf("[INFO] [%s] Created pending subscription: id=%d", reqID, sub.ID)

	order, err := s.svc.CreateSubscriptionOrder(r.Context(), req.UserID, sub.ID, plan.PriceCents, plan.GrantPoints)
	if err != nil {
		log.Printf("[ERROR] [%s] Failed to create subscription order: %v", reqID, err)
		s.respondServiceErrorWithContext(w, r, err, "create_subscription_order")
		return
	}
	log.Printf("[INFO] [%s] Created order: id=%d", reqID, order.ID)

	stripe.Key = s.cfg.StripeSecretKey
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL:        stripe.String(req.SuccessURL),
		CancelURL:         stripe.String(req.CancelURL),
		ClientReferenceID: stripe.String(strconv.FormatInt(order.ID, 10)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Metadata: map[string]string{
			"order_id":        strconv.FormatInt(order.ID, 10),
			"subscription_id": strconv.FormatInt(sub.ID, 10),
			"user_id":         strconv.FormatInt(req.UserID, 10),
			"plan_id":         strconv.FormatInt(plan.ID, 10),
		},
	}

	log.Printf("[INFO] [%s] Creating Stripe checkout session...", reqID)
	sess, err := session.New(params)
	if err != nil {
		// 详细记录 Stripe 错误
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) {
			log.Printf("[ERROR] [%s] Stripe API error: type=%s, code=%s, message=%s, param=%s",
				reqID, stripeErr.Type, stripeErr.Code, stripeErr.Msg, stripeErr.Param)
			respondErrorWithLog(w, r, http.StatusBadRequest,
				fmt.Errorf("stripe error: %s - %s", stripeErr.Code, stripeErr.Msg), "stripe_api")
		} else {
			log.Printf("[ERROR] [%s] Failed to create Stripe session: %v", reqID, err)
			respondErrorWithLog(w, r, http.StatusInternalServerError, err, "stripe_session_create")
		}
		return
	}
	log.Printf("[INFO] [%s] Stripe session created: id=%s", reqID, sess.ID)

	if err := s.svc.LinkOrderSession(r.Context(), order.ID, sess.ID); err != nil {
		log.Printf("[ERROR] [%s] Failed to link order session: %v", reqID, err)
		s.respondServiceErrorWithContext(w, r, err, "link_order_session")
		return
	}
	log.Printf("[INFO] [%s] Checkout session completed successfully", reqID)

	respondJSON(w, http.StatusCreated, map[string]any{
		"order_id":        order.ID,
		"subscription_id": sub.ID,
		"stripe_session":  sess.ID,
		"checkout_url":    sess.URL,
	})
}

func (s *Server) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	subscriptionID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	sub, err := s.svc.GetSubscriptionByID(r.Context(), subscriptionID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	// 权限验证：只能取消自己的订阅，管理员可以取消任何人的
	if !canAccessUser(r.Context(), sub.UserID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	if err := s.svc.CancelSubscription(r.Context(), sub.UserID); err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	subscriptionID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	sub, err := s.svc.GetSubscriptionByID(r.Context(), subscriptionID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	// 权限验证：只能查看自己的订阅，管理员可以查看任何人的
	if !canAccessUser(r.Context(), sub.UserID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	respondJSON(w, http.StatusOK, sub)
}

type createPrepaidCheckoutRequest struct {
	UserID     int64  `json:"user_id"`
	AmountCents int   `json:"amount_cents"`
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
}

func (s *Server) handleCreatePrepaidCheckout(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	log.Printf("[INFO] [%s] Starting prepaid checkout", reqID)

	if s.cfg.StripeSecretKey == "" {
		log.Printf("[ERROR] [%s] Stripe not configured", reqID)
		s.respondServiceErrorWithContext(w, r, services.ErrStripeNotConfigured, "stripe_not_configured")
		return
	}
	var req createPrepaidCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] [%s] Failed to decode request: %v", reqID, err)
		respondErrorWithLog(w, r, http.StatusBadRequest, err, "decode_request")
		return
	}
	log.Printf("[INFO] [%s] Prepaid request: user_id=%d, amount=%d cents", reqID, req.UserID, req.AmountCents)

	if req.UserID == 0 || req.AmountCents <= 0 || req.SuccessURL == "" || req.CancelURL == "" {
		respondErrorWithLog(w, r, http.StatusBadRequest, errors.New("user_id, amount_cents, success_url, cancel_url are required"), "validation")
		return
	}
	// 权限验证：只能为自己充值，管理员可以为任何人充值
	if !canAccessUser(r.Context(), req.UserID) {
		respondErrorWithLog(w, r, http.StatusForbidden, errors.New("access denied"), "access_denied")
		return
	}

	order, err := s.svc.CreatePrepaidOrder(r.Context(), req.UserID, req.AmountCents)
	if err != nil {
		log.Printf("[ERROR] [%s] Failed to create prepaid order: %v", reqID, err)
		s.respondServiceErrorWithContext(w, r, err, "create_prepaid_order")
		return
	}
	log.Printf("[INFO] [%s] Created prepaid order: id=%d", reqID, order.ID)

	stripe.Key = s.cfg.StripeSecretKey
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:        stripe.String(req.SuccessURL),
		CancelURL:         stripe.String(req.CancelURL),
		ClientReferenceID: stripe.String(strconv.FormatInt(order.ID, 10)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency:   stripe.String(s.cfg.StripeCurrency),
					UnitAmount: stripe.Int64(int64(req.AmountCents)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String("Prepaid Points"),
					},
				},
				Quantity: stripe.Int64(1),
			},
		},
		Metadata: map[string]string{
			"order_id": strconv.FormatInt(order.ID, 10),
			"user_id":  strconv.FormatInt(req.UserID, 10),
		},
	}

	log.Printf("[INFO] [%s] Creating Stripe checkout session...", reqID)
	sess, err := session.New(params)
	if err != nil {
		// 详细记录 Stripe 错误
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) {
			log.Printf("[ERROR] [%s] Stripe API error: type=%s, code=%s, message=%s, param=%s",
				reqID, stripeErr.Type, stripeErr.Code, stripeErr.Msg, stripeErr.Param)
			respondErrorWithLog(w, r, http.StatusBadRequest,
				fmt.Errorf("stripe error: %s - %s", stripeErr.Code, stripeErr.Msg), "stripe_api")
		} else {
			log.Printf("[ERROR] [%s] Failed to create Stripe session: %v", reqID, err)
			respondErrorWithLog(w, r, http.StatusInternalServerError, err, "stripe_session_create")
		}
		return
	}
	log.Printf("[INFO] [%s] Stripe session created: id=%s", reqID, sess.ID)

	if err := s.svc.LinkOrderSession(r.Context(), order.ID, sess.ID); err != nil {
		log.Printf("[ERROR] [%s] Failed to link order session: %v", reqID, err)
		s.respondServiceErrorWithContext(w, r, err, "link_order_session")
		return
	}
	log.Printf("[INFO] [%s] Prepaid checkout completed successfully", reqID)
	respondJSON(w, http.StatusCreated, map[string]any{
		"order_id":       order.ID,
		"stripe_session": sess.ID,
		"checkout_url":   sess.URL,
	})
}

type reportUsageRequest struct {
	UserID    int64  `json:"user_id"`
	Units     int    `json:"units"`
	RequestID string `json:"request_id"`
}

func (s *Server) handleReportUsage(w http.ResponseWriter, r *http.Request) {
	// 服务间认证：验证 API Key
	if s.cfg.UsageAPIKey == "" {
		respondError(w, http.StatusServiceUnavailable, errors.New("usage API key not configured"))
		return
	}
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusUnauthorized, errors.New("missing X-API-Key header"))
		return
	}
	if apiKey != s.cfg.UsageAPIKey {
		respondError(w, http.StatusUnauthorized, errors.New("invalid API key"))
		return
	}

	var req reportUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	usage, err := s.svc.ReportUsage(r.Context(), req.UserID, req.Units, req.RequestID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, usage)
}

func (s *Server) handleListUsage(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r.URL.Query().Get("user_id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	// 权限验证：只能查看自己的用量，管理员可以查看任何人
	if !canAccessUser(r.Context(), userID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	from, to, err := parseRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	records, err := s.svc.ListUsage(r.Context(), userID, from, to)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, records)
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	orderID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	order, err := s.svc.GetOrder(r.Context(), orderID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}
	// 权限验证：只能查看自己的订单，管理员可以查看任何人的
	if !canAccessUser(r.Context(), order.UserID) {
		respondError(w, http.StatusForbidden, errors.New("access denied"))
		return
	}
	respondJSON(w, http.StatusOK, order)
}

func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.StripeWebhookSecret == "" {
		s.respondServiceError(w, services.ErrStripeNotConfigured)
		return
	}
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	sigHeader := r.Header.Get("Stripe-Signature")
	event, err := webhook.ConstructEvent(payload, sigHeader, s.cfg.StripeWebhookSecret)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			respondError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.processCheckoutSession(r.Context(), &sess); err != nil {
			s.respondServiceError(w, err)
			return
		}
	case "invoice.paid":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			respondError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.processInvoicePaid(r.Context(), &inv); err != nil {
			s.respondServiceError(w, err)
			return
		}
	default:
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) processCheckoutSession(ctx context.Context, sess *stripe.CheckoutSession) error {
	var order models.Order
	var err error

	if sess.ClientReferenceID != "" {
		if orderID, parseErr := strconv.ParseInt(sess.ClientReferenceID, 10, 64); parseErr == nil {
			order, err = s.svc.GetOrder(ctx, orderID)
		}
	}
	if err != nil || order.ID == 0 {
		order, err = s.svc.GetOrderByStripeSessionID(ctx, sess.ID)
	}
	if err != nil {
		return err
	}

	stripeSubID := ""
	if sess.Subscription != nil {
		stripeSubID = sess.Subscription.ID
	}
	stripePaymentID := ""
	if sess.PaymentIntent != nil {
		stripePaymentID = sess.PaymentIntent.ID
	}
	paidOrder, err := s.svc.MarkOrderPaid(ctx, order.ID, sess.ID, stripePaymentID, stripeSubID)
	if err != nil {
		return err
	}
	if paidOrder.OrderType != models.OrderTypeSubscription || paidOrder.SubscriptionID == nil {
		return nil
	}
	sub, err := s.svc.GetSubscriptionByID(ctx, *paidOrder.SubscriptionID)
	if err != nil {
		return err
	}
	if sub.Status == models.SubscriptionActive && sub.EndsAt.After(time.Now().UTC()) {
		return nil
	}
	plan, err := s.svc.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		return err
	}
	return s.svc.ActivateSubscription(ctx, sub.ID, stripeSubID, plan.GrantPoints, plan.PeriodDays)
}

func (s *Server) processInvoicePaid(ctx context.Context, inv *stripe.Invoice) error {
	if inv.Subscription == nil || inv.Subscription.ID == "" {
		return nil
	}
	sub, err := s.svc.GetSubscriptionByStripeID(ctx, inv.Subscription.ID)
	if err != nil {
		if errors.Is(err, services.ErrNotFound) {
			return nil
		}
		return err
	}
	if sub.EndsAt.After(time.Now().UTC().Add(1 * time.Hour)) {
		return nil
	}
	plan, err := s.svc.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		return err
	}
	return s.svc.ActivateSubscription(ctx, sub.ID, inv.Subscription.ID, plan.GrantPoints, plan.PeriodDays)
}

func (s *Server) respondServiceError(w http.ResponseWriter, err error) {
	s.respondServiceErrorWithContext(w, nil, err, "")
}

func (s *Server) respondServiceErrorWithContext(w http.ResponseWriter, r *http.Request, err error, context string) {
	switch {
	case errors.Is(err, services.ErrNotFound):
		respondError(w, http.StatusNotFound, err)
	case errors.Is(err, services.ErrInvalidRequest):
		respondError(w, http.StatusBadRequest, err)
	case errors.Is(err, services.ErrDuplicateRequest):
		respondError(w, http.StatusConflict, err)
	case errors.Is(err, services.ErrInsufficientPoints):
		respondError(w, http.StatusConflict, err)
	case errors.Is(err, services.ErrSubscriptionRequired):
		respondError(w, http.StatusForbidden, err)
	case errors.Is(err, services.ErrStripeNotConfigured):
		respondError(w, http.StatusServiceUnavailable, err)
	case errors.Is(err, services.ErrInvalidCode):
		respondError(w, http.StatusBadRequest, err)
	case errors.Is(err, services.ErrCodeAlreadyUsed):
		respondError(w, http.StatusBadRequest, err)
	case errors.Is(err, services.ErrTooManyRequests):
		respondError(w, http.StatusTooManyRequests, err)
	case errors.Is(err, services.ErrEmailAlreadyExists):
		respondError(w, http.StatusConflict, err)
	case errors.Is(err, services.ErrEmailNotVerified):
		respondError(w, http.StatusForbidden, err)
	case errors.Is(err, services.ErrUserDisabled):
		respondError(w, http.StatusForbidden, err)
	default:
		// 对于未知错误，记录详细日志
		if r != nil {
			respondErrorWithLog(w, r, http.StatusInternalServerError, err, context)
		} else {
			log.Printf("[ERROR] Internal server error: %v | Context: %s", err, context)
			respondError(w, http.StatusInternalServerError, err)
		}
	}
}

func (s *Server) stripePriceForPlan(name string) (string, error) {
	switch name {
	case "monthly":
		if s.cfg.StripePriceMonthly == "" {
			return "", errors.New("stripe monthly price not configured")
		}
		return s.cfg.StripePriceMonthly, nil
	case "quarterly":
		if s.cfg.StripePriceQuarterly == "" {
			return "", errors.New("stripe quarterly price not configured")
		}
		return s.cfg.StripePriceQuarterly, nil
	default:
		return "", errors.New("unknown plan name")
	}
}

func parseID(raw string) (int64, error) {
	if raw == "" {
		return 0, errors.New("id is required")
	}
	return strconv.ParseInt(raw, 10, 64)
}

func parseRange(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	fromRaw := r.URL.Query().Get("from")
	toRaw := r.URL.Query().Get("to")
	if fromRaw == "" && toRaw == "" {
		return now.Add(-30 * 24 * time.Hour), now, nil
	}
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, errors.New("from and to are required together")
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to, nil
}

func parsePagination(r *http.Request) (int, int) {
	page := 1
	pageSize := 20

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}
	return page, pageSize
}

// ========== 管理员接口 Handlers ==========

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	systemCode := r.URL.Query().Get("system_code")
	includeBalances := r.URL.Query().Get("include_balances") == "true"

	opts := services.ListUsersOptions{
		Page:            page,
		PageSize:        pageSize,
		SystemCode:      systemCode,
		IncludeBalances: includeBalances,
	}

	users, total, err := s.svc.ListUsersWithOptions(r.Context(), opts)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"users":     users,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

type updateUserRoleRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleAdminUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	var req updateUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	if req.Role == "" {
		respondError(w, http.StatusBadRequest, errors.New("role is required"))
		return
	}

	if err := s.svc.UpdateUserRole(r.Context(), userID, req.Role); err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminGetUserUsage(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	from, to, err := parseRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	records, err := s.svc.ListUsage(r.Context(), userID, from, to)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, records)
}

func (s *Server) handleAdminGetUserSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	subs, err := s.svc.GetUserSubscriptions(r.Context(), userID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, subs)
}

func (s *Server) handleAdminGetStats(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	stats, err := s.svc.GetStats(r.Context(), from, to)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAdminGetUserBalances(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	balances, err := s.svc.ListBalances(r.Context(), userID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, balances)
}

// ========== 内部服务接口 Handlers ==========

// internalAPIKeyMiddleware 内部服务 API Key 验证中间件
func (s *Server) internalAPIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.UsageAPIKey == "" {
			respondError(w, http.StatusServiceUnavailable, errors.New("internal API key not configured"))
			return
		}
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			respondError(w, http.StatusUnauthorized, errors.New("missing X-API-Key header"))
			return
		}
		if apiKey != s.cfg.UsageAPIKey {
			respondError(w, http.StatusUnauthorized, errors.New("invalid API key"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleInternalGetUserBalances(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	balances, err := s.svc.ListBalances(r.Context(), userID)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, balances)
}

// ========== 验证码相关 Handlers ==========

type sendVerificationCodeRequest struct {
	SystemCode string `json:"system_code"`
	Email      string `json:"email"`
	CodeType   string `json:"code_type"` // signup | reset_password
}

func (s *Server) handleSendVerificationCode(w http.ResponseWriter, r *http.Request) {
	// 检查邮件服务 API Key 是否配置
	if !s.emailClient.IsConfigured() {
		respondError(w, http.StatusServiceUnavailable, email.ErrEmailNotConfigured)
		return
	}

	var req sendVerificationCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.SystemCode == "" || req.Email == "" || req.CodeType == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code, email and code_type are required"))
		return
	}

	// 获取该 system_code 对应的邮件发送配置
	emailConfig, ok := s.cfg.ResendEmailFor(req.SystemCode)
	if !ok || emailConfig.FromEmail == "" {
		respondError(w, http.StatusServiceUnavailable, errors.New("email service not configured for this system"))
		return
	}

	// 验证 code_type
	if req.CodeType != models.CodeTypeSignup && req.CodeType != models.CodeTypeResetPassword {
		respondError(w, http.StatusBadRequest, errors.New("invalid code_type, must be 'signup' or 'reset_password'"))
		return
	}

	// 如果是重置密码，需要验证用户存在
	if req.CodeType == models.CodeTypeResetPassword {
		_, err := s.svc.GetUserByEmail(r.Context(), req.SystemCode, req.Email)
		if err != nil {
			if errors.Is(err, services.ErrNotFound) {
				respondError(w, http.StatusNotFound, errors.New("user not found"))
				return
			}
			s.respondServiceError(w, err)
			return
		}
	}

	// 创建验证码
	code, err := s.svc.CreateVerificationCode(r.Context(), req.SystemCode, req.Email, req.CodeType)
	if err != nil {
		s.respondServiceError(w, err)
		return
	}

	// 发送邮件（使用该 system_code 对应的发件人地址）
	if err := s.emailClient.SendVerificationCode(emailConfig.FromEmail, req.Email, code, req.CodeType); err != nil {
		respondError(w, http.StatusInternalServerError, errors.New("failed to send verification email"))
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "verification code sent",
	})
}

type verifyCodeRequest struct {
	SystemCode string `json:"system_code"`
	Email      string `json:"email"`
	Code       string `json:"code"`
	CodeType   string `json:"code_type"`
}

func (s *Server) handleVerifyCode(w http.ResponseWriter, r *http.Request) {
	var req verifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.SystemCode == "" || req.Email == "" || req.Code == "" || req.CodeType == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code, email, code and code_type are required"))
		return
	}

	if err := s.svc.VerifyCode(r.Context(), req.SystemCode, req.Email, req.Code, req.CodeType); err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "code verified",
	})
}

type resetPasswordRequest struct {
	SystemCode  string `json:"system_code"`
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.SystemCode == "" || req.Email == "" || req.Code == "" || req.NewPassword == "" {
		respondError(w, http.StatusBadRequest, errors.New("system_code, email, code and new_password are required"))
		return
	}

	// 验证验证码
	if err := s.svc.VerifyCode(r.Context(), req.SystemCode, req.Email, req.Code, models.CodeTypeResetPassword); err != nil {
		s.respondServiceError(w, err)
		return
	}

	// 重置密码
	if err := s.svc.ResetPassword(r.Context(), req.SystemCode, req.Email, req.NewPassword); err != nil {
		s.respondServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "password reset successfully",
	})
}
