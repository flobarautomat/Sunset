package api

import (
	"encoding/json"
	"net/http"

	"moonrise/internal/store"

	"github.com/go-chi/chi/v5"
)

type CuesHandler struct {
	Store *store.SessionStore
}

func (h *CuesHandler) List(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "id")

	cues, err := h.Store.ListCues(videoID)
	if err != nil {
		http.Error(w, "failed to list cues", http.StatusInternalServerError)
		return
	}

	if cues == nil {
		cues = []store.Cue{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cues)
}
