package list

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the list service.
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

// Service lists sandboxes with optional filtering.
type Service struct {
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new list service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		repo:   cfg.Repository,
		logger: cfg.Logger,
	}, nil
}

// Request represents the list request parameters.
type Request struct {
	// StatusFilter is an optional filter to only show sandboxes with this status.
	StatusFilter *model.SandboxStatus
}

// Run lists all sandboxes, optionally filtered by status.
func (s *Service) Run(ctx context.Context, req Request) ([]model.Sandbox, error) {
	s.logger.Debugf("listing sandboxes with filter: %v", req.StatusFilter)

	// Get all sandboxes from repository
	sandboxes, err := s.repo.ListSandboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list sandboxes: %w", err)
	}

	// Apply status filter if provided
	if req.StatusFilter != nil {
		filtered := make([]model.Sandbox, 0, len(sandboxes))
		for _, sb := range sandboxes {
			if sb.Status == *req.StatusFilter {
				filtered = append(filtered, sb)
			}
		}
		sandboxes = filtered
	}

	s.logger.Debugf("found %d sandboxes", len(sandboxes))
	return sandboxes, nil
}
