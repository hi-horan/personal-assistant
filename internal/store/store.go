package store

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"personal-assistant/internal/embedding"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	pool     *pgxpool.Pool
	logger   *slog.Logger
	tracer   trace.Tracer
	metrics  storeMetrics
	embedder embedding.Provider
	ids      IDAllocator
	// Maps ADK-provided invocation strings, often UUIDs, to backend bigint IDs.
	invocations sync.Map
}

type storeMetrics struct {
	sessionEventsAppended metric.Int64Counter
	memoryWrites          metric.Int64Counter
	memorySearches        metric.Int64Counter
	memorySearchResults   metric.Int64Histogram
}

var _ session.Service = (*Store)(nil)
var _ memory.Service = (*Store)(nil)

func New(ctx context.Context, databaseURL string, logger *slog.Logger, embedder embedding.Provider) (*Store, error) {
	if embedder == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	metrics, err := newStoreMetrics()
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{
		pool:     pool,
		logger:   logger,
		tracer:   otel.Tracer("personal-assistant/store"),
		metrics:  metrics,
		embedder: embedder,
		ids:      NewMicrosecondIDAllocator(),
	}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) RunMigrations(ctx context.Context) error {
	ctx, span := s.tracer.Start(ctx, "store.RunMigrations")
	defer span.End()

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		path := "migrations/" + entry.Name()
		sql, err := migrationFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		if _, err := s.pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("run migration %s: %w", path, err)
		}
		s.logger.InfoContext(ctx, "migration applied", slog.String("migration", entry.Name()))
	}
	return nil
}

func newStoreMetrics() (storeMetrics, error) {
	meter := otel.Meter("personal-assistant/store")
	sessionEventsAppended, err := meter.Int64Counter(
		"assistant.session.events.appended",
		metric.WithDescription("Number of ADK session events appended."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return storeMetrics{}, fmt.Errorf("create session events appended metric: %w", err)
	}
	memoryWrites, err := meter.Int64Counter(
		"assistant.memory.writes",
		metric.WithDescription("Number of long-term memories written."),
		metric.WithUnit("{memory}"),
	)
	if err != nil {
		return storeMetrics{}, fmt.Errorf("create memory writes metric: %w", err)
	}
	memorySearches, err := meter.Int64Counter(
		"assistant.memory.searches",
		metric.WithDescription("Number of memory searches."),
		metric.WithUnit("{search}"),
	)
	if err != nil {
		return storeMetrics{}, fmt.Errorf("create memory searches metric: %w", err)
	}
	memorySearchResults, err := meter.Int64Histogram(
		"assistant.memory.search.results",
		metric.WithDescription("Number of results returned by memory searches."),
		metric.WithUnit("{result}"),
	)
	if err != nil {
		return storeMetrics{}, fmt.Errorf("create memory search results metric: %w", err)
	}
	return storeMetrics{
		sessionEventsAppended: sessionEventsAppended,
		memoryWrites:          memoryWrites,
		memorySearches:        memorySearches,
		memorySearchResults:   memorySearchResults,
	}, nil
}
