package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/adk/session"
)

func (s *Store) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	ctx, span := s.tracer.Start(ctx, "session.Create")
	defer span.End()

	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("app name and user id are required")
	}
	id := s.ids.NextID()
	sessionID := formatID(id)
	state := State{}
	for key, value := range req.State {
		if strings.HasPrefix(key, session.KeyPrefixTemp) {
			continue
		}
		state[key] = value
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal initial state: %w", err)
	}

	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO sessions (id, app_name, user_id, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, id, req.AppName, req.UserID, stateJSON, now)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, fmt.Errorf("session %q already exists: %w", sessionID, err)
		}
		return nil, fmt.Errorf("insert session: %w", err)
	}

	s.logger.InfoContext(ctx, "session created", slog.String("app", req.AppName), slog.String("user_id", req.UserID), slog.String("session_id", sessionID))
	return &session.CreateResponse{Session: &Session{
		IDVal:         sessionID,
		AppNameVal:    req.AppName,
		UserIDVal:     req.UserID,
		StateVal:      state,
		EventsVal:     Events{},
		LastUpdateVal: now,
	}}, nil
}

func (s *Store) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	ctx, span := s.tracer.Start(ctx, "session.Get")
	defer span.End()
	span.SetAttributes(attribute.String("session.id", req.SessionID))
	sessionID, err := parseID(req.SessionID, "session_id")
	if err != nil {
		return nil, err
	}

	row := s.pool.QueryRow(ctx, `
		SELECT id, app_name, user_id, title, state, updated_at
		FROM sessions
		WHERE id = $1 AND app_name = $2 AND user_id = $3
	`, sessionID, req.AppName, req.UserID)

	var sess Session
	var id int64
	var stateRaw []byte
	if err := row.Scan(&id, &sess.AppNameVal, &sess.UserIDVal, &sess.Title, &stateRaw, &sess.LastUpdateVal); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("session %q not found", req.SessionID)
		}
		return nil, fmt.Errorf("select session: %w", err)
	}
	sess.IDVal = formatID(id)
	state, err := decodeState(stateRaw)
	if err != nil {
		return nil, err
	}
	sess.StateVal = state

	events, err := s.loadEvents(ctx, sessionID, req.After, req.NumRecentEvents)
	if err != nil {
		return nil, err
	}
	sess.EventsVal = events
	return &session.GetResponse{Session: &sess}, nil
}

