package order

// Rating endpoints:
//   POST /api/v1/trip/{tripID}/rate   — passenger rates driver (or driver rates passenger)
//   GET  /api/v1/order/history        — passenger trip history

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"adird.id/vidi/internal/auth"
)

// ─── Rating Repository ────────────────────────────────────────────

type RatingRepo struct {
	db *pgxpool.Pool
}

func NewRatingRepo(db *pgxpool.Pool) *RatingRepo {
	return &RatingRepo{db: db}
}

// SubmitRating inserts a rating and updates the ratee's average.
func (r *RatingRepo) SubmitRating(ctx context.Context, tripID, raterID, rateeID, raterType string, score int, comment string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Insert rating (unique index on trip_id + rater_type prevents duplicates)
	_, err = tx.Exec(ctx, `
		INSERT INTO ratings (trip_id, rater_type, rater_id, ratee_id, score, comment)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, tripID, raterType, raterID, rateeID, score, comment)
	if err != nil {
		return fmt.Errorf("insert rating: %w", err)
	}

	// Update driver's average rating (only when passenger rates driver)
	if raterType == "passenger" {
		_, err = tx.Exec(ctx, `
			UPDATE drivers
			SET rating = (
				SELECT ROUND(AVG(score)::numeric, 2)
				FROM ratings
				WHERE ratee_id = $1 AND rater_type = 'passenger'
			)
			WHERE id = $1
		`, rateeID)
		if err != nil {
			return fmt.Errorf("update driver rating: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetTripsForPassenger returns paginated trip history.
func (r *RatingRepo) GetTripsForPassenger(ctx context.Context, passengerID string, limit, offset int) ([]TripSummary, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.status, t.pickup_address, t.dropoff_address,
		       t.final_fare, t.distance_meters, t.created_at,
		       d.name, d.plate_number, d.rating
		FROM trips t
		LEFT JOIN drivers d ON t.driver_id = d.id
		WHERE t.passenger_id = $1
		ORDER BY t.created_at DESC
		LIMIT $2 OFFSET $3
	`, passengerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []TripSummary
	for rows.Next() {
		var ts TripSummary
		err := rows.Scan(
			&ts.TripID, &ts.Status, &ts.PickupAddress, &ts.DropoffAddress,
			&ts.FinalFare, &ts.DistanceMeters, &ts.CreatedAt,
			&ts.DriverName, &ts.DriverPlate, &ts.DriverRating,
		)
		if err != nil {
			return nil, err
		}
		trips = append(trips, ts)
	}
	return trips, rows.Err()
}

type TripSummary struct {
	TripID         string    `json:"trip_id"`
	Status         string    `json:"status"`
	PickupAddress  string    `json:"pickup_address"`
	DropoffAddress string    `json:"dropoff_address"`
	FinalFare      float64   `json:"final_fare"`
	DistanceMeters int       `json:"distance_meters"`
	CreatedAt      time.Time `json:"created_at"`
	DriverName     *string   `json:"driver_name,omitempty"`
	DriverPlate    *string   `json:"driver_plate,omitempty"`
	DriverRating   *float64  `json:"driver_rating,omitempty"`
}

// ─── Rating + History Handler ─────────────────────────────────────

type RatingHandler struct {
	ratingRepo *RatingRepo
	orderRepo  *Repository
}

func NewRatingHandler(ratingRepo *RatingRepo, orderRepo *Repository) *RatingHandler {
	return &RatingHandler{ratingRepo: ratingRepo, orderRepo: orderRepo}
}

func (h *RatingHandler) RegisterRoutes(r chi.Router, authSvc *auth.Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(authSvc))
		r.Post("/trip/{tripID}/rate", h.rate)
		r.Get("/order/history", h.tripHistory)
	})
}

// ─── POST /api/v1/trip/{tripID}/rate ─────────────────────────────
// Body: {"score": 5, "comment": "Sangat baik!"}
// Works for both passenger (rates driver) and driver (rates passenger).

func (h *RatingHandler) rate(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripID")
	claims, _ := auth.ClaimsFromContext(r.Context())

	var req struct {
		Score   int    `json:"score"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Score < 1 || req.Score > 5 {
		jsonError(w, "score must be between 1 and 5", http.StatusBadRequest)
		return
	}

	// Get trip to find the other party
	trip, err := h.orderRepo.GetTrip(r.Context(), tripID)
	if err != nil {
		jsonError(w, "trip not found", http.StatusNotFound)
		return
	}
	if trip.Status != "completed" {
		jsonError(w, "can only rate completed trips", http.StatusUnprocessableEntity)
		return
	}

	var raterType, rateeID string
	switch claims.Role {
	case auth.RoleUser:
		if trip.PassengerID != claims.UserID {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		if trip.DriverID == nil {
			jsonError(w, "no driver assigned", http.StatusUnprocessableEntity)
			return
		}
		raterType = "passenger"
		rateeID = *trip.DriverID
	case auth.RoleDriver:
		if trip.DriverID == nil || *trip.DriverID != claims.UserID {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		raterType = "driver"
		rateeID = trip.PassengerID
	default:
		jsonError(w, "unknown role", http.StatusForbidden)
		return
	}

	if err := h.ratingRepo.SubmitRating(r.Context(), tripID, claims.UserID, rateeID, raterType, req.Score, req.Comment); err != nil {
		log.Error().Err(err).Str("trip", tripID).Msg("submit rating")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trip_id": tripID,
		"score":   req.Score,
		"message": "rating submitted",
	})
}

// ─── GET /api/v1/order/history ────────────────────────────────────
// Query params: limit (default 20), page (default 1)

func (h *RatingHandler) tripHistory(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims.Role != auth.RoleUser {
		jsonError(w, "only passengers have trip history", http.StatusForbidden)
		return
	}

	limit := 20
	page := 1
	offset := (page - 1) * limit

	trips, err := h.ratingRepo.GetTripsForPassenger(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("trip history")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if trips == nil {
		trips = []TripSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trips": trips,
		"page":  page,
		"limit": limit,
	})
}
