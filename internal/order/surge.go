package order

// Surge pricing — Redis-based demand/supply counter per geo grid cell.
//
// Grid cell: lat/lng floored to 2 decimal places → ~1.1km x 1.1km cells
// Surge key: surge:{grid}  (e.g. surge:-6.20:106.84)
// Value: integer demand counter, expires every 5 minutes
//
// Algorithm:
//   1. On each order creation, increment demand counter for pickup grid
//   2. On each driver GPS ping (MQTT), increment supply counter
//   3. Surge = 1 + max(0, (demand - supply) / 5) capped at 2.5×

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	surgeWindowTTL = 5 * time.Minute
	surgeMaxMult   = 2.5
)

type SurgeService struct {
	rdb *redis.Client
}

func NewSurgeService(rdb *redis.Client) *SurgeService {
	return &SurgeService{rdb: rdb}
}

// gridKey returns the Redis key for a lat/lng grid cell.
func gridKey(prefix string, lat, lng float64) string {
	// Floor to 2 decimal places (~1.1km grid)
	glat := math.Floor(lat*100) / 100
	glng := math.Floor(lng*100) / 100
	return fmt.Sprintf("%s:%.2f:%.2f", prefix, glat, glng)
}

// RecordDemand increments the demand counter for the pickup location.
func (s *SurgeService) RecordDemand(ctx context.Context, lat, lng float64) {
	key := gridKey("surge_demand", lat, lng)
	pipe := s.rdb.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, surgeWindowTTL)
	pipe.Exec(ctx)
}

// GetMultiplier returns the current surge multiplier for a location (1.0 = no surge).
func (s *SurgeService) GetMultiplier(ctx context.Context, lat, lng float64) float64 {
	demandKey := gridKey("surge_demand", lat, lng)
	supplyKey := gridKey("surge_supply", lat, lng)

	pipe := s.rdb.Pipeline()
	demandCmd := pipe.Get(ctx, demandKey)
	supplyCmd := pipe.Get(ctx, supplyKey)
	pipe.Exec(ctx)

	demand, _ := demandCmd.Int()
	supply, _ := supplyCmd.Int()

	if supply == 0 {
		supply = 1 // avoid division by zero; no supply data = treat as 1
	}

	ratio := float64(demand) / float64(supply)
	mult := 1.0 + math.Max(0, (ratio-1.0)*0.3)
	if mult > surgeMaxMult {
		mult = surgeMaxMult
	}
	// Round to nearest 0.1
	return math.Round(mult*10) / 10
}

// GridKey returns the grid identifier string for use in ETA corrections.
func GridKey(lat, lng float64) string {
	glat := math.Floor(lat*100) / 100
	glng := math.Floor(lng*100) / 100
	return fmt.Sprintf("%.2f:%.2f", glat, glng)
}
