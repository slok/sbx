CREATE TABLE IF NOT EXISTS sandboxes (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    status TEXT NOT NULL,
    config_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    stopped_at INTEGER,
    error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_sandboxes_name ON sandboxes(name);
CREATE INDEX idx_sandboxes_status ON sandboxes(status);
CREATE INDEX idx_sandboxes_created_at ON sandboxes(created_at);
