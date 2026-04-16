package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"moonrise/internal/ai"
	"moonrise/internal/pubsub"
	"moonrise/internal/recorder"
)

type ChatHandler struct {
	AI       *ai.Client
	Recorder *recorder.Recorder
	Hub      *pubsub.Hub
}

type chatRequest struct {
	SessionID string       `json:"session_id"`
	Message   string       `json:"message"`
	VideoPos  float64      `json:"video_pos"`
	History   []ai.Message `json:"history"`
}

func (h *ChatHandler) Send(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.Message == "" {
		http.Error(w, "session_id and message are required", http.StatusBadRequest)
		return
	}
	if !h.AI.HasKey() {
		http.Error(w, "AI service not configured", http.StatusServiceUnavailable)
		return
	}

	// Build message list: system prompt, history, new user message
	messages := make([]ai.Message, 0, len(req.History)+2)
	messages = append(messages, ai.Message{
		Role:    "system",
		Content: fmt.Sprintf("You are a helpful assistant discussing a video the user is watching. The user is currently at %.1f seconds into the video. Keep responses concise.", req.VideoPos),
	})
	messages = append(messages, req.History...)
	messages = append(messages, ai.Message{
		Role:    "user",
		Content: req.Message,
	})

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	stream := h.AI.ChatStream(r.Context(), messages)

	var fullResponse string
	for chunk := range stream {
		if chunk.Err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", mustJSON(map[string]string{"error": chunk.Err.Error()}))
			flusher.Flush()
			return
		}
		if chunk.Done {
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			break
		}
		fullResponse += chunk.Text
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]string{"content": chunk.Text}))
		flusher.Flush()
	}

	// Persist ai_message + ai_response events
	now := time.Now().UnixMilli()
	videoPos := req.VideoPos
	events := []recorder.Event{
		{
			Kind:     "ai_message",
			At:       now,
			VideoPos: &videoPos,
			Payload:  json.RawMessage(mustJSON(map[string]string{"text": req.Message})),
		},
		{
			Kind:     "ai_response",
			At:       now,
			VideoPos: &videoPos,
			Payload:  json.RawMessage(mustJSON(map[string]string{"text": fullResponse})),
		},
	}
	if err := h.Recorder.RecordEvents(req.SessionID, events); err != nil {
		log.Printf("failed to record chat events: %v", err)
	}

	if h.Hub != nil {
		payload, _ := json.Marshal(events)
		h.Hub.Publish(pubsub.Message{
			Type:      pubsub.EventsRecorded,
			SessionID: req.SessionID,
			Payload:   payload,
		})
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
