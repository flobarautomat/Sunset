package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

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

// FilmStats holds aggregate event counts per video for the admin dashboard.
type FilmStats struct {
	VideoID        string `json:"video_id"`
	SessionCount   int    `json:"session_count"`
	ActiveSessions int    `json:"active_sessions"`
	PlayCount      int    `json:"play_count"`
	ChatMessages   int    `json:"chat_messages"`
	AIResponses    int    `json:"ai_responses"`
	CuesTriggered  int    `json:"cues_triggered"`
}

// ListFilmStats returns aggregate stats per video_id across all sessions.
func (s *SessionStore) ListFilmStats() ([]FilmStats, error) {
	now := fmt.Sprintf("%d", nowMilli()-30000)
	rows, err := s.db.Query(`
		SELECT s.video_id,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(DISTINCT CASE WHEN s.last_seen_at > `+now+` THEN s.id END) AS active_sessions,
			SUM(CASE WHEN e.kind='video_play' THEN 1 ELSE 0 END) AS play_count,
			SUM(CASE WHEN e.kind='ai_message' THEN 1 ELSE 0 END) AS chat_messages,
			SUM(CASE WHEN e.kind='ai_response' THEN 1 ELSE 0 END) AS ai_responses,
			SUM(CASE WHEN e.kind='cue_played' THEN 1 ELSE 0 END) AS cues_triggered
		FROM sessions s
		LEFT JOIN events e ON e.session_id = s.id
		GROUP BY s.video_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FilmStats
	for rows.Next() {
		var fs FilmStats
		if err := rows.Scan(&fs.VideoID, &fs.SessionCount, &fs.ActiveSessions, &fs.PlayCount, &fs.ChatMessages, &fs.AIResponses, &fs.CuesTriggered); err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}

func nowMilli() int64 {
	return timeNow().UnixMilli()
}

var timeNow = func() time.Time { return time.Now() }

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

// SystemStats holds aggregate counts for the system health widget.
type SystemStats struct {
	TotalSessions  int `json:"total_sessions"`
	ActiveSessions int `json:"active_sessions"`
	TotalEvents    int `json:"total_events"`
}

// GetSystemStats returns system-wide aggregate counts.
func (s *SessionStore) GetSystemStats() (SystemStats, error) {
	threshold := fmt.Sprintf("%d", nowMilli()-30000)
	var ss SystemStats
	err := s.db.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM sessions) AS total_sessions,
			(SELECT COUNT(*) FROM sessions WHERE last_seen_at > `+threshold+`) AS active_sessions,
			(SELECT COUNT(*) FROM events) AS total_events
	`).Scan(&ss.TotalSessions, &ss.ActiveSessions, &ss.TotalEvents)
	return ss, err
}

// AIStats holds aggregate AI usage metrics.
type AIStats struct {
	TotalMessages  int     `json:"total_messages"`
	TotalResponses int     `json:"total_responses"`
	AvgResponseLen float64 `json:"avg_response_length"`
	TotalCuePlays  int     `json:"total_cue_plays"`
}

// GetAIStats returns aggregate AI chat and cue metrics.
func (s *SessionStore) GetAIStats() (AIStats, error) {
	var as AIStats
	var avgLen *float64
	err := s.db.QueryRow(`
		SELECT
			SUM(CASE WHEN kind = 'ai_message' THEN 1 ELSE 0 END),
			SUM(CASE WHEN kind = 'ai_response' THEN 1 ELSE 0 END),
			AVG(CASE WHEN kind = 'ai_response' THEN LENGTH(JSON_EXTRACT(payload, '$.text')) END),
			SUM(CASE WHEN kind = 'cue_played' THEN 1 ELSE 0 END)
		FROM events
	`).Scan(&as.TotalMessages, &as.TotalResponses, &avgLen, &as.TotalCuePlays)
	if avgLen != nil {
		as.AvgResponseLen = *avgLen
	}
	return as, err
}

// HeatmapBucket holds event counts for a time range in the video.
type HeatmapBucket struct {
	BucketStart float64 `json:"bucket_start"`
	BucketEnd   float64 `json:"bucket_end"`
	PlayCount   int     `json:"play_count"`
	PauseCount  int     `json:"pause_count"`
	SeekCount   int     `json:"seek_count"`
	ChatCount   int     `json:"chat_count"`
	CueCount    int     `json:"cue_count"`
	Total       int     `json:"total"`
}

// GetHeatmap returns event density bucketed by video position.
func (s *SessionStore) GetHeatmap(videoID string, bucketSeconds float64) ([]HeatmapBucket, error) {
	rows, err := s.db.Query(`
		SELECT
			CAST(CAST(e.video_pos / ? AS INTEGER) * ? AS REAL) AS bucket_start,
			CAST(CAST(e.video_pos / ? AS INTEGER) * ? + ? AS REAL) AS bucket_end,
			SUM(CASE WHEN e.kind = 'video_play' THEN 1 ELSE 0 END),
			SUM(CASE WHEN e.kind = 'video_pause' THEN 1 ELSE 0 END),
			SUM(CASE WHEN e.kind = 'video_seek' THEN 1 ELSE 0 END),
			SUM(CASE WHEN e.kind IN ('ai_message', 'ai_response') THEN 1 ELSE 0 END),
			SUM(CASE WHEN e.kind = 'cue_played' THEN 1 ELSE 0 END),
			COUNT(*)
		FROM events e
		JOIN sessions s ON e.session_id = s.id
		WHERE s.video_id = ? AND e.video_pos IS NOT NULL AND e.kind != 'heartbeat'
		GROUP BY CAST(e.video_pos / ? AS INTEGER)
		ORDER BY bucket_start`,
		bucketSeconds, bucketSeconds, bucketSeconds, bucketSeconds, bucketSeconds, videoID, bucketSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HeatmapBucket
	for rows.Next() {
		var b HeatmapBucket
		if err := rows.Scan(&b.BucketStart, &b.BucketEnd, &b.PlayCount, &b.PauseCount, &b.SeekCount, &b.ChatCount, &b.CueCount, &b.Total); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
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
