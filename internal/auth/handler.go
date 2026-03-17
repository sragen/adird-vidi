package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/otp/request", h.requestOTP)
	r.Post("/otp/verify", h.verifyOTP)
	r.Post("/token/refresh", h.refreshToken)
	return r
}

// ─── POST /api/v1/auth/otp/request ───────────────────────────────
// Body: {"phone": "+6281234567890", "role": "user"|"driver"}

func (h *Handler) requestOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Phone == "" || req.Role == "" {
		jsonError(w, "phone and role are required", http.StatusBadRequest)
		return
	}

	err := h.svc.RequestOTP(r.Context(), req.Phone, req.Role)
	if err != nil {
		switch {
		case errors.Is(err, ErrRateLimited):
			jsonError(w, "too many requests, try again in 10 minutes", http.StatusTooManyRequests)
		case errors.Is(err, ErrUnknownRole):
			jsonError(w, "role must be 'user' or 'driver'", http.StatusBadRequest)
		default:
			log.Error().Err(err).Msg("requestOTP failed")
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "OTP sent",
		"phone":   req.Phone,
	})
}

// ─── POST /api/v1/auth/otp/verify ────────────────────────────────
// Body: {"phone": "+6281...", "role": "user", "code": "123456"}

func (h *Handler) verifyOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
		Role  string `json:"role"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Phone == "" || req.Code == "" || req.Role == "" {
		jsonError(w, "phone, role, and code are required", http.StatusBadRequest)
		return
	}

	access, refresh, userID, err := h.svc.VerifyOTP(r.Context(), req.Phone, req.Role, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidOTP):
			jsonError(w, "invalid or expired OTP", http.StatusUnauthorized)
		case errors.Is(err, ErrUnknownRole):
			jsonError(w, "role must be 'user', 'driver', or 'admin'", http.StatusBadRequest)
		case errors.Is(err, ErrAdminNotFound):
			jsonError(w, "phone not registered as admin", http.StatusForbidden)
		default:
			log.Error().Err(err).Msg("verifyOTP failed")
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	log.Info().Str("user_id", userID).Str("role", req.Role).Msg("auth: login")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"access_token":  access,
		"refresh_token": refresh,
		"user_id":       userID,
		"role":          req.Role,
	})
}

// ─── POST /api/v1/auth/token/refresh ─────────────────────────────
// Body: {"refresh_token": "..."}

func (h *Handler) refreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		jsonError(w, "refresh_token is required", http.StatusBadRequest)
		return
	}

	access, refresh, err := h.svc.RefreshTokens(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			jsonError(w, "invalid or expired refresh token", http.StatusUnauthorized)
		} else {
			log.Error().Err(err).Msg("refreshToken failed")
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"access_token":  access,
		"refresh_token": refresh,
	})
}

// ─── helpers ──────────────────────────────────────────────────────

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
