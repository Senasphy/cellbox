CREATE TABLE IF NOT EXISTS egress_log (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    key_id TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    resolved_ip TEXT NOT NULL DEFAULT '',
    verdict TEXT NOT NULL CHECK (verdict IN ('allow', 'deny')),
    reason TEXT NOT NULL,
    bytes_up INTEGER NOT NULL DEFAULT 0,
    bytes_down INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS egress_log_session_time_idx ON egress_log(session_id, timestamp, id);
