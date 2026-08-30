// Command coordinator is the control plane: OpenAI-compatible API, provider WebSocket hub,
// scheduler, and encrypted relay.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/api"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/jobs"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

func main() {
	level := slog.LevelInfo
	if os.Getenv("SC_DEBUG") != "" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	var st store.Store
	if cfg.DatabaseURL != "" {
		pg, err := store.NewPG(context.Background(), cfg.DatabaseURL, log,
			cfg.ConsumerAPIKeys, cfg.ProviderRegistrationTokens)
		if err != nil {
			log.Error("database", "err", err)
			os.Exit(1)
		}
		if err := pg.Migrate(context.Background()); err != nil {
			log.Error("migrate", "err", err)
			os.Exit(1)
		}
		log.Info("persistence: postgres")
		st = pg
	} else {
		log.Info("persistence: in-memory (set SC_DATABASE_URL for durable keys + usage)")
		st = store.NewMem(cfg.ConsumerAPIKeys, cfg.ProviderRegistrationTokens)
	}
	defer st.Close()

	reg := registry.New()
	job := jobs.New()
	srv := api.NewServer(cfg, reg, job, st, log)

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: SSE responses are long-lived.
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("coordinator listening", "addr", cfg.HTTPAddr, "heartbeat_s", cfg.HeartbeatSeconds)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
