package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"personal-assistant/internal/app"
	"personal-assistant/internal/apperr"
	"personal-assistant/internal/store"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Server struct {
	app     *app.Service
	store   *store.Store
	logger  *slog.Logger
	mux     *http.ServeMux
	metrics serverMetrics
}

type serverMetrics struct {
	errors metric.Int64Counter
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    apperr.Code `json:"code"`
	Message string      `json:"message"`
}

type saveMemoryRequest struct {
	UserID     string         `json:"user_id"`
	Kind       string         `json:"kind"`
	Content    string         `json:"content"`
	Metadata   map[string]any `json:"metadata"`
	Importance float64        `json:"importance"`
}

func New(appSvc *app.Service, store *store.Store, logger *slog.Logger) http.Handler {
	server := &Server{
		app:    appSvc,
		store:  store,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	server.metrics = newServerMetrics()
	server.routes()
	return otelhttp.NewHandler(server.mux, "http.server")
}

func newServerMetrics() serverMetrics {
	meter := otel.Meter("personal-assistant/httpapi")
	errors, err := meter.Int64Counter(
		"assistant.http.errors",
		metric.WithDescription("Number of HTTP error responses."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return serverMetrics{}
	}
	return serverMetrics{errors: errors}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.webIndex)
	s.mux.HandleFunc("GET /web/{file}", s.webAsset)
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("POST /v1/chat", s.chat)
	s.mux.HandleFunc("POST /v1/chat/stream", s.chatStream)
	s.mux.HandleFunc("POST /v1/sessions", s.createSession)
	s.mux.HandleFunc("GET /v1/sessions", s.listSessions)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}", s.getSession)
	s.mux.HandleFunc("DELETE /v1/sessions/{session_id}", s.deleteSession)
	s.mux.HandleFunc("POST /v1/memories", s.saveMemory)
	s.mux.HandleFunc("GET /v1/memories/search", s.searchMemory)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.writeError(w, r, apperr.Wrap(apperr.CodeUnavailable, "database unavailable", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req app.ChatRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	normalizeChatRequest(&req)
	resp, err := s.app.Chat(r.Context(), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) chatStream(w http.ResponseWriter, r *http.Request) {
	var req app.ChatRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	normalizeChatRequest(&req)
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, r, apperr.New(apperr.CodeInternal, "streaming is unavailable"))
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	emit := func(event app.ChatStreamEvent) error {
		if err := writeSSE(w, event.Type, event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := s.app.ChatStream(r.Context(), req, emit); err != nil {
		_ = writeSSE(w, "error", map[string]any{"message": apperr.MessageOf(err), "code": apperr.CodeOf(err)})
		flusher.Flush()
		return
	}
	_ = writeSSE(w, "done", map[string]any{"ok": true})
	flusher.Flush()
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req app.CreateSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	normalizeCreateSessionRequest(&req)
	resp, err := s.app.CreateSession(r.Context(), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	resp, err := s.app.ListSessions(r.Context(), userID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": resp})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	resp, err := s.app.GetSession(r.Context(), strings.TrimSpace(r.URL.Query().Get("user_id")), strings.TrimSpace(r.PathValue("session_id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteSession(r.Context(), strings.TrimSpace(r.URL.Query().Get("user_id")), strings.TrimSpace(r.PathValue("session_id"))); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) saveMemory(w http.ResponseWriter, r *http.Request) {
	var req saveMemoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	normalizeSaveMemoryRequest(&req)
	resp, err := s.app.SaveMemory(r.Context(), store.MemoryRecord{
		UserID:     req.UserID,
		Kind:       req.Kind,
		Content:    req.Content,
		Metadata:   req.Metadata,
		Importance: req.Importance,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) searchMemory(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(defaultValue(r.URL.Query().Get("limit"), "8"))
	if err != nil {
		s.writeError(w, r, apperr.New(apperr.CodeInvalid, "limit must be an integer"))
		return
	}
	resp, err := s.app.SearchMemory(r.Context(), strings.TrimSpace(r.URL.Query().Get("user_id")), strings.TrimSpace(r.URL.Query().Get("q")), limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": resp})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("content-type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{
			Error: errorBody{Code: apperr.CodeInvalid, Message: "content-type must be application/json"},
		})
		return false
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: errorBody{Code: apperr.CodeInvalid, Message: "invalid JSON body"},
		})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: errorBody{Code: apperr.CodeInvalid, Message: "invalid JSON body"},
		})
		return false
	}
	return true
}

func normalizeChatRequest(req *app.ChatRequest) {
	req.UserID = strings.TrimSpace(req.UserID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Message = strings.TrimSpace(req.Message)
}

func normalizeCreateSessionRequest(req *app.CreateSessionRequest) {
	req.UserID = strings.TrimSpace(req.UserID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Title = strings.TrimSpace(req.Title)
}

func normalizeSaveMemoryRequest(req *saveMemoryRequest) {
	req.UserID = strings.TrimSpace(req.UserID)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Content = strings.TrimSpace(req.Content)
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := apperr.CodeOf(err)
	status := statusFor(code)
	msg := apperr.MessageOf(err)
	if status >= 500 && !errors.Is(r.Context().Err(), context.Canceled) {
		s.logger.ErrorContext(r.Context(), "request failed", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("code", string(code)), slog.Any("error", err))
	} else {
		s.logger.InfoContext(r.Context(), "request rejected", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("code", string(code)), slog.String("message", msg))
	}
	if s.metrics.errors != nil {
		s.metrics.errors.Add(r.Context(), 1, metric.WithAttributes(
			attribute.String("method", r.Method),
			attribute.String("code", string(code)),
		))
	}
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: msg}})
}

func statusFor(code apperr.Code) int {
	switch code {
	case apperr.CodeInvalid:
		return http.StatusBadRequest
	case apperr.CodeNotFound:
		return http.StatusNotFound
	case apperr.CodeConflict:
		return http.StatusConflict
	case apperr.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSE(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func defaultValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
