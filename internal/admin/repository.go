package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"adird.id/vidi/internal/shared"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// ─── Drivers ──────────────────────────────────────────────────────

// ListDrivers returns all drivers ordered by newest first.
func (r *Repository) ListDrivers(ctx context.Context, limit, offset int) ([]*shared.Driver, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, phone, name, vehicle_type, plate_number, status,
		       rating, total_trips, cancellation_score, created_at
		FROM drivers
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drivers []*shared.Driver
	for rows.Next() {
		d := &shared.Driver{}
		if err := rows.Scan(
			&d.ID, &d.Phone, &d.Name, &d.VehicleType,
			&d.PlateNumber, &d.Status, &d.Rating, &d.TotalTrips,
			&d.CancellationScore, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		drivers = append(drivers, d)
	}
	return drivers, rows.Err()
}

// CountDrivers returns the total number of registered drivers.
func (r *Repository) CountDrivers(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM drivers`).Scan(&n)
	return n, err
}

// GetDriver returns a single driver by ID.
func (r *Repository) GetDriver(ctx context.Context, id string) (*shared.Driver, error) {
	d := &shared.Driver{}
	err := r.db.QueryRow(ctx, `
		SELECT id, phone, name, vehicle_type, plate_number, status,
		       rating, total_trips, cancellation_score, created_at
		FROM drivers WHERE id = $1
	`, id).Scan(
		&d.ID, &d.Phone, &d.Name, &d.VehicleType,
		&d.PlateNumber, &d.Status, &d.Rating, &d.TotalTrips,
		&d.CancellationScore, &d.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("get driver: %w", err)
	}
	return d, nil
}

// ─── Trips ────────────────────────────────────────────────────────

// TripRow is the admin view of a trip — includes joined driver info and coordinates.
type TripRow struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	PassengerID    string    `json:"passenger_id"`
	DriverID       *string   `json:"driver_id,omitempty"`
	PickupLat      float64   `json:"pickup_lat"`
	PickupLng      float64   `json:"pickup_lng"`
	PickupAddress  string    `json:"pickup_address"`
	DropoffLat     float64   `json:"dropoff_lat"`
	DropoffLng     float64   `json:"dropoff_lng"`
	DropoffAddress string    `json:"dropoff_address"`
	FinalFare      float64   `json:"final_fare"`
	DistanceMeters int       `json:"distance_meters"`
	CreatedAt      time.Time `json:"created_at"`
	DriverName     *string   `json:"driver_name,omitempty"`
	DriverPlate    *string   `json:"driver_plate,omitempty"`
	DriverRating   *float64  `json:"driver_rating,omitempty"`
}

const tripRowSelect = `
	SELECT t.id, t.status, t.passenger_id, t.driver_id,
	       t.pickup_lat, t.pickup_lng, t.pickup_address,
	       t.dropoff_lat, t.dropoff_lng, t.dropoff_address,
	       t.final_fare, t.distance_meters, t.created_at,
	       d.name, d.plate_number, d.rating
	FROM trips t
	LEFT JOIN drivers d ON t.driver_id = d.id`

func scanTripRow(rows pgx.Row) (TripRow, error) {
	var t TripRow
	err := rows.Scan(
		&t.ID, &t.Status, &t.PassengerID, &t.DriverID,
		&t.PickupLat, &t.PickupLng, &t.PickupAddress,
		&t.DropoffLat, &t.DropoffLng, &t.DropoffAddress,
		&t.FinalFare, &t.DistanceMeters, &t.CreatedAt,
		&t.DriverName, &t.DriverPlate, &t.DriverRating,
	)
	return t, err
}

// ListTrips returns all trips ordered by newest first with driver info joined.
func (r *Repository) ListTrips(ctx context.Context, limit, offset int) ([]TripRow, error) {
	rows, err := r.db.Query(ctx,
		tripRowSelect+` ORDER BY t.created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []TripRow
	for rows.Next() {
		t, err := scanTripRow(rows)
		if err != nil {
			return nil, err
		}
		trips = append(trips, t)
	}
	return trips, rows.Err()
}

// CountTrips returns the total number of trips.
func (r *Repository) CountTrips(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM trips`).Scan(&n)
	return n, err
}

// ListActiveTrips returns trips not yet completed or cancelled.
func (r *Repository) ListActiveTrips(ctx context.Context) ([]TripRow, error) {
	rows, err := r.db.Query(ctx,
		tripRowSelect+` WHERE t.status NOT IN ('completed','cancelled') ORDER BY t.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []TripRow
	for rows.Next() {
		t, err := scanTripRow(rows)
		if err != nil {
			return nil, err
		}
		trips = append(trips, t)
	}
	return trips, rows.Err()
}

// ─── Trip GPS Trace ───────────────────────────────────────────────

// TripTracePoint is a single GPS waypoint recorded during a trip.
type TripTracePoint struct {
	Lat        float64    `json:"lat"`
	Lng        float64    `json:"lng"`
	Speed      *float64   `json:"speed,omitempty"`
	Heading    *int16     `json:"heading,omitempty"`
	RecordedAt time.Time  `json:"recorded_at"`
}

// GetTripTrace returns all GPS waypoints for a trip in chronological order.
func (r *Repository) GetTripTrace(ctx context.Context, tripID string) ([]TripTracePoint, error) {
	rows, err := r.db.Query(ctx, `
		SELECT lat, lng, speed, heading, recorded_at
		FROM trip_locations
		WHERE trip_id = $1
		ORDER BY recorded_at ASC
	`, tripID)
	if err != nil {
		return nil, fmt.Errorf("get trip trace: %w", err)
	}
	defer rows.Close()

	var points []TripTracePoint
	for rows.Next() {
		var p TripTracePoint
		if err := rows.Scan(&p.Lat, &p.Lng, &p.Speed, &p.Heading, &p.RecordedAt); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// ─── Analytics ────────────────────────────────────────────────────

// AnalyticsSummary holds the ops dashboard KPIs for the last 7 days.
type AnalyticsSummary struct {
	TotalTrips     int     `json:"total_trips"`
	TotalRevenue   float64 `json:"total_revenue"`
	AvgFare        float64 `json:"avg_fare"`
	CompletedTrips int     `json:"completed_trips"`
	CancelledTrips int     `json:"cancelled_trips"`
	ActiveDrivers  int     `json:"active_drivers"`
}

// GetAnalyticsSummary returns aggregated KPIs for the last 7 days.
func (r *Repository) GetAnalyticsSummary(ctx context.Context) (*AnalyticsSummary, error) {
	s := &AnalyticsSummary{}
	err := r.db.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(final_fare)  FILTER (WHERE status = 'completed'), 0),
			COALESCE(AVG(final_fare)  FILTER (WHERE status = 'completed'), 0),
			COUNT(*) FILTER (WHERE status = 'completed'),
			COUNT(*) FILTER (WHERE status = 'cancelled')
		FROM trips
		WHERE created_at >= NOW() - INTERVAL '7 days'
	`).Scan(&s.TotalTrips, &s.TotalRevenue, &s.AvgFare, &s.CompletedTrips, &s.CancelledTrips)
	if err != nil {
		return nil, fmt.Errorf("analytics summary: %w", err)
	}
	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM drivers WHERE status != 'offline'`,
	).Scan(&s.ActiveDrivers); err != nil {
		return nil, fmt.Errorf("analytics active drivers: %w", err)
	}
	return s, nil
}
