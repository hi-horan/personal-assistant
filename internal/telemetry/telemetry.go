package telemetry

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	ServiceName     string
	TracesEndpoint  string
	MetricsEndpoint string
}

func Setup(ctx context.Context, cfg Config, logger *slog.Logger) (func(), error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", cfg.ServiceName)),
	)
	if err != nil {
		return nil, err
	}

	var shutdowns []func(context.Context) error
	if cfg.TracesEndpoint != "" {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.TracesEndpoint))
		if err != nil {
			return nil, err
		}
		provider := trace.NewTracerProvider(
			trace.WithBatcher(exp),
			trace.WithResource(res),
		)
		otel.SetTracerProvider(provider)
		shutdowns = append(shutdowns, provider.Shutdown)
		logger.Info("otel traces enabled", slog.String("endpoint", cfg.TracesEndpoint))
	}

	if cfg.MetricsEndpoint != "" {
		exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(cfg.MetricsEndpoint))
		if err != nil {
			return nil, err
		}
		provider := metric.NewMeterProvider(
			metric.WithReader(metric.NewPeriodicReader(exp)),
			metric.WithResource(res),
		)
		otel.SetMeterProvider(provider)
		shutdowns = append(shutdowns, provider.Shutdown)
		logger.Info("otel metrics enabled", slog.String("endpoint", cfg.MetricsEndpoint))
	}

	return func() {
		for i := len(shutdowns) - 1; i >= 0; i-- {
			if err := shutdowns[i](context.Background()); err != nil && !errors.Is(err, context.Canceled) {
				logger.Warn("otel shutdown failed", slog.Any("error", err))
			}
		}
	}, nil
}
