package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

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

// Routes mounts admin endpoints. All require admin JWT.
func (h *Handler) Routes(authSvc *auth.Service) chi.Router {
	r := chi.NewRouter()
	r.Use(auth.Middleware(authSvc))
	r.Use(auth.RequireRole(auth.RoleAdmin, authSvc))

	// Driver endpoints
	r.Get("/drivers", h.listDrivers)
	r.Get("/drivers/{driverID}", h.getDriver)

	// Trip endpoints — static "active" must be before parameterized {tripID}
	r.Get("/trips/active", h.listActiveTrips)
	r.Get("/trips", h.listTrips)
	r.Get("/trips/{tripID}/trace", h.getTripTrace)

	// Analytics
	r.Get("/analytics/summary", h.getAnalyticsSummary)

	return r
}

// ─── GET /api/v1/admin/drivers ────────────────────────────────────

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

// ─── GET /api/v1/admin/drivers/{driverID} ────────────────────────

func (h *Handler) getDriver(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "driverID")
	d, err := h.repo.GetDriver(r.Context(), driverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			jsonError(w, "driver not found", http.StatusNotFound)
		} else {
			log.Error().Err(err).Str("driver", driverID).Msg("admin: get driver")
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d)
}

// ─── GET /api/v1/admin/trips ──────────────────────────────────────

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

// ─── GET /api/v1/admin/trips/active ──────────────────────────────

func (h *Handler) listActiveTrips(w http.ResponseWriter, r *http.Request) {
	trips, err := h.repo.ListActiveTrips(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("admin: list active trips")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if trips == nil {
		trips = []TripRow{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trips": trips,
	})
}

// ─── GET /api/v1/admin/trips/{tripID}/trace ───────────────────────

func (h *Handler) getTripTrace(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	points, err := h.repo.GetTripTrace(r.Context(), tripID)
	if err != nil {
		log.Error().Err(err).Str("trip", tripID).Msg("admin: get trip trace")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if points == nil {
		points = []TripTracePoint{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trip_id": tripID,
		"points":  points,
		"count":   len(points),
	})
}

// ─── GET /api/v1/admin/analytics/summary ─────────────────────────

func (h *Handler) getAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	s, err := h.repo.GetAnalyticsSummary(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("admin: analytics summary")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
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
