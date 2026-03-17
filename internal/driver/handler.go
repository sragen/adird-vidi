package driver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"adird.id/vidi/internal/auth"
	"adird.id/vidi/internal/shared"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// Routes returns chi router. All routes require driver JWT.
func (h *Handler) Routes(authSvc *auth.Service) chi.Router {
	r := chi.NewRouter()
	r.Use(auth.Middleware(authSvc))
	r.Use(auth.RequireRole(auth.RoleDriver, authSvc))

	r.Get("/profile", h.getProfile)
	r.Put("/profile", h.updateProfile)
	r.Put("/status", h.updateStatus)
	r.Put("/fcm-token", h.updateFCMToken)
	return r
}

// ─── GET /api/v1/driver/profile ──────────────────────────────────

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	d, err := h.repo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			jsonError(w, "driver not found", http.StatusNotFound)
		} else {
			log.Error().Err(err).Msg("getProfile")
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	jsonOK(w, d)
}

// ─── PUT /api/v1/driver/profile ───────────────────────────────────
// Body: {"name":"Budi","vehicle_type":"motor","plate_number":"B1234XYZ"}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())

	var req struct {
		Name        string `json:"name"`
		VehicleType string `json:"vehicle_type"`
		PlateNumber string `json:"plate_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.VehicleType == "" || req.PlateNumber == "" {
		jsonError(w, "name, vehicle_type, and plate_number are required", http.StatusBadRequest)
		return
	}
	if req.VehicleType != string(shared.VehicleMotor) && req.VehicleType != string(shared.VehicleCar) {
		jsonError(w, "vehicle_type must be 'motor' or 'car'", http.StatusBadRequest)
		return
	}
	req.PlateNumber = strings.ToUpper(req.PlateNumber)

	d, err := h.repo.UpdateProfile(r.Context(), claims.UserID, req.Name, req.VehicleType, req.PlateNumber)
	if err != nil {
		log.Error().Err(err).Msg("updateProfile")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, d)
}

// ─── PUT /api/v1/driver/status ────────────────────────────────────
// Body: {"status":"online"} or {"status":"offline"}
// Drivers cannot set themselves "on_trip" — dispatch does that.

func (h *Handler) updateStatus(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var newStatus shared.DriverStatus
	switch req.Status {
	case "online":
		newStatus = shared.DriverStatusOnline
	case "offline":
		newStatus = shared.DriverStatusOffline
	default:
		jsonError(w, "status must be 'online' or 'offline'", http.StatusBadRequest)
		return
	}

	// Check profile is complete before going online
	if newStatus == shared.DriverStatusOnline {
		d, err := h.repo.GetByID(r.Context(), claims.UserID)
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		if d.PlateNumber == "PENDING" || d.Name == "" {
			jsonError(w, "complete your profile before going online", http.StatusUnprocessableEntity)
			return
		}
	}

	if err := h.repo.UpdateStatus(r.Context(), claims.UserID, newStatus); err != nil {
		log.Error().Err(err).Msg("updateStatus PG")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Sync Redis
	if newStatus == shared.DriverStatusOnline {
		h.repo.SetOnlineInRedis(r.Context(), claims.UserID)
	} else {
		h.repo.SetOfflineInRedis(r.Context(), claims.UserID)
	}

	log.Info().Str("driver", claims.UserID).Str("status", string(newStatus)).Msg("driver status changed")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": string(newStatus)})
}

// ─── PUT /api/v1/driver/fcm-token ────────────────────────────────
// Body: {"token":"fcm-device-token"}

func (h *Handler) updateFCMToken(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		jsonError(w, "token is required", http.StatusBadRequest)
		return
	}

	if err := h.repo.UpdateFCMToken(r.Context(), claims.UserID, req.Token); err != nil {
		log.Error().Err(err).Msg("updateFCMToken")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ─────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
