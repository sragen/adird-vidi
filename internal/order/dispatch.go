package order

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	mqttclient "adird.id/vidi/internal/mqtt"
	"adird.id/vidi/internal/shared"
)

const (
	searchRadiusKm  = 5.0
	maxCandidates   = 10
	offerTimeoutSec = 15
	maxDispatchRounds = 3 // try up to 3 drivers before giving up
)

// Dispatcher finds the best available driver and sends them an offer.
type Dispatcher struct {
	rdb  *redis.Client
	repo *Repository
	mqtt *mqttclient.Client
}

func NewDispatcher(rdb *redis.Client, repo *Repository, mqtt *mqttclient.Client) *Dispatcher {
	return &Dispatcher{rdb: rdb, repo: repo, mqtt: mqtt}
}

// Run tries to assign a driver to the trip. Runs in a goroutine.
// On success: updates trip to 'accepted', notifies passenger via MQTT.
// On failure after maxDispatchRounds: cancels trip, notifies passenger.
func (d *Dispatcher) Run(trip *shared.Trip, vehicleType string) {
	ctx := context.Background()

	for round := 1; round <= maxDispatchRounds; round++ {
		log.Info().Str("trip", trip.ID).Int("round", round).Msg("dispatch: searching drivers")

		candidates, err := d.findCandidates(ctx, trip, vehicleType)
		if err != nil || len(candidates) == 0 {
			log.Warn().Str("trip", trip.ID).Msg("dispatch: no drivers found nearby")
			time.Sleep(5 * time.Second) // wait before next round
			continue
		}

		for _, candidate := range candidates {
			accepted, err := d.sendOffer(ctx, trip, candidate)
			if err != nil {
				log.Error().Err(err).Str("driver", candidate).Msg("dispatch: offer error")
				continue
			}
			if accepted {
				d.onAccepted(ctx, trip, candidate)
				return
			}
		}
	}

	// All rounds exhausted — cancel trip
	log.Warn().Str("trip", trip.ID).Msg("dispatch: no driver accepted, cancelling")
	d.repo.CancelTrip(ctx, trip.ID, "system", "no driver available")
	d.mqtt.PublishTripStatus(trip.ID, shared.TripStatusPayload{
		Status: shared.TripStatusCancelled,
	})
}

// findCandidates queries Redis GEOSEARCH for online drivers near pickup,
// scores them, and returns sorted driver IDs.
func (d *Dispatcher) findCandidates(ctx context.Context, trip *shared.Trip, vehicleType string) ([]string, error) {
	locations, err := d.rdb.GeoSearchLocation(ctx, "drivers:online", &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  trip.PickupLng,
			Latitude:   trip.PickupLat,
			Radius:     searchRadiusKm,
			RadiusUnit: "km",
			Sort:       "ASC",
			Count:      maxCandidates,
		},
		WithCoord: true,
		WithDist:  true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("geo search: %w", err)
	}

	type scored struct {
		id    string
		score float64
	}
	var scored_list []scored

	for _, loc := range locations {
		// Skip if no GPS recently (check Redis hash exists)
		exists, _ := d.rdb.Exists(ctx, "driver:"+loc.Name).Result()
		if exists == 0 {
			continue
		}

		// Score: distance (lower = better, weight 0.7) + random fairness (weight 0.3)
		// In a real system: also factor in ETA from OSRM and driver rating
		distScore := 1.0 / (1.0 + loc.Dist) // 0..1, closer = higher
		score := distScore * 0.7             // simplified; ETA weight added in Layer 4
		scored_list = append(scored_list, scored{id: loc.Name, score: score})
	}

	// Sort by score descending
	sort.Slice(scored_list, func(i, j int) bool {
		return scored_list[i].score > scored_list[j].score
	})

	ids := make([]string, 0, len(scored_list))
	for _, s := range scored_list {
		ids = append(ids, s.id)
	}
	return ids, nil
}

// sendOffer publishes offer to driver and waits offerTimeoutSec for response.
func (d *Dispatcher) sendOffer(ctx context.Context, trip *shared.Trip, driverID string) (bool, error) {
	offer := shared.OfferPayload{
		OrderID:        trip.ID,
		PickupAddress:  trip.PickupAddress,
		DropoffAddress: trip.DropoffAddress,
		FareEstimate:   trip.FinalFare,
		PickupLat:      trip.PickupLat,
		PickupLng:      trip.PickupLng,
		DropoffLat:     trip.DropoffLat,
		DropoffLng:     trip.DropoffLng,
		ExpiresIn:      offerTimeoutSec,
	}

	// Register channel before publishing (avoid race)
	ch := d.mqtt.RegisterOfferChannel(driverID, trip.ID)
	defer d.mqtt.UnregisterOfferChannel(driverID, trip.ID)

	if err := d.mqtt.SendOffer(driverID, offer); err != nil {
		return false, fmt.Errorf("send offer: %w", err)
	}

	log.Info().Str("trip", trip.ID).Str("driver", driverID).Msg("dispatch: offer sent")

	// Wait for driver response or timeout
	select {
	case accepted := <-ch:
		return accepted, nil
	case <-time.After(offerTimeoutSec * time.Second):
		log.Info().Str("driver", driverID).Str("trip", trip.ID).Msg("dispatch: offer timed out")
		return false, nil
	}
}

// onAccepted updates DB + Redis + notifies passenger.
func (d *Dispatcher) onAccepted(ctx context.Context, trip *shared.Trip, driverID string) {
	if err := d.repo.AssignDriver(ctx, trip.ID, driverID); err != nil {
		log.Error().Err(err).Str("trip", trip.ID).Msg("dispatch: assign driver failed")
		return
	}

	// Mark driver as on_trip in Redis (GPS will forward to passenger)
	d.rdb.HSet(ctx, "driver:"+driverID,
		"status", "on_trip",
		"active_trip_id", trip.ID,
	)

	// Get driver info for passenger notification
	driverInfo, err := d.rdb.HGetAll(ctx, "driver:"+driverID).Result()
	if err != nil {
		log.Warn().Err(err).Str("driver", driverID).Msg("dispatch: could not get driver info from redis")
	}

	d.mqtt.PublishTripStatus(trip.ID, shared.TripStatusPayload{
		Status:     shared.TripStatusAccepted,
		DriverName: driverInfo["name"],
		Plate:      driverInfo["plate"],
	})

	log.Info().Str("trip", trip.ID).Str("driver", driverID).Msg("dispatch: driver accepted ✅")
}
