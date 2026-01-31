package storage

import (
	"context"

	"github.com/slok/sbx/internal/model"
)

// Repository is the interface for sandbox persistence.
type Repository interface {
	CreateSandbox(ctx context.Context, s model.Sandbox) error
	GetSandbox(ctx context.Context, id string) (*model.Sandbox, error)
	GetSandboxByName(ctx context.Context, name string) (*model.Sandbox, error)
	ListSandboxes(ctx context.Context) ([]model.Sandbox, error)
	UpdateSandbox(ctx context.Context, s model.Sandbox) error
	DeleteSandbox(ctx context.Context, id string) error
}

// TaskRepository handles task tracking for multi-step operations.
type TaskRepository interface {
	// AddTask adds a single task to an operation.
	AddTask(ctx context.Context, sandboxID, operation, name string) error

	// AddTasks adds multiple tasks to an operation in order.
	AddTasks(ctx context.Context, sandboxID, operation string, names []string) error

	// NextTask returns the next pending task for an operation, or nil if all done.
	NextTask(ctx context.Context, sandboxID, operation string) (*model.Task, error)

	// CompleteTask marks a task as completed.
	CompleteTask(ctx context.Context, taskID string) error

	// FailTask marks a task as failed with an error message.
	FailTask(ctx context.Context, taskID string, err error) error

	// Progress returns the completion progress for an operation.
	Progress(ctx context.Context, sandboxID, operation string) (*model.TaskProgress, error)

	// HasPendingOperation checks if a sandbox has any pending operations.
	// Returns the operation name and true if found, empty string and false otherwise.
	HasPendingOperation(ctx context.Context, sandboxID string) (operation string, hasPending bool, err error)

	// ClearOperation removes all tasks for an operation.
	ClearOperation(ctx context.Context, sandboxID, operation string) error
}
