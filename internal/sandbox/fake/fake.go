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

// Check performs preflight checks for the fake engine.
// Always returns OK since the fake engine has no real dependencies.
func (e *Engine) Check(ctx context.Context) []model.CheckResult {
	return []model.CheckResult{
		{
			ID:      "fake_engine",
			Message: "Fake engine is always ready",
			Status:  model.CheckStatusOK,
		},
	}
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

// Exec simulates executing a command in a sandbox.
// The fake engine validates inputs but doesn't actually execute anything.
func (e *Engine) Exec(ctx context.Context, id string, command []string, opts model.ExecOpts) (*model.ExecResult, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command cannot be empty: %w", model.ErrNotValid)
	}

	e.mu.RLock()
	sandbox, ok := e.sandboxes[id]
	e.mu.RUnlock()

	if !ok {
		// For stateless integration tests, just return success
		e.logger.Debugf("Executing in fake sandbox: %s (not in engine memory)", id)
		return &model.ExecResult{ExitCode: 0}, nil
	}

	if sandbox.Status != model.SandboxStatusRunning {
		return nil, fmt.Errorf("sandbox %s is not running: %w", id, model.ErrNotValid)
	}

	e.logger.Debugf("Fake exec in sandbox %s: %v", id, command)
	return &model.ExecResult{ExitCode: 0}, nil
}

// CopyTo simulates copying a file or directory from the local host to the sandbox.
// The fake engine validates inputs but doesn't actually copy anything.
func (e *Engine) CopyTo(ctx context.Context, id string, srcLocal string, dstRemote string) error {
	if srcLocal == "" {
		return fmt.Errorf("source path cannot be empty: %w", model.ErrNotValid)
	}
	if dstRemote == "" {
		return fmt.Errorf("destination path cannot be empty: %w", model.ErrNotValid)
	}

	e.mu.RLock()
	sandbox, ok := e.sandboxes[id]
	e.mu.RUnlock()

	if !ok {
		// For stateless integration tests, just return success
		e.logger.Debugf("Fake CopyTo in sandbox: %s (not in engine memory): %s -> %s", id, srcLocal, dstRemote)
		return nil
	}

	if sandbox.Status != model.SandboxStatusRunning {
		return fmt.Errorf("sandbox %s is not running: %w", id, model.ErrNotValid)
	}

	e.logger.Debugf("Fake CopyTo in sandbox %s: %s -> %s", id, srcLocal, dstRemote)
	return nil
}

// CopyFrom simulates copying a file or directory from the sandbox to the local host.
// The fake engine validates inputs but doesn't actually copy anything.
func (e *Engine) CopyFrom(ctx context.Context, id string, srcRemote string, dstLocal string) error {
	if srcRemote == "" {
		return fmt.Errorf("source path cannot be empty: %w", model.ErrNotValid)
	}
	if dstLocal == "" {
		return fmt.Errorf("destination path cannot be empty: %w", model.ErrNotValid)
	}

	e.mu.RLock()
	sandbox, ok := e.sandboxes[id]
	e.mu.RUnlock()

	if !ok {
		// For stateless integration tests, just return success
		e.logger.Debugf("Fake CopyFrom in sandbox: %s (not in engine memory): %s -> %s", id, srcRemote, dstLocal)
		return nil
	}

	if sandbox.Status != model.SandboxStatusRunning {
		return fmt.Errorf("sandbox %s is not running: %w", id, model.ErrNotValid)
	}

	e.logger.Debugf("Fake CopyFrom in sandbox %s: %s -> %s", id, srcRemote, dstLocal)
	return nil
}
