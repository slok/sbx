package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/task"
)

// ManagerConfig is the configuration for the SQLite task manager.
type ManagerConfig struct {
	DB     *sql.DB
	Logger log.Logger
}

func (c *ManagerConfig) defaults() error {
	if c.DB == nil {
		return fmt.Errorf("db is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "task.SQLite"})
	return nil
}

// Manager is a SQLite implementation of task.Manager.
type Manager struct {
	db     *sql.DB
	logger log.Logger
}

// NewManager creates a new SQLite task manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Manager{
		db:     cfg.DB,
		logger: cfg.Logger,
	}, nil
}

// AddTask adds a single task to an operation.
func (m *Manager) AddTask(ctx context.Context, sandboxID, operation, name string) error {
	return m.AddTasks(ctx, sandboxID, operation, []string{name})
}

// AddTasks adds multiple tasks to an operation in order.
func (m *Manager) AddTasks(ctx context.Context, sandboxID, operation string, names []string) error {
	if len(names) == 0 {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get the current max sequence for this operation
	var maxSeq int
	query := `SELECT COALESCE(MAX(sequence), 0) FROM tasks WHERE sandbox_id = ? AND operation = ?`
	if err := tx.QueryRowContext(ctx, query, sandboxID, operation).Scan(&maxSeq); err != nil {
		return fmt.Errorf("could not get max sequence: %w", err)
	}

	// Insert all tasks
	insertQuery := `
		INSERT INTO tasks (id, sandbox_id, operation, sequence, name, status, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, '', ?)
	`
	stmt, err := tx.PrepareContext(ctx, insertQuery)
	if err != nil {
		return fmt.Errorf("could not prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for i, name := range names {
		taskID := ulid.Make().String()
		sequence := maxSeq + i + 1
		_, err := stmt.ExecContext(ctx, taskID, sandboxID, operation, sequence, name, task.StatusPending, now.Unix())
		if err != nil {
			return fmt.Errorf("could not insert task: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	m.logger.Debugf("Added %d tasks for sandbox %s operation %s", len(names), sandboxID, operation)
	return nil
}

// NextTask returns the next pending task for an operation, or nil if all done.
func (m *Manager) NextTask(ctx context.Context, sandboxID, operation string) (*task.Task, error) {
	query := `
		SELECT id, sandbox_id, operation, sequence, name, status, error, created_at
		FROM tasks
		WHERE sandbox_id = ? AND operation = ? AND status = ?
		ORDER BY sequence ASC
		LIMIT 1
	`

	var t task.Task
	var createdAt int64

	err := m.db.QueryRowContext(ctx, query, sandboxID, operation, task.StatusPending).Scan(
		&t.ID,
		&t.SandboxID,
		&t.Operation,
		&t.Sequence,
		&t.Name,
		&t.Status,
		&t.Error,
		&createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No pending tasks
		}
		return nil, fmt.Errorf("could not query next task: %w", err)
	}

	t.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &t, nil
}

// CompleteTask marks a task as completed.
func (m *Manager) CompleteTask(ctx context.Context, taskID string) error {
	query := `UPDATE tasks SET status = ? WHERE id = ?`

	result, err := m.db.ExecContext(ctx, query, task.StatusDone, taskID)
	if err != nil {
		return fmt.Errorf("could not update task: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}

	m.logger.Debugf("Completed task: %s", taskID)
	return nil
}

// FailTask marks a task as failed with an error message.
func (m *Manager) FailTask(ctx context.Context, taskID string, taskErr error) error {
	errMsg := ""
	if taskErr != nil {
		errMsg = taskErr.Error()
	}

	query := `UPDATE tasks SET status = ?, error = ? WHERE id = ?`

	result, err := m.db.ExecContext(ctx, query, task.StatusFailed, errMsg, taskID)
	if err != nil {
		return fmt.Errorf("could not update task: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}

	m.logger.Debugf("Failed task: %s (error: %s)", taskID, errMsg)
	return nil
}

// Progress returns the completion progress for an operation.
func (m *Manager) Progress(ctx context.Context, sandboxID, operation string) (*task.Progress, error) {
	query := `
		SELECT 
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) as done
		FROM tasks
		WHERE sandbox_id = ? AND operation = ?
	`

	var total, done int
	err := m.db.QueryRowContext(ctx, query, task.StatusDone, sandboxID, operation).Scan(&total, &done)
	if err != nil {
		return nil, fmt.Errorf("could not query progress: %w", err)
	}

	return &task.Progress{
		Done:  done,
		Total: total,
	}, nil
}

// HasPendingOperation checks if a sandbox has any pending operations.
func (m *Manager) HasPendingOperation(ctx context.Context, sandboxID string) (operation string, hasPending bool, err error) {
	query := `
		SELECT operation
		FROM tasks
		WHERE sandbox_id = ? AND status = ?
		ORDER BY created_at ASC
		LIMIT 1
	`

	err = m.db.QueryRowContext(ctx, query, sandboxID, task.StatusPending).Scan(&operation)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("could not query pending operation: %w", err)
	}

	return operation, true, nil
}

// ClearOperation removes all tasks for an operation.
func (m *Manager) ClearOperation(ctx context.Context, sandboxID, operation string) error {
	query := `DELETE FROM tasks WHERE sandbox_id = ? AND operation = ?`

	result, err := m.db.ExecContext(ctx, query, sandboxID, operation)
	if err != nil {
		return fmt.Errorf("could not delete tasks: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}

	m.logger.Debugf("Cleared %d tasks for sandbox %s operation %s", rows, sandboxID, operation)
	return nil
}
