package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/audit"
	"github.com/11DingKing/cultivar-trial-governance/internal/auth"
	"github.com/11DingKing/cultivar-trial-governance/internal/bootstrap"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/config"
	"github.com/11DingKing/cultivar-trial-governance/internal/httpapi"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
	"github.com/11DingKing/cultivar-trial-governance/internal/service"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
	"github.com/11DingKing/cultivar-trial-governance/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	database, err := store.Open(rootCtx, cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	defer database.Close()
	realClock := clock.Real{}
	ids := idgen.Random{}
	if err := bootstrap.EnsureAdmin(rootCtx, database, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword, realClock.Now()); err != nil {
		return err
	}
	authService := auth.Service{Store: database, Clock: realClock, IDs: ids, SessionTTL: cfg.SessionTTL}
	business := service.Service{
		Store: database, Clock: realClock, IDs: ids, Audit: audit.Factory{IDs: ids},
		WorkerMaxAttempts: cfg.WorkerMaxAttempts,
	}
	api := httpapi.Server{Store: database, Auth: authService, Service: business, IDs: ids, Logger: logger}
	httpServer := &http.Server{
		Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	workerHandlers := worker.BusinessHandlers{Store: database, Logger: logger}
	runner := &worker.Runner{
		Store: database, Clock: realClock, Owner: "server-worker", PollInterval: cfg.WorkerPollInterval,
		Lease: cfg.WorkerLease, JobTimeout: cfg.WorkerJobTimeout, Handlers: workerHandlers.Map(), Logger: logger,
	}
	workerErr := make(chan error, 1)
	go func() { workerErr <- runner.Run(rootCtx) }()
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "address", cfg.HTTPAddr)
		serverErr <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case err := <-workerErr:
		if !errors.Is(err, context.Canceled) {
			stop()
			return fmt.Errorf("run worker: %w", err)
		}
	case <-rootCtx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	return nil
}
