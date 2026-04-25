package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"personal-assistant/internal/embedding"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

const defaultMemoryLimit = 8

type MemoryRecord struct {
	AppName         string
	UserID          string
	Kind            string
	Content         string
	Metadata        map[string]any
	Importance      float64
	SourceSessionID string
	SourceEventID   string
	CreatedAt       time.Time
}

type MemoryResult struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	Content         string         `json:"content"`
	Metadata        map[string]any `json:"metadata"`
	Importance      float64        `json:"importance"`
	SourceSessionID string         `json:"source_session_id,omitempty"`
	SourceEventID   string         `json:"source_event_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	Score           float64        `json:"score,omitempty"`
}

func (s *Store) AddSessionToMemory(ctx context.Context, sess session.Session) error {
	ctx, span := s.tracer.Start(ctx, "memory.AddSessionToMemory")
	defer span.End()

	if sess.Events() == nil {
		return nil
	}
	count := 0
	for event := range sess.Events().All() {
		text := compactWhitespace(contentText(event.Content))
		if text == "" {
			continue
		}
		author := event.Author
		if author == "" {
			author = "unknown"
		}
		_, err := s.SaveMemory(ctx, MemoryRecord{
			AppName:    sess.AppName(),
			UserID:     sess.UserID(),
			Kind:       "episodic",
			Content:    text,
			Importance: 0.4,
			Metadata: map[string]any{
				"author":        author,
				"invocation_id": event.InvocationID,
				"branch":        event.Branch,
			},
			SourceSessionID: sess.ID(),
			SourceEventID:   event.ID,
			CreatedAt:       event.Timestamp,
		})
		if err != nil {
			return err
		}
		count++
	}
	s.logger.DebugContext(ctx, "session added to memory", slog.String("session_id", sess.ID()), slog.Int("events_considered", count))
	return nil
}

func (s *Store) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	results, err := s.SearchMemories(ctx, req.AppName, req.UserID, req.Query, defaultMemoryLimit)
	if err != nil {
		return nil, err
	}
	entries := make([]memory.Entry, 0, len(results))
	for _, result := range results {
		metadata := result.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["kind"] = result.Kind
		metadata["importance"] = result.Importance
		if result.Score != 0 {
			metadata["score"] = result.Score
		}
		entries = append(entries, memory.Entry{
			ID:             result.ID,
			Content:        genai.NewContentFromText(result.Content, genai.RoleUser),
			Author:         "memory",
			Timestamp:      result.CreatedAt,
			CustomMetadata: metadata,
		})
	}
	return &memory.SearchResponse{Memories: entries}, nil
}

func (s *Store) SaveMemory(ctx context.Context, rec MemoryRecord) (string, error) {
	ctx, span := s.tracer.Start(ctx, "memory.SaveMemory")
	defer span.End()

	if strings.TrimSpace(rec.AppName) == "" || strings.TrimSpace(rec.UserID) == "" {
		return "", fmt.Errorf("app name and user id are required")
	}
	rec.Content = compactWhitespace(rec.Content)
	if rec.Content == "" {
		return "", fmt.Errorf("memory content is required")
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
	embeddingValues, err := s.embedder.Embed(ctx, rec.Content, embedding.TaskDocument)
	if err != nil {
		return "", fmt.Errorf("embed memory content: %w", err)
	}
	if err := validateVector(embeddingValues, s.embedder.Dimension()); err != nil {
		return "", err
	}
	embeddingLiteral := vectorLiteral(embeddingValues)

	metadataJSON, err := json.Marshal(rec.Metadata)
	if err != nil {
		return "", fmt.Errorf("marshal memory metadata: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin save memory: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	memoryID := uuid.NewString()
	err = tx.QueryRow(ctx, `
		INSERT INTO memories
			(id, app_name, user_id, kind, content, metadata, importance, source_session_id, source_event_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, $10)
		ON CONFLICT (source_event_id) WHERE source_event_id IS NOT NULL AND source_event_id <> ''
		DO NOTHING
		RETURNING id
	`, memoryID, rec.AppName, rec.UserID, rec.Kind, rec.Content, metadataJSON, rec.Importance, rec.SourceSessionID, rec.SourceEventID, rec.CreatedAt).Scan(&memoryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return "", fmt.Errorf("commit duplicate memory skip: %w", commitErr)
			}
			return "", nil
		}
		return "", fmt.Errorf("insert memory: %w", err)
	}

	chunkID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO memory_chunks (id, memory_id, app_name, user_id, content, metadata, embedding)
		VALUES ($1, $2, $3, $4, $5, $6, $7::vector)
	`, chunkID, memoryID, rec.AppName, rec.UserID, rec.Content, metadataJSON, embeddingLiteral)
	if err != nil {
		return "", fmt.Errorf("insert memory chunk: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit save memory: %w", err)
	}
	s.metrics.memoryWrites.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", rec.Kind)))
	s.logger.InfoContext(ctx, "memory saved", slog.String("memory_id", memoryID), slog.String("app", rec.AppName), slog.String("user_id", rec.UserID), slog.String("kind", rec.Kind))
	return memoryID, nil
}

