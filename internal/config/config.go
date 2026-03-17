package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Redis    RedisConfig
	MQTT     MQTTConfig
	OSRM     OSRMConfig
	JWT      JWTConfig
	SMS      SMSConfig
}

type AppConfig struct {
	Env  string
	Port string
}

type DatabaseConfig struct {
	URL string
}

type RedisConfig struct {
	URL string
}

type MQTTConfig struct {
	BrokerURL    string
	ClientID     string
	Username     string
	Password     string
	CleanSession bool
}

type OSRMConfig struct {
	URL string
}

type JWTConfig struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type SMSConfig struct {
	Provider      string // "console" | "vonage"
	VonageKey     string
	VonageSecret  string
}

func Load() (*Config, error) {
	// Load .env.dev in development
	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = ".env.dev"
	}
	_ = godotenv.Load(envFile) // ignore error — env vars may be set externally

	cfg := &Config{
		App: AppConfig{
			Env:  getEnv("APP_ENV", "development"),
			Port: getEnv("APP_PORT", "8080"),
		},
		Database: DatabaseConfig{
			URL: requireEnv("DATABASE_URL"),
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL", "redis://localhost:6379"),
		},
		MQTT: MQTTConfig{
			BrokerURL:    getEnv("MQTT_BROKER_URL", "tcp://localhost:1883"),
			ClientID:     getEnv("MQTT_CLIENT_ID", "vidi-dev-01"),
			Username:     getEnv("MQTT_USERNAME", ""),
			Password:     getEnv("MQTT_PASSWORD", ""),
			CleanSession: false,
		},
		OSRM: OSRMConfig{
			URL: getEnv("OSRM_URL", "http://localhost:5000"),
		},
		JWT: JWTConfig{
			Secret:     requireEnv("JWT_SECRET"),
			AccessTTL:  parseDuration(getEnv("JWT_ACCESS_TTL", "15m")),
			RefreshTTL: parseDuration(getEnv("JWT_REFRESH_TTL", "720h")),
		},
		SMS: SMSConfig{
			Provider:     getEnv("SMS_PROVIDER", "console"),
			VonageKey:    getEnv("VONAGE_API_KEY", ""),
			VonageSecret: getEnv("VONAGE_API_SECRET", ""),
		},
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 15 * time.Minute
	}
	return d
}
