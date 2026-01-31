CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    sandbox_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_tasks_sandbox_op ON tasks(sandbox_id, operation);
CREATE INDEX idx_tasks_sandbox_id ON tasks(sandbox_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_sequence ON tasks(sandbox_id, operation, sequence);
