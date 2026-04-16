package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port         string
	SunsetAPIKey string
	SunsetAPIURL string
}

func Load() (*Config, error) {
	c := &Config{
		Port:         envOr("PORT", "8080"),
		SunsetAPIKey: os.Getenv("SUNSET_API_KEY"),
		SunsetAPIURL: envOr("SUNSET_API_URL", "https://staging.api.sunset.video"),
	}
	if c.SunsetAPIKey == "" || c.SunsetAPIKey == "your-api-key-here" {
		return nil, fmt.Errorf("SUNSET_API_KEY must be set")
	}
	return c, nil
}

// LoadOrDefault returns a config that won't fail on missing API key.
// Useful during development when you just want the server to start.
func LoadOrDefault() *Config {
	return &Config{
		Port:         envOr("PORT", "8080"),
		SunsetAPIKey: os.Getenv("SUNSET_API_KEY"),
		SunsetAPIURL: envOr("SUNSET_API_URL", "https://staging.api.sunset.video"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
