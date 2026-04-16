package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type sunsetProvider struct {
	apiURL         string
	apiKey         string
	defaultVoiceID string
	client         http.Client
}

type speechRequest struct {
	Input string `json:"input"`
	Voice string `json:"voice"`
}

func (s *sunsetProvider) Speak(ctx context.Context, text, voiceID string) (*Result, error) {
	if s.client.Timeout == 0 {
		s.client.Timeout = 30 * time.Second
	}

	// Use default voice if the provided one isn't a UUID
	if !isUUID(voiceID) && s.defaultVoiceID != "" {
		voiceID = s.defaultVoiceID
	}

	body, err := json.Marshal(speechRequest{Input: text, Voice: voiceID})
	if err != nil {
		return nil, fmt.Errorf("marshal tts request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL+"/api/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create tts request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tts API returned %d: %s", resp.StatusCode, string(b))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tts response: %w", err)
	}

	return &Result{
		Type:  "audio",
		Audio: audio,
	}, nil
}

// isUUID checks if a string looks like a UUID (8-4-4-4-12 hex pattern).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
