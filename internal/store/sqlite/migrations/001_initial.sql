CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    owner_key_id TEXT NOT NULL,
    image_ref TEXT NOT NULL,
    image_digest TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    limits_json TEXT NOT NULL,
    network_policy_json TEXT NOT NULL DEFAULT '{}',
    env_json TEXT NOT NULL DEFAULT '{}',
    labels_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    last_activity_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    parent_snapshot_id TEXT,
    failure_reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    secret_sha256 BLOB NOT NULL,
    scope TEXT NOT NULL,
    quotas_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    last_used_at TEXT,
    revoked_at TEXT
);
