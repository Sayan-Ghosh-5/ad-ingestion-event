package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/cache"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/event"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/httpapi"
	mw "github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/middleware"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/storage"
	"github.com/Sayan-Ghosh-5/ad-ingestion-event/internal/worker"
	"github.com/go-chi/chi/v5"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dsn := env("DATABASE_URL", "postgres://ads:ads@localhost:5432/ads?sslmode=disable")
	redisAddr := env("REDIS_ADDR", "localhost:6379")
	httpAddr := env("HTTP_ADDR", ":8080")

	// Root context cancelled on SIGINT/SIGTERM -> graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- storage ---
	pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
	store, err := storage.NewPostgres(pctx, dsn)
	pcancel()
	if err != nil {
		log.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	// --- cache ---
	c := cache.New(redisAddr, 30*time.Second)
	if err := c.Ping(ctx); err != nil {
		log.Warn("redis ping failed; continuing without cache warmth", "err", err)
	}
	defer c.Close()

	// --- worker pool ---
	cfg := worker.DefaultConfig()
	cfg.Workers = envInt("WORKERS", cfg.Workers)
	cfg.BatchSize = envInt("BATCH_SIZE", cfg.BatchSize)
	pool := worker.New(cfg, store, log)
	pool.Start(ctx)
	log.Info("worker pool started", "workers", cfg.Workers, "batch", cfg.BatchSize, "queue", cfg.QueueSize)

	// --- http ---
	api := &httpapi.API{
		Pool:  pool,
		Store: store,
		Cache: c,
		Log:   log,
		Validation: event.ValidationOptions{
			RequireUserIDForConversion: env("REQUIRE_USER_ID_FOR_CONVERSION", "false") == "true",
		},
		UseRollups: env("USE_ROLLUPS", "false") == "true",
	}

	// Background rollup refresher: keeps hourly_campaign_metrics fresh so that
	// metric reads stay fast regardless of raw event volume.
	if api.UseRollups {
		interval := time.Duration(envInt("ROLLUP_REFRESH_SEC", 60)) * time.Second
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					if err := store.RefreshRollups(rctx); err != nil {
						log.Warn("rollup refresh failed", "err", err)
					}
					cancel()
				}
			}
		}()
		log.Info("rollup refresher started", "interval", interval)
	}
	rateRPS := envInt("RATE_RPS", 1000)
	rateBurst := envInt("RATE_BURST", 2000)
	limiter := mw.NewIPLimiter(
		float64(rateRPS),
		rateBurst,
		time.Duration(envInt("RATE_IDLE_TTL_SEC", 600))*time.Second,
	)
	defer limiter.Stop()
	log.Info("rate limiter configured", "rps_per_ip", rateRPS, "burst", rateBurst)

	r := chi.NewRouter()
	r.Use(mw.Logger(log))
	r.Use(limiter.Middleware)
	api.Routes(r)

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("http server listening", "addr", httpAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	// Stop accepting new requests.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", "err", err)
	}

	// Workers already see ctx.Done(); wait for them to drain the queue.
	pool.Stop()
	log.Info("workers drained", "stored", pool.Stored(), "dropped", pool.Dropped())
	log.Info("bye")
}
