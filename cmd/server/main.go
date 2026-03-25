package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"adird.id/vidi/internal/admin"
	"adird.id/vidi/internal/auth"
	"adird.id/vidi/internal/config"
	drivermod "adird.id/vidi/internal/driver"
	mqttclient "adird.id/vidi/internal/mqtt"
	"adird.id/vidi/internal/order"
	passengermod "adird.id/vidi/internal/passenger"
	"adird.id/vidi/internal/routing"
)

func main() {
	// ─── Logger ────────────────────────────────────────────────────
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"})

	// ─── Config ────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	if cfg.App.Env == "development" {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Info().Msg("🚀 VIDI starting in development mode")
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ─── PostgreSQL ────────────────────────────────────────────────
	log.Info().Str("url", maskURL(cfg.Database.URL)).Msg("connecting to PostgreSQL...")
	dbPool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("postgres connection failed")
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("postgres ping failed — is the DB running? (make dev-up)")
	}
	log.Info().Msg("✅ PostgreSQL connected")

	// ─── Redis ─────────────────────────────────────────────────────
	log.Info().Str("url", cfg.Redis.URL).Msg("connecting to Redis...")
	redisOpts, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("redis URL parse failed")
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("redis ping failed — is Redis running? (make dev-up)")
	}
	log.Info().Msg("✅ Redis connected")

	// ─── MQTT ──────────────────────────────────────────────────────
	log.Info().Str("broker", cfg.MQTT.BrokerURL).Msg("connecting to EMQX...")
	mqttClient, err := mqttclient.NewClient(cfg.MQTT, rdb)
	if err != nil {
		log.Fatal().Err(err).Msg("MQTT connect failed — is EMQX running? (make dev-up)")
	}
	defer mqttClient.Disconnect()
	log.Info().Msg("✅ EMQX (MQTT) connected")

	// ─── HTTP Router ───────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Logger middleware (skip health check)
	r.Use(middleware.RequestLogger(&middleware.DefaultLogFormatter{
		Logger:  &log.Logger,
		NoColor: false,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","service":"vidi","env":"%s"}`, cfg.App.Env)
	})

	// ─── Auth ──────────────────────────────────────────────────────
	authRepo := auth.NewRepository(dbPool, rdb)
	authSvc := auth.NewService(authRepo, cfg.JWT.Secret, &auth.ConsoleSMS{})
	authHandler := auth.NewHandler(authSvc)

	// ─── Driver ────────────────────────────────────────────────────
	driverRepo := drivermod.NewRepository(dbPool, rdb)
	driverHandler := drivermod.NewHandler(driverRepo)

	// ─── OSRM (optional — falls back to haversine if not running) ──
	osrmClient := routing.NewClient(cfg.OSRM.URL)

	// ─── Order + Dispatch ──────────────────────────────────────────
	orderRepo := order.NewRepository(dbPool)
	dispatcher := order.NewDispatcher(rdb, orderRepo, mqttClient)
	surgeService := order.NewSurgeService(rdb)
	orderHandler := order.NewHandler(orderRepo, dispatcher, osrmClient, surgeService)
	ratingHandler := order.NewRatingHandler(order.NewRatingRepo(dbPool), orderRepo)
	tripStateRepo := order.NewTripStateRepo(dbPool, rdb)
	tripHandler := order.NewTripStateHandler(tripStateRepo, mqttClient)

	// ─── Passenger ─────────────────────────────────────────────────
	passengerRepo := passengermod.NewRepository(dbPool)
	passengerHandler := passengermod.NewHandler(passengerRepo)

	// ─── Admin ─────────────────────────────────────────────────────
	adminRepo := admin.NewRepository(dbPool)
	adminHandler := admin.NewHandler(adminRepo)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"message":"pong from VIDI"}`)
		})

		r.Mount("/auth", authHandler.Routes())
		r.Mount("/admin", adminHandler.Routes(authSvc))
		r.Mount("/driver", driverHandler.Routes(authSvc))
		r.Mount("/passenger", passengerHandler.Routes(authSvc))
		r.Mount("/order", orderHandler.Routes(authSvc))
		r.Mount("/trip", tripHandler.Routes(authSvc))
		ratingHandler.RegisterRoutes(r, authSvc)

		// /me — useful for mobile to confirm token validity
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(authSvc))
			r.Get("/me", func(w http.ResponseWriter, r *http.Request) {
				claims, _ := auth.ClaimsFromContext(r.Context())
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"user_id":%q,"role":%q}`, claims.UserID, claims.Role)
			})
		})
	})

	// Log registered routes
	log.Debug().Msg("registered routes:")
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		log.Debug().Str("method", method).Str("route", route).Send()
		return nil
	}); err != nil {
		log.Warn().Err(err).Msg("route walk error")
	}

	// ─── HTTP Server ───────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.App.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server
	serverErr := make(chan error, 1)
	go func() {
		log.Info().
			Str("port", cfg.App.Port).
			Msg("✅ VIDI listening — http://localhost:" + cfg.App.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// ─── Graceful Shutdown ─────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatal().Err(err).Msg("server error")
	case sig := <-quit:
		log.Info().Str("signal", sig.String()).Msg("shutting down...")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
	log.Info().Msg("VIDI stopped cleanly")
}

// maskURL hides password in connection strings for logging.
func maskURL(url string) string {
	if len(url) > 20 {
		return url[:20] + "***"
	}
	return "***"
}
