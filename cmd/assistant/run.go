package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"personal-assistant/internal/chat"
	"personal-assistant/internal/config"
	"personal-assistant/internal/db"
	"personal-assistant/internal/logging"
	"personal-assistant/internal/modelx"
	"personal-assistant/internal/server"
	"personal-assistant/internal/telemetry"

	"github.com/spf13/cobra"
)

func newRunCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the assistant HTTP service",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, logger, shutdown, err := loadRuntime(ctx, *configPath)
			if err != nil {
				return err
			}
			defer shutdown()

			var pool *db.Pool
			if cfg.DatabaseURL != "" {
				logger.Debug("database connect starting")
				pool, err = db.Open(ctx, cfg.DatabaseURL)
				if err != nil {
					return fmt.Errorf("open database: %w", err)
				}
				defer pool.Close()
				logger.Debug("database ping starting")
				if err := pool.Ping(ctx); err != nil {
					return fmt.Errorf("ping database: %w", err)
				}
				logger.Info("database ready")
			}

			model, providerCfg, err := modelx.New(ctx, cfg, logger)
			if err != nil {
				return fmt.Errorf("create model provider: %w", err)
			}
			logger.Info("model provider ready",
				slog.String("provider", providerCfg.Name),
				slog.String("type", providerCfg.Type),
				slog.String("model", model.Name()),
			)
			chatSvc, err := chat.NewService(chat.Config{
				AppName:       cfg.AppName,
				Instruction:   cfg.Instruction,
				Model:         model,
				ModelProvider: providerCfg.Name,
				ModelName:     model.Name(),
				Logger:        logger,
			})
			if err != nil {
				return fmt.Errorf("create chat service: %w", err)
			}

			srv := server.New(server.Config{
				Addr:    cfg.HTTPAddr,
				AppName: cfg.AppName,
				Logger:  logger,
				Chat:    chatSvc,
			})

			errCh := make(chan error, 1)
			go func() {
				logger.Info("http server starting", slog.String("addr", cfg.HTTPAddr))
				errCh <- srv.ListenAndServe()
			}()

			select {
			case <-ctx.Done():
				logger.Info("shutdown requested")
			case err := <-errCh:
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
			}

			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(stopCtx); err != nil {
				return fmt.Errorf("shutdown http server: %w", err)
			}
			logger.Info("http server stopped")
			return nil
		},
	}
}

func newMigrateCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, logger, shutdown, err := loadRuntime(ctx, *configPath)
			if err != nil {
				return err
			}
			defer shutdown()

			if cfg.DatabaseURL == "" {
				return fmt.Errorf("database_url is required for migrate")
			}
			pool, err := db.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer pool.Close()
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("ping database: %w", err)
			}

			logger.Info("migration infrastructure ready", slog.Int("registered_migrations", 0))
			logger.Info("no migrations are registered in phase 0; schema design is intentionally pending review")
			return nil
		},
	}
}

func newDoctorCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate configuration and connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, logger, shutdown, err := loadRuntime(ctx, *configPath)
			if err != nil {
				return err
			}
			defer shutdown()

			logger.Info("configuration valid", slog.String("config", *configPath))
			if cfg.DatabaseURL != "" {
				pool, err := db.Open(ctx, cfg.DatabaseURL)
				if err != nil {
					return fmt.Errorf("open database: %w", err)
				}
				defer pool.Close()
				if err := pool.Ping(ctx); err != nil {
					return fmt.Errorf("ping database: %w", err)
				}
				logger.Info("database ping ok")
			}
			logger.Info("doctor ok")
			return nil
		},
	}
}

func loadRuntime(ctx context.Context, path string) (*config.Config, *slog.Logger, func(), error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, func() {}, err
	}
	logger, err := logging.New(cfg.LogLevel)
	if err != nil {
		return nil, nil, func() {}, err
	}
	slog.SetDefault(logger)

	if cfg.PrintConfig {
		logger.Info("effective config", slog.Any("config", cfg.Redacted()))
	}

	shutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:     cfg.OTelServiceName,
		TracesEndpoint:  cfg.OTelExporterOTLPEndpoint,
		MetricsEndpoint: cfg.OTelMetricsEndpoint,
	}, logger)
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("setup telemetry: %w", err)
	}
	return cfg, logger, shutdown, nil
}
