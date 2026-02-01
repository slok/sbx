-- SQLite doesn't support DROP COLUMN in older versions, so we need to recreate the table
-- This is a simplified down migration that creates a new table without the columns
-- For production use, you'd want to handle data migration properly

CREATE TABLE sandboxes_backup AS SELECT id, name, status, config_json, container_id, created_at, started_at, stopped_at, error FROM sandboxes;
DROP TABLE sandboxes;

CREATE TABLE sandboxes (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    status TEXT NOT NULL,
    config_json TEXT NOT NULL,
    container_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    stopped_at INTEGER,
    error TEXT NOT NULL DEFAULT ''
);

INSERT INTO sandboxes SELECT * FROM sandboxes_backup;
DROP TABLE sandboxes_backup;

CREATE INDEX idx_sandboxes_name ON sandboxes(name);
CREATE INDEX idx_sandboxes_status ON sandboxes(status);
CREATE INDEX idx_sandboxes_created_at ON sandboxes(created_at);
