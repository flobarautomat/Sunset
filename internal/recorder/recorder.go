package recorder

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type EventStore interface {
	CreateSession(s Session) error
	AppendEvents(sessionID string, events []Event) error
	UpdateLastSeen(sessionID string, at int64) error
}

type Session struct {
	ID         string `json:"id"`
	VideoID    string `json:"video_id"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
}

type Event struct {
	Kind     string           `json:"kind"`
	At       int64            `json:"at"`
	VideoPos *float64         `json:"video_pos,omitempty"`
	Payload  json.RawMessage  `json:"payload,omitempty"`
}

var validKinds = map[string]bool{
	"video_play":   true,
	"video_pause":  true,
	"video_seek":   true,
	"video_ended":  true,
	"ai_message":   true,
	"ai_response":  true,
	"cue_played":   true,
	"heartbeat":    true,
}

const heartbeatCollapseWindow = 5 * time.Second

type Recorder struct {
	store EventStore
}

func New(store EventStore) *Recorder {
	return &Recorder{store: store}
}

func (r *Recorder) CreateSession(userAgent, videoID string) (string, error) {
	now := time.Now().UnixMilli()
	s := Session{
		ID:         uuid.New().String(),
		VideoID:    videoID,
		UserAgent:  userAgent,
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if err := r.store.CreateSession(s); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return s.ID, nil
}

func (r *Recorder) RecordEvents(sessionID string, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	validated := make([]Event, 0, len(events))
	for _, e := range events {
		if !validKinds[e.Kind] {
			return fmt.Errorf("unknown event kind: %q", e.Kind)
		}
		if e.VideoPos != nil && *e.VideoPos < 0 {
			zero := 0.0
			e.VideoPos = &zero
		}
		validated = append(validated, e)
	}

	collapsed := collapseHeartbeats(validated)

	if err := r.store.AppendEvents(sessionID, collapsed); err != nil {
		return fmt.Errorf("append events: %w", err)
	}

	lastAt := collapsed[len(collapsed)-1].At
	if err := r.store.UpdateLastSeen(sessionID, lastAt); err != nil {
		return fmt.Errorf("update last seen: %w", err)
	}

	return nil
}

func collapseHeartbeats(events []Event) []Event {
	out := make([]Event, 0, len(events))
	var lastHeartbeatAt int64

	for _, e := range events {
		if e.Kind == "heartbeat" {
			if lastHeartbeatAt > 0 && (e.At-lastHeartbeatAt) < heartbeatCollapseWindow.Milliseconds() {
				continue
			}
			lastHeartbeatAt = e.At
		}
		out = append(out, e)
	}
	return out
}
