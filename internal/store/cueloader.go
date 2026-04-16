package store

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

// SeedCues reads a JSON file of cues and upserts them into the database.
// Non-fatal: logs a warning and returns nil if the file is missing.
func (s *SessionStore) SeedCues(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("cues: %s not found, skipping seed", path)
			return nil
		}
		return fmt.Errorf("read cues file: %w", err)
	}

	var cues []Cue
	if err := json.Unmarshal(data, &cues); err != nil {
		return fmt.Errorf("parse cues file: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO cues (video_id, at_seconds, prompt, voice_id, enabled) VALUES (?, ?, ?, ?, 1)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, c := range cues {
		if _, err := stmt.Exec(c.VideoID, c.AtSeconds, c.Prompt, c.VoiceID); err != nil {
			return fmt.Errorf("upsert cue at %.1fs: %w", c.AtSeconds, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Printf("cues: seeded %d cues from %s", len(cues), path)
	return nil
}
