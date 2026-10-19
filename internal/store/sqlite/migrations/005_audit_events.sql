CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    session_id TEXT,
    key_id TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    detail_json BLOB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS audit_events_session_time_idx ON audit_events(session_id, timestamp, id);
CREATE INDEX IF NOT EXISTS audit_events_time_idx ON audit_events(timestamp);
