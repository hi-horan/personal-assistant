package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"personal-assistant/internal/store"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Retriever struct {
	store   *store.Store
	tracer  trace.Tracer
	metrics ragMetrics
}

type ragMetrics struct {
	retrievals metric.Int64Counter
	duration   metric.Float64Histogram
	results    metric.Int64Histogram
}

type Result struct {
	Context  string
	Memories []store.MemoryResult
	Summary  string
}

func NewRetriever(store *store.Store) (*Retriever, error) {
	metrics, err := newRAGMetrics()
	if err != nil {
		return nil, fmt.Errorf("create rag metrics: %w", err)
	}
	return &Retriever{
		store:   store,
		tracer:  otel.Tracer("personal-assistant/rag"),
		metrics: metrics,
	}, nil
}

func (r *Retriever) Retrieve(ctx context.Context, appName, userID, sessionID, query string, limit int) (Result, error) {
	ctx, span := r.tracer.Start(ctx, "rag.Retrieve")
	defer span.End()
	start := time.Now()
	status := "ok"
	defer func() {
		attrs := metric.WithAttributes(attribute.String("status", status))
		r.metrics.retrievals.Add(ctx, 1, attrs)
		r.metrics.duration.Record(ctx, time.Since(start).Seconds(), attrs)
	}()

	summary, err := r.store.GetSummary(ctx, appName, userID, sessionID)
	if err != nil {
		status = "summary_error"
		return Result{}, err
	}
	memories, err := r.store.SearchMemories(ctx, appName, userID, query, limit)
	if err != nil {
		status = "search_error"
		return Result{}, err
	}
	r.metrics.results.Record(ctx, int64(len(memories)))
	return Result{
		Context:  format(summary, memories),
		Memories: memories,
		Summary:  summary,
	}, nil
}

func newRAGMetrics() (ragMetrics, error) {
	meter := otel.Meter("personal-assistant/rag")
	retrievals, err := meter.Int64Counter(
		"assistant.rag.retrievals",
		metric.WithDescription("Number of RAG retrievals."),
		metric.WithUnit("{retrieval}"),
	)
	if err != nil {
		return ragMetrics{}, err
	}
	duration, err := meter.Float64Histogram(
		"assistant.rag.duration",
		metric.WithDescription("RAG retrieval duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return ragMetrics{}, err
	}
	results, err := meter.Int64Histogram(
		"assistant.rag.results",
		metric.WithDescription("Number of memory results included in RAG context."),
		metric.WithUnit("{result}"),
	)
	if err != nil {
		return ragMetrics{}, err
	}
	return ragMetrics{retrievals: retrievals, duration: duration, results: results}, nil
}

func format(summary string, memories []store.MemoryResult) string {
	sections := []string{}
	if strings.TrimSpace(summary) != "" {
		sections = append(sections, "Session summary:\n"+summary)
	}
	if len(memories) > 0 {
		lines := make([]string, 0, len(memories))
		for i, memory := range memories {
			lines = append(lines, fmt.Sprintf("%d. [%s] %s", i+1, memory.Kind, memory.Content))
		}
		sections = append(sections, "Relevant long-term memories:\n"+strings.Join(lines, "\n"))
	}
	if len(sections) == 0 {
		return "No relevant memory found."
	}
	return strings.Join(sections, "\n\n")
}
