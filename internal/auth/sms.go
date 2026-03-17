package auth

import (
	"context"

	"github.com/rs/zerolog/log"
)

// ConsoleSMS prints OTP to stdout — used in development.
type ConsoleSMS struct{}

func (c *ConsoleSMS) Send(_ context.Context, phone, message string) error {
	log.Info().Str("phone", phone).Str("sms", message).Msg("📱 [SMS-CONSOLE]")
	return nil
}
