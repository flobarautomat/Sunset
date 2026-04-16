package store

import (
	"database/sql"
	"embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// SQLite pragmas for performance and safety.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	for _, e := range entries {
		data, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("exec %s: %w", e.Name(), err)
		}
	}
	return nil
}
