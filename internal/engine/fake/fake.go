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
)

// EngineConfig is the configuration for the fake engine.
type EngineConfig struct {
	Logger log.Logger
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
		logger:    cfg.Logger,
	}, nil
}

// Create creates a new sandbox.
func (e *Engine) Create(ctx context.Context, cfg model.SandboxConfig) (*model.Sandbox, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Generate ULID
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()

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

	sandbox, ok := e.sandboxes[id]
	if !ok {
		return fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
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

	sandbox, ok := e.sandboxes[id]
	if !ok {
		return fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
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

	if _, ok := e.sandboxes[id]; !ok {
		return fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
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
