CREATE TABLE IF NOT EXISTS idempotency (
    key_hash BLOB PRIMARY KEY,
    request_hash BLOB NOT NULL,
    status_code INTEGER NOT NULL,
    headers_json BLOB NOT NULL,
    response_body BLOB NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idempotency_created_at_idx ON idempotency(created_at);
