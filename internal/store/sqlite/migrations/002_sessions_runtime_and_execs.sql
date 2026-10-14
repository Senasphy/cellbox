ALTER TABLE sessions ADD COLUMN runtime_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS sessions_owner_created ON sessions(owner_key_id, created_at DESC);
CREATE TABLE IF NOT EXISTS execs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    argv_json TEXT NOT NULL DEFAULT '[]',
    shell TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    env_json TEXT NOT NULL DEFAULT '{}',
    tty INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    exit_code INTEGER,
    signal INTEGER,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    output_bytes INTEGER NOT NULL DEFAULT 0,
    truncated INTEGER NOT NULL DEFAULT 0,
    output BLOB NOT NULL DEFAULT X''
);
