package order

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"adird.id/vidi/internal/auth"
	"adird.id/vidi/internal/routing"
)

type Handler struct {
	repo       *Repository
	dispatcher *Dispatcher
	osrm       *routing.Client // nil = use haversine fallback
	surge      *SurgeService
}

func NewHandler(repo *Repository, dispatcher *Dispatcher, osrm *routing.Client, surge *SurgeService) *Handler {
	return &Handler{repo: repo, dispatcher: dispatcher, osrm: osrm, surge: surge}
}

// Routes returns chi router. All routes require passenger JWT.
func (h *Handler) Routes(authSvc *auth.Service) chi.Router {
	r := chi.NewRouter()
	r.Use(auth.Middleware(authSvc))

	// Passenger routes
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleUser, authSvc))
		r.Post("/", h.createOrder)
		r.Delete("/{tripID}", h.cancelOrder)
	})

	// Shared (both roles can get trip details)
	r.Get("/{tripID}", h.getTrip)

	return r
}

// ─── POST /api/v1/order ───────────────────────────────────────────
// Creates a trip and starts async dispatch.
//
// Body:
//
//	{
//	  "pickup_lat": -6.2088, "pickup_lng": 106.8456,
//	  "pickup_address": "Jl. Sudirman No.1",
//	  "dropoff_lat": -6.1750, "dropoff_lng": 106.8272,
//	  "dropoff_address": "Jl. Thamrin No.5",
//	  "vehicle_type": "motor"
//	}
func (h *Handler) createOrder(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())

	var req struct {
		PickupLat      float64 `json:"pickup_lat"`
		PickupLng      float64 `json:"pickup_lng"`
		PickupAddress  string  `json:"pickup_address"`
		DropoffLat     float64 `json:"dropoff_lat"`
		DropoffLng     float64 `json:"dropoff_lng"`
		DropoffAddress string  `json:"dropoff_address"`
		VehicleType    string  `json:"vehicle_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PickupLat == 0 || req.PickupLng == 0 || req.DropoffLat == 0 || req.DropoffLng == 0 {
		jsonError(w, "pickup and dropoff coordinates are required", http.StatusBadRequest)
		return
	}
	if req.VehicleType == "" {
		req.VehicleType = "motor" // default
	}

	// Get fare config
	fc, err := h.repo.GetActiveFareConfig(r.Context(), req.VehicleType)
	if err != nil {
		log.Error().Err(err).Msg("createOrder: get fare config")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Get distance/duration — try OSRM first, fall back to haversine
	distMeters, durationSec, polyline := h.routeEstimate(r, req.PickupLat, req.PickupLng, req.DropoffLat, req.DropoffLng)

	// Get surge multiplier for pickup location
	surgeMultiplier := 1.0
	if h.surge != nil {
		surgeMultiplier = h.surge.GetMultiplier(r.Context(), req.PickupLat, req.PickupLng)
		h.surge.RecordDemand(r.Context(), req.PickupLat, req.PickupLng)
	}

	baseFare, finalFare := CalculateFare(fc, distMeters, durationSec, surgeMultiplier)

	trip, err := h.repo.CreateTrip(r.Context(), CreateTripParams{
		PassengerID:          claims.UserID,
		PickupLat:            req.PickupLat,
		PickupLng:            req.PickupLng,
		PickupAddress:        req.PickupAddress,
		DropoffLat:           req.DropoffLat,
		DropoffLng:           req.DropoffLng,
		DropoffAddress:       req.DropoffAddress,
		BaseFare:             baseFare,
		DistanceMeters:       distMeters,
		DurationSeconds:      durationSec,
		SurgeMultiplier:      surgeMultiplier,
		FinalFare:            finalFare,
		PlannedRoutePolyline: polyline,
	})
	if err != nil {
		log.Error().Err(err).Msg("createOrder: create trip")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Start dispatch in background — passenger subscribes to MQTT for updates
	go h.dispatcher.Run(trip, req.VehicleType)

	log.Info().Str("trip", trip.ID).Str("passenger", claims.UserID).
		Float64("fare", finalFare).Msg("order created")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trip_id":          trip.ID,
		"status":           "searching",
		"base_fare":        baseFare,
		"final_fare":       finalFare,
		"distance_meters":  distMeters,
		"duration_seconds": durationSec,
		"mqtt_topic":       "adird/trip/" + trip.ID + "/status",
	})
}

// ─── GET /api/v1/order/{tripID} ───────────────────────────────────

func (h *Handler) getTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	trip, err := h.repo.GetTrip(r.Context(), tripID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			jsonError(w, "trip not found", http.StatusNotFound)
		} else {
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	// Auth check: only passenger or assigned driver can see trip
	claims, ok := auth.ClaimsFromContext(r.Context())
	if ok {
		isPassenger := claims.UserID == trip.PassengerID
		isDriver := trip.DriverID != nil && claims.UserID == *trip.DriverID
		if !isPassenger && !isDriver {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trip)
}

// ─── DELETE /api/v1/order/{tripID} ───────────────────────────────

func (h *Handler) cancelOrder(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	claims, _ := auth.ClaimsFromContext(r.Context())

	trip, err := h.repo.GetTrip(r.Context(), tripID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			jsonError(w, "trip not found", http.StatusNotFound)
		} else {
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if trip.PassengerID != claims.UserID {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if trip.Status == "completed" || trip.Status == "cancelled" {
		jsonError(w, "trip cannot be cancelled", http.StatusUnprocessableEntity)
		return
	}

	if err := h.repo.CancelTrip(r.Context(), tripID, "passenger", "cancelled by passenger"); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// routeEstimate tries OSRM then falls back to haversine + 30km/h estimate.
func (h *Handler) routeEstimate(r *http.Request, lat1, lng1, lat2, lng2 float64) (distMeters, durationSec int, polyline string) {
	if h.osrm != nil {
		result, err := h.osrm.Route(r.Context(), lat1, lng1, lat2, lng2)
		if err == nil {
			return result.DistanceMeters, result.DurationSeconds, result.Polyline
		}
		log.Warn().Err(err).Msg("OSRM unavailable, using haversine fallback")
	}
	dist := haversineMeters(lat1, lng1, lat2, lng2)
	dur := int(float64(dist) / 30000.0 * 3600)
	return dist, dur, ""
}

// ─── helpers ─────────────────────────────────────────────────────

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
