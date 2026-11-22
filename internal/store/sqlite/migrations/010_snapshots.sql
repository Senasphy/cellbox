CREATE TABLE IF NOT EXISTS snapshots (
    id TEXT PRIMARY KEY,
    source_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_snapshot_id TEXT REFERENCES snapshots(id),
    runtime_snapshot_id TEXT NOT NULL,
    layer_path TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    flattened INTEGER NOT NULL DEFAULT 0 CHECK (flattened IN (0, 1)),
    created_at TEXT NOT NULL,
    labels_json BLOB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS snapshots_session_time_idx ON snapshots(source_session_id, created_at, id);
CREATE INDEX IF NOT EXISTS snapshots_parent_idx ON snapshots(parent_snapshot_id);
