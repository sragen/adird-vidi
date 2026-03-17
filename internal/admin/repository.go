package admin

import (
	"context"
	"time"

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

// ─── Trips ────────────────────────────────────────────────────────

// TripRow is the admin view of a trip — includes joined driver info.
type TripRow struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	PassengerID    string    `json:"passenger_id"`
	DriverID       *string   `json:"driver_id,omitempty"`
	PickupAddress  string    `json:"pickup_address"`
	DropoffAddress string    `json:"dropoff_address"`
	FinalFare      float64   `json:"final_fare"`
	DistanceMeters int       `json:"distance_meters"`
	CreatedAt      time.Time `json:"created_at"`
	DriverName     *string   `json:"driver_name,omitempty"`
	DriverPlate    *string   `json:"driver_plate,omitempty"`
}

// ListTrips returns all trips ordered by newest first with driver info joined.
func (r *Repository) ListTrips(ctx context.Context, limit, offset int) ([]TripRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.status, t.passenger_id, t.driver_id,
		       t.pickup_address, t.dropoff_address,
		       t.final_fare, t.distance_meters, t.created_at,
		       d.name, d.plate_number
		FROM trips t
		LEFT JOIN drivers d ON t.driver_id = d.id
		ORDER BY t.created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []TripRow
	for rows.Next() {
		var t TripRow
		if err := rows.Scan(
			&t.ID, &t.Status, &t.PassengerID, &t.DriverID,
			&t.PickupAddress, &t.DropoffAddress,
			&t.FinalFare, &t.DistanceMeters, &t.CreatedAt,
			&t.DriverName, &t.DriverPlate,
		); err != nil {
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
