package fake

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// EngineConfig is the configuration for the fake engine.
type EngineConfig struct {
	TaskRepo storage.TaskRepository // Optional: for testing task system integration
	Logger   log.Logger
}

func (c *EngineConfig) defaults() error {
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "engine.Fake"})
	return nil
}

// Engine is a fake implementation of the engine.Engine interface.
// It simulates sandbox lifecycle without creating real VMs.
type Engine struct {
	sandboxes map[string]*model.Sandbox
	taskRepo  storage.TaskRepository
	mu        sync.RWMutex
	logger    log.Logger
}

// NewEngine creates a new fake engine.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Engine{
		sandboxes: make(map[string]*model.Sandbox),
		taskRepo:  cfg.TaskRepo,
		logger:    cfg.Logger,
	}, nil
}

// Create creates a new sandbox.
func (e *Engine) Create(ctx context.Context, cfg model.SandboxConfig) (*model.Sandbox, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Generate ULID
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		taskNames := []string{"create_sandbox"}
		if err := e.taskRepo.AddTasks(ctx, id, "create", taskNames); err != nil {
			return nil, fmt.Errorf("failed to add tasks: %w", err)
		}

		// Complete the task immediately since fake engine is instant
		tsk, _ := e.taskRepo.NextTask(ctx, id, "create")
		if tsk != nil {
			defer func() {
				if err := e.taskRepo.CompleteTask(ctx, tsk.ID); err != nil {
					e.logger.Errorf("Failed to complete task %s: %v", tsk.ID, err)
				}
			}()
		}
	}

	now := time.Now().UTC()
	sandbox := &model.Sandbox{
		ID:        id,
		Name:      cfg.Name,
		Status:    model.SandboxStatusRunning, // Fake engine goes directly to running
		Config:    cfg,
		CreatedAt: now,
		StartedAt: &now,
	}

	e.sandboxes[id] = sandbox
	e.logger.Infof("Created fake sandbox: %s (name: %s)", id, cfg.Name)

	return sandbox, nil
}

// Start starts a sandbox.
func (e *Engine) Start(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		if err := e.taskRepo.AddTask(ctx, id, "start", "start_sandbox"); err != nil {
			return fmt.Errorf("failed to add task: %w", err)
		}

		// Complete the task immediately since fake engine is instant
		tsk, _ := e.taskRepo.NextTask(ctx, id, "start")
		if tsk != nil {
			defer func() {
				if err := e.taskRepo.CompleteTask(ctx, tsk.ID); err != nil {
					e.logger.Errorf("Failed to complete task %s: %v", tsk.ID, err)
				}
			}()
		}
	}

	// Check if sandbox exists in this engine instance
	sandbox, ok := e.sandboxes[id]
	if !ok {
		// Sandbox not in memory - this is OK for integration tests where engine is stateless.
		// Just log and return success since actual state is managed by storage layer.
		e.logger.Debugf("Starting fake sandbox: %s (not in engine memory, assuming managed by storage)", id)
		return nil
	}

	if sandbox.Status == model.SandboxStatusRunning {
		e.logger.Debugf("Sandbox %s is already running", id)
		return nil // Idempotent
	}

	now := time.Now().UTC()
	sandbox.Status = model.SandboxStatusRunning
	sandbox.StartedAt = &now
	sandbox.StoppedAt = nil

	e.logger.Infof("Started fake sandbox: %s", id)

	return nil
}