func (s *Store) SearchMemories(ctx context.Context, appName, userID, query string, limit int) ([]MemoryResult, error) {
	ctx, span := s.tracer.Start(ctx, "memory.SearchMemories")
	defer span.End()

	if limit <= 0 {
		limit = defaultMemoryLimit
	}
	if limit > 20 {
		limit = 20
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return s.recentMemories(ctx, appName, userID, limit)
	}
	queryEmbedding, err := s.embedder.Embed(ctx, query, embedding.TaskQuery)
	if err != nil {
		return nil, fmt.Errorf("embed memory search query: %w", err)
	}
	if err := validateVector(queryEmbedding, s.embedder.Dimension()); err != nil {
		return nil, err
	}
	queryVector := vectorLiteral(queryEmbedding)
	candidateLimit := limit * 4
	if candidateLimit < 20 {
		candidateLimit = 20
	}

	rows, err := s.pool.Query(ctx, `
		WITH q AS (
			SELECT websearch_to_tsquery('simple', $3) AS text_query, $4::vector AS embedding
		),
		fts AS (
			SELECT
				c.memory_id,
				ts_rank(c.tsv, q.text_query) AS fts_score,
				0::double precision AS vector_score
			FROM memory_chunks c
			CROSS JOIN q
			WHERE c.app_name = $1
			  AND c.user_id = $2
			  AND c.tsv @@ q.text_query
			ORDER BY fts_score DESC
			LIMIT $6
		),
		vector_hits AS (
			SELECT
				c.memory_id,
				0::double precision AS fts_score,
				GREATEST(0::double precision, 1 - (c.embedding <=> q.embedding)) AS vector_score
			FROM memory_chunks c
			CROSS JOIN q
			WHERE c.app_name = $1
			  AND c.user_id = $2
			  AND c.embedding IS NOT NULL
			ORDER BY c.embedding <=> q.embedding
			LIMIT $6
		),
		candidates AS (
			SELECT memory_id, max(fts_score) AS fts_score, max(vector_score) AS vector_score
			FROM (
				SELECT * FROM fts
				UNION ALL
				SELECT * FROM vector_hits
			) raw
			GROUP BY memory_id
		)
		SELECT
			m.id, m.kind, m.content, m.metadata, m.importance,
			COALESCE(m.source_session_id, ''), COALESCE(m.source_event_id, ''),
			m.created_at,
			(0.45 * candidates.fts_score) +
			(0.45 * candidates.vector_score) +
			(0.10 * m.importance) AS score
		FROM candidates
		JOIN memories m ON m.id = candidates.memory_id
		ORDER BY score DESC, candidates.vector_score DESC, candidates.fts_score DESC, m.updated_at DESC
		LIMIT $5
	`, appName, userID, query, queryVector, limit, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	defer rows.Close()
	results, err := scanMemoryResults(rows)
	if err != nil {
		return nil, err
	}
	s.metrics.memorySearches.Add(ctx, 1, metric.WithAttributes(attribute.String("mode", "hybrid")))
	s.metrics.memorySearchResults.Record(ctx, int64(len(results)), metric.WithAttributes(attribute.String("mode", "hybrid")))
	s.logger.DebugContext(ctx, "memory search completed", slog.String("app", appName), slog.String("user_id", userID), slog.String("query", query), slog.Int("count", len(results)))
	return results, nil
}

func (s *Store) recentMemories(ctx context.Context, appName, userID string, limit int) ([]MemoryResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id, kind, content, metadata, importance,
			COALESCE(source_session_id, ''), COALESCE(source_event_id, ''),
			created_at, 0::double precision AS score
		FROM memories
		WHERE app_name = $1 AND user_id = $2
		ORDER BY updated_at DESC
		LIMIT $3
	`, appName, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent memories: %w", err)
	}
	defer rows.Close()
	results, err := scanMemoryResults(rows)
	if err != nil {
		return nil, err
	}
	s.metrics.memorySearches.Add(ctx, 1, metric.WithAttributes(attribute.String("mode", "recent")))
	s.metrics.memorySearchResults.Record(ctx, int64(len(results)), metric.WithAttributes(attribute.String("mode", "recent")))
	return results, nil
}

func scanMemoryResults(rows pgx.Rows) ([]MemoryResult, error) {
	results := []MemoryResult{}
	for rows.Next() {
		var result MemoryResult
		var metadataRaw []byte
		if err := rows.Scan(
			&result.ID,
			&result.Kind,
			&result.Content,
			&metadataRaw,
			&result.Importance,
			&result.SourceSessionID,
			&result.SourceEventID,
			&result.CreatedAt,
			&result.Score,
		); err != nil {
			return nil, fmt.Errorf("scan memory result: %w", err)
		}
		metadata, err := decodeMetadata(metadataRaw)
		if err != nil {
			return nil, err
		}
		result.Metadata = metadata
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory results: %w", err)
	}
	return results, nil
}

func decodeMetadata(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, fmt.Errorf("decode memory metadata: %w", err)
	}
	if metadata == nil {
		return map[string]any{}, nil
	}
	return metadata, nil
}
