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

	"personal-assistant/internal/app"
	"personal-assistant/internal/config"
	"personal-assistant/internal/embedding"
	"personal-assistant/internal/httpapi"
	"personal-assistant/internal/observability"
	"personal-assistant/internal/store"
)

func run(ctx context.Context, configPath string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		slog.Error("load config", "path", configPath, "error", err)
		return err
	}
	logger := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	if cfg.PrintConfig {
		if err := config.PrintEffective(os.Stdout, cfg); err != nil {
			logger.Error("print config", "error", err)
			return err
		}
	}
	logger.Info("config loaded", "path", configPath, "app", cfg.AppName)

	shutdownTracer, err := observability.InitTracer(ctx, cfg.OTelServiceName, cfg.OTelEndpoint, logger)
	if err != nil {
		logger.Error("init telemetry", "error", err)
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracer(shutdownCtx); err != nil {
			logger.Error("shutdown telemetry", "error", err)
		}
	}()
	shutdownMeter, err := observability.InitMeter(ctx, cfg.OTelServiceName, cfg.OTelMetricsEndpoint, logger)
	if err != nil {
		logger.Error("init metrics", "error", err)
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownMeter(shutdownCtx); err != nil {
			logger.Error("shutdown metrics", "error", err)
		}
	}()

	embedder, err := embedding.New(ctx, cfg)
	if err != nil {
		logger.Error("create embedding provider", "error", err)
		return err
	}
	logger.Info("embedding provider configured", "provider", embedder.Name(), "dimension", embedder.Dimension())

	storeSvc, err := store.New(ctx, cfg.DatabaseURL, logger, embedder)
	if err != nil {
		logger.Error("connect store", "error", err)
		return err
	}
	defer storeSvc.Close()

	if err := storeSvc.RunMigrations(ctx); err != nil {
		logger.Error("run migrations", "error", err)
		return err
	}

	appSvc, err := app.New(ctx, cfg, storeSvc, logger)
	if err != nil {
		logger.Error("wire app", "error", err)
		return err
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(appSvc, storeSvc, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", cfg.HTTPAddr, "model_provider", cfg.ModelProvider)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil {
			logger.Error("http server failed", "error", err)
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown failed", "error", err)
		return err
	}
	logger.Info("shutdown complete")
	return nil
}
