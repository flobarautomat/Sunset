package tts

import "context"

// Result holds the output of a TTS request — either raw audio bytes
// (from a real TTS service) or plain text (for browser speech synthesis).
type Result struct {
	Type  string // "audio" or "text"
	Audio []byte // mp3 bytes when Type == "audio"
	Text  string // spoken text when Type == "text"
}

// Provider generates speech from text.
type Provider interface {
	Speak(ctx context.Context, text, voiceID string) (*Result, error)
}

// NewProvider returns a Provider based on the provider name.
// "sunset" calls the Sunset staging TTS API; anything else returns a
// browser-mode provider that passes text back for Web Speech API playback.
func NewProvider(provider, apiURL, apiKey, defaultVoiceID string) Provider {
	if provider == "sunset" {
		return &sunsetProvider{
			apiURL:         apiURL,
			apiKey:         apiKey,
			defaultVoiceID: defaultVoiceID,
		}
	}
	return &browserProvider{}
}
