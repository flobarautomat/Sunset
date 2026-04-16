package tts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// CachedProvider wraps another Provider and caches audio results on disk.
// Text-only results (browser provider) pass through without caching.
type CachedProvider struct {
	inner    Provider
	cacheDir string
}

func NewCachedProvider(inner Provider, cacheDir string) *CachedProvider {
	os.MkdirAll(cacheDir, 0o755)
	return &CachedProvider{inner: inner, cacheDir: cacheDir}
}

func (c *CachedProvider) Speak(ctx context.Context, text, voiceID string) (*Result, error) {
	// Check disk cache first (only relevant for audio providers).
	key := cacheKey(text, voiceID)
	path := filepath.Join(c.cacheDir, key+".mp3")

	if data, err := os.ReadFile(path); err == nil {
		return &Result{Type: "audio", Audio: data}, nil
	}

	// Cache miss — call the inner provider.
	result, err := c.inner.Speak(ctx, text, voiceID)
	if err != nil {
		return nil, err
	}

	// Cache audio results to disk.
	if result.Type == "audio" && len(result.Audio) > 0 {
		os.WriteFile(path, result.Audio, 0o644)
	}

	return result, nil
}

func cacheKey(text, voiceID string) string {
	h := sha256.Sum256([]byte(text + "|" + voiceID))
	return fmt.Sprintf("%x", h)
}
