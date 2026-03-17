package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Errors
var (
	ErrInvalidToken  = errors.New("invalid or expired token")
	ErrRateLimited   = errors.New("too many OTP requests, try again later")
	ErrInvalidOTP    = errors.New("invalid or expired OTP")
	ErrUnknownRole   = errors.New("role must be 'user' or 'driver'")
)

const (
	accessTTL  = 15 * time.Minute
	RoleUser   = "user"
	RoleDriver = "driver"
)

// Claims is the JWT payload.
type Claims struct {
	UserID  string `json:"uid"`
	Role    string `json:"role"`
	TokenID string `json:"jti"` // for refresh token linking
	jwt.RegisteredClaims
}

// Service holds business logic for authentication.
type Service struct {
	repo      *Repository
	jwtSecret []byte
	smsSender SMSSender
}

// SMSSender is an interface so we can swap console/real SMS.
type SMSSender interface {
	Send(ctx context.Context, phone, message string) error
}

func NewService(repo *Repository, jwtSecret string, sms SMSSender) *Service {
	return &Service{
		repo:      repo,
		jwtSecret: []byte(jwtSecret),
		smsSender: sms,
	}
}

// ─── OTP Flow ─────────────────────────────────────────────────────

// RequestOTP generates a 6-digit OTP, stores in Redis, sends via SMS.
func (s *Service) RequestOTP(ctx context.Context, phone, role string) error {
	if role != RoleUser && role != RoleDriver {
		return ErrUnknownRole
	}

	allowed, err := s.repo.CheckRateLimit(ctx, phone, role)
	if err != nil {
		return fmt.Errorf("rate limit check: %w", err)
	}
	if !allowed {
		return ErrRateLimited
	}

	code := generateOTP()
	if err := s.repo.SaveOTP(ctx, phone, role, code); err != nil {
		return fmt.Errorf("save otp: %w", err)
	}

	msg := fmt.Sprintf("[ADIRD] Your verification code is %s. Valid for 5 minutes.", code)
	if err := s.smsSender.Send(ctx, phone, msg); err != nil {
		return fmt.Errorf("send sms: %w", err)
	}

	return nil
}

// VerifyOTP validates the OTP and issues JWT pair.
// Returns accessToken, refreshToken, userID.
func (s *Service) VerifyOTP(ctx context.Context, phone, role, code string) (access, refresh, userID string, err error) {
	if role != RoleUser && role != RoleDriver {
		return "", "", "", ErrUnknownRole
	}

	ok, err := s.repo.VerifyOTP(ctx, phone, role, code)
	if err != nil {
		return "", "", "", fmt.Errorf("verify otp: %w", err)
	}
	if !ok {
		return "", "", "", ErrInvalidOTP
	}

	// Upsert user/driver record
	switch role {
	case RoleUser:
		u, err := s.repo.FindOrCreateUser(ctx, phone)
		if err != nil {
			return "", "", "", err
		}
		userID = u.ID
	case RoleDriver:
		d, err := s.repo.FindOrCreateDriver(ctx, phone)
		if err != nil {
			return "", "", "", err
		}
		userID = d.ID
	}

	access, refresh, err = s.issueTokenPair(ctx, userID, role)
	return access, refresh, userID, err
}

// RefreshTokens validates a refresh token and issues a new pair (rotation).
func (s *Service) RefreshTokens(ctx context.Context, refreshToken string) (access, refresh string, err error) {
	// Parse refresh token to get tokenID
	tokenID, userID, role, err := s.parseRefreshToken(refreshToken)
	if err != nil {
		return "", "", ErrInvalidToken
	}

	// Validate against Redis (also deletes it — rotation)
	storedUserID, storedRole, err := s.repo.ValidateRefreshToken(ctx, tokenID)
	if err != nil {
		return "", "", err
	}
	// Double-check claims match storage
	if storedUserID != userID || storedRole != role {
		return "", "", ErrInvalidToken
	}

	return s.issueTokenPair(ctx, userID, role)
}

// ValidateAccessToken parses and validates an access JWT.
func (s *Service) ValidateAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// ─── Internal helpers ─────────────────────────────────────────────

func (s *Service) issueTokenPair(ctx context.Context, userID, role string) (access, refresh string, err error) {
	tokenID := randomHex(16)

	// Access token (short-lived)
	now := time.Now()
	accessClaims := &Claims{
		UserID:  userID,
		Role:    role,
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTTL)),
			Issuer:    "vidi",
		},
	}
	access, err = jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.jwtSecret)
	if err != nil {
		return "", "", fmt.Errorf("sign access token: %w", err)
	}

	// Refresh token (long-lived, stored in Redis)
	refreshID := randomHex(16)
	refreshClaims := &Claims{
		UserID:  userID,
		Role:    role,
		TokenID: refreshID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
			Issuer:    "vidi",
		},
	}
	refresh, err = jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.jwtSecret)
	if err != nil {
		return "", "", fmt.Errorf("sign refresh token: %w", err)
	}

	if err := s.repo.SaveRefreshToken(ctx, refreshID, userID, role); err != nil {
		return "", "", fmt.Errorf("save refresh token: %w", err)
	}

	return access, refresh, nil
}

func (s *Service) parseRefreshToken(tokenStr string) (tokenID, userID, role string, err error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return "", "", "", ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return "", "", "", ErrInvalidToken
	}
	return claims.TokenID, claims.UserID, claims.Role, nil
}

func generateOTP() string {
	b := make([]byte, 3)
	rand.Read(b)
	// Convert to 6 digits (000000–999999)
	n := (int(b[0])<<16 | int(b[1])<<8 | int(b[2])) % 1_000_000
	return fmt.Sprintf("%06d", n)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
