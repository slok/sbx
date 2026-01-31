package stop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the stop service.
type ServiceConfig struct {
	Engine     sandbox.Engine
	Repository storage.Repository
	Logger     log.Logger
}

func (c *ServiceConfig) defaults() error {
	if c.Engine == nil {
		return fmt.Errorf("engine is required")
	}

	if c.Repository == nil {
		return fmt.Errorf("repository is required")
	}

	if c.Logger == nil {
		c.Logger = log.Noop
	}

	return nil
}

// Service stops a running sandbox.
type Service struct {
	engine sandbox.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new stop service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		engine: cfg.Engine,
		repo:   cfg.Repository,
		logger: cfg.Logger,
	}, nil
}

// Request represents the stop request parameters.
type Request struct {
	// NameOrID is the sandbox name or ID to stop.
	NameOrID string
}

// Run stops a sandbox by name or ID.
// It validates the sandbox is running before attempting to stop it.
func (s *Service) Run(ctx context.Context, req Request) (*model.Sandbox, error) {
	s.logger.Debugf("stopping sandbox: %s", req.NameOrID)

	// Lookup sandbox by name first, then by ID if it looks like a ULID.
	sandbox, err := s.repo.GetSandboxByName(ctx, req.NameOrID)
	if errors.Is(err, model.ErrNotFound) && looksLikeULID(req.NameOrID) {
		sandbox, err = s.repo.GetSandbox(ctx, req.NameOrID)
	}
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, fmt.Errorf("sandbox not found: %s: %w", req.NameOrID, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not get sandbox: %w", err)
	}

	// Validate sandbox is running.
	if sandbox.Status != model.SandboxStatusRunning {
		return nil, fmt.Errorf("cannot stop sandbox: not running (current status: %s): %w", sandbox.Status, model.ErrNotValid)
	}

	// Stop the sandbox via engine.
	if err := s.engine.Stop(ctx, sandbox.ID); err != nil {
		return nil, fmt.Errorf("could not stop sandbox: %w", err)
	}

	// Update sandbox state in repository.
	now := time.Now().UTC()
	sandbox.Status = model.SandboxStatusStopped
	sandbox.StoppedAt = &now

	if err := s.repo.UpdateSandbox(ctx, *sandbox); err != nil {
		return nil, fmt.Errorf("could not update sandbox: %w", err)
	}

	s.logger.Infof("stopped sandbox: %s (ID: %s)", sandbox.Name, sandbox.ID)
	return sandbox, nil
}

// looksLikeULID checks if a string looks like a ULID (26 characters, alphanumeric uppercase).
func looksLikeULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}