// Stop stops a sandbox.
func (e *Engine) Stop(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		if err := e.taskRepo.AddTask(ctx, id, "stop", "stop_sandbox"); err != nil {
			return fmt.Errorf("failed to add task: %w", err)
		}

		// Complete the task immediately since fake engine is instant
		tsk, _ := e.taskRepo.NextTask(ctx, id, "stop")
		if tsk != nil {
			defer func() {
				if err := e.taskRepo.CompleteTask(ctx, tsk.ID); err != nil {
					e.logger.Errorf("Failed to complete task %s: %v", tsk.ID, err)
				}
			}()
		}
	}

	// Check if sandbox exists in this engine instance
	sandbox, ok := e.sandboxes[id]
	if !ok {
		// Sandbox not in memory - this is OK for integration tests where engine is stateless.
		// Just log and return success since actual state is managed by storage layer.
		e.logger.Debugf("Stopping fake sandbox: %s (not in engine memory, assuming managed by storage)", id)
		return nil
	}

	if sandbox.Status == model.SandboxStatusStopped {
		e.logger.Debugf("Sandbox %s is already stopped", id)
		return nil // Idempotent
	}

	now := time.Now().UTC()
	sandbox.Status = model.SandboxStatusStopped
	sandbox.StoppedAt = &now

	e.logger.Infof("Stopped fake sandbox: %s", id)

	return nil
}

// Remove removes a sandbox.
func (e *Engine) Remove(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		if err := e.taskRepo.AddTask(ctx, id, "remove", "remove_sandbox"); err != nil {
			return fmt.Errorf("failed to add task: %w", err)
		}

		// Complete the task immediately since fake engine is instant
		tsk, _ := e.taskRepo.NextTask(ctx, id, "remove")
		if tsk != nil {
			defer func() {
				if err := e.taskRepo.CompleteTask(ctx, tsk.ID); err != nil {
					e.logger.Errorf("Failed to complete task %s: %v", tsk.ID, err)
				}
			}()
		}
	}

	// Check if sandbox exists in this engine instance
	if _, ok := e.sandboxes[id]; !ok {
		// Sandbox not in memory - this is OK for integration tests where engine is stateless.
		// Just log and return success since actual state is managed by storage layer.
		e.logger.Debugf("Removing fake sandbox: %s (not in engine memory, assuming managed by storage)", id)
		return nil
	}

	delete(e.sandboxes, id)
	e.logger.Infof("Removed fake sandbox: %s", id)

	return nil
}

// Status returns the status of a sandbox.
func (e *Engine) Status(ctx context.Context, id string) (*model.Sandbox, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sandbox, ok := e.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
	}

	// Return a copy to avoid external modifications
	sandboxCopy := *sandbox
	return &sandboxCopy, nil
}

// Exec executes a command inside a fake sandbox.
// For testing, it supports configurable responses via ExecHandler.
func (e *Engine) Exec(ctx context.Context, id string, command []string, opts model.ExecOpts) (*model.ExecResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(command) == 0 {
		return nil, fmt.Errorf("command cannot be empty: %w", model.ErrNotValid)
	}

	// Check if sandbox exists in this engine instance
	sandbox, ok := e.sandboxes[id]
	if !ok {
		// Sandbox not in memory - this is OK for integration tests where engine is stateless.
		// Just log and return success since actual state is managed by storage layer.
		e.logger.Debugf("Executing command in fake sandbox: %s (not in engine memory, assuming managed by storage)", id)

		// For stateless fake engine, simulate successful execution
		if opts.Stdout != nil && len(command) > 0 {
			// Write a simple fake output to demonstrate exec is working
			fmt.Fprintf(opts.Stdout, "fake output for: %v\n", command)
		}
		return &model.ExecResult{ExitCode: 0}, nil
	}

	// Check that sandbox is running
	if sandbox.Status != model.SandboxStatusRunning {
		return nil, fmt.Errorf("sandbox %s is not running (status: %s): %w", id, sandbox.Status, model.ErrNotValid)
	}

	e.logger.Infof("Executing command in fake sandbox %s: %v", id, command)

	// Simulate command execution - write to stdout if provided
	if opts.Stdout != nil && len(command) > 0 {
		fmt.Fprintf(opts.Stdout, "fake output for: %v\n", command)
	}

	// Default successful execution with exit code 0
	return &model.ExecResult{
		ExitCode: 0,
	}, nil
}
