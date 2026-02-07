CREATE TABLE IF NOT EXISTS snapshots (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    path TEXT NOT NULL,
    source_sandbox_id TEXT NOT NULL,
    source_sandbox_name TEXT NOT NULL,
    virtual_size_bytes INTEGER NOT NULL,
    allocated_size_bytes INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    CHECK (virtual_size_bytes >= 0),
    CHECK (allocated_size_bytes >= 0)
);

CREATE INDEX idx_snapshots_name ON snapshots(name);
CREATE INDEX idx_snapshots_created_at ON snapshots(created_at);
