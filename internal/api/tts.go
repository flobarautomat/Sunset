package api

import (
	"encoding/json"
	"net/http"

	"moonrise/internal/tts"
)

type TTSHandler struct {
	TTS tts.Provider
}

type ttsRequest struct {
	Text    string `json:"text"`
	VoiceID string `json:"voice_id"`
}

func (h *TTSHandler) Speak(w http.ResponseWriter, r *http.Request) {
	var req ttsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "text required", http.StatusBadRequest)
		return
	}

	result, err := h.TTS.Speak(r.Context(), req.Text, req.VoiceID)
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
