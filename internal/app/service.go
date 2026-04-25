package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"personal-assistant/internal/apperr"
	"personal-assistant/internal/config"
	"personal-assistant/internal/modelx"
	"personal-assistant/internal/rag"
	"personal-assistant/internal/store"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/loadmemorytool"
	"google.golang.org/genai"
)

const ragLimit = 8

type Service struct {
	cfg       config.Config
	store     *store.Store
	retriever *rag.Retriever
	runner    *runner.Runner
	logger    *slog.Logger
	tracer    trace.Tracer
	metrics   serviceMetrics
}

type serviceMetrics struct {
	chatRequests metric.Int64Counter
	chatErrors   metric.Int64Counter
	chatDuration metric.Float64Histogram
}

type ChatRequest struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message"`
}

type ChatResponse struct {
	SessionID string               `json:"session_id"`
	Answer    string               `json:"answer"`
	Events    []EventView          `json:"events"`
	RAG       rag.Result           `json:"rag"`
	Memory    []store.MemoryResult `json:"memory"`
}

type CreateSessionRequest struct {
	UserID    string         `json:"user_id"`
	SessionID string         `json:"session_id,omitempty"`
	Title     string         `json:"title,omitempty"`
	State     map[string]any `json:"state,omitempty"`
}

type SessionView struct {
	ID        string         `json:"id"`
	AppName   string         `json:"app_name"`
	UserID    string         `json:"user_id"`
	Title     string         `json:"title,omitempty"`
	State     map[string]any `json:"state,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
	Events    []EventView    `json:"events,omitempty"`
}

type EventView struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Text      string    `json:"text,omitempty"`
	Final     bool      `json:"final"`
	Timestamp time.Time `json:"timestamp"`
}

func New(ctx context.Context, cfg config.Config, store *store.Store, logger *slog.Logger) (*Service, error) {
	llm, err := modelx.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create model: %w", err)
	}
	metrics, err := newServiceMetrics()
	if err != nil {
		return nil, err
	}

	saveMemoryTool, err := newSaveMemoryTool(store)
	if err != nil {
		return nil, fmt.Errorf("create save memory tool: %w", err)
	}
	tools := []tool.Tool{
		loadmemorytool.New(),
		saveMemoryTool,
	}
	toolsets, err := newMCPToolsets(cfg.MCP)
	if err != nil {
		return nil, err
	}
	logger.Info("agent tools configured", slog.Int("tools", len(tools)), slog.Int("mcp_toolsets", len(toolsets)))

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        "personal_assistant",
		Description: "A personal assistant with session memory and user-scoped long-term memory.",
		Model:       llm,
		Instruction: instruction(),
		Tools:       tools,
		Toolsets:    toolsets,
	})
	if err != nil {
		return nil, fmt.Errorf("create llm agent: %w", err)
	}
	adkRunner, err := runner.New(runner.Config{
		AppName:           cfg.AppName,
		Agent:             rootAgent,
		SessionService:    store,
		MemoryService:     store,
		AutoCreateSession: false,
	})
	if err != nil {
		return nil, fmt.Errorf("create runner: %w", err)
	}

	retriever, err := rag.NewRetriever(store)
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:       cfg,
		store:     store,
		retriever: retriever,
		runner:    adkRunner,
		logger:    logger,
		tracer:    otel.Tracer("personal-assistant/app"),
		metrics:   metrics,
	}, nil
}

func (s *Service) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	ctx, span := s.tracer.Start(ctx, "app.Chat")
	defer span.End()
	start := time.Now()
	status := "ok"
	defer func() {
		attrs := metric.WithAttributes(
			attribute.String("model_provider", s.cfg.ModelProvider),
			attribute.String("status", status),
		)
		s.metrics.chatRequests.Add(ctx, 1, attrs)
		s.metrics.chatDuration.Record(ctx, time.Since(start).Seconds(), attrs)
		if status != "ok" {
			s.metrics.chatErrors.Add(ctx, 1, attrs)
		}
	}()

	req.UserID = strings.TrimSpace(req.UserID)
	req.Message = strings.TrimSpace(req.Message)
	if req.UserID == "" {
		status = "invalid"
		return ChatResponse{}, apperr.New(apperr.CodeInvalid, "user_id is required")
	}
	if req.Message == "" {
		status = "invalid"
		return ChatResponse{}, apperr.New(apperr.CodeInvalid, "message is required")
	}

	sessionID, created, err := s.ensureSession(ctx, req.UserID, req.SessionID, titleFromMessage(req.Message))
	if err != nil {
		status = "session_error"
		return ChatResponse{}, err
	}
	ragResult, err := s.retriever.Retrieve(ctx, s.cfg.AppName, req.UserID, sessionID, req.Message, ragLimit)
	if err != nil {
		status = "rag_error"
		return ChatResponse{}, apperr.Wrap(apperr.CodeInternal, "retrieve memory context", err)
	}

	s.logger.InfoContext(ctx, "chat started", slog.String("user_id", req.UserID), slog.String("session_id", sessionID), slog.Bool("new_session", created), slog.Int("memory_hits", len(ragResult.Memories)))
	events := []EventView{}
	answer := ""
	seq := s.runner.Run(
		ctx,
		req.UserID,
		sessionID,
		genai.NewContentFromText(req.Message, genai.RoleUser),
		agent.RunConfig{StreamingMode: agent.StreamingModeNone},
		runner.WithStateDelta(map[string]any{
			"rag_context": ragResult.Context,
		}),
	)
	for event, runErr := range seq {
		if runErr != nil {
			status = "runner_error"
			s.logger.ErrorContext(ctx, "chat run failed", slog.String("user_id", req.UserID), slog.String("session_id", sessionID), slog.Any("error", runErr))
			return ChatResponse{}, apperr.Wrap(apperr.CodeInternal, "run assistant", runErr)
		}
		view := eventView(event)
		events = append(events, view)
		if view.Text != "" {
			answer = view.Text
		}
		if event.IsFinalResponse() && view.Text != "" {
			answer = view.Text
		}
	}

	getResp, err := s.store.Get(ctx, &session.GetRequest{
		AppName:   s.cfg.AppName,
		UserID:    req.UserID,
		SessionID: sessionID,
	})
	if err != nil {
		status = "session_error"
		return ChatResponse{}, apperr.Wrap(apperr.CodeInternal, "load session after chat", err)
	}
	if err := s.store.AddSessionToMemory(ctx, getResp.Session); err != nil {
		status = "memory_error"
		return ChatResponse{}, apperr.Wrap(apperr.CodeInternal, "update long-term memory", err)
	}
	summary, err := s.store.RefreshSummary(ctx, s.cfg.AppName, req.UserID, sessionID)
	if err != nil {
		status = "summary_error"
		return ChatResponse{}, apperr.Wrap(apperr.CodeInternal, "refresh session summary", err)
	}
	ragResult.Summary = summary

	s.logger.InfoContext(ctx, "chat completed", slog.String("user_id", req.UserID), slog.String("session_id", sessionID), slog.Int("events", len(events)), slog.Int("answer_chars", len(answer)))
	return ChatResponse{
		SessionID: sessionID,
		Answer:    answer,
		Events:    events,
		RAG:       ragResult,
		Memory:    ragResult.Memories,
	}, nil
}

func newServiceMetrics() (serviceMetrics, error) {
	meter := otel.Meter("personal-assistant/app")
	chatRequests, err := meter.Int64Counter(
		"assistant.chat.requests",
		metric.WithDescription("Number of chat requests processed."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return serviceMetrics{}, fmt.Errorf("create chat requests metric: %w", err)
	}
	chatErrors, err := meter.Int64Counter(
		"assistant.chat.errors",
		metric.WithDescription("Number of chat requests that failed."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return serviceMetrics{}, fmt.Errorf("create chat errors metric: %w", err)
	}
	chatDuration, err := meter.Float64Histogram(
		"assistant.chat.duration",
		metric.WithDescription("Chat request duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return serviceMetrics{}, fmt.Errorf("create chat duration metric: %w", err)
	}
	return serviceMetrics{chatRequests: chatRequests, chatErrors: chatErrors, chatDuration: chatDuration}, nil
}

func (s *Service) CreateSession(ctx context.Context, req CreateSessionRequest) (SessionView, error) {
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		return SessionView{}, apperr.New(apperr.CodeInvalid, "user_id is required")
	}
	resp, err := s.store.Create(ctx, &session.CreateRequest{
		AppName:   s.cfg.AppName,
		UserID:    req.UserID,
		SessionID: strings.TrimSpace(req.SessionID),
		State:     req.State,
	})
	if err != nil {
		return SessionView{}, apperr.Wrap(apperr.CodeConflict, "create session", err)
	}
	if title := strings.TrimSpace(req.Title); title != "" {
		if err := s.store.UpdateSessionTitle(ctx, s.cfg.AppName, req.UserID, resp.Session.ID(), title); err != nil {
			return SessionView{}, apperr.Wrap(apperr.CodeInternal, "update session title", err)
		}
	}
	return s.GetSession(ctx, req.UserID, resp.Session.ID())
}

func (s *Service) ListSessions(ctx context.Context, userID string) ([]SessionView, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, apperr.New(apperr.CodeInvalid, "user_id is required")
	}
	resp, err := s.store.List(ctx, &session.ListRequest{AppName: s.cfg.AppName, UserID: userID})
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "list sessions", err)
	}
	views := make([]SessionView, 0, len(resp.Sessions))
	for _, sess := range resp.Sessions {
		views = append(views, sessionView(sess, false))
	}
	return views, nil
}

func (s *Service) GetSession(ctx context.Context, userID, sessionID string) (SessionView, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return SessionView{}, apperr.New(apperr.CodeInvalid, "user_id and session_id are required")
	}
	resp, err := s.store.Get(ctx, &session.GetRequest{
		AppName:   s.cfg.AppName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return SessionView{}, apperr.Wrap(apperr.CodeNotFound, "session not found", err)
	}
	return sessionView(resp.Session, true), nil
}

func (s *Service) DeleteSession(ctx context.Context, userID, sessionID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return apperr.New(apperr.CodeInvalid, "user_id and session_id are required")
	}
	if err := s.store.Delete(ctx, &session.DeleteRequest{
		AppName:   s.cfg.AppName,
		UserID:    userID,
		SessionID: sessionID,
	}); err != nil {
		return apperr.Wrap(apperr.CodeNotFound, "session not found", err)
	}
	return nil
}

func (s *Service) SaveMemory(ctx context.Context, rec store.MemoryRecord) (store.MemoryResult, error) {
	rec.AppName = s.cfg.AppName
	rec.UserID = strings.TrimSpace(rec.UserID)
	rec.Content = strings.TrimSpace(rec.Content)
	if rec.UserID == "" || rec.Content == "" {
		return store.MemoryResult{}, apperr.New(apperr.CodeInvalid, "user_id and content are required")
	}
	if rec.Kind == "" {
		rec.Kind = "semantic"
	}
	if rec.Importance <= 0 {
		rec.Importance = 0.5
	}
	if rec.Metadata == nil {
		rec.Metadata = map[string]any{}
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	id, err := s.store.SaveMemory(ctx, rec)
	if err != nil {
		return store.MemoryResult{}, apperr.Wrap(apperr.CodeInternal, "save memory", err)
	}
	return store.MemoryResult{
		ID:         id,
		Kind:       rec.Kind,
		Content:    rec.Content,
		Metadata:   rec.Metadata,
		Importance: rec.Importance,
		CreatedAt:  rec.CreatedAt,
	}, nil
}

func (s *Service) SearchMemory(ctx context.Context, userID, query string, limit int) ([]store.MemoryResult, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, apperr.New(apperr.CodeInvalid, "user_id is required")
	}
	results, err := s.store.SearchMemories(ctx, s.cfg.AppName, userID, query, limit)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "search memory", err)
	}
	return results, nil
}

func (s *Service) ensureSession(ctx context.Context, userID, sessionID, title string) (string, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		if _, err := s.store.Get(ctx, &session.GetRequest{AppName: s.cfg.AppName, UserID: userID, SessionID: sessionID}); err != nil {
			return "", false, apperr.Wrap(apperr.CodeNotFound, "session not found", err)
		}
		return sessionID, false, nil
	}
	resp, err := s.store.Create(ctx, &session.CreateRequest{AppName: s.cfg.AppName, UserID: userID})
	if err != nil {
		return "", false, apperr.Wrap(apperr.CodeInternal, "create session", err)
	}
	if title != "" {
		if err := s.store.UpdateSessionTitle(ctx, s.cfg.AppName, userID, resp.Session.ID(), title); err != nil {
			return "", false, apperr.Wrap(apperr.CodeInternal, "set session title", err)
		}
	}
	return resp.Session.ID(), true, nil
}

func instruction() string {
	return strings.TrimSpace(`
You are a concise personal assistant.
Use the conversation history, session summary, and long-term memories when they are relevant.
Do not invent memories. If memory is missing or uncertain, answer from the current user message.
When the user states a stable preference, profile detail, or durable fact worth remembering, call memory_save.
You can search memory with the load memory tool if the provided context is insufficient.

Memory context:
{rag_context?}
`)
}

func titleFromMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 64 {
		return message
	}
	return strings.TrimSpace(message[:61]) + "..."
}

func sessionView(sess session.Session, includeEvents bool) SessionView {
	view := SessionView{
		ID:        sess.ID(),
		AppName:   sess.AppName(),
		UserID:    sess.UserID(),
		UpdatedAt: sess.LastUpdateTime(),
		State:     map[string]any{},
	}
	if typed, ok := sess.(*store.Session); ok {
		view.Title = typed.Title
	}
	if sess.State() != nil {
		for key, value := range sess.State().All() {
			view.State[key] = value
		}
	}
	if includeEvents && sess.Events() != nil {
		for event := range sess.Events().All() {
			view.Events = append(view.Events, eventView(event))
		}
	}
	return view
}

func eventView(event *session.Event) EventView {
	return EventView{
		ID:        event.ID,
		Author:    event.Author,
		Text:      contentText(event.Content),
		Final:     event.IsFinalResponse(),
		Timestamp: event.Timestamp,
	}
}

func contentText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	parts := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part != nil && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
