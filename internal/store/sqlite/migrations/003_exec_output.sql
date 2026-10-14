CREATE TABLE IF NOT EXISTS exec_output (
    exec_id TEXT NOT NULL REFERENCES execs(id) ON DELETE CASCADE,
    byte_offset INTEGER NOT NULL,
    stream TEXT NOT NULL,
    data BLOB NOT NULL,
    timestamp TEXT NOT NULL,
    PRIMARY KEY(exec_id, byte_offset)
);
