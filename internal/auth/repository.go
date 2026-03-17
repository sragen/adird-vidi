package auth

import (
	"context"
	"fmt"
	"time"

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

// ─── OTP ──────────────────────────────────────────────────────────

const otpTTL = 5 * time.Minute
const otpRateTTL = 10 * time.Minute
const otpRateLimit = 3

// otpKey returns the Redis key for stored OTP.
func otpKey(phone, role string) string {
	return fmt.Sprintf("otp:%s:%s", role, phone)
}

// otpRateKey returns the Redis key for rate-limiting OTP requests.
func otpRateKey(phone, role string) string {
	return fmt.Sprintf("otp_rate:%s:%s", role, phone)
}

// SaveOTP stores an OTP in Redis with 5-minute TTL.
func (r *Repository) SaveOTP(ctx context.Context, phone, role, code string) error {
	return r.rdb.Set(ctx, otpKey(phone, role), code, otpTTL).Err()
}

// VerifyOTP checks the OTP and only deletes it if the code matches.
// Returns false if not found or wrong code.
func (r *Repository) VerifyOTP(ctx context.Context, phone, role, code string) (bool, error) {
	key := otpKey(phone, role)
	stored, err := r.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil // expired or never requested
	}
	if err != nil {
		return false, err
	}
	if stored != code {
		return false, nil // wrong code — keep key so user can retry
	}
	// Correct — delete it (one-time use)
	r.rdb.Del(ctx, key)
	return true, nil
}

// CheckRateLimit returns true if the phone is allowed to request an OTP.
// Increments counter; counter expires after otpRateTTL.
func (r *Repository) CheckRateLimit(ctx context.Context, phone, role string) (bool, error) {
	key := otpRateKey(phone, role)
	count, err := r.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		r.rdb.Expire(ctx, key, otpRateTTL)
	}
	return count <= otpRateLimit, nil
}

// ─── Refresh Tokens ───────────────────────────────────────────────

const refreshTTL = 7 * 24 * time.Hour

func refreshKey(tokenID string) string {
	return "refresh:" + tokenID
}

// SaveRefreshToken stores a refresh token ID in Redis.
func (r *Repository) SaveRefreshToken(ctx context.Context, tokenID, userID, role string) error {
	return r.rdb.Set(ctx, refreshKey(tokenID), userID+":"+role, refreshTTL).Err()
}

// ValidateRefreshToken returns userID and role if the token exists, then deletes it (rotation).
func (r *Repository) ValidateRefreshToken(ctx context.Context, tokenID string) (userID, role string, err error) {
	val, err := r.rdb.GetDel(ctx, refreshKey(tokenID)).Result()
	if err == redis.Nil {
		return "", "", ErrInvalidToken
	}
	if err != nil {
		return "", "", err
	}
	// val = "userID:role"
	for i := len(val) - 1; i >= 0; i-- {
		if val[i] == ':' {
			return val[:i], val[i+1:], nil
		}
	}
	return "", "", ErrInvalidToken
}

// RevokeRefreshToken deletes a refresh token (logout).
func (r *Repository) RevokeRefreshToken(ctx context.Context, tokenID string) error {
	return r.rdb.Del(ctx, refreshKey(tokenID)).Err()
}

// ─── User / Driver Lookup ─────────────────────────────────────────

// FindOrCreateUser upserts a user by phone and returns their record.
func (r *Repository) FindOrCreateUser(ctx context.Context, phone string) (*shared.User, error) {
	u := &shared.User{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO users (phone)
		VALUES ($1)
		ON CONFLICT (phone) DO UPDATE SET updated_at = NOW()
		RETURNING id, phone, name, created_at
	`, phone).Scan(&u.ID, &u.Phone, &u.Name, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

// FindOrCreateDriver upserts a driver by phone and returns their record.
// vehicle_type and plate_number get placeholder values on first creation;
// the driver completes their profile in a separate onboarding step.
func (r *Repository) FindOrCreateDriver(ctx context.Context, phone string) (*shared.Driver, error) {
	d := &shared.Driver{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO drivers (phone, vehicle_type, plate_number)
		VALUES ($1, 'motor', 'PENDING')
		ON CONFLICT (phone) DO UPDATE SET updated_at = NOW()
		RETURNING id, phone, name, vehicle_type, plate_number, status, rating, total_trips, created_at
	`, phone).Scan(
		&d.ID, &d.Phone, &d.Name, &d.VehicleType,
		&d.PlateNumber, &d.Status, &d.Rating, &d.TotalTrips, &d.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert driver: %w", err)
	}
	return d, nil
}
