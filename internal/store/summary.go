package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const summaryMaxChars = 4000

func (s *Store) RefreshSummary(ctx context.Context, appName, userID, sessionID string) (string, error) {
	ctx, span := s.tracer.Start(ctx, "session.RefreshSummary")
	defer span.End()

	rows, err := s.pool.Query(ctx, `
		SELECT seq, author, content_text
		FROM session_events
		WHERE session_id = $1 AND app_name = $2 AND user_id = $3 AND content_text <> ''
		ORDER BY seq DESC
		LIMIT 24
	`, sessionID, appName, userID)
	if err != nil {
		return "", fmt.Errorf("query summary events: %w", err)
	}
	defer rows.Close()

	type item struct {
		seq    int64
		author string
		text   string
	}
	reversed := []item{}
	var maxSeq int64
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.seq, &it.author, &it.text); err != nil {
			return "", fmt.Errorf("scan summary event: %w", err)
		}
		if it.seq > maxSeq {
			maxSeq = it.seq
		}
		reversed = append(reversed, it)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate summary events: %w", err)
	}
	if len(reversed) == 0 {
		return "", nil
	}

	lines := make([]string, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		author := reversed[i].author
		if author == "" {
			author = "unknown"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", author, compactWhitespace(reversed[i].text)))
	}
	summary := trimString(strings.Join(lines, "\n"), summaryMaxChars)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO session_summaries (session_id, app_name, user_id, summary, covered_until_seq, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (session_id)
		DO UPDATE SET summary = EXCLUDED.summary, covered_until_seq = EXCLUDED.covered_until_seq, updated_at = now()
	`, sessionID, appName, userID, summary, maxSeq)
	if err != nil {
		return "", fmt.Errorf("upsert session summary: %w", err)
	}
	s.logger.DebugContext(ctx, "session summary refreshed", "session_id", sessionID, "chars", len(summary))
	return summary, nil
}

func (s *Store) GetSummary(ctx context.Context, appName, userID, sessionID string) (string, error) {
	ctx, span := s.tracer.Start(ctx, "session.GetSummary")
	defer span.End()

	var summary string
	err := s.pool.QueryRow(ctx, `
		SELECT summary
		FROM session_summaries
		WHERE session_id = $1 AND app_name = $2 AND user_id = $3
	`, sessionID, appName, userID).Scan(&summary)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get session summary: %w", err)
	}
	return summary, nil
}

func trimString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}
