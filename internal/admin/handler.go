package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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

// Routes mounts admin endpoints. All require admin JWT.
func (h *Handler) Routes(authSvc *auth.Service) chi.Router {
	r := chi.NewRouter()
	r.Use(auth.Middleware(authSvc))
	r.Use(auth.RequireRole(auth.RoleAdmin, authSvc))
	r.Get("/drivers", h.listDrivers)
	r.Get("/trips", h.listTrips)
	return r
}

// GET /api/v1/admin/drivers?page=1&limit=20
func (h *Handler) listDrivers(w http.ResponseWriter, r *http.Request) {
	limit, page := parsePagination(r)
	offset := (page - 1) * limit

	drivers, err := h.repo.ListDrivers(r.Context(), limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("admin: list drivers")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	total, err := h.repo.CountDrivers(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("admin: count drivers")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return empty array, not null
	if drivers == nil {
		drivers = []*shared.Driver{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"drivers": drivers,
		"total":   total,
		"page":    page,
	})
}

// GET /api/v1/admin/trips?page=1&limit=20
func (h *Handler) listTrips(w http.ResponseWriter, r *http.Request) {
	limit, page := parsePagination(r)
	offset := (page - 1) * limit

	trips, err := h.repo.ListTrips(r.Context(), limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("admin: list trips")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	total, err := h.repo.CountTrips(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("admin: count trips")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if trips == nil {
		trips = []TripRow{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trips": trips,
		"total": total,
		"page":  page,
	})
}

// ─── helpers ──────────────────────────────────────────────────────

func parsePagination(r *http.Request) (limit, page int) {
	limit = 20
	page = 1
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	return
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
