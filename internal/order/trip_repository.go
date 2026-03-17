package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"adird.id/vidi/internal/shared"
)


// TripStateRepo handles state transitions for active trips.
type TripStateRepo struct {
	db          *pgxpool.Pool
	rdb         *redis.Client
	etaLearner  *ETALearner
}

func NewTripStateRepo(db *pgxpool.Pool, rdb *redis.Client) *TripStateRepo {
	return &TripStateRepo{db: db, rdb: rdb, etaLearner: NewETALearner(db)}
}

// TransitionTrip atomically moves a trip from fromStatus to toStatus.
// If timestampCol is non-empty, that column is set to NOW().
func (r *TripStateRepo) TransitionTrip(ctx context.Context, tripID, driverID string,
	from, to shared.TripStatus, timestampCol string) error {

	var query string
	var args []interface{}

	if timestampCol != "" {
		query = fmt.Sprintf(`
			UPDATE trips
			SET status = $3, %s = NOW()
			WHERE id = $1 AND driver_id = $2 AND status = $4
		`, timestampCol)
		args = []interface{}{tripID, driverID, string(to), string(from)}
	} else {
		query = `
			UPDATE trips SET status = $3
			WHERE id = $1 AND driver_id = $2 AND status = $4
		`
		args = []interface{}{tripID, driverID, string(to), string(from)}
	}

	tag, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("transition %s→%s: %w", from, to, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no rows updated: trip not found, wrong driver, or wrong state")
	}
	return nil
}

// CompleteTrip transitions trip to 'completed' and increments driver's total_trips.
func (r *TripStateRepo) CompleteTrip(ctx context.Context, tripID, driverID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Transition trip
	tag, err := tx.Exec(ctx, `
		UPDATE trips
		SET status = 'completed', completed_at = NOW()
		WHERE id = $1 AND driver_id = $2 AND status = 'ongoing'
	`, tripID, driverID)
	if err != nil {
		return fmt.Errorf("complete trip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("trip not found, wrong driver, or not 'ongoing'")
	}

	// Increment driver total_trips
	_, err = tx.Exec(ctx, `
		UPDATE drivers SET total_trips = total_trips + 1, status = 'online', updated_at = NOW()
		WHERE id = $1
	`, driverID)
	if err != nil {
		return fmt.Errorf("update driver stats: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Clear driver's active_trip_id in Redis, set back to online
	r.rdb.HSet(ctx, "driver:"+driverID,
		"status", "online",
		"active_trip_id", "",
	)

	// Record ETA correction asynchronously
	go r.recordETACorrection(tripID)

	return nil
}

// recordETACorrection fetches trip timing and records actual vs estimated.
func (r *TripStateRepo) recordETACorrection(tripID string) {
	ctx := context.Background()

	var originGrid, destGrid string
	var osrmEstimateSec int
	var actualSec *int // NULL if started_at is missing

	err := r.db.QueryRow(ctx, `
		SELECT
			CONCAT(FLOOR(pickup_lat*100)/100, ':', FLOOR(pickup_lng*100)/100),
			CONCAT(FLOOR(dropoff_lat*100)/100, ':', FLOOR(dropoff_lng*100)/100),
			duration_seconds,
			CASE WHEN started_at IS NOT NULL AND completed_at IS NOT NULL
				THEN EXTRACT(EPOCH FROM (completed_at - started_at))::INT
				ELSE NULL END
		FROM trips WHERE id = $1
	`, tripID).Scan(&originGrid, &destGrid, &osrmEstimateSec, &actualSec)
	if err != nil || actualSec == nil {
		return
	}

	r.etaLearner.RecordCorrection(ctx, tripID, originGrid, destGrid, osrmEstimateSec, *actualSec)
}

// CancelTripByDriver cancels a trip (driver-initiated), increments cancellation score.
func (r *TripStateRepo) CancelTripByDriver(ctx context.Context, tripID, driverID, reason string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE trips
		SET status = 'cancelled', cancelled_at = NOW(),
		    cancelled_by = 'driver', cancel_reason = $3
		WHERE id = $1 AND driver_id = $2
		  AND status NOT IN ('completed', 'cancelled')
	`, tripID, driverID, reason)
	if err != nil {
		return fmt.Errorf("cancel trip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("trip not found, wrong driver, or already terminal")
	}

	// Increment cancellation score (soft penalty — affects dispatch priority)
	_, err = tx.Exec(ctx, `
		UPDATE drivers
		SET cancellation_score = LEAST(cancellation_score + 0.1, 1.0),
		    status = 'online',
		    updated_at = NOW()
		WHERE id = $1
	`, driverID)
	if err != nil {
		return fmt.Errorf("update cancellation score: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Clear active trip in Redis
	r.rdb.HSet(ctx, "driver:"+driverID,
		"status", "online",
		"active_trip_id", "",
	)
	return nil
}
