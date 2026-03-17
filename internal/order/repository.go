package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"adird.id/vidi/internal/shared"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// CreateTrip inserts a new trip in 'searching' state and returns it.
func (r *Repository) CreateTrip(ctx context.Context, p CreateTripParams) (*shared.Trip, error) {
	t := &shared.Trip{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO trips (
			passenger_id,
			pickup_lat, pickup_lng, pickup_address,
			dropoff_lat, dropoff_lng, dropoff_address,
			base_fare, distance_meters, duration_seconds,
			surge_multiplier, final_fare, planned_route_polyline
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
		RETURNING id, passenger_id, status,
			pickup_lat, pickup_lng, pickup_address,
			dropoff_lat, dropoff_lng, dropoff_address,
			base_fare, distance_meters, duration_seconds,
			surge_multiplier, final_fare, created_at
	`,
		p.PassengerID,
		p.PickupLat, p.PickupLng, p.PickupAddress,
		p.DropoffLat, p.DropoffLng, p.DropoffAddress,
		p.BaseFare, p.DistanceMeters, p.DurationSeconds,
		p.SurgeMultiplier, p.FinalFare, p.PlannedRoutePolyline,
	).Scan(
		&t.ID, &t.PassengerID, &t.Status,
		&t.PickupLat, &t.PickupLng, &t.PickupAddress,
		&t.DropoffLat, &t.DropoffLng, &t.DropoffAddress,
		&t.BaseFare, &t.DistanceMeters, &t.DurationSeconds,
		&t.SurgeMultiplier, &t.FinalFare, &t.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create trip: %w", err)
	}
	return t, nil
}

// GetTrip returns a trip by ID.
func (r *Repository) GetTrip(ctx context.Context, id string) (*shared.Trip, error) {
	t := &shared.Trip{}
	err := r.db.QueryRow(ctx, `
		SELECT id, passenger_id, driver_id, status,
			pickup_lat, pickup_lng, pickup_address,
			dropoff_lat, dropoff_lng, dropoff_address,
			base_fare, distance_meters, duration_seconds,
			surge_multiplier, final_fare, created_at
		FROM trips WHERE id = $1
	`, id).Scan(
		&t.ID, &t.PassengerID, &t.DriverID, &t.Status,
		&t.PickupLat, &t.PickupLng, &t.PickupAddress,
		&t.DropoffLat, &t.DropoffLng, &t.DropoffAddress,
		&t.BaseFare, &t.DistanceMeters, &t.DurationSeconds,
		&t.SurgeMultiplier, &t.FinalFare, &t.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}
	return t, nil
}

// AssignDriver sets driver_id and transitions trip to 'accepted'.
func (r *Repository) AssignDriver(ctx context.Context, tripID, driverID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE trips
		SET driver_id = $2, status = 'accepted', driver_accepted_at = NOW()
		WHERE id = $1 AND status = 'searching'
	`, tripID, driverID)
	return err
}

// CancelTrip marks a trip as cancelled.
func (r *Repository) CancelTrip(ctx context.Context, tripID, cancelledBy, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE trips
		SET status = 'cancelled', cancelled_at = NOW(), cancelled_by = $2, cancel_reason = $3
		WHERE id = $1 AND status NOT IN ('completed', 'cancelled')
	`, tripID, cancelledBy, reason)
	return err
}

// GetActiveFareConfig returns the current fare config for a vehicle type.
func (r *Repository) GetActiveFareConfig(ctx context.Context, vehicleType string) (*FareConfig, error) {
	fc := &FareConfig{}
	err := r.db.QueryRow(ctx, `
		SELECT base_fare, per_km_rate, per_min_rate, min_fare
		FROM fare_configs
		WHERE vehicle_type = $1
		  AND effective_from <= NOW()
		  AND (effective_to IS NULL OR effective_to > NOW())
		ORDER BY effective_from DESC
		LIMIT 1
	`, vehicleType).Scan(&fc.BaseFare, &fc.PerKmRate, &fc.PerMinRate, &fc.MinFare)
	if err != nil {
		return nil, fmt.Errorf("get fare config: %w", err)
	}
	return fc, nil
}

// ─── Types ────────────────────────────────────────────────────────

type CreateTripParams struct {
	PassengerID          string
	PickupLat            float64
	PickupLng            float64
	PickupAddress        string
	DropoffLat           float64
	DropoffLng           float64
	DropoffAddress       string
	BaseFare             float64
	DistanceMeters       int
	DurationSeconds      int
	SurgeMultiplier      float64
	FinalFare            float64
	PlannedRoutePolyline string
}

type FareConfig struct {
	BaseFare   float64
	PerKmRate  float64
	PerMinRate float64
	MinFare    float64
}