func (s *Store) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	ctx, span := s.tracer.Start(ctx, "session.List")
	defer span.End()

	rows, err := s.pool.Query(ctx, `
		SELECT id, app_name, user_id, title, state, updated_at
		FROM sessions
		WHERE app_name = $1 AND user_id = $2
		ORDER BY updated_at DESC
	`, req.AppName, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []session.Session
	for rows.Next() {
		var sess Session
		var id int64
		var stateRaw []byte
		if err := rows.Scan(&id, &sess.AppNameVal, &sess.UserIDVal, &sess.Title, &stateRaw, &sess.LastUpdateVal); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.IDVal = formatID(id)
		state, err := decodeState(stateRaw)
		if err != nil {
			return nil, err
		}
		sess.StateVal = state
		sess.EventsVal = Events{}
		sessions = append(sessions, &sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return &session.ListResponse{Sessions: sessions}, nil
}

func (s *Store) Delete(ctx context.Context, req *session.DeleteRequest) error {
	ctx, span := s.tracer.Start(ctx, "session.Delete")
	defer span.End()
	sessionID, err := parseID(req.SessionID, "session_id")
	if err != nil {
		return err
	}

	tag, err := s.pool.Exec(ctx, `
		DELETE FROM sessions
		WHERE id = $1 AND app_name = $2 AND user_id = $3
	`, sessionID, req.AppName, req.UserID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session %q not found", req.SessionID)
	}
	s.logger.InfoContext(ctx, "session deleted", slog.String("app", req.AppName), slog.String("user_id", req.UserID), slog.String("session_id", req.SessionID))
	return nil
}

func (s *Store) UpdateSessionTitle(ctx context.Context, appName, userID, sessionID, title string) error {
	ctx, span := s.tracer.Start(ctx, "session.UpdateSessionTitle")
	defer span.End()
	sessionIDInt, err := parseID(sessionID, "session_id")
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE sessions
		SET title = $4, updated_at = now()
		WHERE id = $1 AND app_name = $2 AND user_id = $3
	`, sessionIDInt, appName, userID, title)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	return nil
}

func (s *Store) AppendEvent(ctx context.Context, sess session.Session, event *session.Event) error {
	ctx, span := s.tracer.Start(ctx, "session.AppendEvent")
	defer span.End()

	if sess == nil {
		return fmt.Errorf("session is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}
	if event.Partial {
		return nil
	}
	sessionID, err := parseID(sess.ID(), "session_id")
	if err != nil {
		return err
	}
	eventID := s.ids.NextID()
	event.ID = formatID(eventID)
	invocationID := s.invocationID(event.InvocationID)
	event.InvocationID = formatID(invocationID)
	event.Author = trimRunes(event.Author, 50)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	filterTemporaryState(event)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin append event: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var stateRaw []byte
	err = tx.QueryRow(ctx, `
		SELECT state
		FROM sessions
		WHERE id = $1 AND app_name = $2 AND user_id = $3
		FOR UPDATE
	`, sessionID, sess.AppName(), sess.UserID()).Scan(&stateRaw)
	if err != nil {
		return fmt.Errorf("lock session for append: %w", err)
	}
	state, err := decodeState(stateRaw)
	if err != nil {
		return err
	}
	for key, value := range event.Actions.StateDelta {
		state[key] = value
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal session state: %w", err)
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	text := contentText(event.Content)

	_, err = tx.Exec(ctx, `
		INSERT INTO session_events
			(id, session_id, app_name, user_id, invocation_id, author, branch, content_text, event_json, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING
	`, eventID, sessionID, sess.AppName(), sess.UserID(), invocationID, event.Author, event.Branch, text, eventJSON, event.Timestamp)
	if err != nil {
		return fmt.Errorf("insert session event: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE sessions
		SET state = $4, updated_at = $5
		WHERE id = $1 AND app_name = $2 AND user_id = $3
	`, sessionID, sess.AppName(), sess.UserID(), stateJSON, event.Timestamp)
	if err != nil {
		return fmt.Errorf("update session after append: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit append event: %w", err)
	}
	s.metrics.sessionEventsAppended.Add(ctx, 1, metric.WithAttributes(attribute.String("author", event.Author)))
	s.logger.DebugContext(ctx, "session event appended", slog.String("session_id", sess.ID()), slog.String("event_id", event.ID), slog.String("author", event.Author))
	return nil
}

func (s *Store) loadEvents(ctx context.Context, sessionID int64, after time.Time, limit int) (Events, error) {
	args := []any{sessionID}
	filter := "WHERE session_id = $1"
	if !after.IsZero() {
		args = append(args, after)
		filter += fmt.Sprintf(" AND created_at >= $%d", len(args))
	}

	query := ""
	if limit > 0 {
		args = append(args, limit)
		query = fmt.Sprintf(`
			SELECT event_json
			FROM (
				SELECT id, event_json
				FROM session_events
				%s
				ORDER BY id DESC
				LIMIT $%d
			) recent
			ORDER BY id ASC
		`, filter, len(args))
	} else {
		query = fmt.Sprintf(`
			SELECT event_json
			FROM session_events
			%s
			ORDER BY id ASC
		`, filter)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query session events: %w", err)
	}
	defer rows.Close()

	events := Events{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan session event: %w", err)
		}
		var event session.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("decode session event: %w", err)
		}
		events = append(events, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session events: %w", err)
	}
	return events, nil
}

func decodeState(raw []byte) (State, error) {
	if len(raw) == 0 {
		return State{}, nil
	}
	state := State{}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode session state: %w", err)
	}
	return state, nil
}

func filterTemporaryState(event *session.Event) {
	if event.Actions.StateDelta == nil {
		return
	}
	for key := range event.Actions.StateDelta {
		if strings.HasPrefix(key, session.KeyPrefixTemp) {
			delete(event.Actions.StateDelta, key)
		}
	}
}
