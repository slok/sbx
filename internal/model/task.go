package model

import (
	"time"
)

// TaskStatus represents the state of a task.
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending"
	TaskStatusDone    TaskStatus = "done"
	TaskStatusFailed  TaskStatus = "failed"
)

// Task represents a single step in a multi-step operation.
type Task struct {
	ID        string
	SandboxID string
	Operation string
	Sequence  int
	Name      string
	Status    TaskStatus
	Error     string
	CreatedAt time.Time
}

// TaskProgress represents the completion state of an operation.
type TaskProgress struct {
	Done  int
	Total int
}
