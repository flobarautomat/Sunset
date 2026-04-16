package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"moonrise/internal/recorder"
)

type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(db *sql.DB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) CreateSession(sess recorder.Session) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, created_at, last_seen_at, user_agent, video_id) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.CreatedAt, sess.LastSeenAt, sess.UserAgent, sess.VideoID,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *SessionStore) AppendEvents(sessionID string, events []recorder.Event) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO events (session_id, kind, at, video_pos, payload) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, e := range events {
		var payload *string
		if len(e.Payload) > 0 {
			s := string(e.Payload)
			payload = &s
		}
		var vp *float64
		if e.VideoPos != nil {
			vp = e.VideoPos
		}
		if _, err := stmt.Exec(sessionID, e.Kind, e.At, vp, payload); err != nil {
			return fmt.Errorf("insert event %s: %w", e.Kind, err)
		}
	}

	return tx.Commit()
}

func (s *SessionStore) UpdateLastSeen(sessionID string, at int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, at, sessionID)
	if err != nil {
		return fmt.Errorf("update last_seen: %w", err)
	}
	return nil
}

// ListSessions returns all sessions ordered by most recent activity.
func (s *SessionStore) ListSessions() ([]recorder.Session, error) {
	rows, err := s.db.Query(`SELECT id, created_at, last_seen_at, user_agent, video_id FROM sessions ORDER BY last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []recorder.Session
	for rows.Next() {
		var sess recorder.Session
		if err := rows.Scan(&sess.ID, &sess.CreatedAt, &sess.LastSeenAt, &sess.UserAgent, &sess.VideoID); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// ListEvents returns all events for a session ordered by time.
func (s *SessionStore) ListEvents(sessionID string) ([]recorder.Event, error) {
	rows, err := s.db.Query(`SELECT kind, at, video_pos, payload FROM events WHERE session_id = ? ORDER BY at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []recorder.Event
	for rows.Next() {
		var e recorder.Event
		var payload *string
		if err := rows.Scan(&e.Kind, &e.At, &e.VideoPos, &payload); err != nil {
			return nil, err
		}
		if payload != nil {
			e.Payload = json.RawMessage(*payload)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
