package task

import (
	"context"
	"time"
)

// Status represents the state of a task.
type Status string

const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Task represents a single step in a multi-step operation.
type Task struct {
	ID        string
	SandboxID string
	Operation string
	Sequence  int
	Name      string
	Status    Status
	Error     string
	CreatedAt time.Time
}

// Progress represents the completion state of an operation.
type Progress struct {
	Done  int
	Total int
}

// Manager handles task tracking for multi-step operations.
type Manager interface {
	// AddTask adds a single task to an operation.
	AddTask(ctx context.Context, sandboxID, operation, name string) error

	// AddTasks adds multiple tasks to an operation in order.
	AddTasks(ctx context.Context, sandboxID, operation string, names []string) error

	// NextTask returns the next pending task for an operation, or nil if all done.
	NextTask(ctx context.Context, sandboxID, operation string) (*Task, error)

	// CompleteTask marks a task as completed.
	CompleteTask(ctx context.Context, taskID string) error

	// FailTask marks a task as failed with an error message.
	FailTask(ctx context.Context, taskID string, err error) error

	// Progress returns the completion progress for an operation.
	Progress(ctx context.Context, sandboxID, operation string) (*Progress, error)

	// HasPendingOperation checks if a sandbox has any pending operations.
	// Returns the operation name and true if found, empty string and false otherwise.
	HasPendingOperation(ctx context.Context, sandboxID string) (operation string, hasPending bool, err error)

	// ClearOperation removes all tasks for an operation.
	ClearOperation(ctx context.Context, sandboxID, operation string) error
}
