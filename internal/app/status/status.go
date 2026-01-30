package status

import (
	"context"
	"errors"
	"fmt"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the status service.
type ServiceConfig struct {
	Repository storage.Repository
	Logger     log.Logger
}

func (c *ServiceConfig) defaults() error {
	if c.Repository == nil {
		return fmt.Errorf("repository is required")
	}

	if c.Logger == nil {
		c.Logger = log.Noop
	}

	return nil
}

// Service retrieves detailed sandbox status.
type Service struct {
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new status service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		repo:   cfg.Repository,
		logger: cfg.Logger,
	}, nil
}

// Request represents the status request parameters.
type Request struct {
	// NameOrID is the sandbox name or ID to query.
	NameOrID string
}

// Run retrieves the status of a sandbox by name or ID.
// It tries name lookup first, then ID lookup if the input looks like a ULID.
func (s *Service) Run(ctx context.Context, req Request) (*model.Sandbox, error) {
	s.logger.Debugf("getting status for sandbox: %s", req.NameOrID)

	// Try lookup by name first.
	sandbox, err := s.repo.GetSandboxByName(ctx, req.NameOrID)
	if err == nil {
		s.logger.Debugf("found sandbox by name: %s", sandbox.ID)
		return sandbox, nil
	}

	// If not found by name and the input looks like a ULID (26 chars, alphanumeric),
	// try lookup by ID.
	if errors.Is(err, model.ErrNotFound) && looksLikeULID(req.NameOrID) {
		s.logger.Debugf("name lookup failed, trying ID lookup")
		sandbox, err = s.repo.GetSandbox(ctx, req.NameOrID)
		if err == nil {
			s.logger.Debugf("found sandbox by ID: %s", sandbox.ID)
			return sandbox, nil
		}
	}

	// Return not found error.
	if errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("sandbox not found: %s: %w", req.NameOrID, model.ErrNotFound)
	}

	return nil, fmt.Errorf("could not get sandbox status: %w", err)
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
