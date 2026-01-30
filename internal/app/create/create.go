package create

import (
	"context"
	"errors"
	"fmt"

	"github.com/slok/sbx/internal/engine"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the create service.
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
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.Create"})
	return nil
}

// Service handles sandbox creation business logic.
type Service struct {
	engine engine.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new create service.
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

// CreateOptions are the options for creating a sandbox.
type CreateOptions struct {
	Config model.SandboxConfig
}

// Create creates a new sandbox.
func (s *Service) Create(ctx context.Context, opts CreateOptions) (*model.Sandbox, error) {
	// 1. Validate config
	if err := opts.Config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 2. Check name uniqueness
	_, err := s.repo.GetSandboxByName(ctx, opts.Config.Name)
	if err == nil {
		return nil, fmt.Errorf("sandbox with name %q already exists: %w", opts.Config.Name, model.ErrAlreadyExists)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("could not check name uniqueness: %w", err)
	}

	// 3. Create via engine
	sandbox, err := s.engine.Create(ctx, opts.Config)
	if err != nil {
		return nil, fmt.Errorf("could not create sandbox: %w", err)
	}

	// 4. Save to repository
	if err := s.repo.CreateSandbox(ctx, *sandbox); err != nil {
		return nil, fmt.Errorf("could not save sandbox: %w", err)
	}

	s.logger.Infof("Created sandbox: %s (%s)", sandbox.Name, sandbox.ID)

	return sandbox, nil
}
