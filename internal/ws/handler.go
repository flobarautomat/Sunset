package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"moonrise/internal/pubsub"
	"moonrise/internal/store"
	"moonrise/internal/video"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Handler struct {
	Hub      *pubsub.Hub
	Store    *store.SessionStore
	Registry *video.Registry
}

type filmStatsWithMeta struct {
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	Year           int         `json:"year,omitempty"`
	Director       string      `json:"director,omitempty"`
	Synopsis       string      `json:"synopsis,omitempty"`
	Duration       float64     `json:"duration"`
	Width          int         `json:"width"`
	Height         int         `json:"height"`
	SessionCount   int         `json:"session_count"`
	ActiveSessions int         `json:"active_sessions"`
	PlayCount      int         `json:"play_count"`
	ChatMessages   int         `json:"chat_messages"`
	AIResponses    int         `json:"ai_responses"`
	CuesTriggered  int         `json:"cues_triggered"`
	Cues           []store.Cue `json:"cues"`
}

type snapshotMessage struct {
	Type      string                   `json:"type"`
	Sessions  []store.SessionWithStats `json:"sessions"`
	Events    []store.EventWithSession `json:"events"`
	FilmStats []filmStatsWithMeta      `json:"film_stats"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // CORS: accept any origin (dev/demo)
	})
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Send initial snapshot
	if err := h.sendSnapshot(ctx, conn); err != nil {
		log.Printf("ws snapshot: %v", err)
		return
	}

	// Subscribe to pub/sub
	ch, unsub := h.Hub.Subscribe()
	defer unsub()

	// Read loop: drain incoming frames to detect disconnects
	go func() {
		defer cancel()
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
		}
	}()

	// Write loop: forward pub/sub messages to the WebSocket client
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
			err = conn.Write(writeCtx, websocket.MessageText, data)
			writeCancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *Handler) sendSnapshot(ctx context.Context, conn *websocket.Conn) error {
	sessions, err := h.Store.ListSessionsWithStats()
	if err != nil {
		return err
	}
	if sessions == nil {
		sessions = []store.SessionWithStats{}
	}

	events, err := h.Store.ListRecentEvents(200)
	if err != nil {
		return err
	}
	if events == nil {
		events = []store.EventWithSession{}
	}

	// Reverse events so they're in chronological order (query returns DESC)
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}

	// Build film stats
	filmStats := h.buildFilmStats()

	return wsjson.Write(ctx, conn, snapshotMessage{
		Type:      "snapshot",
		Sessions:  sessions,
		Events:    events,
		FilmStats: filmStats,
	})
}

func (h *Handler) buildFilmStats() []filmStatsWithMeta {
	statsMap := make(map[string]store.FilmStats)
	if stats, err := h.Store.ListFilmStats(); err == nil {
		for _, s := range stats {
			statsMap[s.VideoID] = s
		}
	}

	var out []filmStatsWithMeta
	for _, v := range h.Registry.List() {
		fs := statsMap[v.ID]
		cues, _ := h.Store.ListCues(v.ID)
		if cues == nil {
			cues = []store.Cue{}
		}
		out = append(out, filmStatsWithMeta{
			ID:             v.ID,
			Title:          v.Title,
			Year:           v.Year,
			Director:       v.Director,
			Synopsis:       v.Synopsis,
			Duration:       v.Duration,
			Width:          v.Width,
			Height:         v.Height,
			SessionCount:   fs.SessionCount,
			ActiveSessions: fs.ActiveSessions,
			PlayCount:      fs.PlayCount,
			ChatMessages:   fs.ChatMessages,
			AIResponses:    fs.AIResponses,
			CuesTriggered:  fs.CuesTriggered,
			Cues:           cues,
		})
	}
	if out == nil {
		out = []filmStatsWithMeta{}
	}
	return out
}
