package order

// ETA Learning — records actual vs OSRM-estimated travel time after each trip.
// Stored in eta_corrections table for future ETA improvement.
//
// Phase 1 correction: multiplier = actual_seconds / osrm_estimate_seconds
// Phase 2 (future): weighted average by origin/dest grid + hour + day

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type ETALearner struct {
	db *pgxpool.Pool
}

func NewETALearner(db *pgxpool.Pool) *ETALearner {
	return &ETALearner{db: db}
}

// RecordCorrection saves an ETA correction entry after a trip completes.
// osrmEstimate: what OSRM said, actual: real trip duration from started_at to completed_at.
func (e *ETALearner) RecordCorrection(ctx context.Context,
	tripID string,
	originGrid, destGrid string,
	osrmEstimateSec, actualSec int,
) {
	if osrmEstimateSec <= 0 || actualSec <= 0 {
		return
	}

	now := time.Now()
	multiplier := float64(actualSec) / float64(osrmEstimateSec)

	_, err := e.db.Exec(ctx, `
		INSERT INTO eta_corrections
			(origin_grid, dest_grid, hour_of_day, day_of_week,
			 osrm_estimate_seconds, actual_seconds, multiplier, trip_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		originGrid, destGrid,
		now.Hour(), int(now.Weekday()),
		osrmEstimateSec, actualSec,
		multiplier, tripID,
	)
	if err != nil {
		log.Warn().Err(err).Str("trip", tripID).Msg("eta_learning: failed to record correction")
		return
	}
	log.Debug().
		Str("trip", tripID).
		Float64("multiplier", multiplier).
		Int("osrm_sec", osrmEstimateSec).
		Int("actual_sec", actualSec).
		Msg("eta_learning: correction recorded")
}

// GetCorrection returns the average multiplier for a route pattern.
// Returns 1.4 (Jakarta default) if no data yet.
func (e *ETALearner) GetCorrection(ctx context.Context, originGrid, destGrid string, hour, dow int) float64 {
	var avg float64
	err := e.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(multiplier), 1.4)
		FROM eta_corrections
		WHERE origin_grid = $1 AND dest_grid = $2
		  AND hour_of_day = $3 AND day_of_week = $4
		LIMIT 100
	`, originGrid, destGrid, hour, dow).Scan(&avg)
	if err != nil {
		return 1.4 // Jakarta default: OSRM × 1.4
	}
	return avg
}
