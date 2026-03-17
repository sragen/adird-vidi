package driver

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"adird.id/vidi/internal/shared"
)

type Repository struct {
	db  *pgxpool.Pool
	rdb *redis.Client
}

func NewRepository(db *pgxpool.Pool, rdb *redis.Client) *Repository {
	return &Repository{db: db, rdb: rdb}
}

// GetByID returns a driver by UUID.
func (r *Repository) GetByID(ctx context.Context, id string) (*shared.Driver, error) {
	d := &shared.Driver{}
	err := r.db.QueryRow(ctx, `
		SELECT id, phone, name, vehicle_type, plate_number, status, rating, total_trips, created_at
		FROM drivers WHERE id = $1
	`, id).Scan(
		&d.ID, &d.Phone, &d.Name, &d.VehicleType,
		&d.PlateNumber, &d.Status, &d.Rating, &d.TotalTrips, &d.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get driver: %w", err)
	}
	return d, nil
}

// UpdateProfile updates name, vehicle_type, and plate_number.
func (r *Repository) UpdateProfile(ctx context.Context, id, name, vehicleType, plateNumber string) (*shared.Driver, error) {
	d := &shared.Driver{}
	err := r.db.QueryRow(ctx, `
		UPDATE drivers
		SET name = $2, vehicle_type = $3, plate_number = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING id, phone, name, vehicle_type, plate_number, status, rating, total_trips, created_at
	`, id, name, vehicleType, plateNumber).Scan(
		&d.ID, &d.Phone, &d.Name, &d.VehicleType,
		&d.PlateNumber, &d.Status, &d.Rating, &d.TotalTrips, &d.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update driver profile: %w", err)
	}
	return d, nil
}

// UpdateStatus sets the driver's status in PostgreSQL.
func (r *Repository) UpdateStatus(ctx context.Context, id string, status shared.DriverStatus) error {
	_, err := r.db.Exec(ctx, `
		UPDATE drivers SET status = $2, updated_at = NOW() WHERE id = $1
	`, id, status)
	return err
}

// UpdateFCMToken saves the driver's FCM token for push notifications.
func (r *Repository) UpdateFCMToken(ctx context.Context, id, token string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE drivers SET fcm_token = $2, updated_at = NOW() WHERE id = $1
	`, id, token)
	return err
}

// SetOnlineInRedis adds driver to the GEO set and sets their status hash.
func (r *Repository) SetOnlineInRedis(ctx context.Context, id string) error {
	return r.rdb.HSet(ctx, "driver:"+id, "status", "online").Err()
}

// SetOfflineInRedis removes driver from GEO set and updates hash.
func (r *Repository) SetOfflineInRedis(ctx context.Context, id string) error {
	pipe := r.rdb.Pipeline()
	pipe.ZRem(ctx, "drivers:online", id)
	pipe.HSet(ctx, "driver:"+id, "status", "offline")
	_, err := pipe.Exec(ctx)
	return err
}
