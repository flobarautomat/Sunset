package api

import (
	"encoding/json"
	"net/http"

	"moonrise/internal/store"

	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	Store *store.SessionStore
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
