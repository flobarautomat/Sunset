package api

import (
	"encoding/json"
	"net/http"

	"moonrise/internal/recorder"

	"github.com/go-chi/chi/v5"
)

type SessionsHandler struct {
	Recorder *recorder.Recorder
}

type createSessionRequest struct {
	VideoID string `json:"video_id"`
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
}

func (h *SessionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.VideoID == "" {
		req.VideoID = "default"
	}

	ua := r.UserAgent()
	id, err := h.Recorder.CreateSession(ua, req.VideoID)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createSessionResponse{SessionID: id})
}

func (h *SessionsHandler) RecordEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	var events []recorder.Event
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.Recorder.RecordEvents(sessionID, events); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
