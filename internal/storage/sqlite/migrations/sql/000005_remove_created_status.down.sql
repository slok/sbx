-- Restore the CHECK constraint to include 'created'.
CREATE TABLE sandboxes_new (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    status TEXT NOT NULL,
    rootfs_path TEXT NOT NULL,
    kernel_image_path TEXT NOT NULL,
    vcpus REAL NOT NULL,
    memory_mb INTEGER NOT NULL,
    disk_gb INTEGER NOT NULL,
    internal_ip TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    stopped_at INTEGER,
    CHECK (status IN ('created', 'running', 'stopped')),
    CHECK (vcpus > 0),
    CHECK (memory_mb > 0),
    CHECK (disk_gb > 0)
);

INSERT INTO sandboxes_new SELECT * FROM sandboxes;
DROP TABLE sandboxes;
ALTER TABLE sandboxes_new RENAME TO sandboxes;

CREATE INDEX idx_sandboxes_name ON sandboxes(name);
CREATE INDEX idx_sandboxes_status ON sandboxes(status);
CREATE INDEX idx_sandboxes_created_at ON sandboxes(created_at);
