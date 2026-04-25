package observability

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

func NewLogger(level slog.Level) *slog.Logger {
	handler := &traceContextHandler{
		Handler: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource:   true,
			Level:       level,
			ReplaceAttr: replaceSourceAttr,
		}),
	}
	return slog.New(handler)
}

type traceContextHandler struct {
	slog.Handler
}

func (h *traceContextHandler) Handle(ctx context.Context, record slog.Record) error {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, record)
}

func (h *traceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceContextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *traceContextHandler) WithGroup(name string) slog.Handler {
	return &traceContextHandler{Handler: h.Handler.WithGroup(name)}
}

func replaceSourceAttr(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key != slog.SourceKey {
		return attr
	}
	source, ok := attr.Value.Any().(*slog.Source)
	if !ok || source == nil {
		return attr
	}
	file := relativeSourceFile(source.File)
	if file == "" {
		return slog.Attr{}
	}
	return slog.Group(
		slog.SourceKey,
		slog.String("file", file),
		slog.Int("line", source.Line),
	)
}

func relativeSourceFile(file string) string {
	if file == "" {
		return ""
	}
	normalized := filepath.ToSlash(file)
	for _, marker := range []string{
		"/personal-assistant/",
		"/personal-assistant-go-build/",
	} {
		if idx := strings.LastIndex(normalized, marker); idx >= 0 {
			return normalized[idx+len(marker):]
		}
	}
	if strings.HasPrefix(normalized, "personal-assistant/") {
		return strings.TrimPrefix(normalized, "personal-assistant/")
	}
	return filepath.Base(file)
}
