package order

// Trip state machine — driver-facing endpoints
//
// Flow:  accepted → en_route → arrived → ongoing → completed
//
// Routes (all require driver JWT):
//   PUT /api/v1/trip/{tripID}/en-route    driver heading to pickup
//   PUT /api/v1/trip/{tripID}/arrived     driver at pickup location
//   PUT /api/v1/trip/{tripID}/start       passenger boarded, trip begins
//   PUT /api/v1/trip/{tripID}/complete    trip finished
//   PUT /api/v1/trip/{tripID}/cancel      driver cancels (penalty applied)

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"adird.id/vidi/internal/auth"
	mqttclient "adird.id/vidi/internal/mqtt"
	"adird.id/vidi/internal/shared"
)

// TripStateHandler handles driver-initiated trip state transitions.
type TripStateHandler struct {
	repo *TripStateRepo
	mqtt *mqttclient.Client
}

func NewTripStateHandler(repo *TripStateRepo, mqtt *mqttclient.Client) *TripStateHandler {
	return &TripStateHandler{repo: repo, mqtt: mqtt}
}

// Routes for driver trip state transitions. Requires driver JWT.
func (h *TripStateHandler) Routes(authSvc *auth.Service) chi.Router {
	r := chi.NewRouter()
	r.Use(auth.Middleware(authSvc))
	r.Use(auth.RequireRole(auth.RoleDriver, authSvc))

	r.Put("/{tripID}/en-route", h.setEnRoute)
	r.Put("/{tripID}/arrived", h.setArrived)
	r.Put("/{tripID}/start", h.startTrip)
	r.Put("/{tripID}/complete", h.completeTrip)
	r.Put("/{tripID}/cancel", h.cancelTrip)
	return r
}

// PUT /api/v1/trip/{tripID}/en-route
func (h *TripStateHandler) setEnRoute(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	claims, _ := auth.ClaimsFromContext(r.Context())

	if err := h.repo.TransitionTrip(r.Context(), tripID, claims.UserID,
		shared.TripStatusAccepted, shared.TripStatusEnRoute, ""); err != nil {
		log.Error().Err(err).Str("trip", tripID).Msg("en_route transition failed")
		jsonError(w, "transition failed — trip may not be in 'accepted' state", http.StatusConflict)
		return
	}

	h.mqtt.PublishTripStatus(tripID, shared.TripStatusPayload{Status: shared.TripStatusEnRoute})
	log.Info().Str("trip", tripID).Str("driver", claims.UserID).Msg("trip: en_route")
	jsonTrip(w, tripID, shared.TripStatusEnRoute)
}

// PUT /api/v1/trip/{tripID}/arrived
func (h *TripStateHandler) setArrived(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	claims, _ := auth.ClaimsFromContext(r.Context())

	if err := h.repo.TransitionTrip(r.Context(), tripID, claims.UserID,
		shared.TripStatusEnRoute, shared.TripStatusArrived, "driver_arrived_at"); err != nil { //nolint
		log.Error().Err(err).Str("trip", tripID).Msg("arrived transition failed")
		jsonError(w, "transition failed — trip may not be in 'en_route' state", http.StatusConflict)
		return
	}

	h.mqtt.PublishTripStatus(tripID, shared.TripStatusPayload{Status: shared.TripStatusArrived})
	log.Info().Str("trip", tripID).Str("driver", claims.UserID).Msg("trip: arrived")
	jsonTrip(w, tripID, shared.TripStatusArrived)
}

// PUT /api/v1/trip/{tripID}/start
func (h *TripStateHandler) startTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	claims, _ := auth.ClaimsFromContext(r.Context())

	if err := h.repo.TransitionTrip(r.Context(), tripID, claims.UserID,
		shared.TripStatusArrived, shared.TripStatusOngoing, "started_at"); err != nil { //nolint
		log.Error().Err(err).Str("trip", tripID).Msg("start transition failed")
		jsonError(w, "transition failed — trip may not be in 'arrived' state", http.StatusConflict)
		return
	}

	h.mqtt.PublishTripStatus(tripID, shared.TripStatusPayload{Status: shared.TripStatusOngoing})
	log.Info().Str("trip", tripID).Str("driver", claims.UserID).Msg("trip: ongoing")
	jsonTrip(w, tripID, shared.TripStatusOngoing)
}

// PUT /api/v1/trip/{tripID}/complete
func (h *TripStateHandler) completeTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	claims, _ := auth.ClaimsFromContext(r.Context())

	if err := h.repo.CompleteTrip(r.Context(), tripID, claims.UserID); err != nil {
		log.Error().Err(err).Str("trip", tripID).Msg("complete transition failed")
		jsonError(w, "transition failed — trip may not be in 'ongoing' state", http.StatusConflict)
		return
	}

	h.mqtt.PublishTripStatus(tripID, shared.TripStatusPayload{Status: shared.TripStatusCompleted})
	log.Info().Str("trip", tripID).Str("driver", claims.UserID).Msg("trip: completed ✅")
	jsonTrip(w, tripID, shared.TripStatusCompleted)
}

// PUT /api/v1/trip/{tripID}/cancel — driver cancels
// Body (optional): {"reason": "..."}
func (h *TripStateHandler) cancelTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	claims, _ := auth.ClaimsFromContext(r.Context())

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Reason == "" {
		req.Reason = "cancelled by driver"
	}

	if err := h.repo.CancelTripByDriver(r.Context(), tripID, claims.UserID, req.Reason); err != nil {
		log.Error().Err(err).Str("trip", tripID).Msg("driver cancel failed")
		jsonError(w, "cancel failed", http.StatusConflict)
		return
	}

	h.mqtt.PublishTripStatus(tripID, shared.TripStatusPayload{Status: shared.TripStatusCancelled})
	log.Info().Str("trip", tripID).Str("driver", claims.UserID).Msg("trip: cancelled by driver")
	jsonTrip(w, tripID, shared.TripStatusCancelled)
}

func jsonTrip(w http.ResponseWriter, tripID string, status shared.TripStatus) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"trip_id": tripID,
		"status":  string(status),
	})
}
