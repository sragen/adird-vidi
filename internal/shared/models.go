package shared

import (
	"time"
)

// ─── Geo ──────────────────────────────────────────────────────────

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// ─── User ─────────────────────────────────────────────────────────

type User struct {
	ID        string    `json:"id" db:"id"`
	Phone     string    `json:"phone" db:"phone"`
	Name      string    `json:"name" db:"name"`
	FCMToken  string    `json:"-" db:"fcm_token"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ─── Driver ───────────────────────────────────────────────────────

type DriverStatus string

const (
	DriverStatusOffline DriverStatus = "offline"
	DriverStatusOnline  DriverStatus = "online"
	DriverStatusOnTrip  DriverStatus = "on_trip"
)

type VehicleType string

const (
	VehicleMotor VehicleType = "motor"
	VehicleCar   VehicleType = "car"
)

type Driver struct {
	ID                string       `json:"id" db:"id"`
	Phone             string       `json:"phone" db:"phone"`
	Name              string       `json:"name" db:"name"`
	VehicleType       VehicleType  `json:"vehicle_type" db:"vehicle_type"`
	PlateNumber       string       `json:"plate_number" db:"plate_number"`
	Status            DriverStatus `json:"status" db:"status"`
	Rating            float64      `json:"rating" db:"rating"`
	TotalTrips        int          `json:"total_trips" db:"total_trips"`
	CancellationScore float64      `json:"-" db:"cancellation_score"`
	FCMToken          string       `json:"-" db:"fcm_token"`
	CreatedAt         time.Time    `json:"created_at" db:"created_at"`
}

// ─── Trip ─────────────────────────────────────────────────────────

type TripStatus string

const (
	TripStatusSearching  TripStatus = "searching"
	TripStatusAccepted   TripStatus = "accepted"
	TripStatusEnRoute    TripStatus = "en_route"
	TripStatusArrived    TripStatus = "arrived"
	TripStatusOngoing    TripStatus = "ongoing"
	TripStatusCompleted  TripStatus = "completed"
	TripStatusCancelled  TripStatus = "cancelled"
)

type Trip struct {
	ID              string     `json:"id" db:"id"`
	PassengerID     string     `json:"passenger_id" db:"passenger_id"`
	DriverID        *string    `json:"driver_id,omitempty" db:"driver_id"`
	Status          TripStatus `json:"status" db:"status"`
	PickupLat       float64    `json:"pickup_lat" db:"pickup_lat"`
	PickupLng       float64    `json:"pickup_lng" db:"pickup_lng"`
	PickupAddress   string     `json:"pickup_address" db:"pickup_address"`
	DropoffLat      float64    `json:"dropoff_lat" db:"dropoff_lat"`
	DropoffLng      float64    `json:"dropoff_lng" db:"dropoff_lng"`
	DropoffAddress  string     `json:"dropoff_address" db:"dropoff_address"`
	BaseFare        float64    `json:"base_fare" db:"base_fare"`
	DistanceMeters  int        `json:"distance_meters" db:"distance_meters"`
	DurationSeconds int        `json:"duration_seconds" db:"duration_seconds"`
	SurgeMultiplier float64    `json:"surge_multiplier" db:"surge_multiplier"`
	FinalFare       float64    `json:"final_fare" db:"final_fare"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
}

// ─── MQTT Payloads ────────────────────────────────────────────────

type GPSPayload struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Speed   float64 `json:"speed"`
	Heading int     `json:"heading"`
	Ts      int64   `json:"ts"`
}

type OfferPayload struct {
	OrderID        string  `json:"order_id"`
	PickupAddress  string  `json:"pickup_address"`
	DropoffAddress string  `json:"dropoff_address"`
	FareEstimate   float64 `json:"fare_estimate"`
	PickupLat      float64 `json:"pickup_lat"`
	PickupLng      float64 `json:"pickup_lng"`
	DropoffLat     float64 `json:"dropoff_lat"`
	DropoffLng     float64 `json:"dropoff_lng"`
	ExpiresIn      int     `json:"expires_in"` // seconds
}

type OfferResponse struct {
	OrderID  string `json:"order_id"`
	Accepted bool   `json:"accepted"`
	Ts       int64  `json:"ts"`
}

type TripLocationPayload struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Speed   float64 `json:"speed"`
	Heading int     `json:"heading"`
}

type TripStatusPayload struct {
	Status     TripStatus `json:"status"`
	DriverName string     `json:"driver_name,omitempty"`
	Plate      string     `json:"plate,omitempty"`
	ETAMinutes int        `json:"eta_minutes,omitempty"`
}
