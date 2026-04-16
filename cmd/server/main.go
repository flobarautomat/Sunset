package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"moonrise/internal/ai"
	"moonrise/internal/api"
	"moonrise/internal/config"
	"moonrise/internal/pubsub"
	"moonrise/internal/recorder"
	"moonrise/internal/store"
	"moonrise/internal/tts"
	"moonrise/internal/video"
	"moonrise/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	startTime := time.Now()
	cfg := config.LoadOrDefault()

	db, err := store.Open("moonrise.db")
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	registry, err := video.NewRegistry("data/films")
	if err != nil {
		log.Printf("warning: video registry: %v", err)
	}

	hub := pubsub.NewHub()

	sessionStore := store.NewSessionStore(db)
	rec := recorder.New(sessionStore)

	if err := sessionStore.SeedCuesFromDir("data/films"); err != nil {
		log.Printf("warning: seed cues: %v", err)
	}

	// Pick AI provider credentials
	aiKey := cfg.SunsetAPIKey
	if cfg.AIProvider == "anthropic" {
		aiKey = cfg.AnthropicKey
	}
	aiClient := ai.NewClient(cfg.AIProvider, cfg.SunsetAPIURL, aiKey, cfg.AIModel)

	// TTS provider
	ttsProvider := tts.NewCachedProvider(
		tts.NewProvider(cfg.TTSProvider, cfg.SunsetAPIURL, cfg.SunsetAPIKey, cfg.TTSVoiceID),
		"cache/cue-audio",
	)

	videosHandler := &api.VideosHandler{Registry: registry}
	sessionsHandler := &api.SessionsHandler{Recorder: rec, Hub: hub}
	cuesHandler := &api.CuesHandler{Store: sessionStore, TTS: ttsProvider}
	chatHandler := &api.ChatHandler{AI: aiClient, Recorder: rec, Hub: hub}
	ttsHandler := &api.TTSHandler{TTS: ttsProvider}
	adminHandler := &api.AdminHandler{Store: sessionStore, Registry: registry, Hub: hub, StartTime: startTime}
	wsHandler := &ws.Handler{Hub: hub, Store: sessionStore, Registry: registry, StartTime: startTime}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok"))
		})

		r.Get("/videos", videosHandler.List)
		r.Get("/videos/{id}/stream", videosHandler.Stream)
		r.Get("/videos/{id}/cues", cuesHandler.List)

		r.Post("/sessions", sessionsHandler.Create)
		r.Post("/sessions/{id}/events", sessionsHandler.RecordEvents)

		r.Post("/chat", chatHandler.Send)

		r.Get("/cue-audio", cuesHandler.Audio)
		r.Post("/tts", ttsHandler.Speak)
		r.Get("/admin/sessions", adminHandler.ListSessions)
		r.Get("/admin/sessions/{id}", adminHandler.GetSession)
		r.Get("/admin/films", adminHandler.ListFilms)
		r.Get("/admin/stats", adminHandler.GetStats)
		r.Get("/admin/heatmap", adminHandler.GetHeatmap)

		r.Get("/config", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"tts_provider": cfg.TTSProvider,
			})
		})
	})

	r.Get("/ws/admin", wsHandler.ServeHTTP)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Idle sweep: detect sessions that stopped sending heartbeats
	go func() {
		idle := make(map[string]bool)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sessions, err := sessionStore.ListSessions()
				if err != nil {
					continue
				}
				now := time.Now().UnixMilli()
				fresh := make(map[string]bool)
				for _, s := range sessions {
					if now-s.LastSeenAt > 30000 {
						fresh[s.ID] = true
						if !idle[s.ID] {
							hub.Publish(pubsub.Message{
								Type:      pubsub.SessionIdle,
								SessionID: s.ID,
							})
						}
					}
				}
				idle = fresh
			}
		}
	}()

	go func() {
		log.Printf("moonrise listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
