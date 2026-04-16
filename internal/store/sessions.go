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

// Cue represents a voice cue configured for a video.
type Cue struct {
	ID        int     `json:"id"`
	VideoID   string  `json:"video_id"`
	AtSeconds float64 `json:"at_seconds"`
	Prompt    string  `json:"prompt"`
	VoiceID   string  `json:"voice_id"`
}

// GetCue returns a single cue by ID.
func (s *SessionStore) GetCue(id int) (*Cue, error) {
	row := s.db.QueryRow(`SELECT id, video_id, at_seconds, prompt, voice_id FROM cues WHERE id = ?`, id)
	var c Cue
	if err := row.Scan(&c.ID, &c.VideoID, &c.AtSeconds, &c.Prompt, &c.VoiceID); err != nil {
		return nil, err
	}
	return &c, nil
}

// ListCues returns enabled cues for a video ordered by time.
func (s *SessionStore) ListCues(videoID string) ([]Cue, error) {
	rows, err := s.db.Query(
		`SELECT id, video_id, at_seconds, prompt, voice_id FROM cues WHERE video_id = ? AND enabled = 1 ORDER BY at_seconds`,
		videoID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cues []Cue
	for rows.Next() {
		var c Cue
		if err := rows.Scan(&c.ID, &c.VideoID, &c.AtSeconds, &c.Prompt, &c.VoiceID); err != nil {
			return nil, err
		}
		cues = append(cues, c)
	}
	return cues, rows.Err()
}

// SessionWithStats is a session with aggregate event information for the dashboard.
type SessionWithStats struct {
	recorder.Session
	EventCount    int     `json:"event_count"`
	LastEventKind *string `json:"last_event_kind,omitempty"`
}

// ListSessionsWithStats returns all sessions with event count and last event kind.
func (s *SessionStore) ListSessionsWithStats() ([]SessionWithStats, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.created_at, s.last_seen_at, s.user_agent, s.video_id,
		       COUNT(e.id) AS event_count,
		       (SELECT kind FROM events WHERE session_id = s.id ORDER BY at DESC LIMIT 1) AS last_event_kind
		FROM sessions s
		LEFT JOIN events e ON e.session_id = s.id
		GROUP BY s.id
		ORDER BY s.last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionWithStats
	for rows.Next() {
		var ss SessionWithStats
		if err := rows.Scan(&ss.ID, &ss.CreatedAt, &ss.LastSeenAt, &ss.UserAgent, &ss.VideoID, &ss.EventCount, &ss.LastEventKind); err != nil {
			return nil, err
		}
		out = append(out, ss)
	}
	return out, rows.Err()
}

// GetSession returns a single session by ID.
func (s *SessionStore) GetSession(id string) (*recorder.Session, error) {
	row := s.db.QueryRow(`SELECT id, created_at, last_seen_at, user_agent, video_id FROM sessions WHERE id = ?`, id)
	var sess recorder.Session
	if err := row.Scan(&sess.ID, &sess.CreatedAt, &sess.LastSeenAt, &sess.UserAgent, &sess.VideoID); err != nil {
		return nil, err
	}
	return &sess, nil
}

// EventWithSession is an event tagged with its session ID for the dashboard feed.
type EventWithSession struct {
	recorder.Event
	SessionID string `json:"session_id"`
}

// ListRecentEvents returns the most recent events across all sessions.
func (s *SessionStore) ListRecentEvents(limit int) ([]EventWithSession, error) {
	rows, err := s.db.Query(`SELECT session_id, kind, at, video_pos, payload FROM events ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EventWithSession
	for rows.Next() {
		var e EventWithSession
		var payload *string
		if err := rows.Scan(&e.SessionID, &e.Kind, &e.At, &e.VideoPos, &payload); err != nil {
			return nil, err
		}
		if payload != nil {
			e.Payload = json.RawMessage(*payload)
		}
		out = append(out, e)
	}
	return out, rows.Err()
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
