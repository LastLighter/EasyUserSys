package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, err error) {
	errMsg := "unknown error"
	if err != nil {
		errMsg = err.Error()
	}
	respondJSON(w, status, ErrorResponse{Error: errMsg})
}

// respondErrorWithLog 带有日志记录的错误响应
func respondErrorWithLog(w http.ResponseWriter, r *http.Request, status int, err error, context string) {
	errMsg := "unknown error"
	if err != nil {
		errMsg = err.Error()
	}
	reqID := middleware.GetReqID(r.Context())
	// 对于服务器错误 (5xx)，记录详细日志
	if status >= 500 {
		log.Printf("[ERROR] [%s] %s %s Status %d: %s | Context: %s", reqID, r.Method, r.URL.Path, status, errMsg, context)
	} else {
		log.Printf("[WARN] [%s] %s %s Status %d: %s | Context: %s", reqID, r.Method, r.URL.Path, status, errMsg, context)
	}
	respondJSON(w, status, ErrorResponse{Error: errMsg})
}
