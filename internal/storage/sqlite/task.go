package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

// TaskRepositoryConfig is the configuration for the SQLite task repository.
type TaskRepositoryConfig struct {
	DB     *sql.DB
	Logger log.Logger
}

func (c *TaskRepositoryConfig) defaults() error {
	if c.DB == nil {
		return fmt.Errorf("db is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "storage.TaskRepository"})
	return nil
}

// TaskRepository is a SQLite implementation of storage.TaskRepository.
type TaskRepository struct {
	db     *sql.DB
	logger log.Logger
}

// NewTaskRepository creates a new SQLite task repository.
func NewTaskRepository(cfg TaskRepositoryConfig) (*TaskRepository, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &TaskRepository{
		db:     cfg.DB,
		logger: cfg.Logger,
	}, nil
}

// AddTask adds a single task to an operation.
func (r *TaskRepository) AddTask(ctx context.Context, sandboxID, operation, name string) error {
	return r.AddTasks(ctx, sandboxID, operation, []string{name})
}

// AddTasks adds multiple tasks to an operation in order.
func (r *TaskRepository) AddTasks(ctx context.Context, sandboxID, operation string, names []string) error {
	if len(names) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback is safe to call after Commit

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
		_, err := stmt.ExecContext(ctx, taskID, sandboxID, operation, sequence, name, model.TaskStatusPending, now.Unix())
		if err != nil {
			return fmt.Errorf("could not insert task: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	r.logger.Debugf("Added %d tasks for sandbox %s operation %s", len(names), sandboxID, operation)
	return nil
}

// NextTask returns the next pending task for an operation, or nil if all done.
func (r *TaskRepository) NextTask(ctx context.Context, sandboxID, operation string) (*model.Task, error) {
	query := `
		SELECT id, sandbox_id, operation, sequence, name, status, error, created_at
		FROM tasks
		WHERE sandbox_id = ? AND operation = ? AND status = ?
		ORDER BY sequence ASC
		LIMIT 1
	`

	var t model.Task
	var createdAt int64

	err := r.db.QueryRowContext(ctx, query, sandboxID, operation, model.TaskStatusPending).Scan(
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
func (r *TaskRepository) CompleteTask(ctx context.Context, taskID string) error {
	query := `UPDATE tasks SET status = ? WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, model.TaskStatusDone, taskID)
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

	r.logger.Debugf("Completed task: %s", taskID)
	return nil
}

// FailTask marks a task as failed with an error message.
func (r *TaskRepository) FailTask(ctx context.Context, taskID string, taskErr error) error {
	errMsg := ""
	if taskErr != nil {
		errMsg = taskErr.Error()
	}

	query := `UPDATE tasks SET status = ?, error = ? WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, model.TaskStatusFailed, errMsg, taskID)
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

	r.logger.Debugf("Failed task: %s (error: %s)", taskID, errMsg)
	return nil
}

// Progress returns the completion progress for an operation.
func (r *TaskRepository) Progress(ctx context.Context, sandboxID, operation string) (*model.TaskProgress, error) {
	query := `
		SELECT 
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) as done
		FROM tasks
		WHERE sandbox_id = ? AND operation = ?
	`

	var total, done int
	err := r.db.QueryRowContext(ctx, query, model.TaskStatusDone, sandboxID, operation).Scan(&total, &done)
	if err != nil {
		return nil, fmt.Errorf("could not query progress: %w", err)
	}

	return &model.TaskProgress{
		Done:  done,
		Total: total,
	}, nil
}

// HasPendingOperation checks if a sandbox has any pending operations.
func (r *TaskRepository) HasPendingOperation(ctx context.Context, sandboxID string) (operation string, hasPending bool, err error) {
	query := `
		SELECT operation
		FROM tasks
		WHERE sandbox_id = ? AND status = ?
		ORDER BY created_at ASC
		LIMIT 1
	`

	err = r.db.QueryRowContext(ctx, query, sandboxID, model.TaskStatusPending).Scan(&operation)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("could not query pending operation: %w", err)
	}

	return operation, true, nil
}

// ClearOperation removes all tasks for an operation.
func (r *TaskRepository) ClearOperation(ctx context.Context, sandboxID, operation string) error {
	query := `DELETE FROM tasks WHERE sandbox_id = ? AND operation = ?`

	result, err := r.db.ExecContext(ctx, query, sandboxID, operation)
	if err != nil {
		return fmt.Errorf("could not delete tasks: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}

	r.logger.Debugf("Cleared %d tasks for sandbox %s operation %s", rows, sandboxID, operation)
	return nil
}
