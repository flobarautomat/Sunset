CREATE TABLE IF NOT EXISTS sessions (
  id           TEXT PRIMARY KEY,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  user_agent   TEXT,
  video_id     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  kind       TEXT NOT NULL,
  at         INTEGER NOT NULL,
  video_pos  REAL,
  payload    TEXT
);

CREATE TABLE IF NOT EXISTS cues (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  video_id   TEXT NOT NULL,
  at_seconds REAL NOT NULL,
  prompt     TEXT NOT NULL,
  voice_id   TEXT NOT NULL,
  enabled    INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id, at);
CREATE INDEX IF NOT EXISTS idx_cues_video ON cues(video_id, at_seconds);
