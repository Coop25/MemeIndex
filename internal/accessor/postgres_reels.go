package accessor

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (s *PostgresStore) LoadReelSessions(ctx context.Context) (map[string]ReelSessionRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, history, position, last_activity
		FROM reel_sessions
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := map[string]ReelSessionRecord{}
	for rows.Next() {
		var (
			id           string
			historyJSON  []byte
			position     int
			lastActivity time.Time
			history      []string
		)
		if err := rows.Scan(&id, &historyJSON, &position, &lastActivity); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(historyJSON, &history); err != nil {
			return nil, err
		}
		sessions[id] = ReelSessionRecord{
			History:      history,
			Position:     position,
			LastActivity: lastActivity,
		}
	}
	return sessions, rows.Err()
}

func (s *PostgresStore) SaveReelSession(ctx context.Context, sessionID string, session ReelSessionRecord) error {
	historyJSON, err := json.Marshal(session.History)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO reel_sessions (id, history, position, last_activity)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET history = EXCLUDED.history,
			position = EXCLUDED.position,
			last_activity = EXCLUDED.last_activity
	`, strings.TrimSpace(sessionID), historyJSON, session.Position, session.LastActivity.UTC())
	return err
}

func (s *PostgresStore) DeleteReelSession(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM reel_sessions WHERE id = $1`, strings.TrimSpace(sessionID))
	return err
}

func (s *PostgresStore) CleanupStaleReelSessions(ctx context.Context, before time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM reel_sessions WHERE last_activity < $1`, before.UTC())
	return err
}
