package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestAppendEventRejectsNilInputsBeforeDatabaseUse(t *testing.T) {
	svc := &Store{tracer: noop.NewTracerProvider().Tracer("test")}

	if err := svc.AppendEvent(context.Background(), nil, &session.Event{}); err == nil || !strings.Contains(err.Error(), "session is nil") {
		t.Fatalf("AppendEvent(nil session) error = %v, want session is nil", err)
	}
	if err := svc.AppendEvent(context.Background(), &Session{}, nil); err == nil || !strings.Contains(err.Error(), "event is nil") {
		t.Fatalf("AppendEvent(nil event) error = %v, want event is nil", err)
	}
}

func TestAppendEventIgnoresPartialEvents(t *testing.T) {
	svc := &Store{tracer: noop.NewTracerProvider().Tracer("test")}
	err := svc.AppendEvent(context.Background(), &Session{
		IDVal:         "1700000000000000",
		AppNameVal:    "app",
		UserIDVal:     "u1",
		StateVal:      State{},
		EventsVal:     Events{},
		LastUpdateVal: time.Now(),
	}, &session.Event{
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText("partial", genai.RoleModel),
			Partial: true,
		},
	})
	if err != nil {
		t.Fatalf("AppendEvent(partial) error = %v, want nil", err)
	}
}

func TestTrimRunes(t *testing.T) {
	if got := trimRunes("assistant", 50); got != "assistant" {
		t.Fatalf("trimRunes(short) = %q, want assistant", got)
	}
	if got := trimRunes("你好世界", 2); got != "你好" {
		t.Fatalf("trimRunes(unicode) = %q, want 你好", got)
	}
}
