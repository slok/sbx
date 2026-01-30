package remove

import (
	"context"
	"errors"
	"fmt"

	"github.com/slok/sbx/internal/engine"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the remove service.
type ServiceConfig struct {
	Engine     engine.Engine
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

// Service removes a sandbox.
type Service struct {
	engine engine.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new remove service.
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

// Request represents the remove request parameters.
type Request struct {
	// NameOrID is the sandbox name or ID to remove.
	NameOrID string
	// Force indicates whether to stop a running sandbox before removal.
	Force bool
}

// Run removes a sandbox by name or ID.
// If the sandbox is running and Force is false, it returns an error.
// If Force is true, it stops the sandbox first then removes it.
func (s *Service) Run(ctx context.Context, req Request) (*model.Sandbox, error) {
	s.logger.Debugf("removing sandbox: %s (force: %v)", req.NameOrID, req.Force)

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

	// Check if sandbox is running.
	if sandbox.Status == model.SandboxStatusRunning {
		if !req.Force {
			return nil, fmt.Errorf("cannot remove running sandbox without --force: %w", model.ErrNotValid)
		}

		// Stop the sandbox first (ignore errors, best effort).
		s.logger.Infof("force removing running sandbox, stopping first: %s", sandbox.ID)
		_ = s.engine.Stop(ctx, sandbox.ID)
	}

	// Remove the sandbox via engine.
	if err := s.engine.Remove(ctx, sandbox.ID); err != nil {
		return nil, fmt.Errorf("could not remove sandbox: %w", err)
	}

	// Delete from repository.
	if err := s.repo.DeleteSandbox(ctx, sandbox.ID); err != nil {
		return nil, fmt.Errorf("could not delete sandbox from repository: %w", err)
	}

	s.logger.Infof("removed sandbox: %s (ID: %s)", sandbox.Name, sandbox.ID)
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
