package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"moonrise/internal/video"

	"github.com/go-chi/chi/v5"
)

type VideosHandler struct {
	Registry *video.Registry
}

func (h *VideosHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.Registry.List())
}

func (h *VideosHandler) Stream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	v, ok := h.Registry.Get(id)
	if !ok {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}

	f, err := os.Open(v.Path)
	if err != nil {
		http.Error(w, "failed to open video", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "failed to stat video", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, filepath.Base(v.Path), info.ModTime(), f)
}
