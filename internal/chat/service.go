package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

const defaultUserID = "local"

type Config struct {
	AppName       string
	Instruction   string
	Model         model.LLM
	ModelProvider string
	ModelName     string
	Logger        *slog.Logger
}

type Service struct {
	appName       string
	userID        string
	modelProvider string
	modelName     string
	logger        *slog.Logger
	sessionSvc    session.Service
	runner        *runner.Runner

	mu       sync.RWMutex
	metadata map[string]SessionMetadata
}

type SessionMetadata struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	Partial   bool      `json:"partial"`
	CreatedAt time.Time `json:"created_at"`
}

type ChatResult struct {
	SessionID string    `json:"session_id"`
	Message   Message   `json:"message"`
	Messages  []Message `json:"messages"`
}

type StreamEvent struct {
	Type    string   `json:"type"`
	Message *Message `json:"message,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func NewService(cfg Config) (*Service, error) {
	if cfg.AppName == "" {
		cfg.AppName = "personal-assistant"
	}
	if cfg.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        "assistant",
		Description: "Personal chat assistant",
		Model:       cfg.Model,
		Instruction: cfg.Instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("create adk llm agent: %w", err)
	}

	sessionSvc := session.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:           cfg.AppName,
		Agent:             rootAgent,
		SessionService:    sessionSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create adk runner: %w", err)
	}

	return &Service{
		appName:       cfg.AppName,
		userID:        defaultUserID,
		modelProvider: cfg.ModelProvider,
		modelName:     cfg.ModelName,
		logger:        cfg.Logger,
		sessionSvc:    sessionSvc,
		runner:        r,
		metadata:      make(map[string]SessionMetadata),
	}, nil
}

func (s *Service) CreateSession(ctx context.Context, title string) (SessionMetadata, error) {
	sessionID := uuid.NewString()
	if strings.TrimSpace(title) == "" {
		title = "新会话"
	}
	resp, err := s.sessionSvc.Create(ctx, &session.CreateRequest{
		AppName:   s.appName,
		UserID:    s.userID,
		SessionID: sessionID,
	})
	if err != nil {
		return SessionMetadata{}, fmt.Errorf("create session: %w", err)
	}
	now := resp.Session.LastUpdateTime()
	meta := SessionMetadata{
		ID:        sessionID,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	s.metadata[sessionID] = meta
	s.mu.Unlock()
	return meta, nil
}

func (s *Service) ListSessions(ctx context.Context) ([]SessionMetadata, error) {
	resp, err := s.sessionSvc.List(ctx, &session.ListRequest{AppName: s.appName, UserID: s.userID})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	out := make([]SessionMetadata, 0, len(resp.Sessions))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range resp.Sessions {
		meta := s.metadata[sess.ID()]
		if meta.ID == "" {
			meta = SessionMetadata{ID: sess.ID(), Title: "新会话", CreatedAt: sess.LastUpdateTime()}
		}
		meta.UpdatedAt = sess.LastUpdateTime()
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Service) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	sess, err := s.getSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return messagesFromSession(sess), nil
}

func (s *Service) Chat(ctx context.Context, sessionID, text string) (ChatResult, error) {
	var final Message
	for ev := range s.ChatStream(ctx, sessionID, text) {
		if ev.Error != "" {
			return ChatResult{}, errors.New(ev.Error)
		}
		if ev.Message != nil && !ev.Message.Partial && ev.Message.Role == "assistant" {
			final = *ev.Message
		}
	}
	messages, err := s.Messages(ctx, sessionID)
	if err != nil {
		return ChatResult{}, err
	}
	if final.ID == "" {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" {
				final = messages[i]
				break
			}
		}
	}
	return ChatResult{SessionID: sessionID, Message: final, Messages: messages}, nil
}

func (s *Service) ChatStream(ctx context.Context, sessionID, text string) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		if strings.TrimSpace(sessionID) == "" {
			ch <- StreamEvent{Type: "error", Error: "session_id is required"}
			return
		}
		if strings.TrimSpace(text) == "" {
			ch <- StreamEvent{Type: "error", Error: "message is required"}
			return
		}
		if err := s.ensureMetadata(ctx, sessionID, text); err != nil {
			ch <- StreamEvent{Type: "error", Error: err.Error()}
			return
		}

		s.logger.Debug("adk chat run starting", slog.String("session_id", sessionID))
		content := genai.NewContentFromText(text, genai.RoleUser)
		for event, err := range s.runner.Run(ctx, s.userID, sessionID, content, agent.RunConfig{StreamingMode: agent.StreamingModeSSE}) {
			if err != nil {
				ch <- StreamEvent{Type: "error", Error: err.Error()}
				return
			}
			if event == nil {
				continue
			}
			msg := messageFromEvent(sessionID, event)
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			ch <- StreamEvent{Type: "message", Message: &msg}
		}
		s.touchSession(sessionID)
		s.logger.Debug("adk chat run completed", slog.String("session_id", sessionID))
	}()
	return ch
}

func (s *Service) ensureMetadata(ctx context.Context, sessionID, firstMessage string) error {
	if _, err := s.getSession(ctx, sessionID); err == nil {
		s.mu.Lock()
		meta := s.metadata[sessionID]
		if meta.ID == "" {
			meta = SessionMetadata{ID: sessionID, Title: titleFromMessage(firstMessage), CreatedAt: time.Now()}
		}
		meta.UpdatedAt = time.Now()
		s.metadata[sessionID] = meta
		s.mu.Unlock()
		return nil
	}
	_, err := s.sessionSvc.Create(ctx, &session.CreateRequest{
		AppName:   s.appName,
		UserID:    s.userID,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	now := time.Now()
	s.mu.Lock()
	s.metadata[sessionID] = SessionMetadata{ID: sessionID, Title: titleFromMessage(firstMessage), CreatedAt: now, UpdatedAt: now}
	s.mu.Unlock()
	return nil
}

func (s *Service) getSession(ctx context.Context, sessionID string) (session.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	resp, err := s.sessionSvc.Get(ctx, &session.GetRequest{
		AppName:   s.appName,
		UserID:    s.userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	return resp.Session, nil
}

func (s *Service) touchSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := s.metadata[sessionID]
	if meta.ID == "" {
		meta.ID = sessionID
		meta.Title = "新会话"
		meta.CreatedAt = time.Now()
	}
	meta.UpdatedAt = time.Now()
	s.metadata[sessionID] = meta
}

func messagesFromSession(sess session.Session) []Message {
	events := sess.Events()
	out := make([]Message, 0, events.Len())
	for event := range events.All() {
		msg := messageFromEvent(sess.ID(), event)
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func messageFromEvent(sessionID string, event *session.Event) Message {
	role := "assistant"
	if event.Author == "user" {
		role = "user"
	}
	return Message{
		ID:        event.ID,
		SessionID: sessionID,
		Role:      role,
		Author:    event.Author,
		Content:   textFromContent(event.Content),
		Partial:   event.Partial,
		CreatedAt: event.Timestamp,
	}
}

func textFromContent(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range content.Parts {
		if part == nil || part.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(part.Text)
	}
	return b.String()
}

func titleFromMessage(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return "新会话"
	}
	runes := []rune(text)
	if len(runes) > 24 {
		return string(runes[:24])
	}
	return text
}
