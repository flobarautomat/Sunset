package video

import (
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
}

type Registry struct {
	videos map[string]Video
}

func NewRegistry(dir string) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read video dir %s: %w", dir, err)
	}

	r := &Registry{videos: make(map[string]Video)}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		v, err := probe(path)
		if err != nil {
			log.Printf("warning: skipping %s: %v", e.Name(), err)
			continue
		}
		r.videos[v.ID] = v
	}

	if len(r.videos) == 0 {
		log.Printf("warning: no video files found in %s", dir)
	} else {
		log.Printf("loaded %d video(s) from %s", len(r.videos), dir)
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

	name := filepath.Base(path)
	id := strings.TrimSuffix(name, filepath.Ext(name))

	return Video{
		ID:         id,
		Path:       path,
		Duration:   duration,
		Width:      width,
		Height:     height,
		BitrateBps: bitrate,
		SizeBytes:  info.Size(),
	}, nil
}
