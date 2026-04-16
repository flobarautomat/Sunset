package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"moonrise/internal/pubsub"
	"moonrise/internal/store"
	"moonrise/internal/video"

	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	Store     *store.SessionStore
	Registry  *video.Registry
	Hub       *pubsub.Hub
	StartTime time.Time
}

type sessionDetailResponse struct {
	Session store.SessionWithStats `json:"session"`
	Events  []store.EventWithSession `json:"events"`
}

func (h *AdminHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.Store.ListSessionsWithStats()
	if err != nil {
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []store.SessionWithStats{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (h *AdminHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	sessions, err := h.Store.ListSessionsWithStats()
	if err != nil {
		http.Error(w, "failed to get session", http.StatusInternalServerError)
		return
	}

	var found *store.SessionWithStats
	for i := range sessions {
		if sessions[i].ID == id {
			found = &sessions[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	events, err := h.Store.ListEvents(id)
	if err != nil {
		http.Error(w, "failed to list events", http.StatusInternalServerError)
		return
	}

	eventsWithSession := make([]store.EventWithSession, len(events))
	for i, e := range events {
		eventsWithSession[i] = store.EventWithSession{
			Event:     e,
			SessionID: id,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessionDetailResponse{
		Session: *found,
		Events:  eventsWithSession,
	})
}

type filmResponse struct {
	video.Video
	SessionCount   int          `json:"session_count"`
	ActiveSessions int          `json:"active_sessions"`
	PlayCount      int          `json:"play_count"`
	ChatMessages   int          `json:"chat_messages"`
	AIResponses    int          `json:"ai_responses"`
	CuesTriggered  int          `json:"cues_triggered"`
	Cues           []store.Cue  `json:"cues"`
}

func (h *AdminHandler) ListFilms(w http.ResponseWriter, r *http.Request) {
	// Build stats map
	statsMap := make(map[string]store.FilmStats)
	if stats, err := h.Store.ListFilmStats(); err == nil {
		for _, s := range stats {
			statsMap[s.VideoID] = s
		}
	}

	var films []filmResponse
	for _, v := range h.Registry.List() {
		fs := statsMap[v.ID]
		cues, _ := h.Store.ListCues(v.ID)
		if cues == nil {
			cues = []store.Cue{}
		}
		films = append(films, filmResponse{
			Video:          v,
			SessionCount:   fs.SessionCount,
			ActiveSessions: fs.ActiveSessions,
			PlayCount:      fs.PlayCount,
			ChatMessages:   fs.ChatMessages,
			AIResponses:    fs.AIResponses,
			CuesTriggered:  fs.CuesTriggered,
			Cues:           cues,
		})
	}
	if films == nil {
		films = []filmResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(films)
}

type statsResponse struct {
	System         store.SystemStats `json:"system"`
	AI             store.AIStats     `json:"ai"`
	UptimeSeconds  int64             `json:"uptime_seconds"`
	WsConnections  int               `json:"ws_connections"`
	CacheFiles     int               `json:"cache_files"`
	CacheSizeBytes int64             `json:"cache_size_bytes"`
}

func (h *AdminHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	sys, _ := h.Store.GetSystemStats()
	ai, _ := h.Store.GetAIStats()

	cacheFiles, cacheSize := ttsCacheStats("cache/cue-audio")

	resp := statsResponse{
		System:         sys,
		AI:             ai,
		UptimeSeconds:  int64(time.Since(h.StartTime).Seconds()),
		WsConnections:  h.Hub.ConnectionCount(),
		CacheFiles:     cacheFiles,
		CacheSizeBytes: cacheSize,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *AdminHandler) GetHeatmap(w http.ResponseWriter, r *http.Request) {
	videoID := r.URL.Query().Get("video_id")
	if videoID == "" {
		// Default to first film
		films := h.Registry.List()
		if len(films) > 0 {
			videoID = films[0].ID
		}
	}

	bucketSize := 60.0
	if bs := r.URL.Query().Get("bucket_size"); bs != "" {
		if v, err := strconv.ParseFloat(bs, 64); err == nil && v > 0 {
			bucketSize = v
		}
	}

	buckets, err := h.Store.GetHeatmap(videoID, bucketSize)
	if err != nil {
		http.Error(w, "failed to get heatmap", http.StatusInternalServerError)
		return
	}
	if buckets == nil {
		buckets = []store.HeatmapBucket{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buckets)
}

func ttsCacheStats(dir string) (int, int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	var totalSize int64
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			count++
			totalSize += info.Size()
		}
	}
	return count, totalSize
}
