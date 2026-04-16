package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"moonrise/internal/store"
	"moonrise/internal/tts"

	"github.com/go-chi/chi/v5"
)

type CuesHandler struct {
	Store *store.SessionStore
	TTS   tts.Provider
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

func (h *CuesHandler) Audio(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("cue_id")
	if idStr == "" {
		http.Error(w, "cue_id required", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid cue_id", http.StatusBadRequest)
		return
	}

	cue, err := h.Store.GetCue(id)
	if err != nil {
		http.Error(w, "cue not found", http.StatusNotFound)
		return
	}

	result, err := h.TTS.Speak(r.Context(), cue.Prompt, cue.VoiceID)
	if err != nil {
		http.Error(w, "tts failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if result.Type == "audio" {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(result.Audio)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"type": "browser",
			"text": result.Text,
		})
	}
}
