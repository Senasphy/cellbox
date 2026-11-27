CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL UNIQUE,
    secret_sha256 BYTEA NOT NULL,
    scope TEXT NOT NULL,
    quotas_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    owner_key_id TEXT NOT NULL,
    image_reference TEXT NOT NULL,
    image_digest TEXT NOT NULL DEFAULT '',
    runtime_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    limits_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    network_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    last_activity_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    parent_snapshot_id TEXT NOT NULL DEFAULT '',
    failure_reason TEXT NOT NULL DEFAULT '',
    create_source TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_owner_created ON sessions (owner_key_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions (state);

CREATE TABLE IF NOT EXISTS execs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    argv_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    shell TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    tty BOOLEAN NOT NULL DEFAULT FALSE,
    state TEXT NOT NULL,
    exit_code INTEGER,
    signal INTEGER,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    output_bytes BIGINT NOT NULL DEFAULT 0,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    peak_memory_bytes BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_execs_session_started ON execs (session_id, started_at ASC, id ASC);

CREATE TABLE IF NOT EXISTS exec_output (
    exec_id TEXT NOT NULL REFERENCES execs(id) ON DELETE CASCADE,
    offset_bytes BIGINT NOT NULL,
    stream TEXT NOT NULL,
    data BYTEA NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (exec_id, offset_bytes)
);

CREATE TABLE IF NOT EXISTS idempotency (
    key_hash BYTEA PRIMARY KEY,
    request_hash BYTEA NOT NULL,
    status_code INTEGER NOT NULL,
    headers_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    body BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    key_id TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_audit_session_ts ON audit_events (session_id, timestamp DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_type_ts ON audit_events (type, timestamp DESC, id DESC);

CREATE TABLE IF NOT EXISTS egress_log (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    key_id TEXT NOT NULL DEFAULT '',
    timestamp TIMESTAMPTZ NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    resolved_ip TEXT NOT NULL DEFAULT '',
    verdict TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    bytes_up BIGINT NOT NULL DEFAULT 0,
    bytes_down BIGINT NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_egress_session_ts ON egress_log (session_id, timestamp DESC, id DESC);

CREATE TABLE IF NOT EXISTS snapshots (
    id TEXT PRIMARY KEY,
    source_session_id TEXT NOT NULL,
    parent_snapshot_id TEXT NOT NULL DEFAULT '',
    runtime_snapshot_id TEXT NOT NULL DEFAULT '',
    layer_path TEXT NOT NULL DEFAULT '',
    size BIGINT NOT NULL DEFAULT 0,
    flattened BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    labels_json JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_snapshots_session_created ON snapshots (source_session_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS cluster_workers (
    id TEXT PRIMARY KEY,
    advertise_url TEXT NOT NULL,
    grpc_addr TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    total_cpu_milli BIGINT NOT NULL DEFAULT 0,
    total_memory_mb BIGINT NOT NULL DEFAULT 0,
    total_pids BIGINT NOT NULL DEFAULT 0,
    used_cpu_milli BIGINT NOT NULL DEFAULT 0,
    used_memory_mb BIGINT NOT NULL DEFAULT 0,
    used_pids BIGINT NOT NULL DEFAULT 0,
    active_sessions INTEGER NOT NULL DEFAULT 0,
    last_heartbeat_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS session_placements (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    worker_id TEXT NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_placements_worker ON session_placements (worker_id);
