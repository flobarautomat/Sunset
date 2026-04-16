package video

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/alfg/mp4"
)

type Video struct {
	ID         string  `json:"id"`
	Path       string  `json:"-"`
	Duration   float64 `json:"duration"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	BitrateBps int64   `json:"bitrate_bps"`
	SizeBytes  int64   `json:"size_bytes"`
	Title      string  `json:"title"`
	Year       int     `json:"year,omitempty"`
	Director   string  `json:"director,omitempty"`
	Synopsis   string  `json:"synopsis,omitempty"`
}

type filmMetadata struct {
	Title    string `json:"title"`
	Year     int    `json:"year"`
	Director string `json:"director"`
	Synopsis string `json:"synopsis"`
}

type Registry struct {
	videos map[string]Video
}

// NewRegistry scans subdirectories of dir, treating each as a film.
// Each subdirectory should contain film.mp4 and optionally metadata.json.
func NewRegistry(dir string) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read films dir %s: %w", dir, err)
	}

	r := &Registry{videos: make(map[string]Video)}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		id := e.Name()
		mp4Path := filepath.Join(dir, id, "film.mp4")

		if _, err := os.Stat(mp4Path); err != nil {
			log.Printf("warning: skipping %s: no film.mp4 found", id)
			continue
		}

		v, err := probe(mp4Path)
		if err != nil {
			log.Printf("warning: skipping %s: %v", id, err)
			continue
		}
		v.ID = id

		// Load metadata.json if present
		metaPath := filepath.Join(dir, id, "metadata.json")
		if data, err := os.ReadFile(metaPath); err == nil {
			var meta filmMetadata
			if err := json.Unmarshal(data, &meta); err != nil {
				log.Printf("warning: %s/metadata.json: %v", id, err)
			} else {
				v.Title = meta.Title
				v.Year = meta.Year
				v.Director = meta.Director
				v.Synopsis = meta.Synopsis
			}
		}

		// Default title to folder name if not set
		if v.Title == "" {
			v.Title = strings.ToUpper(id[:1]) + id[1:]
		}

		r.videos[id] = v
	}

	if len(r.videos) == 0 {
		log.Printf("warning: no films found in %s", dir)
	} else {
		log.Printf("loaded %d film(s) from %s", len(r.videos), dir)
	}

	return r, nil
}

func (r *Registry) Get(id string) (Video, bool) {
	v, ok := r.videos[id]
	return v, ok
}

func (r *Registry) List() []Video {
	out := make([]Video, 0, len(r.videos))
	for _, v := range r.videos {
		out = append(out, v)
	}
	return out
}

func probe(path string) (Video, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Video{}, err
	}

	f, err := mp4.Open(path)
	if err != nil {
		return Video{}, fmt.Errorf("parse mp4: %w", err)
	}

	if f.Moov == nil || f.Moov.Mvhd == nil {
		return Video{}, fmt.Errorf("missing moov/mvhd atom")
	}

	timescale := f.Moov.Mvhd.Timescale
	if timescale == 0 {
		return Video{}, fmt.Errorf("timescale is zero")
	}
	duration := float64(f.Moov.Mvhd.Duration) / float64(timescale)

	var width, height int
	for _, trak := range f.Moov.Traks {
		if trak.Tkhd == nil {
			continue
		}
		w := int(trak.Tkhd.GetWidth())
		h := int(trak.Tkhd.GetHeight())
		if w > 0 && h > 0 {
			width, height = w, h
			break
		}
	}

	var bitrate int64
	if duration > 0 {
		bitrate = int64(float64(info.Size()*8) / duration)
	}

	return Video{
		Path:       path,
		Duration:   duration,
		Width:      width,
		Height:     height,
		BitrateBps: bitrate,
		SizeBytes:  info.Size(),
	}, nil
}
